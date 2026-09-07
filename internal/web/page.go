package web

import (
	"embed"
	"net/http"
)

//go:embed assets/index.html
var assets embed.FS

// pageHandler serves the single page. It is embedded in the binary so the
// deployable unit stays one file — the same property essaim and bkn have, and
// the reason neither needs an asset directory next to it in production.
func pageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, err := assets.ReadFile("assets/index.html")
		if err != nil {
			http.Error(w, "page missing from binary", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// No external origins: everything is inline, so the page keeps working
		// with no network beyond this host and cannot be altered by a CDN.
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(b)
	})
}
