package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-telegram/bot"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"relaybot/internal/cache/rediscache"
	"relaybot/internal/config"
	"relaybot/internal/httpserver"
	"relaybot/internal/relay"
	"relaybot/internal/store/postgres"
	"relaybot/internal/telegram"
	"relaybot/internal/worker"
)

type App struct {
	cfg        config.Config
	logger     *slog.Logger
	httpServer *http.Server
	store      *postgres.Store
	redis      *redis.Client
	bot        *bot.Bot
	runner     *worker.Runner
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if err := postgres.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	store := postgres.New(pool)
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	cache := rediscache.New(redisClient, rediscache.Options{
		UploadLimit:   int64(cfg.UploadRateLimit),
		UploadWindow:  cfg.UploadRateWindow,
		ClaimLimit:    int64(cfg.ClaimRateLimit),
		ClaimWindow:   cfg.ClaimRateWindow,
		BadCodeLimit:  int64(cfg.BadCodeRateLimit),
		BadCodeWindow: cfg.BadCodeRateWindow,
		SeenUpdateTTL: 25 * time.Hour,
	})

	forbiddenExtensions := cfg.DangerousExtensions
	if cfg.AllowDangerousFiles {
		forbiddenExtensions = nil
	}

	sender := telegram.NewSender()
	service := relay.NewService(
		store,
		cache,
		sender,
		relay.NewHMACCodeManager(cfg.AppSecret),
		nil,
		relay.Limits{
			MaxFileBytes:         cfg.MaxFileBytes,
			MaxActiveRelays:      cfg.ActiveRelaysPerUser,
			DefaultTTL:           cfg.RelayTTL,
			UnknownDeliveryAfter: cfg.StaleDeliveryAfter,
			ExpiredDeliveryPurge: cfg.ExpiredDeliveryRetain,
			ForbiddenExtensions:  forbiddenExtensions,
		},
	)

	router := telegram.NewRouter(logger)
	router.Bind(service)

	options := []bot.Option{
		bot.WithWorkers(8),
		bot.WithDefaultHandler(router.HandleUpdate),
	}
	if cfg.WebhookEnabled() && cfg.WebhookSecret != "" {
		options = append(options, bot.WithWebhookSecretToken(cfg.WebhookSecret))
	}

	botClient, err := bot.New(cfg.BotToken, options...)
	if err != nil {
		pool.Close()
		_ = redisClient.Close()
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}
	sender.Bind(botClient)

	httpSrv := httpserver.New(
		cfg.HTTPAddr,
		cfg.WebhookPath,
		webhookHandler(cfg, botClient),
		func(checkCtx context.Context) error {
			if err := store.Ping(checkCtx); err != nil {
				return err
			}
			return cache.Ping(checkCtx)
		},
	)

	runner := worker.NewRunner(store, nil, relay.Limits{
		UnknownDeliveryAfter: cfg.StaleDeliveryAfter,
		ExpiredDeliveryPurge: cfg.ExpiredDeliveryRetain,
	}, logger)

	return &App{
		cfg:        cfg,
		logger:     logger,
		httpServer: httpSrv,
		store:      store,
		redis:      redisClient,
		bot:        botClient,
		runner:     runner,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		errCh <- a.httpServer.ListenAndServe()
	}()
	go a.runner.Run(ctx)

	if a.cfg.WebhookEnabled() {
		if _, err := a.bot.SetWebhook(ctx, &bot.SetWebhookParams{
			URL:         a.cfg.WebhookURL(),
			SecretToken: a.cfg.WebhookSecret,
		}); err != nil {
			return fmt.Errorf("set webhook: %w", err)
		}
		go a.bot.StartWebhook(ctx)
	} else {
		if _, err := a.bot.DeleteWebhook(ctx, &bot.DeleteWebhookParams{}); err != nil {
			a.logger.Warn("delete webhook failed before polling", "error", err)
		}
		go a.bot.Start(ctx)
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := a.httpServer.Shutdown(shutdownCtx)
		_ = a.redis.Close()
		a.store.Close()
		if err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func (a *App) Close() error {
	if a.redis != nil {
		_ = a.redis.Close()
	}
	if a.store != nil {
		a.store.Close()
	}
	return nil
}

func webhookHandler(cfg config.Config, botClient *bot.Bot) http.Handler {
	if !cfg.WebhookEnabled() {
		return nil
	}
	return botClient.WebhookHandler()
}
