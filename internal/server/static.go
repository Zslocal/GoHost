package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:assets
var assets embed.FS

func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
