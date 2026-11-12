package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"dockerview/internal/engine"
	"dockerview/internal/monitor"
)

type Server struct {
	engine    engine.Engine
	monitor   *monitor.Monitor
	mux       *http.ServeMux
	composeMu sync.Mutex
	topCache  topCache
	done      chan struct{}
	closeOnce sync.Once
}

func New(eng engine.Engine, mon *monitor.Monitor) *Server {
	s := &Server{
		engine:  eng,
		monitor: mon,
		mux:     http.NewServeMux(),
		done:    make(chan struct{}),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return loggingMiddleware(s.mux)
}

func (s *Server) Shutdown() {
	s.closeOnce.Do(func() { close(s.done) })
}

func (s *Server) Done() <-chan struct{} {
	return s.done
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/system", s.handleSystem)
	s.mux.HandleFunc("GET /api/system/stream", s.handleSystemStream)

	s.mux.HandleFunc("GET /api/containers/top", s.handleTopContainers)
	s.mux.HandleFunc("GET /api/containers", s.handleListContainers)
	s.mux.HandleFunc("GET /api/containers/usage", s.handleContainerUsage)
	s.mux.HandleFunc("POST /api/containers", s.handleCreateContainer)
	s.mux.HandleFunc("GET /api/containers/{id}", s.handleInspectContainer)
	s.mux.HandleFunc("DELETE /api/containers/{id}", s.handleRemoveContainer)
	s.mux.HandleFunc("POST /api/containers/{id}/{action}", s.handleContainerAction)
	s.mux.HandleFunc("GET /api/containers/{id}/stats", s.handleContainerStats)
	s.mux.HandleFunc("GET /api/containers/{id}/logs", s.handleContainerLogs)
	s.mux.HandleFunc("GET /api/containers/{id}/terminal", s.handleTerminal)

	s.mux.HandleFunc("GET /api/images", s.handleListImages)
	s.mux.HandleFunc("POST /api/images/pull", s.handlePullImage)
	s.mux.HandleFunc("DELETE /api/images/{id}", s.handleRemoveImage)

	s.mux.HandleFunc("GET /api/networks", s.handleListNetworks)
	s.mux.HandleFunc("POST /api/networks", s.handleCreateNetwork)
	s.mux.HandleFunc("DELETE /api/networks/{id}", s.handleRemoveNetwork)

	s.mux.HandleFunc("GET /api/volumes", s.handleListVolumes)
	s.mux.HandleFunc("POST /api/volumes", s.handleCreateVolume)
	s.mux.HandleFunc("DELETE /api/volumes/{name}", s.handleRemoveVolume)

	s.mux.HandleFunc("GET /api/compose/projects", s.handleComposeProjects)
	s.mux.HandleFunc("POST /api/compose/up", s.handleComposeUp)
	s.mux.HandleFunc("GET /api/compose/files/{name}", s.handleComposeFileGet)
	s.mux.HandleFunc("DELETE /api/compose/files/{name}", s.handleComposeFileDelete)
	s.mux.HandleFunc("POST /api/compose/{name}/{action}", s.handleComposeAction)

	s.mux.HandleFunc("GET /api/meta", s.handleMeta)
	s.mux.Handle("/", s.staticHandler())
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if r.URL.Path != "/api/system/stream" && r.URL.Path != "/api/containers/stream" {
			log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
