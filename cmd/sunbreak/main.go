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
	if len(os.Args) > 1 && os.Args[1] == "backfill" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		os.Exit(runBackfillCommand(ctx, os.Args[2:], os.Stdout, os.Stderr))
	}

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
	writeCommandResultTo(os.Stdout, output, logger, value)
}

func writeCommandResultTo(stdout io.Writer, output string, logger *slog.Logger, value map[string]any) {
	if output == "json" {
		writeJSON(stdout, value)
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
			"When local data is too sparse for analysis, use sunbreak backfill probe before calling source APIs directly.",
			"Backfill run and other mutating commands should support dry-run before they write local state.",
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
			"sunbreak backfill probe hackernews --query cloudflare --from 2024-01-01 --to 2026-05-17 --output json",
		},
		"commands": []map[string]any{
			{
				"name":        "backfill probe hackernews",
				"description": "Estimate historical Hacker News Algolia hit counts and time-slice plans without writing local state.",
				"writes":      false,
				"flags": []map[string]any{
					{"name": "query", "type": "string", "required": false, "description": "Single keyword or query to probe"},
					{"name": "keywords", "type": "string", "required": false, "description": "Comma-separated keywords to probe"},
					{"name": "from", "type": "string", "required": false, "description": "Start date/time, YYYY-MM-DD or RFC3339"},
					{"name": "to", "type": "string", "required": false, "description": "End date/time, YYYY-MM-DD or RFC3339. Defaults to now"},
					{"name": "since", "type": "string", "required": false, "description": "Relative window such as 24h, 30d, 52w, or 1y"},
					{"name": "max-slice-hits", "type": "int", "default": 800, "description": "Target maximum hits per planned time slice"},
					{"name": "max-slices", "type": "int", "default": 128, "description": "Maximum planned time slices before truncating"},
					{"name": "output", "type": "enum", "values": []string{"json", "text"}, "default": "json", "description": "Output format"},
				},
			},
		},
	}
}
