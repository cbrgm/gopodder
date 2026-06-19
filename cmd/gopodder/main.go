package main

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/cbrgm/gopodder/gopodder"
)

var (
	Version   = "dev"
	Revision  = "unknown"
	BuildDate = "unknown"
)

type CLI struct {
	LogLevel string           `name:"log-level" default:"info" enum:"debug,info,warn,error" env:"GOPODDER_LOG_LEVEL" help:"Log level (debug, info, warn, error)."`
	Version  kong.VersionFlag `name:"version" help:"Print version and exit."`

	Serve ServeCmd `cmd:"" default:"withargs" help:"Start the gPodder sync server."`
}

type ServeCmd struct {
	ListenAddr         string `name:"listen-address" default:"0.0.0.0:8080" env:"GOPODDER_LISTEN_ADDRESS" help:"HTTP listen address (host:port)."`
	DebugAddr          string `name:"debug-address" default:"" env:"GOPODDER_DEBUG_ADDRESS" help:"Debug/metrics listen address (e.g. 127.0.0.1:6060). Disabled if empty."`
	DBBackend          string `name:"db-backend" default:"sqlite" enum:"sqlite,postgres" env:"GOPODDER_DB_BACKEND" help:"Database backend (sqlite, postgres)."`
	DBPath             string `name:"db-path" default:"gopodder.db" env:"GOPODDER_DB_PATH" help:"Path to SQLite database file."`
	DBPostgres         string `name:"db-postgres" default:"" env:"GOPODDER_DB_POSTGRES" help:"PostgreSQL connection string (e.g. postgres://user:pass@host:5432/dbname)."`
	DBPostgresPassword string `name:"db-postgres-password" default:"" env:"GOPODDER_DB_POSTGRES_PASSWORD" help:"PostgreSQL password (injected into connection string if set)."`
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("gopodder"),
		kong.Description("A gPodder-compatible podcast synchronization server."),
		kong.Vars{"version": fmt.Sprintf("%s (revision: %s, built: %s)", Version, Revision, BuildDate)},
	)

	logger := setupLogger(cli.LogLevel)

	switch ctx.Command() {
	case "serve", "":
		if err := gopodder.Run(logger, gopodder.Config{
			ListenAddr:         cli.Serve.ListenAddr,
			DebugAddr:          cli.Serve.DebugAddr,
			DBBackend:          cli.Serve.DBBackend,
			DBPath:             cli.Serve.DBPath,
			DBPostgres:         cli.Serve.DBPostgres,
			DBPostgresPassword: cli.Serve.DBPostgresPassword,
			Build: gopodder.BuildInfo{
				Version:   Version,
				Revision:  Revision,
				BuildDate: BuildDate,
				GoVersion: runtime.Version(),
				Platform:  runtime.GOOS + "/" + runtime.GOARCH,
			},
		}); err != nil {
			logger.Error("server exited with error", "err", err)
			os.Exit(1)
		}
	default:
		ctx.FatalIfErrorf(fmt.Errorf("unknown command: %s", ctx.Command()))
	}
}

func setupLogger(level string) *slog.Logger {
	logLevel := stringToLogLevel(level)
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	return slog.New(handler)
}

func stringToLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
