package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"relaybot/internal/bootstrap"
	"relaybot/internal/config"
)

func main() {
	os.Exit(run())
}

func run() int {
	baseLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(baseLogger)

	logger := baseLogger.With(slog.String("component", "main"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startedAt := time.Now()
	logger.Info("relaybot startup began")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config failed", slog.Any("error", err))
		return 1
	}
	logger.Info(
		"config loaded",
		slog.Bool("webhook_enabled", cfg.WebhookEnabled()),
		slog.String("http_addr", cfg.HTTPAddr),
		slog.String("webhook_path", cfg.WebhookPath),
	)

	app, err := bootstrap.New(ctx, cfg)
	if err != nil {
		logger.Error("bootstrap app failed", slog.Any("error", err))
		return 1
	}
	defer func() {
		if err := app.Close(); err != nil {
			logger.Warn("application close failed", slog.Any("error", err))
		}
	}()
	logger.Info("application bootstrapped", slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()))

	logger.Info("application run loop starting")
	if err := app.Run(ctx); err != nil {
		logger.Error("application stopped with error", slog.Any("error", err))
		return 1
	}

	logger.Info("relaybot stopped gracefully", slog.Int64("uptime_ms", time.Since(startedAt).Milliseconds()))
	return 0
}
