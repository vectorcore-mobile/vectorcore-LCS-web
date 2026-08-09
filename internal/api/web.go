package api

import (
	"io"
	"io/fs"
	"net/http"

	webui "github.com/vectorcore/lcs/web"
)

// uiHandler serves the embedded React SPA from web/dist at the root path.
// Requests for a path that doesn't match a real embedded file fall back to
// index.html so React Router's client-side routing works correctly.
func uiHandler() http.Handler {
	sub, err := fs.Sub(webui.FS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`<!doctype html><html><body style="font-family:monospace;background:#0a0e14;color:#c8d0dc;padding:2rem">
<h2>VectorCore LCS</h2><p>UI not built. Run <code>make ui</code> then rebuild the binary.</p></body></html>`))
		})
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if len(path) > 0 && path[0] == '/' {
			path = path[1:]
		}

		if path != "" {
			if _, err := fs.Stat(sub, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA fallback: serve index.html directly (not via the file server,
		// which would redirect "/index.html" -> "/" and loop).
		f, err := sub.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "index.html", stat.ModTime(), f.(io.ReadSeeker))
	})
}
