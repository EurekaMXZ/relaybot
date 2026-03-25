package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	baseLogger := slog.Default()
	logger := baseLogger.With(slog.String("component", "app"))
	routerLogger := baseLogger
	runnerLogger := baseLogger
	startedAt := time.Now()

	logger.Info(
		"application bootstrap started",
		slog.String("mode", runMode(cfg)),
		slog.String("http_addr", cfg.HTTPAddr),
		slog.Bool("webhook_enabled", cfg.WebhookEnabled()),
		slog.String("webhook_path", cfg.WebhookPath),
		slog.String("redis_addr", cfg.RedisAddr),
	)

	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	logger.Info("postgres pool created")
	if err := postgres.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	logger.Info("postgres migrations applied")

	store := postgres.New(pool)
	logger.Info("postgres store initialized")

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	logger.Info("redis client initialized", slog.Int("redis_db", cfg.RedisDB))

	cache := rediscache.New(redisClient, rediscache.Options{
		UploadLimit:   int64(cfg.UploadRateLimit),
		UploadWindow:  cfg.UploadRateWindow,
		ClaimLimit:    int64(cfg.ClaimRateLimit),
		ClaimWindow:   cfg.ClaimRateWindow,
		BadCodeLimit:  int64(cfg.BadCodeRateLimit),
		BadCodeWindow: cfg.BadCodeRateWindow,
		SeenUpdateTTL: 25 * time.Hour,
	})
	logger.Info("relay cache initialized")

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
	logger.Info(
		"relay service initialized",
		slog.Duration("relay_ttl", cfg.RelayTTL),
		slog.Int64("max_file_bytes", cfg.MaxFileBytes),
		slog.Int64("active_relays_per_user", cfg.ActiveRelaysPerUser),
		slog.Bool("allow_dangerous_files", cfg.AllowDangerousFiles),
	)

	router := telegram.NewRouter(routerLogger)
	router.Bind(service)
	logger.Info("telegram router bound")

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
	logger.Info("telegram bot client initialized", slog.Int("worker_count", 8))

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
	logger.Info(
		"http server prepared",
		slog.String("http_addr", cfg.HTTPAddr),
		slog.Bool("webhook_enabled", cfg.WebhookEnabled()),
		slog.String("webhook_path", cfg.WebhookPath),
	)

	runner := worker.NewRunner(store, nil, relay.Limits{
		UnknownDeliveryAfter: cfg.StaleDeliveryAfter,
		ExpiredDeliveryPurge: cfg.ExpiredDeliveryRetain,
	}, runnerLogger)
	logger.Info(
		"maintenance runner prepared",
		slog.Duration("unknown_delivery_after", cfg.StaleDeliveryAfter),
		slog.Duration("expired_delivery_purge", cfg.ExpiredDeliveryRetain),
	)

	app := &App{
		cfg:        cfg,
		logger:     logger,
		httpServer: httpSrv,
		store:      store,
		redis:      redisClient,
		bot:        botClient,
		runner:     runner,
	}

	logger.Info(
		"application bootstrap completed",
		slog.String("mode", runMode(cfg)),
		slog.Duration("duration", time.Since(startedAt)),
	)

	return app, nil
}

func (a *App) Run(ctx context.Context) error {
	startedAt := time.Now()
	a.logger.Info(
		"application run started",
		slog.String("mode", runMode(a.cfg)),
		slog.String("http_addr", a.cfg.HTTPAddr),
		slog.Bool("webhook_enabled", a.cfg.WebhookEnabled()),
	)

	a.syncBotCommands(ctx)

	errCh := make(chan error, 1)

	go func() {
		a.logger.Info("http server starting", slog.String("http_addr", a.cfg.HTTPAddr))
		errCh <- a.httpServer.ListenAndServe()
	}()
	go func() {
		a.runner.Run(ctx)
	}()

	if a.cfg.WebhookEnabled() {
		a.logger.Info(
			"registering telegram webhook",
			slog.String("webhook_path", a.cfg.WebhookPath),
		)
		if _, err := a.bot.SetWebhook(ctx, &bot.SetWebhookParams{
			URL:         a.cfg.WebhookURL(),
			SecretToken: a.cfg.WebhookSecret,
		}); err != nil {
			a.logger.Error("set webhook failed", slog.Any("error", err))
			return fmt.Errorf("set webhook: %w", err)
		}
		a.logger.Info("telegram webhook registered", slog.String("webhook_path", a.cfg.WebhookPath))
		go func() {
			a.logger.Info("telegram webhook worker started")
			a.bot.StartWebhook(ctx)
			a.logger.Info("telegram webhook worker stopped", slog.Any("reason", ctx.Err()))
		}()
	} else {
		a.logger.Info("deleting webhook before polling")
		if _, err := a.bot.DeleteWebhook(ctx, &bot.DeleteWebhookParams{}); err != nil {
			a.logger.Warn("delete webhook failed before polling", slog.Any("error", err))
		} else {
			a.logger.Info("telegram webhook deleted before polling")
		}
		go func() {
			a.logger.Info("telegram polling worker started")
			a.bot.Start(ctx)
			a.logger.Info("telegram polling worker stopped", slog.Any("reason", ctx.Err()))
		}()
	}

	select {
	case <-ctx.Done():
		shutdownStartedAt := time.Now()
		a.logger.Info(
			"application shutdown requested",
			slog.Duration("uptime", time.Since(startedAt)),
			slog.Any("reason", ctx.Err()),
		)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := a.httpServer.Shutdown(shutdownCtx)
		if err != nil {
			a.logger.Error("http server shutdown failed", slog.Any("error", err))
		} else {
			a.logger.Info("http server shutdown completed")
		}
		if a.redis != nil {
			if closeErr := a.redis.Close(); closeErr != nil {
				a.logger.Warn("redis close failed", slog.Any("error", closeErr))
			} else {
				a.logger.Info("redis client closed")
			}
			a.redis = nil
		}
		if a.store != nil {
			a.store.Close()
			a.logger.Info("postgres store closed")
			a.store = nil
		}
		if err != nil {
			return err
		}
		a.logger.Info(
			"application shutdown completed",
			slog.Duration("duration", time.Since(shutdownStartedAt)),
			slog.Duration("uptime", time.Since(startedAt)),
		)
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Error(
				"http server stopped unexpectedly",
				slog.Any("error", err),
				slog.Duration("uptime", time.Since(startedAt)),
			)
			return err
		}
		a.logger.Info("http server stopped", slog.Duration("uptime", time.Since(startedAt)))
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

func runMode(cfg config.Config) string {
	if cfg.WebhookEnabled() {
		return "webhook"
	}
	return "polling"
}

func webhookHandler(cfg config.Config, botClient *bot.Bot) http.Handler {
	if !cfg.WebhookEnabled() {
		return nil
	}
	return botClient.WebhookHandler()
}

func (a *App) syncBotCommands(ctx context.Context) {
	if !a.cfg.SyncBotCommands {
		a.logger.Info("skip telegram bot commands sync", slog.Bool("enabled", false))
		return
	}

	syncCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	a.logger.Info(
		"syncing telegram bot commands",
		slog.String("scope", "all_private_chats"),
		slog.Int("command_count", len(telegram.DefaultCommands())),
	)
	if err := telegram.SyncPrivateCommands(syncCtx, a.bot); err != nil {
		a.logger.Warn("sync telegram bot commands failed", slog.Any("error", err))
		return
	}

	a.logger.Info(
		"telegram bot commands synced",
		slog.String("scope", "all_private_chats"),
	)
}
