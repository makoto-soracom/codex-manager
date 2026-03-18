package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// NewShareServer serves only exact filenames from the share directory.
func NewShareServer(shareDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = fmt.Fprint(w, "<!doctype html><html><head><meta charset=\"utf-8\"><title>Codex Manager Share Server</title></head><body><h1>Codex Manager Share Server</h1><p>This server only serves exact share filenames. Use the Share button in the main UI and open the generated URL, for example:</p><p><code>http://localhost:8081/&lt;uuid&gt;.html</code></p></body></html>")
			}
			return
		}
		if strings.Contains(path, "/") || strings.Contains(path, "\\") || strings.Contains(path, "..") {
			http.NotFound(w, r)
			return
		}

		target := filepath.Join(shareDir, path)
		info, err := os.Stat(target)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		http.ServeFile(w, r, target)
	})
}
