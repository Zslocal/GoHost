package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dockerview/internal/compose"
	"dockerview/internal/model"
)

func (s *Server) composeDir() string {
	dir := os.Getenv("DOCKERVIEW_COMPOSE_DIR")
	if dir == "" {
		dir = "./data/compose"
	}
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func (s *Server) handleListImages(w http.ResponseWriter, r *http.Request) {
	list, err := s.engine.ListImages(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handlePullImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reference string `json:"reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	req.Reference = strings.TrimSpace(req.Reference)
	if req.Reference == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("reference is required"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	emit := func(s string) error {
		payload, _ := json.Marshal(s)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	writer := &sseWriter{emit: emit}
	if err := s.engine.PullImage(r.Context(), req.Reference, writer); err != nil {
		_ = emit("ERROR: " + err.Error())
		return
	}
	_ = emit("DONE")
}

type sseWriter struct {
	emit func(string) error
}

func (w *sseWriter) Write(p []byte) (int, error) {
	parts := strings.Split(strings.TrimRight(string(p), "\n"), "\n")
	for _, line := range parts {
		if line == "" {
			continue
		}
		if err := w.emit(line); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (s *Server) handleRemoveImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	force := r.URL.Query().Get("force") == "true"
	if err := s.engine.RemoveImage(r.Context(), id, force); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleListNetworks(w http.ResponseWriter, r *http.Request) {
	list, err := s.engine.ListNetworks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req model.CreateNetworkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("network name is required"))
		return
	}
	id, err := s.engine.CreateNetwork(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleRemoveNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.engine.RemoveNetwork(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleListVolumes(w http.ResponseWriter, r *http.Request) {
	list, err := s.engine.ListVolumes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Driver string `json:"driver"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("volume name is required"))
		return
	}
	name, err := s.engine.CreateVolume(r.Context(), req.Name, req.Driver)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (s *Server) handleRemoveVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	force := r.URL.Query().Get("force") == "true"
	if err := s.engine.RemoveVolume(r.Context(), name, force); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleComposeProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.engine.ComposeProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	files := s.listComposeFiles()
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects, "files": files})
}

type composeFileInfo struct {
	Name     string `json:"name"`
	Content  string `json:"content,omitempty"`
	Services int    `json:"services,omitempty"`
}

func (s *Server) listComposeFiles() []composeFileInfo {
	dir := s.composeDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]composeFileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yml") && !strings.HasSuffix(e.Name(), ".yaml")) {
			continue
		}
		info := composeFileInfo{Name: e.Name()}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err == nil {
			info.Content = string(data)
			if file, err := compose.Parse(data); err == nil {
				info.Services = len(file.Services)
			}
		}
		out = append(out, info)
	}
	return out
}

func (s *Server) handleComposeUp(w http.ResponseWriter, r *http.Request) {
	s.composeMu.Lock()
	defer s.composeMu.Unlock()

	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("compose content is required"))
		return
	}
	file, err := compose.Parse([]byte(req.Content))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		file.Name = req.Name
	}
	if strings.TrimSpace(file.Name) == "" {
		file.Name = "compose"
	}

	if err := s.saveComposeFile(file.Name, req.Content); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.engine.ComposeUp(r.Context(), file); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "project": file.Name, "services": len(file.Services)})
}

func (s *Server) saveComposeFile(name, content string) error {
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".yml"), ".yaml")
	name = strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(name)
	path := filepath.Join(s.composeDir(), name+".yml")
	return os.WriteFile(path, []byte(content), 0o644)
}

func (s *Server) handleComposeFileGet(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	path := filepath.Join(s.composeDir(), name)
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("compose file %s not found", name))
		return
	}
	writeJSON(w, http.StatusOK, composeFileInfo{Name: name, Content: string(data)})
}

func (s *Server) handleComposeFileDelete(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	path := filepath.Join(s.composeDir(), name)
	if _, err := os.Stat(path); err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("compose file %s not found", name))
		return
	}
	if err := os.Remove(path); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleComposeAction(w http.ResponseWriter, r *http.Request) {
	s.composeMu.Lock()
	defer s.composeMu.Unlock()
	name := r.PathValue("name")
	action := r.PathValue("action")
	ctx := r.Context()
	var err error
	switch action {
	case "down":
		err = s.engine.ComposeDown(ctx, name)
	case "stop":
		err = s.engine.ComposeStop(ctx, name)
	case "start":
		err = s.engine.ComposeStart(ctx, name)
	case "restart":
		err = s.engine.ComposeRestart(ctx, name)
	case "up":
		path := filepath.Join(s.composeDir(), name+".yml")
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			path = filepath.Join(s.composeDir(), name)
			data, rerr = os.ReadFile(path)
		}
		if rerr != nil {
			writeError(w, http.StatusNotFound, fmt.Errorf("compose file for project %s not found", name))
			return
		}
		file, perr := compose.Parse(data)
		if perr != nil {
			writeError(w, http.StatusBadRequest, perr)
			return
		}
		if file.Name == "" {
			file.Name = name
		}
		err = s.engine.ComposeUp(ctx, file)
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown action %q", action))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "action": action})
}

var _ = io.Discard
