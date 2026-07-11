// Package adminui embeds the compiled admin SPA and serves it under /admin with
// SPA fallback (unknown paths return index.html so client-side routing works).
package adminui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed dist
var distFS embed.FS

// dist returns the embedded build output rooted at the "dist" directory.
func dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Only possible if the embed directive is malformed at build time.
		panic(err)
	}
	return sub
}

// Handler serves the admin SPA. It is mounted by the server at /admin (and
// /admin/*). Genuine embedded sub-files (e.g. assets/*) are served directly;
// the index and every unmatched path fall back to index.html.
//
// Note: we deliberately do NOT route the index through http.FileServer.
// http.FileServer 301-redirects any request whose path ends in "/index.html"
// to "./", so GET /admin and GET /admin/ would return 301 instead of the index
// HTML. Serving the index bytes directly keeps those a clean 200.
//
// If the SPA has not been built yet (index.html absent), serveIndex returns 503
// so a fresh checkout still compiles/tests.
func Handler() http.Handler {
	root := dist()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Normalise the path relative to the /admin mount.
		p := strings.TrimPrefix(r.URL.Path, "/admin")
		p = strings.TrimPrefix(p, "/")

		// Empty path or an explicit index request → serve the index directly
		// (never via FileServer, which would 301 "/index.html" → "./").
		if p == "" || p == "index.html" {
			serveIndex(w, root)
			return
		}

		// Genuine embedded file (e.g. assets/app.js): read and write it
		// directly, setting Content-Type by extension. Anything that isn't a
		// real file falls through to the SPA index.
		if data, err := fs.ReadFile(root, p); err == nil {
			if ct := mime.TypeByExtension(path.Ext(p)); ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}

		// SPA fallback: serve index.html for any unmatched path.
		serveIndex(w, root)
	})
}

func serveIndex(w http.ResponseWriter, root fs.FS) {
	data, err := fs.ReadFile(root, "index.html")
	if err != nil {
		// SPA not built yet (fresh checkout before `make build-admin`).
		http.Error(w, "admin UI not built", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
