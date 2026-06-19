package gopodder

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestBuildPostgresDSN(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		password string
		want     string
		wantErr  bool
	}{
		{
			name:     "empty password returns dsn unchanged",
			dsn:      "postgres://user@host:5432/db",
			password: "",
			want:     "postgres://user@host:5432/db",
		},
		{
			name:     "password injected with existing user",
			dsn:      "postgres://gopodder@host:5432/db",
			password: "secret",
			want:     "postgres://gopodder:secret@host:5432/db",
		},
		{
			name:     "password replaces existing password",
			dsn:      "postgres://gopodder:oldpass@host:5432/db",
			password: "newpass",
			want:     "postgres://gopodder:newpass@host:5432/db",
		},
		{
			name:     "password with special characters is escaped",
			dsn:      "postgres://user@host:5432/db",
			password: "p@ss:w/rd",
			want:     "postgres://user:p%40ss%3Aw%2Frd@host:5432/db",
		},
		{
			name:     "password with no user in dsn",
			dsn:      "postgres://host:5432/db",
			password: "secret",
			want:     "postgres://:secret@host:5432/db",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildPostgresDSN(tt.dsn, tt.password)
			if (err != nil) != tt.wantErr {
				t.Fatalf("buildPostgresDSN() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("buildPostgresDSN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpenStore_SQLite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := openStore(Config{DBBackend: "sqlite", DBPath: dbPath})
	if err != nil {
		t.Fatalf("openStore sqlite: %v", err)
	}
	defer func() { _ = store.Close() }()

	if _, ok := store.(*SQLiteStore); !ok {
		t.Errorf("expected *SQLiteStore, got %T", store)
	}
}

func TestOpenStore_SQLiteDefault(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := openStore(Config{DBBackend: "", DBPath: dbPath})
	if err != nil {
		t.Fatalf("openStore default: %v", err)
	}
	defer func() { _ = store.Close() }()

	if _, ok := store.(*SQLiteStore); !ok {
		t.Errorf("expected *SQLiteStore when backend is empty, got %T", store)
	}
}

func TestOpenStore_UnknownBackend(t *testing.T) {
	_, err := openStore(Config{DBBackend: "mysql"})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestOpenStore_PostgresInvalidDSN(t *testing.T) {
	_, err := openStore(Config{DBBackend: "postgres", DBPostgres: "postgres://invalid:invalid@localhost:1/nonexistent"})
	if err == nil {
		t.Fatal("expected error for invalid postgres DSN")
	}
}

func TestRun_InvalidDBPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	err := Run(logger, Config{
		ListenAddr: "127.0.0.1:0",
		DBBackend: "sqlite",
		DBPath:    "/nonexistent/path/to/db",
		Build:     BuildInfo{Version: "test"},
	})
	if err == nil {
		t.Fatal("expected error for invalid db path")
	}
}

func TestRun_InvalidBackend(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	err := Run(logger, Config{
		ListenAddr: "127.0.0.1:0",
		DBBackend: "unknown",
		Build:     BuildInfo{Version: "test"},
	})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestGetClampedSetting(t *testing.T) {
	tests := []struct {
		name       string
		stored     string
		storeErr   bool
		defaultVal int
		min        int
		max        int
		want       int
	}{
		{"missing setting returns default", "", true, 90, 7, 3650, 90},
		{"zero returns zero (disabled)", "0", false, 90, 7, 3650, 0},
		{"valid value within range", "30", false, 90, 7, 3650, 30},
		{"below min clamps to min", "3", false, 90, 7, 3650, 7},
		{"above max clamps to max", "9999", false, 90, 7, 3650, 3650},
		{"negative clamps to min", "-5", false, 90, 7, 3650, 7},
		{"non-numeric treated as zero", "abc", false, 90, 7, 3650, 0},
		{"default zero with missing returns zero", "", true, 0, 30, 3650, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockStore()
			if !tt.storeErr {
				store.settings[SettingEpisodeRetention] = tt.stored
			}
			got := getClampedSetting(store, SettingEpisodeRetention, tt.defaultVal, tt.min, tt.max)
			if got != tt.want {
				t.Errorf("getClampedSetting() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCleanupEpisodes_Disabled(t *testing.T) {
	store := newMockStore()
	store.settings[SettingEpisodeRetention] = "0"
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	cleanupEpisodes(logger, store)
}

func TestCleanupInactiveAccounts_Disabled(t *testing.T) {
	store := newMockStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", Role: RoleStandard}
	cleanupInactiveAccounts(logger, store)

	if _, ok := store.accounts["u1"]; !ok {
		t.Error("account should not have been deleted when setting is disabled (default 0)")
	}
}

func TestCleanupInactiveAccounts_SkipsAdmins(t *testing.T) {
	store := newMockStore()
	store.settings[SettingInactiveAccountDays] = "30"
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	store.accounts["admin1"] = &Account{ID: "admin1", Username: "admin", Role: RoleAdmin}
	cleanupInactiveAccounts(logger, store)

	if _, ok := store.accounts["admin1"]; !ok {
		t.Error("admin account should never be deleted by cleanup")
	}
}

func TestRun_StartsAndShutdown(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(logger, Config{
			ListenAddr: "127.0.0.1:19876",
			DBBackend:  "sqlite",
			DBPath:     dbPath,
			Build:      BuildInfo{Version: "test"},
		})
	}()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://127.0.0.1:19876/api/2/devices/test.json")
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated request, got %d", resp.StatusCode)
	}

	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess: %v", err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within 5 seconds")
	}
}
