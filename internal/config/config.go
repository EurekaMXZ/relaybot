package config

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BotToken              string
	SyncBotCommands       bool
	AppSecret             string
	PostgresDSN           string
	RedisAddr             string
	RedisPassword         string
	RedisDB               int
	HTTPAddr              string
	WebhookPath           string
	WebhookPublicURL      string
	WebhookSecret         string
	RelayTTL              time.Duration
	MaxFileBytes          int64
	ActiveRelaysPerUser   int64
	MaxBatchItems         int
	UploadRateLimit       int
	UploadRateWindow      time.Duration
	ClaimRateLimit        int
	ClaimRateWindow       time.Duration
	BadCodeRateLimit      int
	BadCodeRateWindow     time.Duration
	BatchSessionTTL       time.Duration
	StaleDeliveryAfter    time.Duration
	ExpiredDeliveryRetain time.Duration
	AllowDangerousFiles   bool
	DangerousExtensions   map[string]struct{}
}

func Load() (Config, error) {
	loadDotEnv(".env")

	cfg := Config{
		BotToken:              strings.TrimSpace(os.Getenv("BOT_TOKEN")),
		SyncBotCommands:       envBool("SYNC_BOT_COMMANDS", true),
		AppSecret:             strings.TrimSpace(os.Getenv("APP_SECRET")),
		PostgresDSN:           firstNonEmptyEnv("PG_DSN", "POSTGRES_DSN"),
		RedisAddr:             envString("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:         os.Getenv("REDIS_PASSWORD"),
		RedisDB:               envInt("REDIS_DB", 0),
		HTTPAddr:              envString("HTTP_ADDR", ":8080"),
		WebhookPath:           envString("WEBHOOK_PATH", "/telegram/webhook"),
		WebhookPublicURL:      firstNonEmptyEnv("WEBHOOK_BASE_URL", "WEBHOOK_PUBLIC_URL"),
		WebhookSecret:         strings.TrimSpace(os.Getenv("WEBHOOK_SECRET")),
		RelayTTL:              envDuration("RELAY_TTL", 24*time.Hour),
		MaxFileBytes:          envInt64("MAX_FILE_BYTES", 10*1024*1024*1024),
		ActiveRelaysPerUser:   envInt64("ACTIVE_RELAYS_PER_USER", 100),
		MaxBatchItems:         envInt("MAX_BATCH_ITEMS", 100),
		UploadRateLimit:       envInt("UPLOAD_RATE_LIMIT", 5),
		UploadRateWindow:      envDuration("UPLOAD_RATE_WINDOW", 10*time.Minute),
		ClaimRateLimit:        envInt("CLAIM_RATE_LIMIT", 15),
		ClaimRateWindow:       envDuration("CLAIM_RATE_WINDOW", 10*time.Minute),
		BadCodeRateLimit:      envInt("BAD_CODE_RATE_LIMIT", 10),
		BadCodeRateWindow:     envDuration("BAD_CODE_RATE_WINDOW", 10*time.Minute),
		BatchSessionTTL:       envDuration("BATCH_SESSION_TTL", 30*time.Minute),
		StaleDeliveryAfter:    envDuration("STALE_DELIVERY_AFTER", 2*time.Minute),
		ExpiredDeliveryRetain: envDuration("EXPIRED_DELIVERY_RETAIN", 7*24*time.Hour),
		AllowDangerousFiles:   envBool("ALLOW_DANGEROUS_FILES", false),
		DangerousExtensions:   csvSet("BLOCKED_EXTENSIONS", ".exe,.msi,.bat,.cmd,.sh,.apk,.dmg,.iso,.js"),
	}

	switch {
	case cfg.BotToken == "":
		return Config{}, errors.New("BOT_TOKEN is required")
	case cfg.AppSecret == "":
		return Config{}, errors.New("APP_SECRET is required")
	case cfg.PostgresDSN == "":
		return Config{}, errors.New("PG_DSN (or POSTGRES_DSN) is required")
	case cfg.MaxBatchItems <= 0:
		return Config{}, errors.New("MAX_BATCH_ITEMS must be greater than 0")
	}

	return cfg, nil
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		_ = os.Setenv(key, value)
	}
}

func (c Config) WebhookEnabled() bool {
	return c.WebhookPublicURL != ""
}

func (c Config) WebhookURL() string {
	base := strings.TrimRight(c.WebhookPublicURL, "/")
	return base + c.WebhookPath
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func csvSet(key, fallback string) map[string]struct{} {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		value = fallback
	}

	result := make(map[string]struct{})
	for _, part := range strings.Split(value, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		result[part] = struct{}{}
	}
	return result
}
