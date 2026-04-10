# relaybot

Telegram 文件中转 bot，基于 Telegram 原生文件中转方案实现。

- 上传时只保存 `file_id`、消息引用和元数据，不保存实际文件。
- 默认支持 `document`、`photo`、`video`、`audio`、`voice`。
- 默认 code 有效期为 `24h`，在过期前可重复获取。
- 支持单文件单 code，也支持通过批量上传会话让多个文件共用一个 code。

## 本地部署

支持以下环境变量：

- `BOT_TOKEN`
- `SYNC_BOT_COMMANDS`（可选，默认 `true`）
- `APP_SECRET`
- `PG_DSN` 或 `POSTGRES_DSN`
- `REDIS_ADDR`
- `REDIS_PASSWORD`
- `REDIS_DB`
- `HTTP_ADDR`
- `WEBHOOK_BASE_URL` 或 `WEBHOOK_PUBLIC_URL`
- `WEBHOOK_PATH`
- `WEBHOOK_SECRET`
- `RELAY_TTL`
- `MAX_FILE_BYTES`
- `ACTIVE_RELAYS_PER_USER`
- `MAX_BATCH_ITEMS`
- `UPLOAD_RATE_LIMIT`
- `UPLOAD_RATE_WINDOW`
- `CLAIM_RATE_LIMIT`
- `CLAIM_RATE_WINDOW`
- `BAD_CODE_RATE_LIMIT`
- `BAD_CODE_RATE_WINDOW`
- `BATCH_SESSION_TTL`
- `STALE_DELIVERY_AFTER`
- `EXPIRED_DELIVERY_RETAIN`
- `ALLOW_DANGEROUS_FILES`
- `BLOCKED_EXTENSIONS`

说明：

- 未设置 `WEBHOOK_BASE_URL` / `WEBHOOK_PUBLIC_URL` 时，bot 以 long polling 方式运行。
- 设置了 `WEBHOOK_BASE_URL` / `WEBHOOK_PUBLIC_URL` 时，bot 会自动注册 webhook。
- 默认会在启动时把 `/start`、`/help`、`/batch_start`、`/batch_done`、`/batch_cancel` 同步到 Telegram 的私聊命令列表；可用 `SYNC_BOT_COMMANDS=false` 关闭。
- `BATCH_SESSION_TTL` 用于控制批量上传会话的存活时间，默认 `30m`。
- `MAX_BATCH_ITEMS` 用于限制单个批量上传会话可包含的文件数，默认 `100`。
- 本地启动时会优先读取仓库根目录的 `.env`，但已存在于进程环境中的变量不会被覆盖。

## 本地开发

启动依赖：

```bash
docker compose up -d
```

当前 `compose.yaml` / `compose-dev.yaml` 已为 `postgres` 和 `redis` 配置命名卷持久化：

- `postgres_data` 挂载到 `/var/lib/postgresql/data`
- `redis_data` 挂载到 `/data`
- `redis` 额外开启了 AOF（`appendonly yes`），降低容器重建后的数据丢失风险
- 宿主机端口可通过环境变量覆盖：`HOST_POSTGRES_PORT`、`HOST_REDIS_PORT`

查看卷：

```bash
docker volume ls | grep relaybot
```

清空本地依赖数据时，需要连同卷一起删除：

```bash
docker compose down -v
```

示例环境变量：

```bash
export BOT_TOKEN=...
export APP_SECRET=change-me
export PG_DSN='postgres://relaybot:relaybot@127.0.0.1:5432/relaybot?sslmode=disable'
export REDIS_ADDR='127.0.0.1:6379'
```

启动服务：

```bash
go run ./cmd/relaybot
```

服务启动时会自动执行仓库 `db/migrations` 下的 SQL migration，并暴露：

- `/healthz`
- `/readyz`
- `/metrics`

## Docker 构建与部署

构建镜像：

```bash
docker build -t relaybot:latest .
```

如需在构建阶段配置 HTTP(S) 代理和 Go 模块代理（`GOPROXY`），可传入 build args：

```bash
docker build \
  --build-arg HTTP_PROXY='http://127.0.0.1:7890' \
  --build-arg HTTPS_PROXY='http://127.0.0.1:7890' \
  --build-arg NO_PROXY='127.0.0.1,localhost,postgres,redis' \
  --build-arg GOPROXY='https://goproxy.cn,direct' \
  -t relaybot:latest .
```

私有模块场景可额外传入：

```bash
docker build \
  --build-arg GOPRIVATE='github.com/your-org/*' \
  --build-arg GONOSUMDB='github.com/your-org/*' \
  -t relaybot:latest .
```

运行容器（假设数据库和 Redis 也在 Docker 网络内）：

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

使用 Compose 同时启动依赖和 bot：

```bash
docker compose -f compose.yaml -f compose-app.yaml up -d --build
```

说明：

- `compose.yaml` 负责 `postgres` 和 `redis`。
- `compose-app.yaml` 负责构建并运行 `relaybot` 服务。
- `compose-app.yaml` 默认使用容器网络地址：`postgres` / `redis`。可通过 `COMPOSE_PG_DSN`、`COMPOSE_REDIS_ADDR` 覆盖。
- 宿主机端口映射由 `HOST_HTTP_PORT` 控制（默认 `8080`）。仅修改 `HTTP_ADDR` 不会改变宿主机映射端口。
- 容器内监听端口由 `CONTAINER_HTTP_PORT` 控制（默认 `8080`）。
- 如需显式覆盖容器内监听地址，可使用 `COMPOSE_HTTP_ADDR`。
- `compose-app.yaml` 会自动读取环境中的 `HTTP_PROXY`、`HTTPS_PROXY`、`NO_PROXY`、`GOPROXY`、`GOPRIVATE` 等构建参数并传给 Docker build。

## 使用方式

单文件上传：

1. 直接把一个支持的文件发给 bot。
2. bot 返回一个 `relaybot_...` code。
3. 把这个 code 再发给 bot，即可取回文件。

批量上传：

1. 发送 `/batch_start`
2. 连续发送多个文件
3. 发送 `/batch_done` 生成一个共享 code
4. 如果放弃本次会话，发送 `/batch_cancel`
5. 把共享 code 发给 bot，即可取回整批文件。
6. 单个批次默认最多可包含 `100` 个文件，可通过 `MAX_BATCH_ITEMS` 调整。

领取文件：

- 任意消息里只要包含一个或多个 `relaybot_...` code，bot 都会尝试提取并领取。
- 同一条消息里支持多条 code。

## 当前限制

- 本项目基于 Telegram `file_id` 做文件中转，不保存原始文件内容；因此只能转发 bot 已经成功接收并可再次调用的 Telegram 文件对象。
- 批量领取当前是批次级投递记录，不跟踪每个文件的单独投递结果。如果 Telegram 在一批文件发送过程中出现“前半部分已成功、后半部分失败”的情况，后续重试可能会重复收到此前已成功发送的那部分文件。
