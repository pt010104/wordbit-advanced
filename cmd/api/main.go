package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"wordbit-advanced-app/backend/internal/config"
	"wordbit-advanced-app/backend/internal/database"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "migrate":
		direction := "up"
		if len(os.Args) > 2 {
			direction = os.Args[2]
		}
		if err := database.RunMigrations(ctx, cfg.DatabaseURL, direction); err != nil {
			slog.Error("run migrations", "error", err)
			os.Exit(1)
		}
	default:
		if err := runServer(ctx, cfg); err != nil {
			slog.Error("run server", "error", err)
			os.Exit(1)
		}
	}
}
