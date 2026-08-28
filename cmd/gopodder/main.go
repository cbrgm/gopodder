package main

import (
	"cmp"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/cbrgm/gopodder/gopodder"
)

const (
	devVersion   = "dev"
	unknownValue = "unknown"
)

// Injected at link time by the Makefile. Builds made without it recover the
// same information from the VCS stamps of the Go toolchain, see resolveBuildInfo.
var (
	Version   = devVersion
	Revision  = unknownValue
	BuildDate = unknownValue
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
	info, _ := debug.ReadBuildInfo()
	version, revision, buildDate := resolveBuildInfo(Version, Revision, BuildDate, info)

	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("gopodder"),
		kong.Description("A gPodder-compatible podcast synchronization server."),
		kong.Vars{"version": fmt.Sprintf("%s (revision: %s, built: %s)", version, revision, buildDate)},
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
				Version:   version,
				Revision:  revision,
				BuildDate: buildDate,
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

// resolveBuildInfo fills in build metadata the linker did not provide. Values
// injected via -X always win; the rest is recovered from the VCS stamps the Go
// toolchain embeds, so a plain "go build" or "go install" still reports the
// commit it was built from instead of a bare "dev".
func resolveBuildInfo(version, revision, buildDate string, info *debug.BuildInfo) (string, string, string) {
	var vcsRevision, vcsTime string
	var vcsModified bool
	var moduleVersion string

	if info != nil {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				vcsRevision = setting.Value
			case "vcs.time":
				vcsTime = setting.Value
			case "vcs.modified":
				vcsModified = setting.Value == "true"
			}
		}
		if info.Main.Version != "(devel)" {
			moduleVersion = strings.TrimPrefix(info.Main.Version, "v")
		}
	}

	shortRevision := vcsRevision
	if len(shortRevision) > 7 {
		shortRevision = shortRevision[:7]
	}
	if shortRevision != "" && vcsModified {
		shortRevision += "-dirty"
	}

	if version == "" || version == devVersion {
		version = cmp.Or(moduleVersion, shortRevision, version, devVersion)
	}
	if revision == "" || revision == unknownValue {
		revision = cmp.Or(shortRevision, revision, unknownValue)
	}
	if buildDate == "" || buildDate == unknownValue {
		buildDate = unknownValue
		if stamped, err := time.Parse(time.RFC3339, vcsTime); err == nil {
			buildDate = stamped.UTC().Format("20060102")
		}
	}

	return version, revision, buildDate
}

func setupLogger(level string) *slog.Logger {
	logLevel := stringToLogLevel(level)
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	return slog.New(handler)
}

// stringToLogLevel parses a level name, falling back to info for anything
// slog does not recognize (the zero Level is info).
func stringToLogLevel(level string) slog.Level {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return l
}
