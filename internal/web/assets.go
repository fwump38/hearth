package web

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

// Version is the release version, set at build time (-ldflags "-X .../internal/web.Version=1.2.3").
var Version = "dev"

var (
	assetOnce sync.Once
	assetVer  string
)

// assetVersion is a short hash over every embedded static file. Appending it to asset URLs gives each
// release its own cache key, so Cloudflare / browser caches never serve stale CSS, JS or icons.
func assetVersion() string {
	assetOnce.Do(func() {
		h := sha256.New()
		_ = fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			b, _ := staticFS.ReadFile(p)
			h.Write([]byte(p))
			h.Write(b)
			return nil
		})
		assetVer = hex.EncodeToString(h.Sum(nil))[:10]
	})
	return assetVer
}

func assetURL(p string) string { return p + "?v=" + assetVersion() }

// staticHandler serves embedded assets. Versioned URLs (?v=...) are immutable; bare URLs must revalidate.
func staticHandler() http.Handler {
	fsrv := http.FileServerFS(staticFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fsrv.ServeHTTP(w, r)
	})
}

// rootIcon serves an embedded icon at a fixed root path (browsers request /favicon.ico unprompted).
func rootIcon(file string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := staticFS.ReadFile("static/icons/" + file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(file, ".ico") {
			w.Header().Set("Content-Type", "image/x-icon")
		} else {
			w.Header().Set("Content-Type", "image/png")
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	}
}
