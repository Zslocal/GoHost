package demo

import (
	"context"
	"strings"
	"testing"

	"dockerview/internal/model"
)

func TestSeedAndList(t *testing.T) {
	e := New()
	ctx := context.Background()
	all, err := e.ListContainers(ctx, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("expected seeded containers")
	}
	running, err := e.ListContainers(ctx, false)
	if err != nil {
		t.Fatalf("list running: %v", err)
	}
	if len(running) >= len(all) {
		t.Errorf("running (%d) should be fewer than all (%d)", len(running), len(all))
	}
}

func TestCreateStartStopRemove(t *testing.T) {
	e := New()
	ctx := context.Background()
	id, err := e.CreateContainer(ctx, model.CreateContainerRequest{
		Name:  "unit-test",
		Image: "nginx:alpine",
		Ports: []string{"8088:80"},
		Env:   []string{"A=B"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	detail, err := e.InspectContainer(ctx, id)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if detail.Name != "unit-test" || detail.State != "running" {
		t.Errorf("unexpected detail: name=%q state=%q", detail.Name, detail.State)
	}
	if err := e.StopContainer(ctx, id); err != nil {
		t.Fatalf("stop: %v", err)
	}
	detail, _ = e.InspectContainer(ctx, id)
	if detail.State != "exited" {
		t.Errorf("state after stop = %q", detail.State)
	}
	if err := e.RestartContainer(ctx, id); err != nil {
		t.Fatalf("restart: %v", err)
	}
	detail, _ = e.InspectContainer(ctx, id)
	if detail.State != "running" {
		t.Errorf("state after restart = %q", detail.State)
	}
	if err := e.RemoveContainer(ctx, id, true, false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := e.InspectContainer(ctx, id); err == nil {
		t.Error("expected inspect to fail after removal")
	}
}

func TestCreateRequiresImage(t *testing.T) {
	e := New()
	if _, err := e.CreateContainer(context.Background(), model.CreateContainerRequest{Name: "x"}); err == nil {
		t.Error("expected error when image is empty")
	}
}

func TestLogsStream(t *testing.T) {
	e := New()
	ctx := context.Background()
	list, _ := e.ListContainers(ctx, true)
	reader, _, err := e.ContainerLogs(ctx, list[0].ID, 5, false)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	defer reader.Close()
	buf := make([]byte, 4096)
	n, err := reader.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("read logs: n=%d err=%v", n, err)
	}
	data := string(buf[:n])
	if !strings.Contains(data, "INFO") && !strings.Contains(data, "WARN") && !strings.Contains(data, "ERROR") {
		t.Errorf("unexpected log content: %q", data)
	}
}

func TestNetworkVolumeLifecycle(t *testing.T) {
	e := New()
	ctx := context.Background()
	id, err := e.CreateNetwork(ctx, model.CreateNetworkRequest{Name: "unit-net", Driver: "bridge"})
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	if _, err := e.CreateNetwork(ctx, model.CreateNetworkRequest{Name: "unit-net"}); err == nil {
		t.Error("expected duplicate network error")
	}
	nets, _ := e.ListNetworks(ctx)
	found := false
	for _, n := range nets {
		if n.ID == id {
			found = true
		}
	}
	if !found {
		t.Error("created network not in list")
	}
	if err := e.RemoveNetwork(ctx, id); err != nil {
		t.Fatalf("remove network: %v", err)
	}

	if _, err := e.CreateVolume(ctx, "unit-vol", "local"); err != nil {
		t.Fatalf("create volume: %v", err)
	}
	if err := e.RemoveVolume(ctx, "unit-vol", false); err != nil {
		t.Fatalf("remove volume: %v", err)
	}
	if err := e.RemoveVolume(ctx, "missing-vol", false); err == nil {
		t.Error("expected error removing missing volume")
	}
}

func TestComposeLifecycle(t *testing.T) {
	e := New()
	ctx := context.Background()
	file := model.ComposeFile{
		Name: "unit-project",
		Services: []model.ComposeService{
			{Name: "web", Image: "nginx:alpine", Ports: []string{"9090:80"}},
			{Name: "db", Image: "postgres:16", DependsOn: []string{"web"}},
		},
		Volumes: []string{"data"},
	}
	if err := e.ComposeUp(ctx, file); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	projects, err := e.ComposeProjects(ctx)
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	var found *model.ComposeProject
	for i := range projects {
		if projects[i].Name == "unit-project" {
			found = &projects[i]
		}
	}
	if found == nil {
		t.Fatal("project not found after up")
	}
	if found.Services != 2 || found.Running != 2 {
		t.Errorf("unexpected project state: %+v", found)
	}
	containers, _ := e.ListContainers(ctx, true)
	names := map[string]bool{}
	for _, c := range containers {
		if c.Project == "unit-project" {
			names[c.Name] = true
		}
	}
	if !names["unit-project-web-1"] || !names["unit-project-db-1"] {
		t.Errorf("compose containers missing: %v", names)
	}
	if err := e.ComposeDown(ctx, "unit-project"); err != nil {
		t.Fatalf("compose down: %v", err)
	}
	projects, _ = e.ComposeProjects(ctx)
	for _, p := range projects {
		if p.Name == "unit-project" {
			t.Error("project still present after down")
		}
	}
}

func TestTerminalIO(t *testing.T) {
	e := New()
	ctx := context.Background()
	list, _ := e.ListContainers(ctx, false)
	term, cleanup, err := e.ContainerTerminal(ctx, list[0].ID, 80, 24)
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	defer cleanup()

	buf := make([]byte, 4096)
	n, err := term.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("read banner: n=%d err=%v", n, err)
	}
	if _, err := term.Write([]byte("echo hello-from-test\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out strings.Builder
	deadline := 20
	for i := 0; i < deadline; i++ {
		n, err := term.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			if strings.Contains(out.String(), "hello-from-test") && strings.Contains(out.String(), "# ") {
				return
			}
		}
		if err != nil {
			break
		}
	}
	t.Fatalf("did not observe echoed output, got %q", out.String())
}

func TestStatsRunningOnly(t *testing.T) {
	e := New()
	ctx := context.Background()
	list, _ := e.ListContainers(ctx, true)
	for _, c := range list {
		if c.State == "exited" {
			s, err := e.ContainerStats(ctx, c.ID)
			if err != nil {
				t.Fatalf("stats: %v", err)
			}
			if s.CPUPercent != 0 {
				t.Errorf("exited container should have 0 cpu, got %v", s.CPUPercent)
			}
			return
		}
	}
}
