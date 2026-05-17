package collector

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"sunbreak/internal/model"
)

func TestRegistryReturnsCollectors(t *testing.T) {
	registry := NewRegistry(http.DefaultClient, RedditOptions{})
	for _, sourceType := range []string{"rss", "atom", "hackernews", "reddit"} {
		if _, err := registry.Get(sourceType); err != nil {
			t.Fatalf("expected collector for %s: %v", sourceType, err)
		}
	}
	if _, err := registry.Get("unknown"); err == nil {
		t.Fatal("expected unsupported source error")
	}
}

func TestRedditCollectorMockMode(t *testing.T) {
	collector := NewRedditCollector(http.DefaultClient, RedditOptions{})
	result, err := collector.Collect(context.Background(), model.Source{
		ID:   7,
		Type: "reddit",
		Name: "Reddit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected mock item, got %+v", result.Items)
	}
	if result.Items[0].SourceID != 7 || result.Checkpoint == "" {
		t.Fatalf("unexpected mock result: %+v", result)
	}
}

func TestRedditCollectorPreservesCheckpointInMockMode(t *testing.T) {
	collector := NewRedditCollector(http.DefaultClient, RedditOptions{})
	result, err := collector.Collect(context.Background(), model.Source{
		Checkpoint:   "reddit-adapter-mock-v1",
		ETag:         "etag",
		LastModified: "modified",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 0 || result.Checkpoint != "reddit-adapter-mock-v1" || result.ETag != "etag" || result.LastModified != "modified" {
		t.Fatalf("unexpected checkpoint result: %+v", result)
	}
}

func TestRedditCollectorRequiresCredentialedImplementation(t *testing.T) {
	collector := NewRedditCollector(http.DefaultClient, RedditOptions{
		ClientID:     "id",
		ClientSecret: "secret",
		UserAgent:    "sunbreak-test",
	})
	_, err := collector.Collect(context.Background(), model.Source{})
	if err == nil {
		t.Fatal("expected implementation error")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}
