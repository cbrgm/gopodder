package gopodder

import (
	"log/slog"
	"net/http"
	"net/http/pprof"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func startDebugServer(logger *slog.Logger, addr string) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	mux.Handle("GET /metrics", promhttp.Handler())

	logger.Info("starting debug server", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("debug server failed", "err", err)
	}
}
