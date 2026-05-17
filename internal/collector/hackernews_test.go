package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sunbreak/internal/model"
)

func TestHNCollectorCollectsAlgoliaItemsWhenCommentsDisabled(t *testing.T) {
	var sawQuery bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("hitsPerPage") != "50" {
			t.Fatalf("expected hitsPerPage=50, got %q", r.URL.Query().Get("hitsPerPage"))
		}
		if r.URL.Query().Get("tags") != "(story,comment)" {
			t.Fatalf("expected tags=(story,comment), got %q", r.URL.Query().Get("tags"))
		}
		if r.URL.Query().Get("numericFilters") != "created_at_i>100" {
			t.Fatalf("expected checkpoint numeric filter, got %q", r.URL.Query().Get("numericFilters"))
		}
		sawQuery = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"nbPages": 1,
			"hits": [
				{
					"objectID": "101",
					"title": "SQLite monitoring",
					"url": "https://example.com/sqlite",
					"author": "alice",
					"created_at": "2026-05-17T20:00:00Z",
					"created_at_i": 101
				},
				{
					"objectID": "103",
					"story_title": "Signal scout",
					"story_url": "https://example.com/scout",
					"author": "bob",
					"created_at_i": 103
				}
			]
		}`))
	}))
	defer server.Close()

	collector := NewHNCollector(server.Client())
	collector.searchEndpoint = server.URL + "/search"
	result, err := collector.Collect(context.Background(), model.Source{
		ID:         9,
		Type:       "hackernews",
		Name:       "HN",
		Checkpoint: "100",
		ConfigJSON: `{"include_comments":false,"overlap_seconds":0}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawQuery {
		t.Fatal("test server did not see query")
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
	if result.Checkpoint != "103" {
		t.Fatalf("expected checkpoint 103, got %q", result.Checkpoint)
	}
	if result.Items[0].ExternalID != "101" || result.Items[0].Title != "SQLite monitoring" {
		t.Fatalf("unexpected first item: %+v", result.Items[0])
	}
	if result.Items[1].URL != "https://example.com/scout" {
		t.Fatalf("unexpected story URL fallback: %+v", result.Items[1])
	}
}

func TestHNCollectorCollectsThreadRelationsAndLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{
				"nbPages": 1,
				"hits": [
					{"objectID": "100", "title": "Thread root", "story_id": 100, "created_at_i": 200},
					{"objectID": "101", "story_title": "Thread root", "story_id": 100, "comment_text": "linked comment", "created_at_i": 201}
				]
			}`))
		case "/item/100.json":
			_, _ = w.Write([]byte(`{
				"id": 100,
				"type": "story",
				"by": "alice",
				"time": 1779050000,
				"title": "Thread root",
				"url": "https://example.com/root",
				"kids": [101, 102]
			}`))
		case "/item/101.json":
			_, _ = w.Write([]byte(`{
				"id": 101,
				"type": "comment",
				"by": "bob",
				"time": 1779050100,
				"parent": 100,
				"text": "First <a href=\"https://example.com/a?x=1#frag\">useful link</a>",
				"kids": [103]
			}`))
		case "/item/102.json":
			_, _ = w.Write([]byte(`{
				"id": 102,
				"type": "comment",
				"by": "carol",
				"time": 1779050200,
				"parent": 100,
				"text": "Plain https://example.com/plain."
			}`))
		case "/item/103.json":
			_, _ = w.Write([]byte(`{
				"id": 103,
				"type": "comment",
				"by": "dave",
				"time": 1779050300,
				"parent": 101,
				"text": "Nested reply"
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	collector := NewHNCollector(server.Client())
	collector.searchEndpoint = server.URL + "/search"
	collector.itemEndpoint = server.URL + "/item/%d.json"
	result, err := collector.Collect(context.Background(), model.Source{
		ID:         9,
		Type:       "hackernews",
		Name:       "HN",
		ConfigJSON: `{"max_stories_per_run":1,"max_comments_per_story":10}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 4 {
		t.Fatalf("expected 4 thread items, got %d", len(result.Items))
	}
	if len(result.Relations) != 4 {
		t.Fatalf("expected 4 relations, got %d", len(result.Relations))
	}
	var nested model.ItemRelation
	for _, relation := range result.Relations {
		if relation.ChildExternalID == "103" {
			nested = relation
			break
		}
	}
	if nested.ChildExternalID != "103" || nested.Depth != 2 || nested.Path != "100/101/103" {
		t.Fatalf("nested relation was not preserved: %+v", nested)
	}
	var sawAnchor, sawPlain bool
	for _, link := range result.Links {
		if link.ItemExternalID == "101" && link.NormalizedURL == "https://example.com/a?x=1" && link.AnchorText == "useful link" {
			sawAnchor = true
		}
		if link.ItemExternalID == "102" && link.NormalizedURL == "https://example.com/plain" {
			sawPlain = true
		}
	}
	if !sawAnchor || !sawPlain {
		t.Fatalf("expected extracted comment links, got %+v", result.Links)
	}
}

func TestHNCollectorRejectsBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	collector := NewHNCollector(server.Client())
	collector.searchEndpoint = server.URL
	_, err := collector.Collect(context.Background(), model.Source{
		Type:       "hackernews",
		Name:       "HN",
		ConfigJSON: `{"include_comments":false}`,
	})
	if err == nil {
		t.Fatal("expected status error")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected status code in error, got %v", err)
	}
}
