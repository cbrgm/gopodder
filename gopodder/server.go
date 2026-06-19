package gopodder

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

type Config struct {
	ListenAddr         string
	DebugAddr          string
	DBBackend          string
	DBPath             string
	DBPostgres         string
	DBPostgresPassword string
	Build              BuildInfo
}

func Run(logger *slog.Logger, cfg Config) error {
	store, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	listenAddr := cmp.Or(cfg.ListenAddr, "0.0.0.0:8080")
	dbBackend := cmp.Or(cfg.DBBackend, "sqlite")

	var metrics Metrics
	if cfg.DebugAddr != "" {
		metrics = newPrometheusMetrics()
	} else {
		metrics = noopMetrics{}
	}

	api := NewAPI(logger, store, metrics, cfg.Build, listenAddr, dbBackend)

	srv := &http.Server{
		Addr:         listenAddr,
		Handler:      api.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	if cfg.DebugAddr != "" {
		go startDebugServer(logger, cfg.DebugAddr)
	}

	logger.Info("goPodder", "version", cfg.Build.Version, "revision", cfg.Build.Revision, "go", cfg.Build.GoVersion, "platform", cfg.Build.Platform)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting server", "addr", listenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	stopCleanup := make(chan struct{})
	go runCleanup(logger, store, stopCleanup)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		logger.Info("shutting down server")
	case err := <-errCh:
		close(stopCleanup)
		return err
	}

	close(stopCleanup)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

const (
	SettingEpisodeRetention     = "episode_retention_days"
	defaultEpisodeRetentionDays = 90
	minEpisodeRetentionDays     = 7
	maxEpisodeRetentionDays     = 3650

	SettingInactiveAccountDays = "inactive_account_days"
	minInactiveAccountDays     = 30
	maxInactiveAccountDays     = 3650
)

func runCleanup(logger *slog.Logger, store Store, stop <-chan struct{}) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	run := func() {
		cleanupEpisodes(logger, store)
		cleanupInactiveAccounts(logger, store)
	}

	run()
	for {
		select {
		case <-ticker.C:
			run()
		case <-stop:
			return
		}
	}
}

func cleanupEpisodes(logger *slog.Logger, store Store) {
	retentionDays := getClampedSetting(store, SettingEpisodeRetention, defaultEpisodeRetentionDays, minEpisodeRetentionDays, maxEpisodeRetentionDays)
	if retentionDays == 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	n, err := store.DeleteEpisodesOlderThan(context.Background(), cutoff)
	if err != nil {
		logger.Error("episode cleanup failed", "err", err)
		return
	}
	if n > 0 {
		logger.Info("episode cleanup completed", "deleted", n, "retention_days", retentionDays)
	}
}

func cleanupInactiveAccounts(logger *slog.Logger, store Store) {
	days := getClampedSetting(store, SettingInactiveAccountDays, 0, minInactiveAccountDays, maxInactiveAccountDays)
	if days == 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days).Unix()
	accounts, err := store.ListInactiveAccounts(context.Background(), cutoff)
	if err != nil {
		logger.Error("inactive account cleanup failed", "err", err)
		return
	}
	for _, acct := range accounts {
		deleteAccountCascade(context.Background(), store, acct.ID)
		logger.Info("deleted inactive account", "username", acct.Username, "inactive_days", days)
	}
}

func getClampedSetting(store Store, key string, defaultVal, min, max int) int {
	val, err := store.GetSetting(context.Background(), key)
	if err != nil {
		return defaultVal
	}
	n, _ := strconv.Atoi(val)
	if n == 0 {
		return 0
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func openStore(cfg Config) (Store, error) {
	switch cfg.DBBackend {
	case "sqlite", "":
		return NewSQLiteStore(cfg.DBPath)
	case "postgres":
		dsn, err := buildPostgresDSN(cfg.DBPostgres, cfg.DBPostgresPassword)
		if err != nil {
			return nil, err
		}
		return NewPostgresStore(dsn)
	default:
		return nil, fmt.Errorf("unsupported database backend: %q (supported: sqlite, postgres)", cfg.DBBackend)
	}
}

func buildPostgresDSN(dsn, password string) (string, error) {
	if password == "" {
		return dsn, nil
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parsing postgres connection string: %w", err)
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), password)
	} else {
		u.User = url.UserPassword("", password)
	}
	return u.String(), nil
}
