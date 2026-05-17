package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sunbreak/internal/model"
)

var itemSink []model.Item
var matchSink []model.Match

func BenchmarkStoreInsertItem(b *testing.B) {
	ctx := context.Background()
	store, sourceID := benchmarkStore(b, "insert-item")
	defer store.Close()

	content := benchmarkContent(2048)
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, inserted, err := store.InsertItem(ctx, model.Item{
			SourceID:    sourceID,
			SourceType:  "rss",
			SourceName:  "Benchmark Feed",
			ExternalID:  fmt.Sprintf("insert-%d", i),
			URL:         fmt.Sprintf("https://example.com/items/%d", i),
			Title:       fmt.Sprintf("SQLite monitoring item %d", i),
			Content:     content,
			Author:      "benchmark",
			PublishedAt: time.Unix(int64(i), 0).UTC(),
			FetchedAt:   time.Now().UTC(),
		})
		if err != nil {
			b.Fatal(err)
		}
		if !inserted {
			b.Fatal("expected item insert")
		}
	}
}

func BenchmarkStoreSearchItems(b *testing.B) {
	ctx := context.Background()
	store, sourceID := benchmarkStore(b, "search-items")
	defer store.Close()
	_ = seedItems(b, store, sourceID, 10000)
	if !store.HasFTS() {
		b.Skip("SQLite FTS5 is not enabled; run with -tags sqlite_fts5")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := store.SearchItems(ctx, "sqlite", 50)
		if err != nil {
			b.Fatal(err)
		}
		itemSink = items
	}
}

func BenchmarkStoreRecentMatches(b *testing.B) {
	ctx := context.Background()
	store, sourceID := benchmarkStore(b, "recent-matches")
	defer store.Close()
	ruleID, err := store.UpsertRule(ctx, model.Rule{
		Name:    "SQLite",
		Type:    "keyword",
		Pattern: "sqlite",
		Enabled: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	itemIDs := seedItems(b, store, sourceID, 5000)
	for i, itemID := range itemIDs {
		if _, err := store.InsertMatch(ctx, model.Match{
			ItemID:      itemID,
			RuleID:      ruleID,
			MatchedText: fmt.Sprintf("sqlite-%d", i),
			Score:       1,
		}); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matches, err := store.RecentMatches(ctx, time.Now().Add(-24*time.Hour), 100)
		if err != nil {
			b.Fatal(err)
		}
		matchSink = matches
	}
}

func benchmarkStore(b *testing.B, name string) (*Store, int64) {
	b.Helper()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(b.TempDir(), name+".db"))
	if err != nil {
		b.Fatal(err)
	}
	sourceID, err := store.UpsertSource(ctx, model.Source{
		Type:            "rss",
		Name:            "Benchmark Feed",
		URL:             "https://example.com/feed.xml",
		Enabled:         true,
		IntervalSeconds: 300,
	})
	if err != nil {
		_ = store.Close()
		b.Fatal(err)
	}
	return store, sourceID
}

func seedItems(b *testing.B, store *Store, sourceID int64, count int) []int64 {
	b.Helper()
	ctx := context.Background()
	content := benchmarkContent(1024)
	now := time.Now().UTC()
	ids := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		term := "collector"
		if i%10 == 0 {
			term = "sqlite"
		}
		id, _, err := store.InsertItem(ctx, model.Item{
			SourceID:    sourceID,
			SourceType:  "rss",
			SourceName:  "Benchmark Feed",
			ExternalID:  fmt.Sprintf("seed-%d", i),
			URL:         fmt.Sprintf("https://example.com/seed/%d", i),
			Title:       fmt.Sprintf("%s monitoring item %d", term, i),
			Content:     content,
			Author:      "benchmark",
			PublishedAt: now.Add(-time.Duration(i) * time.Second),
			FetchedAt:   now.Add(-time.Duration(i) * time.Second),
		})
		if err != nil {
			b.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func benchmarkContent(size int) string {
	chunk := "public data ingestion source checkpoint outbox digest query filtering "
	var builder strings.Builder
	for builder.Len() < size {
		builder.WriteString(chunk)
	}
	return builder.String()
}
