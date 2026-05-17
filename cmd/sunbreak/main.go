package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"sunbreak/internal/app"
	"sunbreak/internal/config"
	"sunbreak/internal/storage"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config YAML")
	dbPath := flag.String("db", "", "Override SQLite database path")
	collectOnce := flag.Bool("collect-once", false, "Run one due-source collection pass and exit")
	digestOnce := flag.Bool("digest-once", false, "Build one digest and exit")
	dispatchOutbox := flag.Bool("dispatch-outbox", false, "Dispatch pending outbox messages and exit")
	migrateOnly := flag.Bool("migrate", false, "Open database, run migrations, and exit")
	output := flag.String("output", "text", "Output format for one-shot commands: text or json")
	describe := flag.Bool("describe", false, "Print machine-readable CLI schema and exit")
	flag.Parse()

	if *describe {
		writeJSON(os.Stdout, cliDescription())
		return
	}
	logWriter := io.Writer(os.Stdout)
	if *output == "json" {
		logWriter = os.Stderr
	}
	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: slog.LevelInfo}))
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
		writeCommandResult(*output, logger, map[string]any{
			"ok":            true,
			"command":       "migrate",
			"database_path": cfg.Database.Path,
			"fts":           store.HasFTS(),
		})
		return
	}

	service := app.New(cfg, store, logger)
	switch {
	case *collectOnce:
		if err := service.RunOnce(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "collect once: %v\n", err)
			os.Exit(1)
		}
		writeCommandResult(*output, logger, map[string]any{"ok": true, "command": "collect-once"})
	case *digestOnce:
		if err := service.Seed(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "seed config: %v\n", err)
			os.Exit(1)
		}
		if err := service.RunDigest(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "digest once: %v\n", err)
			os.Exit(1)
		}
		writeCommandResult(*output, logger, map[string]any{"ok": true, "command": "digest-once"})
	case *dispatchOutbox:
		if err := service.RunOutbox(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "dispatch outbox: %v\n", err)
			os.Exit(1)
		}
		writeCommandResult(*output, logger, map[string]any{"ok": true, "command": "dispatch-outbox"})
	default:
		if err := service.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "run service: %v\n", err)
			os.Exit(1)
		}
	}
}

func writeCommandResult(output string, logger *slog.Logger, value map[string]any) {
	if output == "json" {
		writeJSON(os.Stdout, value)
		return
	}
	logger.Info("command completed", "command", value["command"])
}

func writeJSON(w io.Writer, value any) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func cliDescription() map[string]any {
	return map[string]any{
		"name": "sunbreak",
		"agent_guidance": []string{
			"Prefer -output json for one-shot commands.",
			"Treat stdout as data and stderr as diagnostics when -output json is used.",
			"Use -describe to introspect supported flags at runtime instead of relying on stale documentation.",
			"Backfill and mutating commands should support dry-run before they write local state.",
		},
		"flags": []map[string]any{
			{"name": "config", "type": "string", "default": "config.yaml", "description": "Path to config YAML"},
			{"name": "db", "type": "string", "default": "", "description": "Override SQLite database path"},
			{"name": "collect-once", "type": "bool", "default": false, "description": "Run one due-source collection pass and exit"},
			{"name": "digest-once", "type": "bool", "default": false, "description": "Build one digest and exit"},
			{"name": "dispatch-outbox", "type": "bool", "default": false, "description": "Dispatch pending outbox messages and exit"},
			{"name": "migrate", "type": "bool", "default": false, "description": "Open database, run migrations, and exit"},
			{"name": "output", "type": "enum", "values": []string{"text", "json"}, "default": "text", "description": "Output format for one-shot commands"},
			{"name": "describe", "type": "bool", "default": false, "description": "Print machine-readable CLI schema and exit"},
		},
		"examples": []string{
			"sunbreak -describe",
			"sunbreak -config config.yaml -migrate -output json",
			"sunbreak -config config.yaml -collect-once -output json",
		},
	}
}
