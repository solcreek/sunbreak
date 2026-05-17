package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"radar/internal/app"
	"radar/internal/config"
	"radar/internal/storage"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config YAML")
	dbPath := flag.String("db", "", "Override SQLite database path")
	collectOnce := flag.Bool("collect-once", false, "Run one due-source collection pass and exit")
	digestOnce := flag.Bool("digest-once", false, "Build one digest and exit")
	dispatchOutbox := flag.Bool("dispatch-outbox", false, "Dispatch pending outbox messages and exit")
	migrateOnly := flag.Bool("migrate", false, "Open database, run migrations, and exit")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	if *dbPath != "" {
		cfg.Database.Path = *dbPath
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.Open(ctx, cfg.Database.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	if *migrateOnly {
		logger.Info("database migrated", "path", cfg.Database.Path, "fts", store.HasFTS())
		return
	}

	service := app.New(cfg, store, logger)
	switch {
	case *collectOnce:
		if err := service.RunOnce(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "collect once: %v\n", err)
			os.Exit(1)
		}
	case *digestOnce:
		if err := service.Seed(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "seed config: %v\n", err)
			os.Exit(1)
		}
		if err := service.RunDigest(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "digest once: %v\n", err)
			os.Exit(1)
		}
	case *dispatchOutbox:
		if err := service.RunOutbox(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "dispatch outbox: %v\n", err)
			os.Exit(1)
		}
	default:
		if err := service.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "run service: %v\n", err)
			os.Exit(1)
		}
	}
}
