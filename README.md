# relaybot

`relaybot` 是一个基于 Telegram 原生 `file_id` 的文件中转 bot。

它不会把文件二进制内容保存到你的服务器，而是保存 Telegram 文件引用、消息信息和必要元数据，并通过 `relaybot_...` code 让用户在有效期内重新领取文件。

<p align="center">
  <img src="./assets/screenshot_20260410_234112.png" width="45%" />
  <img src="./assets/screenshot_20260410_234015.png" width="45%" />
</p>

## 特性

- 不保存原始文件内容，只保存 `file_id`、消息引用和元数据
- 支持 `document`、`photo`、`video`、`audio`、`voice`
- 支持单文件单 code，也支持批量上传后为多个文件生成一个共享 code
- 支持在一条消息中提取多个 code，并在文件较多时分页投递
- 支持 long polling 和 webhook 两种运行模式
- 内置有效期、上传限流、领取限流、危险扩展名拦截等策略

## 工作原理

1. 用户把文件发送给 bot。
2. bot 校验媒体类型、文件大小和文件策略。
3. bot 在 PostgreSQL 中保存中转记录，在 Redis 中保存限流与批量上传会话等短期状态。
4. bot 返回一个 `relaybot_...` code。
5. 任何用户再次把该 code 发给 bot 时，bot 会通过 Telegram 原生接口重新投递文件。

## 运行要求

- Telegram Bot Token
- PostgreSQL
- Redis
- Go `1.26+`，如果你打算直接从源码运行
- Docker / Docker Compose，如果你打算容器化部署

## 快速开始

### 方式一：本地源码运行

1. 复制配置模板并填写必要变量：

```bash
cp .env.example .env
```

至少需要设置：

- `BOT_TOKEN`
- `APP_SECRET`
- `PG_DSN`

如果 Redis 不在默认地址 `127.0.0.1:6379`，还需要设置 `REDIS_ADDR`。

2. 启动 PostgreSQL 和 Redis：

```bash
docker compose up -d
```

如果宿主机的 `5432` 或 `6379` 已被占用，可使用开发覆盖配置：

```bash
docker compose -f compose.yaml -f compose-dev.yaml up -d
```

这种情况下，需要同步调整 `.env` 中的连接地址，例如：

```env
PG_DSN=postgres://relaybot:relaybot@127.0.0.1:15432/relaybot?sslmode=disable
REDIS_ADDR=127.0.0.1:16379
```

3. 启动 bot：

```bash
make run
```

或：

```bash
go run ./cmd/relaybot
```

如需清理本地依赖与卷数据，可执行：

```bash
docker compose down -v
```

### 方式二：使用 Docker Compose 部署完整服务

1. 复制并编辑配置：

```bash
cp .env.example .env
```

至少设置：

- `BOT_TOKEN`
- `APP_SECRET`

2. 启动依赖和应用：

```bash
docker compose -f compose.yaml -f compose-app.yaml up -d --build
```

如果服务器上的 `8080` 已被占用，不要只修改 `HTTP_ADDR`，而是应在 `.env` 中设置：

```env
HOST_HTTP_PORT=18080
```

如需同时修改容器内监听端口，再额外设置：

```env
CONTAINER_HTTP_PORT=18080
```

## Docker 部署说明

### Compose 文件职责

- [`compose.yaml`](compose.yaml) 负责启动 `postgres` 和 `redis`
- [`compose-app.yaml`](compose-app.yaml) 负责构建并运行 `relaybot`
- [`compose-dev.yaml`](compose-dev.yaml) 是本地开发时的可选覆盖配置，用于把依赖端口改为 `15432` / `16379`

### 构建镜像

```bash
docker build -t relaybot:latest .
```

Dockerfile 支持以下常见 build args：

- `HTTP_PROXY`
- `HTTPS_PROXY`
- `NO_PROXY`
- `ALL_PROXY`
- `GOPROXY`
- `GOSUMDB`
- `GOPRIVATE`
- `GONOSUMDB`

### 运行容器

如果 PostgreSQL 和 Redis 已在同一个 Docker 网络内，可直接运行：

```bash
docker run --rm \
  --name relaybot \
  --env-file .env \
  -e PG_DSN='postgres://relaybot:relaybot@postgres:5432/relaybot?sslmode=disable' \
  -e REDIS_ADDR='redis:6379' \
  -e HTTP_ADDR=':8080' \
  -p 8080:8080 \
  relaybot:latest
```

## 配置

完整配置项请参考 [`.env.example`](.env.example)。

程序启动时会自动读取仓库根目录的 `.env`，但不会覆盖已经存在的进程环境变量。

### 核心运行变量

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `BOT_TOKEN` | 是 | 无 | Telegram bot token |
| `APP_SECRET` | 是 | 无 | 用于 HMAC 签名 relay code 的密钥 |
| `PG_DSN` / `POSTGRES_DSN` | 是 | 无 | PostgreSQL 连接串 |
| `REDIS_ADDR` | 否 | `127.0.0.1:6379` | Redis 地址 |
| `REDIS_PASSWORD` | 否 | 空 | Redis 密码 |
| `REDIS_DB` | 否 | `0` | Redis DB 编号 |
| `HTTP_ADDR` | 否 | `:8080` | HTTP 服务监听地址 |
| `WEBHOOK_BASE_URL` / `WEBHOOK_PUBLIC_URL` | 否 | 空 | 设置后启用 webhook；为空时使用 long polling |
| `WEBHOOK_PATH` | 否 | `/telegram/webhook` | webhook 路径 |
| `WEBHOOK_SECRET` | 否 | 空 | Telegram webhook secret token |
| `SYNC_BOT_COMMANDS` | 否 | `true` | 启动时同步 Telegram 私聊命令 |
| `RELAY_TTL` | 否 | `24h` | relay code 默认有效期 |
| `MAX_FILE_BYTES` | 否 | `10737418240` | 单文件大小上限 |
| `ACTIVE_RELAYS_PER_USER` | 否 | `100` | 单用户可保留的活跃 relay 数量上限 |
| `MAX_BATCH_ITEMS` | 否 | `100` | 单个批量上传会话允许的最大文件数 |
| `ALLOW_DANGEROUS_FILES` | 否 | `false` | 是否允许危险扩展名文件 |
| `BLOCKED_EXTENSIONS` | 否 | 内置黑名单 | 被拒绝的文件扩展名列表 |

### Compose 相关变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `HOST_POSTGRES_PORT` | `5432` | `compose.yaml` 发布到宿主机的 PostgreSQL 端口 |
| `HOST_REDIS_PORT` | `6379` | `compose.yaml` 发布到宿主机的 Redis 端口 |
| `HOST_HTTP_PORT` | `8080` | `compose-app.yaml` 发布到宿主机的 HTTP 端口 |
| `CONTAINER_HTTP_PORT` | `8080` | 容器内应用监听端口 |
| `COMPOSE_HTTP_ADDR` | `:${CONTAINER_HTTP_PORT}` | Compose 场景下覆盖容器内 `HTTP_ADDR` |
| `COMPOSE_PG_DSN` | `postgres://relaybot:relaybot@postgres:5432/relaybot?sslmode=disable` | Compose 场景下覆盖容器内 `PG_DSN` |
| `COMPOSE_REDIS_ADDR` | `redis:6379` | Compose 场景下覆盖容器内 `REDIS_ADDR` |

注意：

- 在 `docker compose -f compose.yaml -f compose-app.yaml` 场景下，宿主机暴露端口由 `HOST_HTTP_PORT` 控制。
- 仅修改 `HTTP_ADDR` 不会改变宿主机映射端口。
- 如需显式覆盖容器内监听地址，可使用 `COMPOSE_HTTP_ADDR`。

## 运行模式

### Long Polling

- 不设置 `WEBHOOK_BASE_URL` 或 `WEBHOOK_PUBLIC_URL` 时，bot 使用 long polling。
- 这更适合本地调试或没有公网入口的环境。
- 如果之前注册过 webhook，程序会在启动时删除已有 webhook，再进入 polling。

### Webhook

- 设置 `WEBHOOK_BASE_URL` 或 `WEBHOOK_PUBLIC_URL` 后，bot 会在启动时自动注册 Telegram webhook。
- 可通过 `WEBHOOK_SECRET` 配置 Telegram webhook secret token。
- 反向代理或网关需要把请求转发到 `WEBHOOK_PATH` 对应的 HTTP 路径。

## 使用方式

### 单文件中转

1. 直接把一个支持的文件发给 bot。
2. bot 返回一个形如 `relaybot_XXXXXXXXXXXXXXXXXXXX` 的 code。
3. 把这串 code 再发给 bot，即可取回原文件。

### 批量中转

1. 发送 `/batch_start`
2. 连续发送多个文件
3. 发送 `/batch_done` 生成一个共享 code
4. 如果放弃本次会话，发送 `/batch_cancel`
5. 把共享 code 发给 bot，即可取回整批文件

### 领取文件

- 任意消息里只要包含一个或多个 `relaybot_...` code，bot 都会尝试提取并领取
- 同一条消息里支持多条 code
- 文件较多时会分页发送，并通过按钮翻页

### 默认命令

- `/start`
- `/help`
- `/batch_start`
- `/batch_done`
- `/batch_cancel`

## 健康检查与监控

服务启动后会暴露以下 HTTP 端点：

- `/healthz`
- `/readyz`
- `/metrics`

另外，应用会在启动时自动执行 [`db/migrations`](db/migrations) 下的 migration。

其中 `/readyz` 会检查 PostgreSQL 和 Redis 的可用性。

## 开发

### 常用命令

```bash
make run
make test
make fmt
```

说明：

- `make test` 对应 `go test ./...`
- PostgreSQL 集成测试依赖 `TEST_DATABASE_DSN`
- 如果没有设置 `TEST_DATABASE_DSN`，相关集成测试会自动跳过

### 项目结构

- [`cmd/relaybot`](cmd/relaybot) 入口程序
- [`internal/bootstrap`](internal/bootstrap) 应用装配与生命周期管理
- [`internal/config`](internal/config) 配置加载
- [`internal/httpserver`](internal/httpserver) HTTP 服务与探针端点
- [`internal/relay`](internal/relay) 核心中转业务逻辑
- [`internal/store`](internal/store) PostgreSQL 持久化
- [`internal/cache`](internal/cache) Redis 缓存、限流与会话状态
- [`internal/telegram`](internal/telegram) Telegram 路由、命令与发送逻辑
- [`internal/worker`](internal/worker) 后台任务执行
- [`db/migrations`](db/migrations) 数据库 migration

如果你打算提交修改，至少应在本地执行：

```bash
make fmt
make test
```

## 当前限制

- 本项目基于 Telegram `file_id` 做中转，不保存原始文件内容，因此只能重新投递 bot 已成功接收且 Telegram 仍可访问的文件
- 当前仅支持 `document`、`photo`、`video`、`audio`、`voice`
- 批量领取目前按批次和分页进行投递记录，不跟踪每个文件的独立投递状态
- 如果 Telegram 在批量发送过程中出现“前半部分成功、后半部分失败”，后续重试可能会重复收到此前已经成功发送的部分文件

## License

本项目基于 [MIT License](LICENSE) 开源。
