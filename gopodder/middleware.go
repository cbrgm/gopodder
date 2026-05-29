package gopodder

import (
	"net/http"
	"time"
)

const maxRequestBody = 5 << 20 // 5 MiB

func withMaxBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (a *API) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		a.logger.Debug("request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
		next.ServeHTTP(rw, r)

		path := metricsPath(r.URL.Path)
		a.metrics.IncHTTPRequest(r.Method, path, rw.status)
		a.metrics.ObserveHTTPDuration(r.Method, path, time.Since(start))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func metricsPath(path string) string {
	if len(path) >= 7 && path[:7] == "/api/v1" {
		return "/api/v1"
	}
	if hasAPIPrefix(path) {
		return "/api/v2"
	}
	if len(path) > 1 && path[0] == '/' {
		for i := 1; i < len(path); i++ {
			if path[i] == '/' {
				return path[:i]
			}
		}
	}
	return path
}

func hasAPIPrefix(path string) bool {
	return len(path) >= 5 && path[:5] == "/api/"
}
