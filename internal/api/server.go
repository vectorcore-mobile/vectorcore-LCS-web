// Package api wires the LCS console's own thin HTTP API (which proxies to
// the GMLC's Le REST/JSON adapter) together with the embedded React UI.
package api

import (
	"log/slog"
	"net/http"

	"github.com/vectorcore/lcs/internal/gmlc"
)

// NewServer mounts the default profile's routes unconditionally, plus an
// identical set under /api/v1/emergency/ if emergencyClient is non-nil.
// The emergency routes are a genuinely separate GMLC client identity, not
// a flag on the default request — see docs/mlp-le-interface-plan.md's L3:
// GMLC resolves LCS-Client-Type purely from which client credential
// authenticated, so this only works because emergencyClient carries its
// own, separately-configured credentials (see config.GMLCClient), not
// because the request body says anything different.
func NewServer(client gmlc.LocationClient, emergencyClient gmlc.LocationClient, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	lh := &locationHandler{client: client}
	mux.HandleFunc("GET /api/v1/health", health)
	mux.HandleFunc("GET /api/v1/gmlc/status", lh.gmlcStatus)
	mux.HandleFunc("POST /api/v1/location-requests", lh.submit)
	mux.HandleFunc("GET /api/v1/location-requests/{id}", lh.get)
	mux.HandleFunc("DELETE /api/v1/location-requests/{id}", lh.cancel)
	mux.HandleFunc("GET /api/v1/history", lh.history)

	if emergencyClient != nil {
		eh := &locationHandler{client: emergencyClient}
		mux.HandleFunc("GET /api/v1/emergency/gmlc/status", eh.gmlcStatus)
		mux.HandleFunc("POST /api/v1/emergency/location-requests", eh.submit)
		mux.HandleFunc("GET /api/v1/emergency/location-requests/{id}", eh.get)
		mux.HandleFunc("DELETE /api/v1/emergency/location-requests/{id}", eh.cancel)
		mux.HandleFunc("GET /api/v1/emergency/history", eh.history)
	} else {
		// Without this, these paths fall through to uiHandler's SPA
		// fallback (mux.Handle("/", ...) below matches anything more
		// specific doesn't) and return 200 HTML — indistinguishable from
		// "configured" to a caller checking response.ok, which is exactly
		// what App.jsx's emergency-nav-entry probe does. Register real 404s
		// instead so "route not mounted" reads as absent, not present.
		mux.HandleFunc("GET /api/v1/emergency/gmlc/status", emergencyNotConfigured)
		mux.HandleFunc("POST /api/v1/emergency/location-requests", emergencyNotConfigured)
		mux.HandleFunc("GET /api/v1/emergency/location-requests/{id}", emergencyNotConfigured)
		mux.HandleFunc("DELETE /api/v1/emergency/location-requests/{id}", emergencyNotConfigured)
		mux.HandleFunc("GET /api/v1/emergency/history", emergencyNotConfigured)
	}

	mux.Handle("/", uiHandler())

	return withLogging(mux, logger)
}

func withLogging(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
