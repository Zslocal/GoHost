BINARY := dockerview
PKG := ./...
GO ?= go
DIST := dist
# 统一构建参数：-trimpath 去掉本机绝对路径，-s -w 剥离符号表与调试信息
BUILD_FLAGS := -trimpath -ldflags "-s -w"
# macOS 没有 sha256sum，退回 shasum -a 256
SHA256 := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo "shasum -a 256")

.PHONY: all build run dev test test-integration vet fmt dist dist-clean clean docker-build docker-run help

all: build

build:
	$(GO) build $(BUILD_FLAGS) -o $(BINARY) .

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

# 三平台产物：CGO_ENABLED=0 才能交叉编译，也才能保证产物不依赖 glibc / musl
dist:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(BUILD_FLAGS) -o $(DIST)/$(BINARY)-windows-amd64.exe .
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 $(GO) build $(BUILD_FLAGS) -o $(DIST)/$(BINARY)-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 $(GO) build $(BUILD_FLAGS) -o $(DIST)/$(BINARY)-linux-arm64 .
	cd $(DIST) && $(SHA256) $(BINARY)-windows-amd64.exe $(BINARY)-linux-amd64 $(BINARY)-linux-arm64 > SHA256SUMS
	@ls -lh $(DIST)/$(BINARY)-*

# 只删构建产物，保留 dist/data（运行期生成的数据目录）
dist-clean:
	rm -f $(DIST)/$(BINARY)-* $(DIST)/SHA256SUMS

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
