package demo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"dockerview/internal/model"
)

type Engine struct {
	mu         sync.Mutex
	containers map[string]*containerState
	images     map[string]*model.Image
	networks   map[string]*model.Network
	volumes    map[string]*model.Volume
	order      []string
}

type containerState struct {
	detail  model.ContainerDetail
	created time.Time
	logs    *logBuffer
	stats   model.ContainerStats
}

func New() *Engine {
	e := &Engine{
		containers: map[string]*containerState{},
		images:     map[string]*model.Image{},
		networks:   map[string]*model.Network{},
		volumes:    map[string]*model.Volume{},
	}
	e.seed()
	return e
}

func (e *Engine) Kind() string { return "demo" }
func (e *Engine) Close() error { return nil }

func randID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (e *Engine) seed() {
	now := time.Now()
	images := []model.Image{
		{ID: randID(), RepoTags: []string{"nginx:1.27-alpine"}, Created: now.Add(-72 * time.Hour).Unix(), Size: 48 * 1024 * 1024},
		{ID: randID(), RepoTags: []string{"postgres:16-alpine"}, Created: now.Add(-120 * time.Hour).Unix(), Size: 240 * 1024 * 1024},
		{ID: randID(), RepoTags: []string{"redis:7-alpine"}, Created: now.Add(-200 * time.Hour).Unix(), Size: 41 * 1024 * 1024},
		{ID: randID(), RepoTags: []string{"grafana/grafana:11.2.0"}, Created: now.Add(-48 * time.Hour).Unix(), Size: 420 * 1024 * 1024},
		{ID: randID(), RepoTags: []string{"traefik:v3.1"}, Created: now.Add(-300 * time.Hour).Unix(), Size: 156 * 1024 * 1024},
	}
	for _, img := range images {
		e.images[img.ID] = &img
	}

	nets := []model.Network{
		{ID: randID(), Name: "bridge", Driver: "bridge", Scope: "local", Created: now.Add(-720 * time.Hour).Format(time.RFC3339)},
		{ID: randID(), Name: "host", Driver: "host", Scope: "local", Created: now.Add(-720 * time.Hour).Format(time.RFC3339)},
		{ID: randID(), Name: "none", Driver: "null", Scope: "local", Created: now.Add(-720 * time.Hour).Format(time.RFC3339)},
		{ID: randID(), Name: "shop_default", Driver: "bridge", Scope: "local", Created: now.Add(-30 * time.Hour).Format(time.RFC3339)},
		{ID: randID(), Name: "monitoring", Driver: "bridge", Scope: "local", Created: now.Add(-20 * time.Hour).Format(time.RFC3339)},
	}
	for _, n := range nets {
		e.networks[n.ID] = &n
	}

	vols := []model.Volume{
		{Name: "shop_db_data", Driver: "local", Mountpoint: "/var/lib/docker/volumes/shop_db_data/_data", Scope: "local", CreatedAt: now.Add(-30 * time.Hour).Format(time.RFC3339), InUse: true},
		{Name: "grafana_data", Driver: "local", Mountpoint: "/var/lib/docker/volumes/grafana_data/_data", Scope: "local", CreatedAt: now.Add(-20 * time.Hour).Format(time.RFC3339), InUse: true},
		{Name: "redis_cache", Driver: "local", Mountpoint: "/var/lib/docker/volumes/redis_cache/_data", Scope: "local", CreatedAt: now.Add(-18 * time.Hour).Format(time.RFC3339), InUse: true},
		{Name: "backup_archive", Driver: "local", Mountpoint: "/var/lib/docker/volumes/backup_archive/_data", Scope: "local", CreatedAt: now.Add(-96 * time.Hour).Format(time.RFC3339)},
	}
	for _, v := range vols {
		vv := v
		e.volumes[v.Name] = &vv
	}

	type seedContainer struct {
		name     string
		image    string
		state    string
		project  string
		service  string
		ports    []model.Port
		cmd      []string
		env      []string
		networks []string
		mounts   []model.Mount
		health   string
	}
	seeds := []seedContainer{
		{
			name: "shop-web-1", image: "nginx:1.27-alpine", state: "running", project: "shop", service: "web",
			ports:    []model.Port{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}},
			cmd:      []string{"nginx", "-g", "daemon off;"},
			env:      []string{"NGINX_ENTRYPOINT_QUIET_LOGS=1"},
			networks: []string{"shop_default"},
			mounts:   []model.Mount{{Type: "bind", Source: "/srv/shop/html", Destination: "/usr/share/nginx/html", RW: true}},
			health:   "healthy",
		},
		{
			name: "shop-api-1", image: "traefik:v3.1", state: "running", project: "shop", service: "api",
			ports:    []model.Port{{IP: "0.0.0.0", PrivatePort: 8080, PublicPort: 9090, Type: "tcp"}},
			cmd:      []string{"/api-server", "--config", "/etc/api/config.yaml"},
			env:      []string{"DATABASE_URL=postgres://shop:secret@shop-db-1:5432/shop", "LOG_LEVEL=info"},
			networks: []string{"shop_default"},
			mounts:   []model.Mount{},
		},
		{
			name: "shop-db-1", image: "postgres:16-alpine", state: "running", project: "shop", service: "db",
			ports:    []model.Port{{IP: "127.0.0.1", PrivatePort: 5432, PublicPort: 5432, Type: "tcp"}},
			cmd:      []string{"postgres"},
			env:      []string{"POSTGRES_DB=shop", "POSTGRES_USER=shop", "POSTGRES_PASSWORD=secret"},
			networks: []string{"shop_default"},
			mounts:   []model.Mount{{Type: "volume", Source: "shop_db_data", Destination: "/var/lib/postgresql/data", RW: true}},
			health:   "healthy",
		},
		{
			name: "redis-cache", image: "redis:7-alpine", state: "running", project: "shop", service: "cache",
			ports:    []model.Port{{IP: "0.0.0.0", PrivatePort: 6379, PublicPort: 6379, Type: "tcp"}},
			cmd:      []string{"redis-server", "--appendonly", "yes"},
			env:      []string{},
			networks: []string{"shop_default"},
			mounts:   []model.Mount{{Type: "volume", Source: "redis_cache", Destination: "/data", RW: true}},
		},
		{
			name: "grafana", image: "grafana/grafana:11.2.0", state: "running", project: "monitoring", service: "grafana",
			ports:    []model.Port{{IP: "0.0.0.0", PrivatePort: 3000, PublicPort: 3000, Type: "tcp"}},
			cmd:      []string{"/run.sh"},
			env:      []string{"GF_SECURITY_ADMIN_PASSWORD=admin", "GF_INSTALL_PLUGINS="},
			networks: []string{"monitoring"},
			mounts:   []model.Mount{{Type: "volume", Source: "grafana_data", Destination: "/var/lib/grafana", RW: true}},
			health:   "healthy",
		},
		{
			name: "traefik-proxy", image: "traefik:v3.1", state: "exited", project: "", service: "",
			ports:    []model.Port{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 80, Type: "tcp"}},
			cmd:      []string{"traefik", "--providers.docker"},
			env:      []string{},
			networks: []string{"bridge"},
			mounts:   []model.Mount{},
		},
	}
	for i, s := range seeds {
		id := randID()
		created := now.Add(-time.Duration(len(seeds)-i) * 3 * time.Hour)
		statusText := ""
		switch s.state {
		case "running":
			statusText = "Up " + humanizeDuration(now.Sub(created))
			if s.health != "" {
				statusText += " (" + s.health + ")"
			}
		case "exited":
			statusText = "Exited (0) " + humanizeDuration(now.Sub(created.Add(2*time.Hour)))
		}
		detail := model.ContainerDetail{
			Container: model.Container{
				ID:       id,
				Name:     s.name,
				Image:    s.image,
				Created:  created.Unix(),
				State:    s.state,
				Status:   statusText,
				Command:  strings.Join(s.cmd, " "),
				Ports:    s.ports,
				Networks: s.networks,
				Mounts:   s.mounts,
				Project:  s.project,
				Service:  s.service,
				Labels: map[string]string{
					"com.docker.compose.project": s.project,
					"com.docker.compose.service": s.service,
				},
			},
			Env:           s.env,
			Cmd:           s.cmd,
			RestartPolicy: "unless-stopped",
			CreatedAt:     created.Format(time.RFC3339),
			StartedAt:     created.Format(time.RFC3339),
			WorkingDir:    "/",
			Hostname:      shortName(s.name),
			IPAddress:     "172.18.0." + fmt.Sprint(2+i),
			Pid:           1000 + i,
			Tty:           false,
		}
		if s.state == "exited" {
			detail.RestartPolicy = "no"
		}
		cs := &containerState{
			detail:  detail,
			created: created,
			logs:    newLogBuffer(500),
		}
		cs.seedLogs(s.name, s.state)
		cs.stats = syntheticStats(s.state)
		e.containers[id] = cs
		e.order = append(e.order, id)
	}
}

func shortName(name string) string {
	if len(name) > 12 {
		return name[:12]
	}
	return name
}

func humanizeDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

func syntheticStats(state string) model.ContainerStats {
	if state != "running" {
		return model.ContainerStats{Timestamp: time.Now()}
	}
	return model.ContainerStats{
		CPUPercent: 1.4 + float64(time.Now().UnixNano()%400)/100,
		MemUsage:   120 * 1024 * 1024,
		MemLimit:   512 * 1024 * 1024,
		MemPercent: 23.4,
		NetRx:      2 * 1024 * 1024,
		NetTx:      1 * 1024 * 1024,
		Pids:       12,
		Timestamp:  time.Now(),
	}
}

func (e *Engine) Info(ctx context.Context) (model.Info, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	running, stopped := 0, 0
	for _, c := range e.containers {
		if c.detail.State == "running" {
			running++
		} else {
			stopped++
		}
	}
	return model.Info{
		Kind:              "demo",
		Name:              "demo-host",
		ServerVersion:     "27.3.1",
		APIVersion:        "1.47",
		OS:                "Demo Linux",
		Arch:              "x86_64",
		Driver:            "overlay2",
		CgroupVersion:     "2",
		KernelVersion:     "6.6.0-demo",
		RootDir:           "/var/lib/docker",
		Containers:        len(e.containers),
		ContainersRunning: running,
		ContainersStopped: stopped,
		Images:            len(e.images),
		NCPU:              4,
		MemTotal:          8 * 1024 * 1024 * 1024,
	}, nil
}

func (e *Engine) ListContainers(ctx context.Context, all bool) ([]model.Container, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]model.Container, 0, len(e.order))
	for _, id := range e.order {
		c := e.containers[id]
		if c == nil {
			continue
		}
		if !all && c.detail.State != "running" {
			continue
		}
		out = append(out, c.detail.Container)
	}
	return out, nil
}

func (e *Engine) InspectContainer(ctx context.Context, id string) (model.ContainerDetail, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.lookupLocked(id)
	if err != nil {
		return model.ContainerDetail{}, err
	}
	return c.detail, nil
}

func (e *Engine) lookupLocked(id string) (*containerState, error) {
	if c, ok := e.containers[id]; ok {
		return c, nil
	}
	for cid, c := range e.containers {
		if c.detail.Name == id || strings.HasPrefix(cid, id) {
			return c, nil
		}
	}
	return nil, fmt.Errorf("container %s not found", id)
}

func (e *Engine) CreateContainer(ctx context.Context, req model.CreateContainerRequest) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if req.Image == "" {
		return "", fmt.Errorf("image is required")
	}
	id := randID()
	name := req.Name
	if name == "" {
		name = "container-" + id[:8]
	}
	now := time.Now()
	ports := make([]model.Port, 0)
	for _, spec := range req.Ports {
		if spec == "" {
			continue
		}
		ports = append(ports, parseDemoPort(spec))
	}
	labels := req.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	detail := model.ContainerDetail{
		Container: model.Container{
			ID:       id,
			Name:     name,
			Image:    req.Image,
			Created:  now.Unix(),
			State:    "running",
			Status:   "Up 1 second",
			Command:  req.Command,
			Ports:    ports,
			Labels:   labels,
			Networks: req.Networks,
			Mounts:   parseDemoMounts(req.Volumes),
		},
		Env:           req.Env,
		Cmd:           parseDemoArgs(req.Command),
		CreatedAt:     now.Format(time.RFC3339),
		StartedAt:     now.Format(time.RFC3339),
		WorkingDir:    req.WorkingDir,
		Hostname:      req.Hostname,
		IPAddress:     "172.18.0." + fmt.Sprint(len(e.containers)+2),
		Pid:           2000 + len(e.containers),
		Tty:           req.Tty,
		RestartPolicy: req.RestartPolicy,
	}
	if detail.Hostname == "" {
		detail.Hostname = shortName(name)
	}
	cs := &containerState{detail: detail, created: now, logs: newLogBuffer(500)}
	cs.seedLogs(name, "running")
	cs.stats = syntheticStats("running")
	e.containers[id] = cs
	e.order = append(e.order, id)
	if req.MemoryMB > 0 || req.CPULimit > 0 {
		if req.MemoryMB > 0 {
			cs.stats.MemLimit = uint64(req.MemoryMB) * 1024 * 1024
		}
	}
	return id, nil
}

func parseDemoPort(spec string) model.Port {
	proto := "tcp"
	if idx := strings.Index(spec, "/"); idx >= 0 {
		proto = spec[idx+1:]
		spec = spec[:idx]
	}
	parts := strings.Split(spec, ":")
	p := model.Port{Type: proto}
	switch len(parts) {
	case 1:
		fmt.Sscanf(parts[0], "%d", &p.PrivatePort)
	case 2:
		fmt.Sscanf(parts[0], "%d", &p.PublicPort)
		fmt.Sscanf(parts[1], "%d", &p.PrivatePort)
		p.IP = "0.0.0.0"
	case 3:
		p.IP = parts[0]
		fmt.Sscanf(parts[1], "%d", &p.PublicPort)
		fmt.Sscanf(parts[2], "%d", &p.PrivatePort)
	}
	return p
}

func parseDemoMounts(specs []string) []model.Mount {
	out := make([]model.Mount, 0, len(specs))
	for _, s := range specs {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			continue
		}
		t := "volume"
		if strings.HasPrefix(parts[0], "/") || strings.HasPrefix(parts[0], ".") {
			t = "bind"
		}
		out = append(out, model.Mount{Type: t, Source: parts[0], Destination: parts[1], RW: true})
	}
	return out
}

func parseDemoArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func (e *Engine) setState(id, state, status string) error {
	c, err := e.lookupLocked(id)
	if err != nil {
		return err
	}
	c.detail.State = state
	c.detail.Status = status
	switch state {
	case "running":
		c.detail.StartedAt = time.Now().Format(time.RFC3339)
		c.stats = syntheticStats("running")
		c.logs.add("INFO", "container started")
	case "exited":
		c.detail.FinishedAt = time.Now().Format(time.RFC3339)
		c.stats = syntheticStats("exited")
		c.logs.add("INFO", "container stopped")
	case "paused":
		c.logs.add("INFO", "container paused")
	}
	return nil
}

func (e *Engine) StartContainer(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.setState(id, "running", "Up 1 second")
}

func (e *Engine) StopContainer(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.setState(id, "exited", "Exited (0) 1 second ago")
}

func (e *Engine) RestartContainer(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.setState(id, "exited", "Exited (0) 1 second ago"); err != nil {
		return err
	}
	c, _ := e.lookupLocked(id)
	c.detail.RestartCount++
	return e.setState(id, "running", "Up 1 second")
}

func (e *Engine) PauseContainer(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.setState(id, "paused", "Up (Paused)")
}

func (e *Engine) UnpauseContainer(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.setState(id, "running", "Up 1 second")
}

func (e *Engine) KillContainer(ctx context.Context, id string, signal string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.lookupLocked(id)
	if err != nil {
		return err
	}
	c.logs.add("WARN", "received signal "+defaultSignal(signal))
	return e.setState(id, "exited", "Exited (137) 1 second ago")
}

func defaultSignal(s string) string {
	if s == "" {
		return "SIGKILL"
	}
	return s
}

func (e *Engine) RemoveContainer(ctx context.Context, id string, force, volumes bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.lookupLocked(id)
	if err != nil {
		return err
	}
	delete(e.containers, c.detail.ID)
	next := e.order[:0]
	for _, oid := range e.order {
		if oid != c.detail.ID {
			next = append(next, oid)
		}
	}
	e.order = next
	if volumes {
		for _, m := range c.detail.Mounts {
			if m.Type == "volume" {
				delete(e.volumes, m.Source)
			}
		}
	}
	return nil
}

func (e *Engine) ContainerStats(ctx context.Context, id string) (model.ContainerStats, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.lookupLocked(id)
	if err != nil {
		return model.ContainerStats{}, err
	}
	s := c.stats
	if c.detail.State == "running" {
		jitter := float64(time.Now().UnixNano()%1000) / 1000
		s.CPUPercent = 1.2 + jitter*6
		s.MemUsage = uint64(float64(c.stats.MemLimit) * (0.18 + jitter*0.1))
		if s.MemLimit > 0 {
			s.MemPercent = float64(s.MemUsage) / float64(s.MemLimit) * 100
		}
		s.NetRx += uint64(jitter * 4096)
		s.NetTx += uint64(jitter * 8192)
		s.Timestamp = time.Now()
	}
	return s, nil
}

func (e *Engine) ContainerLogs(ctx context.Context, id string, tail int, timestamps bool) (io.ReadCloser, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.lookupLocked(id)
	if err != nil {
		return nil, false, err
	}
	return c.logs.stream(tail, timestamps), true, nil
}

func (e *Engine) ContainerTerminal(ctx context.Context, id string, cols, rows uint) (model.Terminal, func(), error) {
	e.mu.Lock()
	c, err := e.lookupLocked(id)
	e.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	if c.detail.State != "running" {
		return nil, nil, fmt.Errorf("container %s is not running", c.detail.Name)
	}
	term := newDemoTerminal(c.detail.Name)
	return term, func() { term.Close() }, nil
}

func (e *Engine) ListImages(ctx context.Context) ([]model.Image, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]model.Image, 0, len(e.images))
	for _, img := range e.images {
		out = append(out, *img)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}

func (e *Engine) PullImage(ctx context.Context, ref string, progress io.Writer) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	steps := []string{
		`{"status":"Pulling from library/` + ref + `"}`,
		`{"status":"Pulling fs layer"}`,
		`{"status":"Downloading","progressDetail":{"current":1048576,"total":4194304}}`,
		`{"status":"Download complete"}`,
		`{"status":"Extracting"}`,
		`{"status":"Pull complete"}`,
		`{"status":"Status: Downloaded newer image for ` + ref + `"}`,
	}
	for _, s := range steps {
		if _, err := io.WriteString(progress, s+"\n"); err != nil {
			return err
		}
		time.Sleep(120 * time.Millisecond)
	}
	img := &model.Image{ID: randID(), RepoTags: []string{ref}, Created: time.Now().Unix(), Size: 128 * 1024 * 1024}
	e.images[img.ID] = img
	return nil
}

func (e *Engine) RemoveImage(ctx context.Context, id string, force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for imgID, img := range e.images {
		if imgID == id {
			delete(e.images, imgID)
			return nil
		}
		for _, t := range img.RepoTags {
			if t == id {
				delete(e.images, imgID)
				return nil
			}
		}
	}
	return fmt.Errorf("image %s not found", id)
}

func (e *Engine) ListNetworks(ctx context.Context) ([]model.Network, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]model.Network, 0, len(e.networks))
	for _, n := range e.networks {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (e *Engine) CreateNetwork(ctx context.Context, req model.CreateNetworkRequest) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if req.Name == "" {
		return "", fmt.Errorf("network name is required")
	}
	for _, n := range e.networks {
		if n.Name == req.Name {
			return "", fmt.Errorf("network %s already exists", req.Name)
		}
	}
	id := randID()
	driver := req.Driver
	if driver == "" {
		driver = "bridge"
	}
	e.networks[id] = &model.Network{
		ID:       id,
		Name:     req.Name,
		Driver:   driver,
		Scope:    "local",
		Internal: req.Internal,
		Created:  time.Now().Format(time.RFC3339),
	}
	return id, nil
}

func (e *Engine) RemoveNetwork(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.networks[id]; ok {
		delete(e.networks, id)
		return nil
	}
	for nid, n := range e.networks {
		if n.Name == id {
			delete(e.networks, nid)
			return nil
		}
	}
	return fmt.Errorf("network %s not found", id)
}

func (e *Engine) ListVolumes(ctx context.Context) ([]model.Volume, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]model.Volume, 0, len(e.volumes))
	for _, v := range e.volumes {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (e *Engine) CreateVolume(ctx context.Context, name, driver string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if name == "" {
		return "", fmt.Errorf("volume name is required")
	}
	if _, ok := e.volumes[name]; ok {
		return "", fmt.Errorf("volume %s already exists", name)
	}
	if driver == "" {
		driver = "local"
	}
	e.volumes[name] = &model.Volume{
		Name:       name,
		Driver:     driver,
		Mountpoint: "/var/lib/docker/volumes/" + name + "/_data",
		Scope:      "local",
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	return name, nil
}

func (e *Engine) RemoveVolume(ctx context.Context, name string, force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.volumes[name]
	if !ok {
		return fmt.Errorf("volume %s not found", name)
	}
	if v.InUse && !force {
		return fmt.Errorf("volume %s is in use", name)
	}
	delete(e.volumes, name)
	return nil
}

func (e *Engine) ComposeUp(ctx context.Context, file model.ComposeFile) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	project := normalizeProject(file.Name)
	networkNames := []string{"default"}
	networkNames = append(networkNames, file.Networks...)
	for _, n := range networkNames {
		name := project + "_" + n
		found := false
		for _, net := range e.networks {
			if net.Name == name {
				found = true
				break
			}
		}
		if !found {
			id := randID()
			e.networks[id] = &model.Network{ID: id, Name: name, Driver: "bridge", Scope: "local", Created: time.Now().Format(time.RFC3339)}
		}
	}
	for _, v := range file.Volumes {
		name := project + "_" + v
		if _, ok := e.volumes[name]; !ok {
			e.volumes[name] = &model.Volume{Name: name, Driver: "local", Mountpoint: "/var/lib/docker/volumes/" + name + "/_data", Scope: "local", CreatedAt: time.Now().Format(time.RFC3339)}
		}
	}
	for _, svc := range file.Services {
		name := svc.ContainerName
		if name == "" {
			name = project + "-" + svc.Name + "-1"
		}
		for _, c := range e.containers {
			if c.detail.Name == name {
				delete(e.containers, c.detail.ID)
			}
		}
		id := randID()
		now := time.Now()
		nets := svc.Networks
		if len(nets) == 0 {
			nets = []string{project + "_default"}
		}
		detail := model.ContainerDetail{
			Container: model.Container{
				ID:       id,
				Name:     name,
				Image:    svc.Image,
				Created:  now.Unix(),
				State:    "running",
				Status:   "Up 1 second",
				Command:  svc.Command,
				Ports:    demoPorts(svc.Ports),
				Networks: nets,
				Mounts:   parseDemoMounts(svc.Volumes),
				Project:  project,
				Service:  svc.Name,
				Labels: map[string]string{
					"com.docker.compose.project": project,
					"com.docker.compose.service": svc.Name,
				},
			},
			Env:       svc.Environment,
			Cmd:       parseDemoArgs(svc.Command),
			Hostname:  name,
			Pid:       3000 + len(e.containers),
			CreatedAt: now.Format(time.RFC3339),
			StartedAt: now.Format(time.RFC3339),
		}
		cs := &containerState{detail: detail, created: now, logs: newLogBuffer(500)}
		cs.seedLogs(name, "running")
		cs.stats = syntheticStats("running")
		e.containers[id] = cs
		e.order = append(e.order, id)
	}
	return nil
}

func demoPorts(specs []string) []model.Port {
	out := make([]model.Port, 0, len(specs))
	for _, s := range specs {
		if s != "" {
			out = append(out, parseDemoPort(s))
		}
	}
	return out
}

func (e *Engine) ComposeDown(ctx context.Context, project string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	project = normalizeProject(project)
	keep := e.order[:0]
	for _, id := range e.order {
		c := e.containers[id]
		if c != nil && c.detail.Project == project {
			delete(e.containers, id)
			continue
		}
		keep = append(keep, id)
	}
	e.order = keep
	for nid, n := range e.networks {
		if strings.HasPrefix(n.Name, project+"_") {
			delete(e.networks, nid)
		}
	}
	return nil
}

func (e *Engine) ComposeStop(ctx context.Context, project string) error {
	return e.bulkProject(project, "exited", "Exited (0) 1 second ago")
}

func (e *Engine) ComposeStart(ctx context.Context, project string) error {
	return e.bulkProject(project, "running", "Up 1 second")
}

func (e *Engine) ComposeRestart(ctx context.Context, project string) error {
	if err := e.ComposeStop(ctx, project); err != nil {
		return err
	}
	return e.ComposeStart(ctx, project)
}

func (e *Engine) bulkProject(project, state, status string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	project = normalizeProject(project)
	for _, id := range e.order {
		c := e.containers[id]
		if c != nil && c.detail.Project == project {
			_ = e.setState(id, state, status)
		}
	}
	return nil
}

func (e *Engine) ComposeProjects(ctx context.Context) ([]model.ComposeProject, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	type agg struct {
		services map[string]bool
		total    int
		running  int
	}
	projects := map[string]*agg{}
	for _, c := range e.containers {
		if c.detail.Project == "" {
			continue
		}
		a, ok := projects[c.detail.Project]
		if !ok {
			a = &agg{services: map[string]bool{}}
			projects[c.detail.Project] = a
		}
		a.total++
		if c.detail.State == "running" {
			a.running++
		}
		a.services[c.detail.Service] = true
	}
	out := make([]model.ComposeProject, 0, len(projects))
	for name, a := range projects {
		status := "stopped"
		if a.running == a.total && a.total > 0 {
			status = "running"
		} else if a.running > 0 {
			status = "partial"
		}
		out = append(out, model.ComposeProject{
			Name:       name,
			File:       name + "/docker-compose.yml",
			Services:   len(a.services),
			Containers: a.total,
			Running:    a.running,
			Status:     status,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func normalizeProject(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '.' {
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_-")
	if out == "" {
		out = "compose"
	}
	return out
}
