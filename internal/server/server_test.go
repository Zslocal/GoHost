package server

import (
	"encoding/json"
	"io"
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

func TestTopContainersEndpoint(t *testing.T) {
	ts, _ := newTestServer(t)

	var body struct {
		CPU    []model.ContainerMetric `json:"cpu"`
		Memory []model.ContainerMetric `json:"memory"`
	}
	if code := doJSON(t, "GET", ts.URL+"/api/containers/top", "", &body); code != 200 {
		t.Fatalf("top status = %d", code)
	}
	if len(body.CPU) == 0 {
		t.Fatal("expected running containers in cpu ranking")
	}
	if len(body.CPU) > defaultTopLimit {
		t.Errorf("cpu ranking length = %d, want <= %d", len(body.CPU), defaultTopLimit)
	}
	if len(body.CPU) != len(body.Memory) {
		t.Errorf("cpu/memory ranking length mismatch: %d vs %d", len(body.CPU), len(body.Memory))
	}
	for i := 1; i < len(body.CPU); i++ {
		if body.CPU[i-1].CPUPercent < body.CPU[i].CPUPercent {
			t.Errorf("cpu ranking not descending at %d: %v < %v", i, body.CPU[i-1].CPUPercent, body.CPU[i].CPUPercent)
		}
	}
	for i := 1; i < len(body.Memory); i++ {
		if body.Memory[i-1].MemUsage < body.Memory[i].MemUsage {
			t.Errorf("memory ranking not descending at %d", i)
		}
	}
	for _, m := range body.Memory {
		if m.Pid <= 0 {
			t.Errorf("memory ranking entry %q missing pid", m.Name)
		}
		if m.Command == "" {
			t.Errorf("memory ranking entry %q missing command", m.Name)
		}
	}
	for _, m := range body.CPU {
		if m.State != "running" {
			t.Errorf("top ranking should only include running containers, got %q", m.State)
		}
		if m.Name == "" {
			t.Error("ranking entry missing container name")
		}
		if m.Pid <= 0 {
			t.Errorf("ranking entry %q missing pid", m.Name)
		}
		if m.Command == "" {
			t.Errorf("ranking entry %q missing command", m.Name)
		}
	}

	var limited struct {
		CPU    []model.ContainerMetric `json:"cpu"`
		Memory []model.ContainerMetric `json:"memory"`
	}
	if code := doJSON(t, "GET", ts.URL+"/api/containers/top?limit=2", "", &limited); code != 200 {
		t.Fatalf("top limit status = %d", code)
	}
	if len(limited.CPU) > 2 {
		t.Errorf("limit=2 cpu length = %d", len(limited.CPU))
	}
}

func TestTopContainersClampsAndEmpty(t *testing.T) {
	ts, _ := newTestServer(t)

	raw, err := http.Get(ts.URL + "/api/containers/top?limit=999")
	if err != nil {
		t.Fatalf("top max limit: %v", err)
	}
	defer raw.Body.Close()
	var body struct {
		CPU    []model.ContainerMetric `json:"cpu"`
		Memory []model.ContainerMetric `json:"memory"`
	}
	if err := json.NewDecoder(raw.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.CPU) > maxTopLimit {
		t.Errorf("limit should clamp to %d, got %d", maxTopLimit, len(body.CPU))
	}

	emptyTS, _ := newTestServer(t)
	var containers []model.Container
	doJSON(t, "GET", emptyTS.URL+"/api/containers", "", &containers)
	for _, c := range containers {
		if c.State == "running" {
			if code := doJSON(t, "POST", emptyTS.URL+"/api/containers/"+c.ID+"/stop", "", nil); code != 200 {
				t.Fatalf("stop %s status = %d", c.Name, code)
			}
		}
	}

	raw2, err := http.Get(emptyTS.URL + "/api/containers/top")
	if err != nil {
		t.Fatalf("top empty: %v", err)
	}
	defer raw2.Body.Close()
	payload, _ := io.ReadAll(raw2.Body)
	if !strings.Contains(string(payload), `"cpu":[]`) {
		t.Errorf("expected cpu to serialize as [], got %s", string(payload))
	}
	if !strings.Contains(string(payload), `"memory":[]`) {
		t.Errorf("expected memory to serialize as [], got %s", string(payload))
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
