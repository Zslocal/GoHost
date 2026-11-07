package compose

import (
	"strings"
	"testing"

	"dockerview/internal/model"
)

func TestParseFull(t *testing.T) {
	content := `
name: shop
services:
  web:
    image: nginx:alpine
    container_name: shop-web
    command: nginx -g "daemon off;"
    ports:
      - "8080:80"
      - "127.0.0.1:5432:5432/udp"
    volumes:
      - ./html:/usr/share/nginx/html
      - dbdata:/data
    environment:
      APP_ENV: production
      DEBUG: "false"
    depends_on:
      - api
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1.5"
          memory: 512M
  api:
    image: redis:7
    environment:
      - REDIS_URL=redis://cache:6379
    networks:
      - backend
    labels:
      com.example.role: api
networks:
  backend:
volumes:
  dbdata:
`
	file, err := Parse([]byte(content))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if file.Name != "shop" {
		t.Errorf("name = %q, want shop", file.Name)
	}
	if len(file.Services) != 2 {
		t.Fatalf("services = %d, want 2", len(file.Services))
	}
	web, ok := serviceByName(file.Services, "web")
	if !ok || web.Image != "nginx:alpine" {
		t.Fatalf("web service missing or wrong: %+v", web)
	}
	if web.ContainerName != "shop-web" {
		t.Errorf("container_name = %q", web.ContainerName)
	}
	if len(web.Ports) != 2 || web.Ports[0] != "8080:80" {
		t.Errorf("ports = %v", web.Ports)
	}
	if len(web.Volumes) != 2 {
		t.Errorf("volumes = %v", web.Volumes)
	}
	if got := findEnv(web.Environment, "APP_ENV"); got != "production" {
		t.Errorf("APP_ENV = %q", got)
	}
	if len(web.DependsOn) != 1 || web.DependsOn[0] != "api" {
		t.Errorf("depends_on = %v", web.DependsOn)
	}
	if web.MemLimitMB != 512 {
		t.Errorf("mem limit = %d, want 512", web.MemLimitMB)
	}
	if web.CPULimit != 1.5 {
		t.Errorf("cpu limit = %v, want 1.5", web.CPULimit)
	}
	api, ok := serviceByName(file.Services, "api")
	if !ok {
		t.Fatalf("api service missing")
	}
	if len(api.Networks) != 1 || api.Networks[0] != "backend" {
		t.Errorf("networks = %v", api.Networks)
	}
	if api.Labels["com.example.role"] != "api" {
		t.Errorf("labels = %v", api.Labels)
	}
	if len(file.Networks) != 1 || file.Networks[0] != "backend" {
		t.Errorf("top networks = %v", file.Networks)
	}
	if len(file.Volumes) != 1 || file.Volumes[0] != "dbdata" {
		t.Errorf("top volumes = %v", file.Volumes)
	}
}

func TestParseInvalid(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"no services":  "version: '3'\nnetworks:\n  default:\n",
		"broken yaml":  "services: [",
		"service null": "services:\n  web:\n",
	}
	for name, content := range cases {
		if _, err := Parse([]byte(content)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestParseCommandForms(t *testing.T) {
	file, err := Parse([]byte(`
services:
  a:
    image: x
    command: ["sh", "-c", "echo hi"]
  b:
    image: y
    command: echo hello world
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	byName := map[string]string{}
	for _, svc := range file.Services {
		byName[svc.Name] = svc.Command
	}
	if byName["a"] != "sh -c echo hi" {
		t.Errorf("array command = %q", byName["a"])
	}
	if !strings.Contains(byName["b"], "echo hello world") {
		t.Errorf("string command = %q", byName["b"])
	}
}

func TestParseMemoryUnits(t *testing.T) {
	cases := map[string]int64{
		"512M":  512,
		"1G":    1024,
		"2gb":   2048,
		"2048k": 2,
	}
	for input, want := range cases {
		if got := parseMemoryMB(input); got != want {
			t.Errorf("parseMemoryMB(%q) = %d, want %d", input, got, want)
		}
	}
}

func serviceByName(list []model.ComposeService, name string) (model.ComposeService, bool) {
	for _, svc := range list {
		if svc.Name == name {
			return svc, true
		}
	}
	return model.ComposeService{}, false
}

func findEnv(list []string, key string) string {
	for _, e := range list {
		if strings.HasPrefix(e, key+"=") {
			return strings.TrimPrefix(e, key+"=")
		}
	}
	return ""
}
