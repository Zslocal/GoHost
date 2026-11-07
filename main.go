package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dockerview/internal/engine"
	demoengine "dockerview/internal/engine/demo"
	dockerengine "dockerview/internal/engine/docker"
	"dockerview/internal/monitor"
	"dockerview/internal/server"
)

func main() {
	addr := flag.String("addr", defaultString(os.Getenv("DOCKERVIEW_ADDR"), ":8080"), "HTTP listen address")
	host := flag.String("docker-host", os.Getenv("DOCKER_HOST"), "Docker daemon address")
	forceDemo := flag.Bool("demo", os.Getenv("DOCKERVIEW_DEMO") == "1", "force demo mode with synthetic Docker data")
	flag.Parse()

	eng, err := buildEngine(*host, *forceDemo)
	if err != nil {
		log.Fatalf("startup failed: %v", err)
	}
	defer eng.Close()

	if eng.Kind() == "demo" {
		log.Printf("Docker daemon unavailable, running in DEMO mode with synthetic containers")
	} else {
		log.Printf("Connected to Docker daemon at %s", defaultString(*host, "DOCKER_HOST/default socket"))
	}

	mon := monitor.New()
	srv := server.New(eng, mon)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("DockerView listening on %s", *addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	srv.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("DockerView stopped")
}

func buildEngine(host string, forceDemo bool) (engine.Engine, error) {
	if forceDemo {
		return demoengine.New(), nil
	}
	eng, err := dockerengine.New(host)
	if err == nil {
		return eng, nil
	}
	log.Printf("Docker connection failed: %v", err)
	if os.Getenv("DOCKERVIEW_STRICT") == "1" {
		return nil, err
	}
	return demoengine.New(), nil
}

func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
