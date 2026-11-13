package api

import (
	"io/fs"
	"net/http"
	"strings"
)

func SPAHandler(fsys fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(fsys))

	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		if f, err := fsys.Open(path); err == nil {
			f.Close()
			if path == "" || path == "index.html" {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				w.Header().Set("Pragma", "no-cache")
				w.Header().Set("Expires", "0")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		r.URL.Path = "/"
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		fileServer.ServeHTTP(w, r)
	}
}
