# relaybot

Telegram 文件中转 bot，基于 Telegram 原生文件中转方案实现。

- 上传时只保存 `file_id`、消息引用和元数据，不保存实际文件。
- 默认支持 `document`、`photo`、`video`、`audio`、`voice`。
- 默认 code 有效期为 `24h`，在过期前可重复获取。

## 本地部署

需要配置以下环境变量：

- `BOT_TOKEN`
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
- `UPLOAD_RATE_LIMIT`
- `UPLOAD_RATE_WINDOW`
- `CLAIM_RATE_LIMIT`
- `CLAIM_RATE_WINDOW`
- `BAD_CODE_RATE_LIMIT`
- `BAD_CODE_RATE_WINDOW`
- `STALE_DELIVERY_AFTER`
- `EXPIRED_DELIVERY_RETAIN`
- `ALLOW_DANGEROUS_FILES`
- `BLOCKED_EXTENSIONS`

说明：

- 未设置 `WEBHOOK_BASE_URL` / `WEBHOOK_PUBLIC_URL` 时，bot 以 long polling 方式运行。
- 设置了 `WEBHOOK_BASE_URL` / `WEBHOOK_PUBLIC_URL` 时，bot 会自动注册 webhook。
- 本地启动时会优先读取仓库根目录的 `.env`，但已存在于进程环境中的变量不会被覆盖。

## 本地开发

启动依赖：

```bash
docker compose up -d
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
