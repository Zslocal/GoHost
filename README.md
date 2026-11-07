# DockerView

一个用 Go 编写的 Docker 可视化管理面板。单二进制、零外部前端依赖，内置仪表盘、容器/镜像/网络/数据卷管理、Compose 编排、实时日志与浏览器终端。

## 特性

- **仪表盘**：CPU / 内存 / 磁盘 / 网络吞吐实时监控，SSE 推送资源曲线
- **容器管理**：启停、重启、暂停、Kill、删除、创建；实时状态与端口映射
- **容器详情**：CPU/内存/网络/进程统计，挂载点、环境变量、配置项
- **实时日志**：跟随输出、时间戳开关、tail 行数控制
- **浏览器终端**：基于 xterm.js 的交互式 TTY，支持窗口自适应 resize
- **镜像管理**：列表、搜索、拉取（流式进度）、删除
- **网络与数据卷**：创建、查看关联容器、删除
- **Compose**：YAML 编辑部署、内置模板、项目启停/重启/下线、依赖顺序解析
- **双引擎**：连接真实 Docker daemon；不可用时自动降级为内置 Demo 模式

## 快速开始

### 本地运行

```bash
# 编译并运行（自动连接本机 Docker）
make run

# 强制 Demo 模式（无需 Docker）
make dev
```

浏览器打开 http://localhost:8080

### Docker 部署

```bash
# 方式一：compose
docker compose up -d

# 方式二：手动构建运行
make docker-build
make docker-run
```

面板自身作为容器运行时，需要挂载宿主机的 `/var/run/docker.sock`。

## 配置

通过命令行参数或环境变量配置：

| 参数 | 环境变量 | 默认值 | 说明 |
|------|----------|--------|------|
| `-addr` | `DOCKERVIEW_ADDR` | `:8080` | HTTP 监听地址 |
| `-docker-host` | `DOCKER_HOST` | 系统默认 socket | Docker daemon 地址 |
| `-demo` | `DOCKERVIEW_DEMO=1` | `false` | 强制 Demo 模式 |
| — | `DOCKERVIEW_STRICT=1` | `false` | Docker 连接失败时直接退出，不降级 |
| — | `DOCKERVIEW_COMPOSE_DIR` | `./data/compose` | Compose 文件保存目录 |

## 开发

```bash
make test              # 单元测试
make test-integration  # Docker 集成测试（需本机 daemon）
make vet               # go vet
make fmt               # gofmt
make build             # 编译到 ./dockerview
```

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
└── Makefile
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

面板可操作宿主机 Docker（等价于 root 权限）。部署到网络环境前请自行配置认证与访问控制，仅限受信网络使用。
