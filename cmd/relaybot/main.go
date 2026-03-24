package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"relaybot/internal/bootstrap"
	"relaybot/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	app, err := bootstrap.New(ctx, cfg)
	if err != nil {
		log.Fatalf("bootstrap app: %v", err)
	}
	defer app.Close()

	if err := app.Run(ctx); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
