BINARY := dockerview
PKG := ./...
GO ?= go

.PHONY: all build run dev test test-integration vet fmt clean docker-build docker-run help

all: build

build:
	$(GO) build -trimpath -ldflags "-s -w" -o $(BINARY) .

run: build
	./$(BINARY)

dev:
	DOCKERVIEW_DEMO=1 $(GO) run .

test:
	$(GO) test $(PKG) -timeout 120s

test-integration:
	DOCKERVIEW_INTEGRATION=1 $(GO) test ./internal/engine/docker/ -timeout 300s -v

vet:
	$(GO) vet $(PKG)

fmt:
	gofmt -w $(shell find . -name '*.go' -not -path './dist/*')

docker-build:
	docker build -t dockerview:latest .

docker-run:
	docker run --rm -p 8080:8080 \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(PWD)/data/compose:/app/data/compose \
		dockerview:latest

clean:
	$(GO) clean
	rm -f $(BINARY)

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
