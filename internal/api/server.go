// Package api wires the LCS console's own thin HTTP API (which proxies to
// the GMLC's Le REST/JSON adapter) together with the embedded React UI.
package api

import (
	"log/slog"
	"net/http"

	"github.com/vectorcore/lcs/internal/gmlc"
)

func NewServer(client gmlc.LocationClient, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	lh := &locationHandler{client: client}

	mux.HandleFunc("GET /api/v1/health", health)
	mux.HandleFunc("GET /api/v1/gmlc/status", lh.gmlcStatus)
	mux.HandleFunc("POST /api/v1/location-requests", lh.submit)
	mux.HandleFunc("GET /api/v1/location-requests/{id}", lh.get)
	mux.HandleFunc("DELETE /api/v1/location-requests/{id}", lh.cancel)

	mux.Handle("/", uiHandler())

	return withLogging(mux, logger)
}

func withLogging(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
