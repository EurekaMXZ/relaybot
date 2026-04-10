package rediscache

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"relaybot/internal/relay"
)

type Options struct {
	UploadLimit   int64
	UploadWindow  time.Duration
	ClaimLimit    int64
	ClaimWindow   time.Duration
	BadCodeLimit  int64
	BadCodeWindow time.Duration
	SeenUpdateTTL time.Duration
}

type Cache struct {
	client  *redis.Client
	options Options
}

var allowScript = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local ttl_ms = tonumber(ARGV[2])

local current = redis.call("INCR", key)
if current == 1 then
	redis.call("PEXPIRE", key, ttl_ms)
end

if current > limit then
	return 0
end

return 1
`)

func New(client *redis.Client, options Options) *Cache {
	return &Cache{
		client:  client,
		options: options,
	}
}

func (c *Cache) GetRelayIDByCodeHash(ctx context.Context, codeHash string) (int64, bool, error) {
	value, err := c.client.Get(ctx, relayKey(codeHash)).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	relayID, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false, err
	}
	return relayID, true, nil
}

func (c *Cache) SetRelayIDByCodeHash(ctx context.Context, codeHash string, relayID int64, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	return c.client.Set(ctx, relayKey(codeHash), strconv.FormatInt(relayID, 10), ttl).Err()
}

func (c *Cache) GetCreatedCodeBySourceUpdate(ctx context.Context, sourceUpdateID int64) (string, bool, error) {
	value, err := c.client.Get(ctx, sourceUpdateKey(sourceUpdateID)).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (c *Cache) SetCreatedCodeBySourceUpdate(ctx context.Context, sourceUpdateID int64, code string, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	return c.client.Set(ctx, sourceUpdateKey(sourceUpdateID), code, ttl).Err()
}

func (c *Cache) AllowUpload(ctx context.Context, userID int64) (bool, error) {
	return c.allow(ctx, rateKey("upload", userID), c.options.UploadLimit, c.options.UploadWindow)
}

func (c *Cache) AllowClaim(ctx context.Context, userID int64) (bool, error) {
	return c.allow(ctx, rateKey("claim", userID), c.options.ClaimLimit, c.options.ClaimWindow)
}

func (c *Cache) AllowBadCode(ctx context.Context, userID int64) (bool, error) {
	return c.allow(ctx, rateKey("badcode", userID), c.options.BadCodeLimit, c.options.BadCodeWindow)
}

func (c *Cache) MarkSeenUpdate(ctx context.Context, updateID int64) (bool, error) {
	return c.client.SetNX(ctx, updateKey(updateID), "1", c.options.SeenUpdateTTL).Result()
}

func (c *Cache) GetBatchUploadSession(ctx context.Context, chatID int64) (relay.BatchUploadSession, bool, error) {
	value, err := c.client.Get(ctx, batchSessionKey(chatID)).Result()
	if err == redis.Nil {
		return relay.BatchUploadSession{}, false, nil
	}
	if err != nil {
		return relay.BatchUploadSession{}, false, err
	}

	var session relay.BatchUploadSession
	if err := json.Unmarshal([]byte(value), &session); err != nil {
		return relay.BatchUploadSession{}, false, err
	}
	return session, true, nil
}

func (c *Cache) SetBatchUploadSession(ctx context.Context, session relay.BatchUploadSession, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	body, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, batchSessionKey(session.UploaderChatID), body, ttl).Err()
}

func (c *Cache) MergeBatchUploadSession(ctx context.Context, session relay.BatchUploadSession, ttl time.Duration) (relay.BatchUploadSession, error) {
	if ttl <= 0 {
		return session, nil
	}

	key := batchSessionKey(session.UploaderChatID)
	for {
		merged := session
		err := c.client.Watch(ctx, func(tx *redis.Tx) error {
			value, err := tx.Get(ctx, key).Result()
			switch {
			case err == nil:
				var current relay.BatchUploadSession
				if err := json.Unmarshal([]byte(value), &current); err != nil {
					return err
				}
				if current.RelayID != 0 && current.RelayID != session.RelayID {
					merged = current
					return nil
				}
				merged = mergeBatchUploadSession(current, session)
			case err == redis.Nil:
				merged = session
			default:
				return err
			}

			body, err := json.Marshal(merged)
			if err != nil {
				return err
			}

			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, body, ttl)
				return nil
			})
			return err
		}, key)
		if err == redis.TxFailedErr {
			continue
		}
		return merged, err
	}
}

func (c *Cache) DeleteBatchUploadSession(ctx context.Context, chatID int64) error {
	return c.client.Del(ctx, batchSessionKey(chatID)).Err()
}

func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Cache) allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error) {
	result, err := allowScript.Run(ctx, c.client, []string{key}, limit, window.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func relayKey(codeHash string) string {
	return "relay:code:" + codeHash
}

func updateKey(updateID int64) string {
	return fmt.Sprintf("tg:update:%d", updateID)
}

func sourceUpdateKey(updateID int64) string {
	return fmt.Sprintf("relay:source:%d", updateID)
}

func rateKey(kind string, userID int64) string {
	return fmt.Sprintf("rl:%s:u:%d", kind, userID)
}

func batchSessionKey(chatID int64) string {
	return fmt.Sprintf("batch:session:chat:%d", chatID)
}

func mergeBatchUploadSession(current, next relay.BatchUploadSession) relay.BatchUploadSession {
	if current.RelayID == 0 {
		return next
	}

	merged := current
	if merged.UploaderUserID == 0 {
		merged.UploaderUserID = next.UploaderUserID
	}
	if merged.UploaderChatID == 0 {
		merged.UploaderChatID = next.UploaderChatID
	}
	if next.ItemCount > merged.ItemCount {
		merged.ItemCount = next.ItemCount
	}
	if next.ProgressMessageID > 0 {
		merged.ProgressMessageID = next.ProgressMessageID
	}
	if next.LastProgressNotifiedCount > merged.LastProgressNotifiedCount {
		merged.LastProgressNotifiedCount = next.LastProgressNotifiedCount
	}
	if next.LastProgressNotifiedAt.After(merged.LastProgressNotifiedAt) {
		merged.LastProgressNotifiedAt = next.LastProgressNotifiedAt
	}
	if merged.StartedAt.IsZero() || (!next.StartedAt.IsZero() && next.StartedAt.Before(merged.StartedAt)) {
		merged.StartedAt = next.StartedAt
	}
	if next.LastActivityAt.After(merged.LastActivityAt) {
		merged.LastActivityAt = next.LastActivityAt
	}
	return merged
}
