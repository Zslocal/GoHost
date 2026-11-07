package docker

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"

	"dockerview/internal/model"
)

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
	out := b.String()
	out = strings.Trim(out, "_-")
	if out == "" {
		out = "compose"
	}
	return out
}

func (e *Engine) ensureNetwork(ctx context.Context, name string) error {
	list, err := e.cli.NetworkList(ctx, types.NetworkListOptions{Filters: filters.NewArgs(filters.Arg("name", name))})
	if err != nil {
		return err
	}
	for _, n := range list {
		if n.Name == name {
			return nil
		}
	}
	_, err = e.cli.NetworkCreate(ctx, name, types.NetworkCreate{
		Driver: "bridge",
		Labels: map[string]string{"com.docker.compose.network": name},
	})
	return err
}

func (e *Engine) ensureVolume(ctx context.Context, name string) error {
	_, err := e.cli.VolumeCreate(ctx, volumeCreateOptions(name))
	if err != nil && !isAlreadyExists(err) {
		return err
	}
	return nil
}

func (e *Engine) ComposeUp(ctx context.Context, file model.ComposeFile) error {
	project := normalizeProject(file.Name)
	if project == "" {
		return fmt.Errorf("compose project name is required")
	}

	networkMode := project + "_default"
	if err := e.ensureNetwork(ctx, networkMode); err != nil {
		return fmt.Errorf("create network %s: %w", networkMode, err)
	}
	for _, n := range file.Networks {
		if n == "default" {
			continue
		}
		if err := e.ensureNetwork(ctx, project+"_"+n); err != nil {
			return fmt.Errorf("create network %s: %w", project+"_"+n, err)
		}
	}

	volumeNames := map[string]string{}
	for _, v := range file.Volumes {
		actual := project + "_" + v
		if err := e.ensureVolume(ctx, actual); err != nil {
			return fmt.Errorf("create volume %s: %w", actual, err)
		}
		volumeNames[v] = actual
	}

	order, err := serviceOrder(file.Services)
	if err != nil {
		return err
	}

	serviceByName := map[string]model.ComposeService{}
	for _, svc := range file.Services {
		serviceByName[svc.Name] = svc
	}

	for _, name := range order {
		svc := serviceByName[name]
		if err := e.composeServiceUp(ctx, project, networkMode, volumeNames, svc); err != nil {
			return fmt.Errorf("service %s: %w", name, err)
		}
	}
	return nil
}

func (e *Engine) composeServiceUp(ctx context.Context, project, networkMode string, volumeNames map[string]string, svc model.ComposeService) error {
	containerName := svc.ContainerName
	if containerName == "" {
		containerName = project + "-" + svc.Name + "-1"
	}

	if existing := e.findProjectContainer(ctx, project, svc.Name); existing != "" {
		if err := e.cli.ContainerRemove(ctx, existing, container.RemoveOptions{Force: true}); err != nil && !isNotFound(err) {
			return fmt.Errorf("remove old container: %w", err)
		}
	}

	if svc.Image == "" {
		return fmt.Errorf("service has no image")
	}
	if err := e.ensureImage(ctx, svc.Image); err != nil {
		return err
	}

	exposed, bindings, err := buildPorts(svc.Ports)
	if err != nil {
		return err
	}

	labels := map[string]string{
		labelProject:                          project,
		labelService:                          svc.Name,
		"com.docker.compose.container-number": "1",
		"com.docker.compose.oneoff":           "False",
		"dockerview.managed":                  "true",
	}
	for k, v := range svc.Labels {
		labels[k] = v
	}

	cfg := &container.Config{
		Image:        svc.Image,
		Env:          svc.Environment,
		ExposedPorts: exposed,
		WorkingDir:   svc.WorkingDir,
		User:         svc.User,
		Hostname:     svc.Hostname,
		Tty:          svc.Tty,
		OpenStdin:    svc.StdInOpen,
		Labels:       labels,
	}
	if strings.TrimSpace(svc.Command) != "" {
		cfg.Cmd = splitArgs(svc.Command)
	}
	if strings.TrimSpace(svc.Entrypoint) != "" {
		cfg.Entrypoint = splitArgs(svc.Entrypoint)
	}

	binds := make([]string, 0, len(svc.Volumes))
	for _, v := range svc.Volumes {
		parts := strings.SplitN(v, ":", 2)
		if len(parts) == 2 {
			if actual, ok := volumeNames[parts[0]]; ok {
				binds = append(binds, actual+":"+parts[1])
				continue
			}
		}
		binds = append(binds, v)
	}

	hostCfg := &container.HostConfig{
		Binds:         binds,
		PortBindings:  bindings,
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyMode(defaultString(svc.Restart, "no"))},
		Privileged:    svc.Privileged,
		NetworkMode:   container.NetworkMode(networkMode),
	}
	if svc.MemLimitMB > 0 {
		hostCfg.Memory = svc.MemLimitMB * 1024 * 1024
	}
	if svc.CPULimit > 0 {
		hostCfg.NanoCPUs = int64(svc.CPULimit * 1e9)
	}

	networking := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{}}
	svcNets := svc.Networks
	if len(svcNets) == 0 {
		networking.EndpointsConfig[networkMode] = &network.EndpointSettings{Aliases: []string{svc.Name}}
	} else {
		for _, n := range svcNets {
			actual := project + "_" + n
			if n == "default" {
				actual = networkMode
			}
			_ = e.ensureNetwork(ctx, actual)
			networking.EndpointsConfig[actual] = &network.EndpointSettings{Aliases: []string{svc.Name}}
		}
	}

	resp, err := e.cli.ContainerCreate(ctx, cfg, hostCfg, networking, nil, containerName)
	if err != nil {
		return err
	}
	if err := e.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

func (e *Engine) findProjectContainer(ctx context.Context, project, service string) string {
	list, err := e.cli.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", labelProject+"="+project),
			filters.Arg("label", labelService+"="+service),
		),
	})
	if err != nil || len(list) == 0 {
		return ""
	}
	return list[0].ID
}

func serviceOrder(services []model.ComposeService) ([]string, error) {
	byName := map[string]model.ComposeService{}
	for _, s := range services {
		byName[s.Name] = s
	}
	visited := map[string]int{}
	var order []string
	var visit func(name string, stack []string) error
	visit = func(name string, stack []string) error {
		svc, ok := byName[name]
		if !ok {
			return fmt.Errorf("unknown dependency %q", name)
		}
		switch visited[name] {
		case 1:
			return fmt.Errorf("circular dependency detected: %s -> %s", strings.Join(stack, " -> "), name)
		case 2:
			return nil
		}
		visited[name] = 1
		for _, dep := range svc.DependsOn {
			if err := visit(dep, append(stack, name)); err != nil {
				return err
			}
		}
		visited[name] = 2
		order = append(order, name)
		return nil
	}
	names := make([]string, 0, len(services))
	for _, s := range services {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func (e *Engine) projectContainers(ctx context.Context, project string) ([]types.Container, error) {
	return e.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", labelProject+"="+project)),
	})
}

func (e *Engine) ComposeStop(ctx context.Context, project string) error {
	list, err := e.projectContainers(ctx, project)
	if err != nil {
		return err
	}
	timeout := 10
	var firstErr error
	for _, c := range list {
		if err := e.cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil && !isNotFound(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (e *Engine) ComposeStart(ctx context.Context, project string) error {
	list, err := e.projectContainers(ctx, project)
	if err != nil {
		return err
	}
	var firstErr error
	for _, c := range list {
		if err := e.cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil && !isNotFound(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (e *Engine) ComposeRestart(ctx context.Context, project string) error {
	if err := e.ComposeStop(ctx, project); err != nil {
		return err
	}
	return e.ComposeStart(ctx, project)
}

func (e *Engine) ComposeDown(ctx context.Context, project string) error {
	list, err := e.projectContainers(ctx, project)
	if err != nil {
		return err
	}
	for _, c := range list {
		if err := e.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil && !isNotFound(err) {
			return err
		}
	}
	nets, err := e.cli.NetworkList(ctx, types.NetworkListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "com.docker.compose.network")),
	})
	if err == nil {
		for _, n := range nets {
			if strings.HasPrefix(n.Name, project+"_") || n.Name == project+"_default" {
				_ = e.cli.NetworkRemove(ctx, n.ID)
			}
		}
	}
	return nil
}

func (e *Engine) ComposeProjects(ctx context.Context) ([]model.ComposeProject, error) {
	list, err := e.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	type agg struct {
		services map[string]bool
		total    int
		running  int
	}
	projects := map[string]*agg{}
	for _, c := range list {
		proj := c.Labels[labelProject]
		if proj == "" {
			continue
		}
		a, ok := projects[proj]
		if !ok {
			a = &agg{services: map[string]bool{}}
			projects[proj] = a
		}
		a.total++
		if c.State == "running" {
			a.running++
		}
		if svc := c.Labels[labelService]; svc != "" {
			a.services[svc] = true
		}
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
