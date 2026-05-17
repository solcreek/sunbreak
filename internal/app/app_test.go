package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"sunbreak/internal/config"
	"sunbreak/internal/storage"
)

func TestRunOnceParsesMatchesPersistsAndSearches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", `"pipeline-v1"`)
		w.Header().Set("Last-Modified", "Sun, 17 May 2026 20:30:00 GMT")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Pipeline Feed</title>
    <item>
      <guid>pipeline-1</guid>
      <title>Sunbreak watches SQLite signals</title>
      <link>https://example.com/pipeline-1</link>
      <description>keyword monitoring moves through parsing preprocessing and persistence</description>
      <pubDate>Sun, 17 May 2026 20:15:00 GMT</pubDate>
      <author>sol@example.com</author>
    </item>
  </channel>
</rss>`))
	}))
	defer server.Close()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "pipeline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	enabled := true
	cfg := config.Default()
	cfg.Notifications.Stdout = false
	cfg.Sources = []config.SourceConfig{
		{
			Type:            "rss",
			Name:            "Pipeline Feed",
			URL:             server.URL,
			Enabled:         &enabled,
			IntervalSeconds: 300,
		},
	}
	cfg.Rules = []config.RuleConfig{
		{
			Name:    "SQLite",
			Type:    "keyword",
			Pattern: "sqlite",
			Enabled: &enabled,
		},
	}

	service := New(cfg, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	sources, err := store.ListSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Checkpoint != "pipeline-1" {
		t.Fatalf("expected checkpoint to be persisted, got %q", sources[0].Checkpoint)
	}
	if sources[0].ETag != `"pipeline-v1"` {
		t.Fatalf("expected etag to be persisted, got %q", sources[0].ETag)
	}

	items, err := store.SearchItems(ctx, "sqlite", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 searchable item, got %d", len(items))
	}
	if items[0].Title != "Sunbreak watches SQLite signals" {
		t.Fatalf("unexpected searchable item: %+v", items[0])
	}

	matches, err := store.RecentMatches(ctx, time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Rule.Name != "SQLite" || matches[0].Item.ExternalID != "pipeline-1" {
		t.Fatalf("unexpected match: %+v", matches[0])
	}

	outbox, err := store.PendingOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox) != 1 {
		t.Fatalf("expected 1 outbox notification, got %d", len(outbox))
	}
	if outbox[0].Subject != "Sunbreak match: SQLite" {
		t.Fatalf("unexpected outbox subject: %q", outbox[0].Subject)
	}

	if err := service.RunDigest(ctx); err != nil {
		t.Fatal(err)
	}
	digests, err := store.RecentDigests(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) != 1 {
		t.Fatalf("expected 1 digest, got %d", len(digests))
	}
	if digests[0].Subject != "Sunbreak digest: 1 matches" {
		t.Fatalf("unexpected digest subject: %q", digests[0].Subject)
	}
}
