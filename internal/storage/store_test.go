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
