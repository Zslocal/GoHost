package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"dockerview/internal/model"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now()})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"engine": s.engine.Kind(),
	})
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := s.monitor.Snapshot(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	info, err := s.engine.Info(ctx)
	if err != nil {
		info = model.Info{Kind: s.engine.Kind()}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"host":   stats,
		"docker": info,
		"engine": s.engine.Kind(),
	})
}

func (s *Server) handleSystemStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	send := func() bool {
		stats, err := s.monitor.Snapshot(r.Context())
		if err != nil {
			return true
		}
		payload, err := json.Marshal(stats)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: stats\ndata: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send() {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

func (s *Server) handleListContainers(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") != "false"
	list, err := s.engine.ListContainers(r.Context(), all)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleContainerUsage 批量返回运行中容器的 CPU / 内存占用。
//
// 单独成一个接口而不并进 /api/containers，是因为 stats 需要等 daemon 采一帧，
// 每个容器几十到上百毫秒，串在列表里会让整个容器页白屏等它。分开后前端可以先
// 渲染列表，再就地把这一列填上。
//
// 只取运行中的容器（已停止的没有意义），并发上限 6，整体 4 秒超时：
// 超时或出错的容器直接不出现在结果里，前端显示「—」，不影响列表本身。
func (s *Server) handleContainerUsage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	list, err := s.engine.ListContainers(ctx, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	ids := make([]string, 0, len(list))
	for _, c := range list {
		if c.State == "running" {
			ids = append(ids, c.ID)
		}
	}

	type result struct {
		id    string
		stats model.ContainerStats
		err   error
	}
	sem := make(chan struct{}, 6)
	out := make(chan result, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			stats, err := s.engine.ContainerStats(ctx, id)
			out <- result{id: id, stats: stats, err: err}
		}(id)
	}
	wg.Wait()
	close(out)

	usage := make(map[string]model.ContainerStats, len(ids))
	for res := range out {
		if res.err == nil {
			usage[res.id] = res.stats
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"usage":     usage,
		"collected": len(usage),
		"requested": len(ids),
	})
}

func (s *Server) handleInspectContainer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	detail, err := s.engine.InspectContainer(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCreateContainer(w http.ResponseWriter, r *http.Request) {
	var req model.CreateContainerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	id, err := s.engine.CreateContainer(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleRemoveContainer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	force := r.URL.Query().Get("force") != "false"
	volumes := r.URL.Query().Get("volumes") == "true"
	if err := s.engine.RemoveContainer(r.Context(), id, force, volumes); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleContainerAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	action := r.PathValue("action")
	ctx := r.Context()
	var err error
	switch action {
	case "start":
		err = s.engine.StartContainer(ctx, id)
	case "stop":
		err = s.engine.StopContainer(ctx, id)
	case "restart":
		err = s.engine.RestartContainer(ctx, id)
	case "pause":
		err = s.engine.PauseContainer(ctx, id)
	case "unpause":
		err = s.engine.UnpauseContainer(ctx, id)
	case "kill":
		sig := r.URL.Query().Get("signal")
		err = s.engine.KillContainer(ctx, id, sig)
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

func (s *Server) handleContainerStats(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	stats, err := s.engine.ContainerStats(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tail := 200
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tail = n
		}
	}
	timestamps := r.URL.Query().Get("timestamps") == "true"
	reader, tty, err := s.engine.ContainerLogs(r.Context(), id, tail, timestamps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer reader.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	emit := func(line string) bool {
		payload, _ := json.Marshal(line)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if tty {
		buf := make([]byte, 8192)
		for {
			select {
			case <-s.done:
				return
			default:
			}
			n, err := reader.Read(buf)
			if n > 0 {
				for _, line := range strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n") {
					if !emit(line) {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}
	if err := streamDockerLogs(w, flusher, reader, emit); err != nil {
		return
	}
}

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  32 * 1024,
		WriteBufferSize: 32 * 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	id := r.PathValue("id")
	cols := parseUint(r.URL.Query().Get("cols"), 120)
	rows := parseUint(r.URL.Query().Get("rows"), 30)
	term, cleanup, err := s.engine.ContainerTerminal(r.Context(), id, cols, rows)
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[1;31m"+err.Error()+"\x1b[0m\r\n"))
		return
	}
	defer cleanup()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 8192)
		for {
			n, err := term.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		default:
		}
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if mt == websocket.TextMessage && strings.HasPrefix(string(data), "\x00resize:") {
			parts := strings.Split(strings.TrimPrefix(string(data), "\x00resize:"), "x")
			if len(parts) == 2 {
				c, _ := strconv.Atoi(parts[0])
				rw, _ := strconv.Atoi(parts[1])
				if c > 0 && rw > 0 {
					_ = term.Resize(uint(c), uint(rw))
				}
			}
			continue
		}
		if _, err := term.Write(data); err != nil {
			return
		}
	}
}

func parseUint(s string, def uint) uint {
	if s == "" {
		return def
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil || v == 0 {
		return def
	}
	return uint(v)
}
