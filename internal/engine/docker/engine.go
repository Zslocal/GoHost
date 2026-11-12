package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"

	"dockerview/internal/model"
)

const labelProject = "com.docker.compose.project"
const labelService = "com.docker.compose.service"

type Engine struct {
	cli *client.Client
}

func New(host string) (*Engine, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	} else {
		opts = append(opts, client.FromEnv)
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		return nil, fmt.Errorf("cannot reach docker daemon: %w", err)
	}
	return &Engine{cli: cli}, nil
}

func (e *Engine) Kind() string { return "docker" }

func (e *Engine) Close() error { return e.cli.Close() }

func (e *Engine) Info(ctx context.Context) (model.Info, error) {
	sysInfo, err := e.cli.Info(ctx)
	if err != nil {
		return model.Info{}, err
	}
	ver, err := e.cli.ServerVersion(ctx)
	info := model.Info{
		Kind:              "docker",
		Name:              sysInfo.Name,
		ServerVersion:     sysInfo.ServerVersion,
		OS:                sysInfo.OperatingSystem,
		Arch:              sysInfo.Architecture,
		Driver:            sysInfo.Driver,
		CgroupVersion:     sysInfo.CgroupVersion,
		KernelVersion:     sysInfo.KernelVersion,
		StorageDriver:     sysInfo.Driver,
		RootDir:           sysInfo.DockerRootDir,
		Containers:        sysInfo.Containers,
		ContainersRunning: sysInfo.ContainersRunning,
		ContainersPaused:  sysInfo.ContainersPaused,
		ContainersStopped: sysInfo.ContainersStopped,
		Images:            sysInfo.Images,
		NCPU:              sysInfo.NCPU,
		MemTotal:          sysInfo.MemTotal,
	}
	if err == nil {
		info.APIVersion = ver.APIVersion
	}
	if sysInfo.Warnings != nil {
		info.Warnings = sysInfo.Warnings
	}
	return info, nil
}

func mapContainer(c types.Container) model.Container {
	name := ""
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}
	ports := make([]model.Port, 0, len(c.Ports))
	for _, p := range c.Ports {
		ports = append(ports, model.Port{
			IP:          p.IP,
			PrivatePort: p.PrivatePort,
			PublicPort:  p.PublicPort,
			Type:        p.Type,
		})
	}
	networks := []string{}
	if c.NetworkSettings != nil {
		for n := range c.NetworkSettings.Networks {
			networks = append(networks, n)
		}
	}
	mounts := make([]model.Mount, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		mounts = append(mounts, model.Mount{
			Type:        string(m.Type),
			Source:      m.Source,
			Destination: m.Destination,
			Mode:        m.Mode,
			RW:          m.RW,
		})
	}
	result := model.Container{
		ID:       c.ID,
		Name:     name,
		Image:    c.Image,
		ImageID:  c.ImageID,
		Command:  c.Command,
		Created:  c.Created,
		State:    c.State,
		Status:   c.Status,
		Ports:    ports,
		Labels:   c.Labels,
		Networks: networks,
		Mounts:   mounts,
	}
	if c.Labels != nil {
		result.Project = c.Labels[labelProject]
		result.Service = c.Labels[labelService]
	}
	return result
}

func (e *Engine) ListContainers(ctx context.Context, all bool) ([]model.Container, error) {
	list, err := e.cli.ContainerList(ctx, container.ListOptions{All: all})
	if err != nil {
		return nil, err
	}
	out := make([]model.Container, 0, len(list))
	for _, c := range list {
		out = append(out, mapContainer(c))
	}
	return out, nil
}

func (e *Engine) InspectContainer(ctx context.Context, id string) (model.ContainerDetail, error) {
	info, err := e.cli.ContainerInspect(ctx, id)
	if err != nil {
		return model.ContainerDetail{}, err
	}
	detail := model.ContainerDetail{
		Container: model.Container{
			ID:      info.ID,
			Name:    strings.TrimPrefix(info.Name, "/"),
			Image:   info.Image,
			Created: parseUnix(info.Created),
		},
		Tty:          false,
		RestartCount: info.RestartCount,
		Platform:     info.Platform,
	}
	if info.Config != nil {
		detail.Env = info.Config.Env
		detail.Cmd = []string(info.Config.Cmd)
		detail.Entrypoint = []string(info.Config.Entrypoint)
		detail.WorkingDir = info.Config.WorkingDir
		detail.User = info.Config.User
		detail.Hostname = info.Config.Hostname
		detail.Tty = info.Config.Tty
		detail.Image = info.Config.Image
		detail.Labels = info.Config.Labels
		detail.Project = info.Config.Labels[labelProject]
		detail.Service = info.Config.Labels[labelService]
	}
	if info.HostConfig != nil {
		detail.RestartPolicy = string(info.HostConfig.RestartPolicy.Name)
	}
	if info.State != nil {
		detail.State = info.State.Status
		detail.Status = info.State.Status
		detail.Pid = info.State.Pid
		detail.StartedAt = info.State.StartedAt
		detail.FinishedAt = info.State.FinishedAt
		if info.State.Health != nil {
			detail.Status = info.State.Status + " (" + info.State.Health.Status + ")"
		}
	}
	detail.CreatedAt = info.Created
	if info.NetworkSettings != nil {
		for name, ep := range info.NetworkSettings.Networks {
			detail.Networks = append(detail.Networks, name)
			if detail.IPAddress == "" {
				detail.IPAddress = ep.IPAddress
			}
		}
	}
	if info.Mounts != nil {
		for _, m := range info.Mounts {
			detail.Mounts = append(detail.Mounts, model.Mount{
				Type:        string(m.Type),
				Source:      m.Source,
				Destination: m.Destination,
				Mode:        m.Mode,
				RW:          m.RW,
			})
		}
	}
	raw, err := json.Marshal(info)
	if err == nil {
		detail.Raw = raw
	}
	return detail, nil
}

func parseUnix(s string) int64 {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0
	}
	return t.Unix()
}

func (e *Engine) ensureImage(ctx context.Context, ref string) error {
	_, _, err := e.cli.ImageInspectWithRaw(ctx, ref)
	if err == nil {
		return nil
	}
	reader, err := e.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", ref, err)
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

func (e *Engine) CreateContainer(ctx context.Context, req model.CreateContainerRequest) (string, error) {
	if req.Image == "" {
		return "", errors.New("image is required")
	}
	if err := e.ensureImage(ctx, req.Image); err != nil {
		return "", err
	}

	exposed, bindings, err := buildPorts(req.Ports)
	if err != nil {
		return "", err
	}

	config := &container.Config{
		Image:        req.Image,
		Env:          req.Env,
		ExposedPorts: exposed,
		WorkingDir:   req.WorkingDir,
		User:         req.User,
		Hostname:     req.Hostname,
		Tty:          req.Tty,
		OpenStdin:    req.Tty,
		AttachStdin:  req.Tty,
		Labels:       req.Labels,
		Volumes:      map[string]struct{}{},
	}
	if config.Labels == nil {
		config.Labels = map[string]string{}
	}
	config.Labels["dockerview.managed"] = "true"

	if strings.TrimSpace(req.Command) != "" {
		config.Cmd = splitArgs(req.Command)
	}
	if strings.TrimSpace(req.Entrypoint) != "" {
		config.Entrypoint = splitArgs(req.Entrypoint)
	}
	for _, v := range req.Volumes {
		parts := strings.SplitN(v, ":", 2)
		if len(parts) == 2 && !strings.HasPrefix(parts[0], "/") && !strings.HasPrefix(parts[0], ".") {
			config.Volumes[parts[1]] = struct{}{}
		}
	}

	hostConfig := &container.HostConfig{
		Binds:         req.Volumes,
		PortBindings:  bindings,
		AutoRemove:    req.AutoRemove,
		Privileged:    req.Privileged,
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyMode(defaultString(req.RestartPolicy, "no"))},
	}
	if req.MemoryMB > 0 {
		hostConfig.Memory = req.MemoryMB * 1024 * 1024
	}
	if req.CPULimit > 0 {
		hostConfig.NanoCPUs = int64(req.CPULimit * 1e9)
	}

	var networking *network.NetworkingConfig
	if len(req.Networks) > 0 {
		networking = &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{}}
		for _, n := range req.Networks {
			networking.EndpointsConfig[n] = &network.EndpointSettings{Aliases: req.NetworkAliases}
		}
		if hostConfig.NetworkMode == "" {
			hostConfig.NetworkMode = container.NetworkMode(req.Networks[0])
		}
	}

	resp, err := e.cli.ContainerCreate(ctx, config, hostConfig, networking, nil, req.Name)
	if err != nil {
		return "", err
	}
	if err := e.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return resp.ID, fmt.Errorf("container created but failed to start: %w", err)
	}
	return resp.ID, nil
}

func buildPorts(specs []string) (nat.PortSet, nat.PortMap, error) {
	exposed := nat.PortSet{}
	bindings := nat.PortMap{}
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		port, binding, err := parsePortSpec(spec)
		if err != nil {
			return nil, nil, err
		}
		exposed[port] = struct{}{}
		if binding != nil {
			bindings[port] = append(bindings[port], *binding)
		}
	}
	return exposed, bindings, nil
}

func parsePortSpec(spec string) (nat.Port, *nat.PortBinding, error) {
	proto := "tcp"
	if idx := strings.Index(spec, "/"); idx >= 0 {
		proto = spec[idx+1:]
		spec = spec[:idx]
	}
	parts := strings.Split(spec, ":")
	var hostPort, containerPort, hostIP string
	switch len(parts) {
	case 1:
		containerPort = parts[0]
	case 2:
		hostPort, containerPort = parts[0], parts[1]
	case 3:
		hostIP, hostPort, containerPort = parts[0], parts[1], parts[2]
	default:
		return "", nil, fmt.Errorf("invalid port spec %q", spec)
	}
	p, err := nat.NewPort(proto, containerPort)
	if err != nil {
		return "", nil, fmt.Errorf("invalid port spec %q: %w", spec, err)
	}
	if hostPort == "" {
		return p, nil, nil
	}
	return p, &nat.PortBinding{HostIP: hostIP, HostPort: hostPort}, nil
}

func splitArgs(s string) []string {
	args, err := shellSplit(s)
	if err != nil || len(args) == 0 {
		return []string{"sh", "-c", s}
	}
	return args
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func (e *Engine) StartContainer(ctx context.Context, id string) error {
	return e.cli.ContainerStart(ctx, id, container.StartOptions{})
}

func (e *Engine) StopContainer(ctx context.Context, id string) error {
	timeout := 10
	return e.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
}

func (e *Engine) RestartContainer(ctx context.Context, id string) error {
	timeout := 10
	return e.cli.ContainerRestart(ctx, id, container.StopOptions{Timeout: &timeout})
}

func (e *Engine) PauseContainer(ctx context.Context, id string) error {
	return e.cli.ContainerPause(ctx, id)
}

func (e *Engine) UnpauseContainer(ctx context.Context, id string) error {
	return e.cli.ContainerUnpause(ctx, id)
}

func (e *Engine) KillContainer(ctx context.Context, id string, signal string) error {
	return e.cli.ContainerKill(ctx, id, defaultString(signal, "SIGKILL"))
}

func (e *Engine) RemoveContainer(ctx context.Context, id string, force, volumes bool) error {
	return e.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: force, RemoveVolumes: volumes})
}

func (e *Engine) ContainerStats(ctx context.Context, id string) (model.ContainerStats, error) {
	resp, err := e.cli.ContainerStatsOneShot(ctx, id)
	if err != nil {
		return model.ContainerStats{}, err
	}
	defer resp.Body.Close()
	var stats types.StatsJSON
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return model.ContainerStats{}, err
	}
	return calculateStats(stats), nil
}

func calculateStats(stats types.StatsJSON) model.ContainerStats {
	out := model.ContainerStats{Timestamp: stats.Read}
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	online := float64(stats.CPUStats.OnlineCPUs)
	if online == 0 {
		online = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if sysDelta > 0 && cpuDelta > 0 {
		out.CPUPercent = (cpuDelta / sysDelta) * online * 100.0
	}
	out.MemUsage = stats.MemoryStats.Usage
	if v, ok := stats.MemoryStats.Stats["inactive_file"]; ok && v <= out.MemUsage {
		out.MemUsage -= v
	}
	out.MemLimit = stats.MemoryStats.Limit
	if out.MemLimit > 0 {
		out.MemPercent = float64(out.MemUsage) / float64(out.MemLimit) * 100.0
	}
	for _, n := range stats.Networks {
		out.NetRx += n.RxBytes
		out.NetTx += n.TxBytes
	}
	for _, b := range stats.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(b.Op) {
		case "read":
			out.BlockRead += b.Value
		case "write":
			out.BlockWrite += b.Value
		}
	}
	out.Pids = stats.PidsStats.Current
	return out
}

func (e *Engine) ContainerLogs(ctx context.Context, id string, tail int, timestamps bool) (io.ReadCloser, bool, error) {
	info, err := e.cli.ContainerInspect(ctx, id)
	if err != nil {
		return nil, false, err
	}
	tty := info.Config != nil && info.Config.Tty
	reader, err := e.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       fmt.Sprintf("%d", tail),
		Timestamps: timestamps,
	})
	if err != nil {
		return nil, false, err
	}
	return reader, tty, nil
}

func (e *Engine) ContainerTerminal(ctx context.Context, id string, cols, rows uint) (model.Terminal, func(), error) {
	shell := resolveShell(ctx, e.cli, id)
	execResp, err := e.cli.ContainerExecCreate(ctx, id, types.ExecConfig{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          shell,
	})
	if err != nil {
		return nil, nil, err
	}
	attach, err := e.cli.ContainerExecAttach(ctx, execResp.ID, types.ExecStartCheck{Tty: true})
	if err != nil {
		return nil, nil, err
	}
	if cols > 0 && rows > 0 {
		_ = e.cli.ContainerExecResize(context.Background(), execResp.ID, container.ResizeOptions{Width: cols, Height: rows})
	}
	term := &dockerTerminal{cli: e.cli, execID: execResp.ID, attach: attach}
	cleanup := func() { attach.Close() }
	return term, cleanup, nil
}

func resolveShell(ctx context.Context, cli *client.Client, id string) []string {
	candidates := [][]string{
		{"bash"},
		{"sh"},
		{"ash"},
	}
	for _, c := range candidates {
		resp, err := cli.ContainerExecCreate(ctx, id, types.ExecConfig{Cmd: []string{"which", c[0]}})
		if err != nil {
			continue
		}
		attach, err := cli.ContainerExecAttach(ctx, resp.ID, types.ExecStartCheck{})
		if err != nil {
			continue
		}
		out, _ := io.ReadAll(attach.Reader)
		attach.Close()
		if len(strings.TrimSpace(string(out))) > 0 {
			return c
		}
	}
	return []string{"sh"}
}

type dockerTerminal struct {
	cli    *client.Client
	execID string
	attach types.HijackedResponse
}

func (t *dockerTerminal) Read(p []byte) (int, error) {
	return t.attach.Reader.Read(p)
}

func (t *dockerTerminal) Write(p []byte) (int, error) {
	return t.attach.Conn.Write(p)
}

func (t *dockerTerminal) Close() error {
	t.attach.Close()
	return nil
}

func (t *dockerTerminal) Resize(cols, rows uint) error {
	return t.cli.ContainerExecResize(context.Background(), t.execID, container.ResizeOptions{Width: cols, Height: rows})
}

func (e *Engine) ListImages(ctx context.Context) ([]model.Image, error) {
	list, err := e.cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]model.Image, 0, len(list))
	for _, img := range list {
		out = append(out, model.Image{
			ID:          img.ID,
			RepoTags:    img.RepoTags,
			RepoDigests: img.RepoDigests,
			Created:     img.Created,
			Size:        img.Size,
			Containers:  img.Containers,
		})
	}
	return out, nil
}

func (e *Engine) PullImage(ctx context.Context, ref string, progress io.Writer) error {
	reader, err := e.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	_, err = io.Copy(progress, reader)
	return err
}

func (e *Engine) RemoveImage(ctx context.Context, id string, force bool) error {
	_, err := e.cli.ImageRemove(ctx, id, image.RemoveOptions{Force: force, PruneChildren: true})
	return err
}

func (e *Engine) ListNetworks(ctx context.Context) ([]model.Network, error) {
	list, err := e.cli.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return nil, err
	}
	// 网络数量不多（通常个位数），逐个 inspect 才能稳定拿到 IPAM.Config 与接入点地址：
	// /networks 列表接口在某些版本上会省略 Containers，直接列表会让容器列变空。
	resources := make([]types.NetworkResource, len(list))
	for i, n := range list {
		resources[i] = n
		if inspected, err := e.cli.NetworkInspect(ctx, n.ID, types.NetworkInspectOptions{}); err == nil {
			resources[i] = inspected
		}
	}

	out := make([]model.Network, 0, len(list))
	for i, n := range list {
		res := resources[i]

		subnet, gateway, ipRange := "", "", ""
		subnetV6, gatewayV6 := "", ""
		for _, cfg := range res.IPAM.Config {
			switch {
			case strings.Contains(cfg.Subnet, ":"):
				if subnetV6 == "" {
					subnetV6, gatewayV6 = cfg.Subnet, cfg.Gateway
				}
			default:
				if subnet == "" && cfg.Subnet != "" {
					subnet, gateway, ipRange = cfg.Subnet, cfg.Gateway, cfg.IPRange
				}
			}
		}

		containers := make([]model.NetworkEndpoint, 0, len(res.Containers))
		for cid, ep := range res.Containers {
			containers = append(containers, model.NetworkEndpoint{
				Name: ep.Name,
				ID:   shortID(cid),
				IPv4: cidrHost(ep.IPv4Address),
				IPv6: cidrHost(ep.IPv6Address),
				MAC:  ep.MacAddress,
			})
		}
		sort.Slice(containers, func(a, b int) bool { return containers[a].Name < containers[b].Name })

		out = append(out, model.Network{
			ID:         n.ID,
			Name:       n.Name,
			Driver:     n.Driver,
			Scope:      n.Scope,
			Internal:   n.Internal,
			Attachable: n.Attachable,
			Subnet:     subnet,
			Gateway:    gateway,
			IPRange:    ipRange,
			SubnetV6:   subnetV6,
			GatewayV6:  gatewayV6,
			Containers: containers,
			Created:    n.Created.Format(time.RFC3339),
		})
	}
	return out, nil
}

// cidrHost 把 "172.18.0.2/16" 这种带前缀长度的地址裁成纯 IP。
func cidrHost(addr string) string {
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		return addr[:i]
	}
	return addr
}

func (e *Engine) CreateNetwork(ctx context.Context, req model.CreateNetworkRequest) (string, error) {
	resp, err := e.cli.NetworkCreate(ctx, req.Name, types.NetworkCreate{
		Driver:   defaultString(req.Driver, "bridge"),
		Internal: req.Internal,
		Labels:   map[string]string{"dockerview.managed": "true"},
	})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (e *Engine) RemoveNetwork(ctx context.Context, id string) error {
	return e.cli.NetworkRemove(ctx, id)
}

func (e *Engine) ListVolumes(ctx context.Context) ([]model.Volume, error) {
	resp, err := e.cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}
	inUse, _ := e.volumeUsage(ctx)
	out := make([]model.Volume, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		out = append(out, model.Volume{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Scope:      v.Scope,
			CreatedAt:  v.CreatedAt,
			InUse:      inUse[v.Name],
		})
	}
	return out, nil
}

func (e *Engine) volumeUsage(ctx context.Context) (map[string]bool, error) {
	used := map[string]bool{}
	list, err := e.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return used, err
	}
	for _, c := range list {
		for _, m := range c.Mounts {
			if m.Type == "volume" && m.Name != "" {
				used[m.Name] = true
			}
		}
	}
	return used, nil
}

func (e *Engine) CreateVolume(ctx context.Context, name, driver string) (string, error) {
	v, err := e.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   name,
		Driver: defaultString(driver, "local"),
		Labels: map[string]string{"dockerview.managed": "true"},
	})
	if err != nil {
		return "", err
	}
	return v.Name, nil
}

func (e *Engine) RemoveVolume(ctx context.Context, name string, force bool) error {
	return e.cli.VolumeRemove(ctx, name, force)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func isNotFound(err error) bool {
	return errdefs.IsNotFound(err)
}
