package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	demoengine "dockerview/internal/engine/demo"
	"dockerview/internal/model"
	"dockerview/internal/monitor"
)

func newTestServer(t *testing.T) (*httptest.Server, *demoengine.Engine) {
	t.Helper()
	t.Setenv("DOCKERVIEW_COMPOSE_DIR", t.TempDir())
	eng := demoengine.New()
	srv := New(eng, monitor.New())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		eng.Close()
	})
	return ts, eng
}

func doJSON(t *testing.T, method, url string, body string, out any) int {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if out != nil && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestHealthAndMeta(t *testing.T) {
	ts, _ := newTestServer(t)
	var health map[string]any
	if code := doJSON(t, "GET", ts.URL+"/api/health", "", &health); code != 200 {
		t.Fatalf("health status = %d", code)
	}
	if health["status"] != "ok" {
		t.Errorf("health body = %v", health)
	}
	var meta map[string]string
	if code := doJSON(t, "GET", ts.URL+"/api/meta", "", &meta); code != 200 {
		t.Fatalf("meta status = %d", code)
	}
	if meta["engine"] != "demo" {
		t.Errorf("engine = %q", meta["engine"])
	}
}

func TestSystemEndpoint(t *testing.T) {
	ts, _ := newTestServer(t)
	var body struct {
		Host   model.HostStats `json:"host"`
		Docker model.Info      `json:"docker"`
		Engine string          `json:"engine"`
	}
	if code := doJSON(t, "GET", ts.URL+"/api/system", "", &body); code != 200 {
		t.Fatalf("system status = %d", code)
	}
	if body.Host.MemTotal == 0 {
		t.Error("host mem total should be non-zero")
	}
	if body.Host.CPUCores == 0 {
		t.Error("expected cpu core count")
	}
	if body.Docker.Kind != "demo" {
		t.Errorf("docker kind = %q", body.Docker.Kind)
	}
}

func TestContainerCRUDFlow(t *testing.T) {
	ts, _ := newTestServer(t)

	var created struct {
		ID string `json:"id"`
	}
	payload := `{"name":"http-test","image":"busybox","command":"sleep 60","ports":["8123:80"],"env":["K=V"]}`
	if code := doJSON(t, "POST", ts.URL+"/api/containers", payload, &created); code != 201 {
		t.Fatalf("create status = %d", code)
	}
	if created.ID == "" {
		t.Fatal("no id returned")
	}

	var list []model.Container
	doJSON(t, "GET", ts.URL+"/api/containers", "", &list)
	found := false
	for _, c := range list {
		if c.Name == "http-test" {
			found = true
		}
	}
	if !found {
		t.Error("created container not in list")
	}

	var detail model.ContainerDetail
	if code := doJSON(t, "GET", ts.URL+"/api/containers/"+created.ID, "", &detail); code != 200 {
		t.Fatalf("inspect status = %d", code)
	}
	if detail.Name != "http-test" {
		t.Errorf("detail name = %q", detail.Name)
	}

	if code := doJSON(t, "POST", ts.URL+"/api/containers/"+created.ID+"/stop", "", nil); code != 200 {
		t.Fatalf("stop status = %d", code)
	}
	doJSON(t, "GET", ts.URL+"/api/containers/"+created.ID, "", &detail)
	if detail.State != "exited" {
		t.Errorf("state after stop = %q", detail.State)
	}

	if code := doJSON(t, "DELETE", ts.URL+"/api/containers/"+created.ID+"?force=true", "", nil); code != 200 {
		t.Fatalf("delete status = %d", code)
	}
	if code := doJSON(t, "GET", ts.URL+"/api/containers/"+created.ID, "", nil); code != 404 {
		t.Errorf("expected 404 after delete, got %d", code)
	}
}

func TestContainerInvalidRequests(t *testing.T) {
	ts, _ := newTestServer(t)
	if code := doJSON(t, "POST", ts.URL+"/api/containers", "{bad", nil); code != 400 {
		t.Errorf("invalid json status = %d, want 400", code)
	}
	if code := doJSON(t, "POST", ts.URL+"/api/containers", `{"name":"x"}`, nil); code != 500 {
		t.Errorf("missing image status = %d", code)
	}
	if code := doJSON(t, "POST", ts.URL+"/api/containers/nope/start", "", nil); code != 500 {
		t.Errorf("missing container status = %d", code)
	}
	if code := doJSON(t, "POST", ts.URL+"/api/containers/nope/frobnicate", "", nil); code != 400 {
		t.Errorf("unknown action status = %d", code)
	}
}

func TestNetworkAndVolumeEndpoints(t *testing.T) {
	ts, _ := newTestServer(t)
	var created struct {
		ID string `json:"id"`
	}
	if code := doJSON(t, "POST", ts.URL+"/api/networks", `{"name":"api-net","driver":"bridge"}`, &created); code != 201 {
		t.Fatalf("create network status = %d", code)
	}
	var nets []model.Network
	doJSON(t, "GET", ts.URL+"/api/networks", "", &nets)
	if code := doJSON(t, "DELETE", ts.URL+"/api/networks/"+created.ID, "", nil); code != 200 {
		t.Fatalf("delete network status = %d", code)
	}

	var vol struct {
		Name string `json:"name"`
	}
	if code := doJSON(t, "POST", ts.URL+"/api/volumes", `{"name":"api-vol"}`, &vol); code != 201 {
		t.Fatalf("create volume status = %d", code)
	}
	var vols []model.Volume
	doJSON(t, "GET", ts.URL+"/api/volumes", "", &vols)
	if code := doJSON(t, "DELETE", ts.URL+"/api/volumes/api-vol?force=true", "", nil); code != 200 {
		t.Fatalf("delete volume status = %d", code)
	}
}

func TestComposeEndpoints(t *testing.T) {
	ts, _ := newTestServer(t)
	compose := `{"name":"api-project","content":"services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"8080:80\"\n  db:\n    image: postgres:16\n    depends_on:\n      - web\n"}`
	var deploy struct {
		Project  string `json:"project"`
		Services int    `json:"services"`
	}
	if code := doJSON(t, "POST", ts.URL+"/api/compose/up", compose, &deploy); code != 200 {
		t.Fatalf("compose up status = %d", code)
	}
	if deploy.Project != "api-project" || deploy.Services != 2 {
		t.Errorf("unexpected deploy response: %+v", deploy)
	}

	var projects struct {
		Projects []model.ComposeProject `json:"projects"`
		Files    []composeFileInfo      `json:"files"`
	}
	doJSON(t, "GET", ts.URL+"/api/compose/projects", "", &projects)
	foundProject := false
	for _, p := range projects.Projects {
		if p.Name == "api-project" {
			foundProject = true
			if p.Services != 2 || p.Running != 2 {
				t.Errorf("unexpected project state: %+v", p)
			}
		}
	}
	if !foundProject {
		t.Errorf("api-project not found in %+v", projects.Projects)
	}
	if len(projects.Files) == 0 {
		t.Error("expected saved compose file")
	}

	if code := doJSON(t, "POST", ts.URL+"/api/compose/api-project/stop", "", nil); code != 200 {
		t.Errorf("compose stop status = %d", code)
	}
	if code := doJSON(t, "POST", ts.URL+"/api/compose/api-project/down", "", nil); code != 200 {
		t.Errorf("compose down status = %d", code)
	}

	if code := doJSON(t, "POST", ts.URL+"/api/compose/up", `{"content":"services: ["}`, nil); code != 400 {
		t.Errorf("invalid compose status = %d, want 400", code)
	}
}

func TestStaticAssets(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("root status = %d", resp.StatusCode)
	}
	body := make([]byte, 512)
	_, _ = resp.Body.Read(body)
	if !strings.Contains(string(body), "<!doctype html") {
		t.Errorf("unexpected index content: %q", string(body))
	}

	for _, path := range []string{"/app.js", "/app.css", "/vendor/xterm.js"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("%s status = %d", path, resp.StatusCode)
		}
	}
}

// 批量占用接口只应返回运行中的容器，且每条都要是能直接展示的完整数据。
func TestContainerUsageEndpoint(t *testing.T) {
	ts, eng := newTestServer(t)

	var resp struct {
		Usage     map[string]model.ContainerStats `json:"usage"`
		Collected int                             `json:"collected"`
		Requested int                             `json:"requested"`
	}
	if code := doJSON(t, "GET", ts.URL+"/api/containers/usage", "", &resp); code != 200 {
		t.Fatalf("usage status = %d", code)
	}
	if resp.Requested == 0 {
		t.Fatal("demo engine should have running containers")
	}
	if resp.Collected != resp.Requested {
		t.Errorf("collected %d != requested %d", resp.Collected, resp.Requested)
	}
	if len(resp.Usage) != resp.Collected {
		t.Errorf("usage map has %d entries, collected = %d", len(resp.Usage), resp.Collected)
	}

	running := map[string]bool{}
	list, err := eng.ListContainers(t.Context(), false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, c := range list {
		if c.State == "running" {
			running[c.ID] = true
		}
	}
	for id, u := range resp.Usage {
		if !running[id] {
			t.Errorf("usage contains non-running container %s", id)
		}
		if u.MemLimit == 0 || u.MemUsage == 0 {
			t.Errorf("container %s has empty memory stats: %+v", id, u)
		}
		if u.MemPercent <= 0 || u.MemPercent > 100 {
			t.Errorf("container %s memPercent = %v", id, u.MemPercent)
		}
	}
}
