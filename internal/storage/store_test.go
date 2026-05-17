package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"sunbreak/internal/model"
)

func TestStorePersistsItemRelationsAndLinks(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sourceID, err := store.UpsertSource(ctx, model.Source{
		Type:            "hackernews",
		Name:            "HN",
		Enabled:         true,
		IntervalSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []model.Item{
		{SourceID: sourceID, SourceType: "hackernews", SourceName: "HN", ExternalID: "100", Title: "Root", FetchedAt: time.Now().UTC()},
		{SourceID: sourceID, SourceType: "hackernews", SourceName: "HN", ExternalID: "101", Title: "Root", Content: "Child", FetchedAt: time.Now().UTC()},
		{SourceID: sourceID, SourceType: "hackernews", SourceName: "HN", ExternalID: "102", Title: "Root", Content: "Nested", FetchedAt: time.Now().UTC()},
	} {
		if _, _, err := store.InsertItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}

	err = store.UpsertItemRelations(ctx, sourceID, []model.ItemRelation{
		{RootExternalID: "100", ChildExternalID: "100", RelationType: "root", Depth: 0, Path: "100"},
		{RootExternalID: "100", ParentExternalID: "100", ChildExternalID: "101", RelationType: "comment", Depth: 1, Path: "100/101"},
		{RootExternalID: "100", ParentExternalID: "101", ChildExternalID: "102", RelationType: "comment", Depth: 2, Path: "100/101/102"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertItemLinks(ctx, sourceID, []model.ItemLink{
		{ItemExternalID: "101", URL: "https://example.com/a#ignored", NormalizedURL: "https://example.com/a", AnchorText: "A"},
		{ItemExternalID: "101", URL: "https://example.com/a#ignored", NormalizedURL: "https://example.com/a", AnchorText: "A"},
	}); err != nil {
		t.Fatal(err)
	}

	relations, err := store.ItemRelations(ctx, sourceID, "100")
	if err != nil {
		t.Fatal(err)
	}
	if len(relations) != 3 {
		t.Fatalf("expected 3 relations, got %d", len(relations))
	}
	if relations[2].ParentExternalID != "101" || relations[2].ChildExternalID != "102" || relations[2].Depth != 2 {
		t.Fatalf("unexpected nested relation: %+v", relations[2])
	}
	links, err := store.ItemLinks(ctx, sourceID, "101")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected de-duped link, got %+v", links)
	}
	if links[0].NormalizedURL != "https://example.com/a" || links[0].AnchorText != "A" {
		t.Fatalf("unexpected link: %+v", links[0])
	}
}

func TestStoreCRUDAndQueryPaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sourceID, err := store.UpsertSource(ctx, model.Source{
		Type:            "rss",
		Name:            "Feed",
		URL:             "https://example.com/feed.xml",
		Enabled:         true,
		IntervalSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := store.ListSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].ID != sourceID {
		t.Fatalf("unexpected sources: %+v", sources)
	}
	due, err := store.DueSources(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("expected due source, got %+v", due)
	}
	nextRun := time.Now().Add(time.Hour)
	if err := store.MarkSourceSuccess(ctx, sourceID, "checkpoint", "etag", "modified", nextRun); err != nil {
		t.Fatal(err)
	}
	sources, err = store.ListSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sources[0].Checkpoint != "checkpoint" || sources[0].ETag != "etag" || sources[0].LastModified != "modified" {
		t.Fatalf("source success metadata not persisted: %+v", sources[0])
	}
	if err := store.MarkSourceFailure(ctx, sourceID, "temporary failure", time.Now()); err != nil {
		t.Fatal(err)
	}
	sources, err = store.ListSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sources[0].ErrorCount != 1 || sources[0].LastError != "temporary failure" {
		t.Fatalf("source failure metadata not persisted: %+v", sources[0])
	}

	itemID, inserted, err := store.InsertItem(ctx, model.Item{
		SourceID:    sourceID,
		SourceType:  "rss",
		SourceName:  "Feed",
		ExternalID:  "item-1",
		URL:         "https://example.com/item-1",
		Title:       "SQLite monitoring",
		Content:     "public data pipeline",
		Author:      "alice",
		PublishedAt: time.Now().UTC(),
		FetchedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inserted || itemID == 0 {
		t.Fatalf("expected inserted item, got id=%d inserted=%v", itemID, inserted)
	}
	if _, inserted, err = store.InsertItem(ctx, model.Item{
		SourceID:   sourceID,
		ExternalID: "item-1",
		FetchedAt:  time.Now().UTC(),
	}); err != nil || inserted {
		t.Fatalf("expected duplicate item ignore, inserted=%v err=%v", inserted, err)
	}
	items, err := store.SearchItems(ctx, "sqlite", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != itemID {
		t.Fatalf("unexpected search items: %+v", items)
	}
	items, err = store.SearchItems(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected recent item search fallback, got %+v", items)
	}

	ruleID, err := store.UpsertRule(ctx, model.Rule{Name: "SQLite", Pattern: "sqlite", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.ActiveRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Type != "keyword" {
		t.Fatalf("unexpected active rules: %+v", active)
	}
	rules, err := store.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != ruleID {
		t.Fatalf("unexpected rules: %+v", rules)
	}

	saved, err := store.InsertMatch(ctx, model.Match{ItemID: itemID, RuleID: ruleID, MatchedText: "sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if !saved {
		t.Fatal("expected match insert")
	}
	if saved, err = store.InsertMatch(ctx, model.Match{ItemID: itemID, RuleID: ruleID, MatchedText: "sqlite"}); err != nil || saved {
		t.Fatalf("expected duplicate match ignore, saved=%v err=%v", saved, err)
	}
	matches, err := store.RecentMatches(ctx, time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Item.ExternalID != "item-1" || matches[0].Rule.Name != "SQLite" {
		t.Fatalf("unexpected matches: %+v", matches)
	}

	outboxID, err := store.InsertOutbox(ctx, model.OutboxMessage{Subject: "Subject", Body: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.PendingOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != outboxID {
		t.Fatalf("unexpected pending outbox: %+v", pending)
	}
	if err := store.MarkOutboxRetry(ctx, outboxID, "try again", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkOutboxSent(ctx, outboxID); err != nil {
		t.Fatal(err)
	}
	pending, err = store.PendingOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected no pending outbox, got %+v", pending)
	}

	digestID, err := store.SaveDigest(ctx, model.Digest{
		WindowStart: time.Now().Add(-time.Hour),
		WindowEnd:   time.Now(),
		Subject:     "Digest",
		Body:        "Body",
	})
	if err != nil {
		t.Fatal(err)
	}
	digests, err := store.RecentDigests(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) != 1 || digests[0].ID != digestID {
		t.Fatalf("unexpected digests: %+v", digests)
	}
}

func TestStoreHelpers(t *testing.T) {
	if FormatError(nil) != "" {
		t.Fatal("expected empty nil error format")
	}
	if FormatError(context.Canceled) == "" {
		t.Fatal("expected non-empty error format")
	}
	if parseDBTime("not-a-time") != (time.Time{}) {
		t.Fatal("expected invalid DB time to return zero")
	}
}
