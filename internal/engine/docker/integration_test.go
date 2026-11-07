package docker

import (
	"context"
	"os"
	"testing"
	"time"

	"dockerview/internal/model"
)

func integrationEngine(t *testing.T) *Engine {
	t.Helper()
	if os.Getenv("DOCKERVIEW_INTEGRATION") != "1" {
		t.Skip("set DOCKERVIEW_INTEGRATION=1 to run Docker integration tests")
	}
	eng, err := New("")
	if err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	t.Cleanup(func() { eng.Close() })
	return eng
}

func TestIntegrationInfo(t *testing.T) {
	eng := integrationEngine(t)
	info, err := eng.Info(context.Background())
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.ServerVersion == "" {
		t.Error("expected server version")
	}
	if info.Kind != "docker" {
		t.Errorf("kind = %q", info.Kind)
	}
}

func TestIntegrationContainerLifecycle(t *testing.T) {
	eng := integrationEngine(t)
	ctx := context.Background()
	image := testImage(t)

	id, err := eng.CreateContainer(ctx, model.CreateContainerRequest{
		Name:    "dockerview-it-" + time.Now().Format("150405"),
		Image:   image,
		Command: "sleep 120",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_ = eng.RemoveContainer(context.Background(), id, true, false)
	})

	detail, err := eng.InspectContainer(ctx, id)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if detail.State != "running" {
		t.Errorf("state = %q", detail.State)
	}

	if _, err := eng.ContainerStats(ctx, id); err != nil {
		t.Errorf("stats: %v", err)
	}

	if err := eng.StopContainer(ctx, id); err != nil {
		t.Errorf("stop: %v", err)
	}
	if err := eng.StartContainer(ctx, id); err != nil {
		t.Errorf("start: %v", err)
	}
	if err := eng.RestartContainer(ctx, id); err != nil {
		t.Errorf("restart: %v", err)
	}
}

func TestIntegrationNetworkAndVolume(t *testing.T) {
	eng := integrationEngine(t)
	ctx := context.Background()
	suffix := time.Now().Format("150405")

	netID, err := eng.CreateNetwork(ctx, model.CreateNetworkRequest{Name: "dv-it-net-" + suffix})
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	t.Cleanup(func() { _ = eng.RemoveNetwork(context.Background(), netID) })

	if _, err := eng.CreateVolume(ctx, "dv-it-vol-"+suffix, "local"); err != nil {
		t.Fatalf("create volume: %v", err)
	}
	t.Cleanup(func() { _ = eng.RemoveVolume(context.Background(), "dv-it-vol-"+suffix, true) })

	vols, err := eng.ListVolumes(ctx)
	if err != nil {
		t.Fatalf("list volumes: %v", err)
	}
	found := false
	for _, v := range vols {
		if v.Name == "dv-it-vol-"+suffix {
			found = true
		}
	}
	if !found {
		t.Error("created volume not listed")
	}
}

func TestIntegrationCompose(t *testing.T) {
	eng := integrationEngine(t)
	ctx := context.Background()
	image := testImage(t)
	project := "dv-it-compose-" + time.Now().Format("150405")

	file := model.ComposeFile{
		Name: project,
		Services: []model.ComposeService{
			{Name: "a", Image: image, Command: "sleep 120"},
			{Name: "b", Image: image, Command: "sleep 120", DependsOn: []string{"a"}},
		},
		Volumes: []string{"data"},
	}
	if err := eng.ComposeUp(ctx, file); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	t.Cleanup(func() { _ = eng.ComposeDown(context.Background(), project) })

	projects, err := eng.ComposeProjects(ctx)
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	found := false
	for _, p := range projects {
		if p.Name == project {
			found = true
			if p.Running != 2 {
				t.Errorf("running = %d, want 2", p.Running)
			}
		}
	}
	if !found {
		t.Error("compose project not listed")
	}

	if err := eng.ComposeStop(ctx, project); err != nil {
		t.Fatalf("compose stop: %v", err)
	}
	if err := eng.ComposeStart(ctx, project); err != nil {
		t.Fatalf("compose start: %v", err)
	}
	if err := eng.ComposeDown(ctx, project); err != nil {
		t.Fatalf("compose down: %v", err)
	}
}

func testImage(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("DOCKERVIEW_TEST_IMAGE"); v != "" {
		return v
	}
	return "alpine:3.20"
}
