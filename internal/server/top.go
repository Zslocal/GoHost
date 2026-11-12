package server

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"dockerview/internal/model"
)

const (
	defaultTopLimit = 10
	maxTopLimit     = 50
	topConcurrency  = 8
	topCacheTTL     = 3 * time.Second
	topCollectTO    = 10 * time.Second
)

type topResponse struct {
	CPU    []model.ContainerMetric `json:"cpu"`
	Memory []model.ContainerMetric `json:"memory"`
}

type topCache struct {
	mu      sync.Mutex
	metrics []model.ContainerMetric
	fetched time.Time
}

func (s *Server) handleTopContainers(w http.ResponseWriter, r *http.Request) {
	limit := defaultTopLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxTopLimit {
		limit = maxTopLimit
	}

	metrics, err := s.runningMetrics(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	byCPU := make([]model.ContainerMetric, len(metrics))
	copy(byCPU, metrics)
	sort.SliceStable(byCPU, func(i, j int) bool {
		if byCPU[i].CPUPercent == byCPU[j].CPUPercent {
			return byCPU[i].MemUsage > byCPU[j].MemUsage
		}
		return byCPU[i].CPUPercent > byCPU[j].CPUPercent
	})
	byCPU = truncateMetrics(byCPU, limit)

	byMem := make([]model.ContainerMetric, len(metrics))
	copy(byMem, metrics)
	sort.SliceStable(byMem, func(i, j int) bool {
		if byMem[i].MemUsage == byMem[j].MemUsage {
			return byMem[i].CPUPercent > byMem[j].CPUPercent
		}
		return byMem[i].MemUsage > byMem[j].MemUsage
	})
	byMem = truncateMetrics(byMem, limit)

	s.enrichWithPid(r.Context(), byCPU, byMem)

	writeJSON(w, http.StatusOK, topResponse{CPU: byCPU, Memory: byMem})
}

func truncateMetrics(list []model.ContainerMetric, limit int) []model.ContainerMetric {
	if len(list) > limit {
		list = list[:limit]
	}
	if list == nil {
		return []model.ContainerMetric{}
	}
	return list
}

// enrichWithPid fills the process id for the containers that made it into the
// ranking. Only the union of both lists is inspected so a large container
// count does not translate into a large number of daemon calls.
func (s *Server) enrichWithPid(ctx context.Context, lists ...[]model.ContainerMetric) {
	seen := map[string]bool{}
	ids := make([]string, 0, len(lists)*maxTopLimit)
	for _, list := range lists {
		for _, m := range list {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, topCollectTO)
	defer cancel()

	pids := make([]int, len(ids))
	sem := make(chan struct{}, topConcurrency)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(idx int, cid string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			detail, err := s.engine.InspectContainer(ctx, cid)
			if err != nil {
				return
			}
			pids[idx] = detail.Pid
		}(i, id)
	}
	wg.Wait()

	pidByID := make(map[string]int, len(ids))
	for i, id := range ids {
		if pids[i] != 0 {
			pidByID[id] = pids[i]
		}
	}
	for _, list := range lists {
		for i := range list {
			if pid, ok := pidByID[list[i].ID]; ok {
				list[i].Pid = pid
			}
		}
	}
}

func (s *Server) runningMetrics(ctx context.Context) ([]model.ContainerMetric, error) {
	if cached, ok := s.cachedMetrics(); ok {
		return cached, nil
	}

	metrics, err := s.collectRunningMetrics(ctx)
	if err != nil {
		return nil, err
	}

	s.topCache.mu.Lock()
	s.topCache.metrics = metrics
	s.topCache.fetched = time.Now()
	s.topCache.mu.Unlock()
	return metrics, nil
}

func (s *Server) cachedMetrics() ([]model.ContainerMetric, bool) {
	s.topCache.mu.Lock()
	defer s.topCache.mu.Unlock()
	if s.topCache.metrics == nil || time.Since(s.topCache.fetched) > topCacheTTL {
		return nil, false
	}
	cached := make([]model.ContainerMetric, len(s.topCache.metrics))
	copy(cached, s.topCache.metrics)
	return cached, true
}

func (s *Server) collectRunningMetrics(ctx context.Context) ([]model.ContainerMetric, error) {
	ctx, cancel := context.WithTimeout(ctx, topCollectTO)
	defer cancel()

	containers, err := s.engine.ListContainers(ctx, false)
	if err != nil {
		return nil, err
	}

	metrics := make([]model.ContainerMetric, len(containers))
	sem := make(chan struct{}, topConcurrency)
	var wg sync.WaitGroup
	for i, c := range containers {
		metrics[i] = model.ContainerMetric{
			ID:      c.ID,
			Name:    c.Name,
			Image:   c.Image,
			State:   c.State,
			Command: c.Command,
		}
		wg.Add(1)
		go func(idx int, id string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			stats, err := s.engine.ContainerStats(ctx, id)
			if err != nil {
				return
			}
			metrics[idx].CPUPercent = stats.CPUPercent
			metrics[idx].MemUsage = stats.MemUsage
			metrics[idx].MemLimit = stats.MemLimit
			metrics[idx].MemPercent = stats.MemPercent
		}(i, c.ID)
	}
	wg.Wait()

	out := make([]model.ContainerMetric, 0, len(metrics))
	for _, m := range metrics {
		if m.State == "running" {
			out = append(out, m)
		}
	}
	return out, nil
}
