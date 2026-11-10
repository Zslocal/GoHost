# DockerView

一个用 Go 编写的 Docker 可视化管理面板。单二进制、零外部前端依赖，内置仪表盘、容器/镜像/网络/数据卷管理、Compose 编排、实时日志与浏览器终端。

## 特性

- **仪表盘**：CPU / 内存 / 磁盘 / 网络吞吐实时监控，SSE 推送资源曲线
- **进程榜**：CPU 与常驻内存双榜，含 PID、属主、线程数、启动命令（Linux 直读 `/proc`）
- **容器管理**：启停、重启、暂停、Kill、删除、创建；实时状态与端口映射
- **容器详情**：CPU/内存/网络/进程统计，挂载点、环境变量、配置项
- **实时日志**：跟随输出、时间戳开关、tail 行数控制
- **浏览器终端**：基于 xterm.js 的交互式 TTY，支持窗口自适应 resize
- **镜像管理**：列表、搜索、拉取（流式进度）、删除
- **网络与数据卷**：创建、查看关联容器、删除
- **Compose**：YAML 编辑部署、内置模板、项目启停/重启/下线、依赖顺序解析
- **双引擎**：连接真实 Docker daemon；不可用时自动降级为内置 Demo 模式

## 快速开始

### 方式一：直接运行预编译二进制（推荐）

`dist/` 下已提供三个平台的产物，按目标架构选择：

| 文件 | 目标平台 | 适用场景 |
|------|----------|----------|
| `dockerview-windows-amd64.exe` | Windows x86-64 | 本机开发机、运维机 |
| `dockerview-linux-amd64` | Linux x86-64 | 常规 x86 服务器 |
| `dockerview-linux-arm64` | Linux AArch64 | ARM 云主机（Anolis / Ubuntu ARM / 鲲鹏 / 倚天） |

三个产物都以 `CGO_ENABLED=0` 编译，是**全静态链接**的单文件程序：不依赖 glibc、不依赖 musl、没有任何 `.so`，Alpine、Debian、CentOS、Anolis、甚至 `scratch` 镜像里都能直接执行。

**Windows**

```powershell
# 连接本机 Docker Desktop
.\dist\dockerview-windows-amd64.exe

# 没有 Docker 时用演示模式（合成数据）
.\dist\dockerview-windows-amd64.exe -demo

# 换端口 / 只监听本机
.\dist\dockerview-windows-amd64.exe -addr 127.0.0.1:9000
```

**Linux**

```bash
# 1. 先确认架构，别挑错文件
uname -m
# x86_64            → dockerview-linux-amd64
# aarch64 / arm64   → dockerview-linux-arm64

# 2. 从 Windows 拷过去后必须补执行位（scp / 共享目录 / zip 常丢）
chmod +x dockerview-linux-arm64
sudo mv dockerview-linux-arm64 /usr/local/bin/dockerview

# 3. 运行
sudo /usr/local/bin/dockerview -addr :8080

# 可选：自证没有动态依赖
ldd /usr/local/bin/dockerview     # 期望输出 not a dynamic executable
```

运行前确认 Docker socket 权限：

```bash
ls -l /var/run/docker.sock
# 非 root 运行需要把用户加入 docker 组（重新登录生效）
sudo usermod -aG docker $USER
```

Compose 文件默认保存到 `./data/compose`（相对**当前工作目录**），该目录必须可写；以服务方式部署时建议用绝对路径，见下方 `DOCKERVIEW_COMPOSE_DIR`。

浏览器打开 http://localhost:8080 ，左上角状态胶囊显示当前是 `docker` 还是 `demo` 引擎。

### 方式二：从源码编译运行

Linux / macOS（需 `make`）：

```bash
make run    # 编译并运行，连接本机 Docker
make dev    # 强制 Demo 模式，无需 Docker
```

Windows（无 `make`，直接用 go 命令；以下为 Git Bash 写法）：

```bash
# 编译
go build -trimpath -ldflags "-s -w" -o dockerview.exe .

# 运行
./dockerview.exe                          # 连接本机 Docker
DOCKERVIEW_DEMO=1 go run . -addr :8080    # Demo 模式
```

PowerShell 里设置环境变量的写法不同：

```powershell
go build -o dockerview.exe .
$env:DOCKERVIEW_DEMO=1; go run . -addr :8080
```

### 方式三：Docker 部署

```bash
# 方式一：compose
docker compose up -d

# 方式二：手动构建运行
make docker-build
make docker-run
```

面板自身作为容器运行时，需要挂载宿主机的 `/var/run/docker.sock`：

```bash
docker run -d --name dockerview -p 8080:8080 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v $(pwd)/data/compose:/app/data/compose \
  dockerview:latest
```

## 交叉编译

一次产出三个平台（`.exe` 后缀只给 Windows）：

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/dockerview-windows-amd64.exe .
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/dockerview-linux-amd64 .
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/dockerview-linux-arm64 .
```

验证产物架构与链接方式：

```bash
go version -m dist/dockerview-linux-arm64     # 看 buildGOOS / buildGOARCH / buildCGO_ENABLED
ldd dist/dockerview-linux-amd64               # not a dynamic executable
```

说明：

- `-trimpath` 去掉本机绝对路径，`-ldflags "-s -w"` 剥离符号表与调试信息，三个产物都在 10 MB 左右。
- 必须保持 `CGO_ENABLED=0`，否则交叉编译会要求目标平台 C 工具链，静态性也会丢失。
- `dist/` 已在 `.gitignore` 中，构建产物不入库。

## 配置

命令行参数优先，未指定时回落到同名环境变量。

| 参数 | 环境变量 | 默认值 | 说明 |
|------|----------|--------|------|
| `-addr` | `DOCKERVIEW_ADDR` | `:8080` | HTTP 监听地址。`:` 前缀表示所有网卡，生产环境建议 `127.0.0.1:8080` 再挂反向代理 |
| `-docker-host` | `DOCKER_HOST` | 系统默认 socket | Docker daemon 地址，如 `unix:///var/run/docker.sock`、`npipe:////./pipe/docker_engine`、`tcp://10.0.0.5:2375` |
| `-demo` | `DOCKERVIEW_DEMO=1` | `false` | 强制 Demo 模式，完全不连 Docker，用内置合成数据 |
| — | `DOCKERVIEW_STRICT=1` | `false` | Docker 连接失败时直接退出，不静默降级为 Demo |
| — | `DOCKERVIEW_COMPOSE_DIR` | `./data/compose` | Compose YAML 的保存目录，启动时自动创建 |

引擎选择逻辑：`-demo` → Demo；否则尝试连接 Docker，失败则打印原因并降级 Demo；`DOCKERVIEW_STRICT=1` 时改为直接退出，适合生产环境避免“以为在看真实容器”。

### 常见启动方式

```bash
# 默认：连本机 socket，监听 8080
./dockerview

# 只监听回环，前面挂 Nginx（配合防火墙 + 认证最稳妥）
./dockerview -addr 127.0.0.1:8080

# 演示模式：没有 Docker 也能完整点一遍 UI
./dockerview -demo

# 连远程未加密的 docker API
./dockerview -docker-host tcp://10.0.0.5:2375
```

> 远程 daemon 走 TLS 时，不要用 `-docker-host`（该参数不会加载证书），改用环境变量，SDK 会自动读取：`DOCKER_HOST=tcp://host:2376 DOCKER_TLS_VERIFY=1 DOCKER_CERT_PATH=/path/to/certs`。

## 部署为系统服务

以 systemd 常驻（`/etc/systemd/system/dockerview.service`）：

```ini
[Unit]
Description=DockerView - Docker web panel
After=docker.service network-online.target
Requires=docker.service

[Service]
Type=simple
User=root
Environment=DOCKERVIEW_ADDR=127.0.0.1:8080
Environment=DOCKERVIEW_COMPOSE_DIR=/var/lib/dockerview/compose
Environment=DOCKERVIEW_STRICT=1
ExecStart=/usr/local/bin/dockerview
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```bash
sudo mkdir -p /var/lib/dockerview/compose
sudo systemctl daemon-reload
sudo systemctl enable --now dockerview
journalctl -u dockerview -f
```

以普通用户运行时可改用 `User=xxx` + `Group=docker`，前提是该用户能读写 `/var/run/docker.sock`。

## 开发

```bash
make test              # 单元测试
make test-integration  # Docker 集成测试（需本机 daemon）
make vet               # go vet
make fmt               # gofmt
make build             # 编译到 ./dockerview
```

Windows 上没有 `make` 时，等价命令：

```bash
go test ./... -timeout 120s
go vet ./...
gofmt -w .
go build -o dockerview.exe .
```

注意：Windows 下 `go.mod` 需要 `github.com/go-ole/go-ole`、`github.com/yusufpapurcu/wmi` 两条间接依赖（gopsutil 的 Windows 代码路径），否则会报 `updates to go.mod needed`。

集成测试默认跳过，需显式开启：

```bash
DOCKERVIEW_INTEGRATION=1 go test ./internal/engine/docker/ -v
```

可用 `DOCKERVIEW_TEST_IMAGE` 指定测试镜像，默认 `alpine:3.20`。

## 项目结构

```
.
├── main.go                     # 入口：参数解析、引擎选择、HTTP 服务、优雅停机
├── internal/
│   ├── engine/
│   │   ├── engine.go           # Engine 接口（真实与 Demo 的统一契约）
│   │   ├── docker/             # 真实 Docker 实现（SDK + compose）
│   │   └── demo/               # 合成数据引擎（演示/降级）
│   ├── model/                  # 领域模型与 JSON DTO
│   ├── monitor/                # 宿主机指标采集（gopsutil）
│   ├── compose/                # docker-compose.yml 解析与依赖排序
│   └── server/
│       ├── server.go           # 路由与中间件
│       ├── handlers.go         # 容器 / 系统 / 日志 / 终端
│       ├── compose_handlers.go # 镜像 / 网络 / 卷 / Compose
│       ├── logstream.go        # 日志流复用与多路广播
│       ├── static.go           # 内嵌静态资源
│       └── assets/             # 前端（原生 JS + xterm.js）
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── dist/                       # 交叉编译产物（.gitignore 忽略）
└── .github/workflows/ci.yml    # CI：gofmt / vet / test -race / build
```

## HTTP API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | 健康检查 |
| GET | `/api/meta` | 引擎类型等元信息 |
| GET | `/api/system` | 宿主机与 Docker 概览 |
| GET | `/api/system/stream` | 系统指标 SSE 流 |
| GET | `/api/containers?all=` | 容器列表 |
| POST | `/api/containers` | 创建容器 |
| GET | `/api/containers/{id}` | 容器详情 |
| DELETE | `/api/containers/{id}?force=&volumes=` | 删除容器 |
| POST | `/api/containers/{id}/{action}` | start/stop/restart/pause/unpause/kill |
| GET | `/api/containers/{id}/stats` | 容器实时统计 |
| GET | `/api/containers/{id}/logs?tail=&timestamps=` | 日志流 |
| GET | `/api/containers/{id}/terminal` | WebSocket 终端 |
| GET/POST/DELETE | `/api/images`, `/api/images/pull`, `/api/images/{id}` | 镜像 |
| GET/POST/DELETE | `/api/networks`, `/api/networks/{id}` | 网络 |
| GET/POST/DELETE | `/api/volumes`, `/api/volumes/{name}` | 数据卷 |
| GET | `/api/compose/projects` | Compose 项目与文件 |
| POST | `/api/compose/up` | 部署 Compose |
| GET/DELETE | `/api/compose/files/{name}` | 读取/删除 Compose 文件 |
| POST | `/api/compose/{name}/{action}` | start/stop/restart/down |

## 安全说明

面板可操作宿主机 Docker，等价于 root 权限。当前版本**不带任何认证**：所有 `/api/*` 接口匿名可访问，WebSocket 终端的 `CheckOrigin` 也恒为放行，任意网页都能跨站连上本机终端。因此：

- 只在受信网络使用，或直接 `-addr 127.0.0.1:8080` 仅监听回环，通过带认证的反向代理暴露；
- 不要把 8080 直接开到公网；需要外网访问时，在 Nginx 层加 Basic Auth / mTLS / IP 白名单；
- 给普通用户授权 `docker` 组等同于给 root，自行评估。

## 常见问题

| 现象 | 原因与处理 |
|------|------------|
| `cannot execute binary file: Exec format error` | 架构不匹配，`uname -m` 后换对应产物（`aarch64` → `arm64`） |
| `permission denied` 访问 `/var/run/docker.sock` | 非 root 且不在 `docker` 组，`usermod -aG docker $USER` 后重新登录 |
| 页面能开但一直显示 DEMO | daemon 连不上，看启动日志 `Docker connection failed: ...`；排查 socket 路径或 `DOCKER_HOST` |
| Compose 保存失败 | `DOCKERVIEW_COMPOSE_DIR` 指向的目录不可写，改成绝对路径并 `chown` |
| 反向代理后仪表盘曲线不动 | Nginx 缓冲了 SSE，加 `proxy_buffering off;` 与 `proxy_read_timeout 3600s;`（服务端已发 `X-Accel-Buffering: no`） |
| 终端连上没反应 | 容器内没有 shell，或容器未运行时无 exec 权限；面板会依次尝试 `bash`/`sh`/`ash` |
