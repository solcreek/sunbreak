package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sunbreak/internal/model"
)

func TestFeedCollectorRSSConditionalRequest(t *testing.T) {
	var sawIfNoneMatch bool
	var sawIfModifiedSince bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/feed.xml" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		sawIfNoneMatch = r.Header.Get("If-None-Match") == `"feed-v1"`
		sawIfModifiedSince = r.Header.Get("If-Modified-Since") == "Sun, 17 May 2026 20:00:00 GMT"
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", `"feed-v2"`)
		w.Header().Set("Last-Modified", "Sun, 17 May 2026 20:10:00 GMT")
		_, _ = w.Write([]byte(rssFixture))
	}))
	defer server.Close()

	source := model.Source{
		ID:           7,
		Type:         "rss",
		Name:         "Test Feed",
		URL:          server.URL + "/feed.xml",
		ETag:         `"feed-v1"`,
		LastModified: "Sun, 17 May 2026 20:00:00 GMT",
	}
	result, err := NewFeedCollector(server.Client()).Collect(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if !sawIfNoneMatch || !sawIfModifiedSince {
		t.Fatalf("conditional headers were not sent: etag=%v last_modified=%v", sawIfNoneMatch, sawIfModifiedSince)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
	if result.Checkpoint != "rss-2" {
		t.Fatalf("expected latest checkpoint rss-2, got %q", result.Checkpoint)
	}
	if result.ETag != `"feed-v2"` {
		t.Fatalf("expected response etag, got %q", result.ETag)
	}
	if result.LastModified != "Sun, 17 May 2026 20:10:00 GMT" {
		t.Fatalf("expected response last-modified, got %q", result.LastModified)
	}
	item := result.Items[0]
	if item.SourceID != source.ID || item.SourceName != source.Name || item.ExternalID != "rss-2" {
		t.Fatalf("unexpected normalized item: %+v", item)
	}
	if item.Title != "Second signal" || !strings.Contains(item.Content, "sqlite") {
		t.Fatalf("unexpected normalized content: %+v", item)
	}
	if item.PublishedAt.IsZero() {
		t.Fatal("expected published_at to be parsed")
	}
}

func TestFeedCollectorNotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	source := model.Source{
		Type:         "rss",
		Name:         "Not Modified",
		URL:          server.URL,
		Checkpoint:   "existing-checkpoint",
		ETag:         `"feed-v1"`,
		LastModified: "Sun, 17 May 2026 20:00:00 GMT",
	}
	result, err := NewFeedCollector(server.Client()).Collect(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("expected no items, got %d", len(result.Items))
	}
	if result.Checkpoint != source.Checkpoint || result.ETag != source.ETag || result.LastModified != source.LastModified {
		t.Fatalf("expected checkpoint metadata to be preserved: %+v", result)
	}
}

func TestFeedCollectorRejectsBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := NewFeedCollector(server.Client()).Collect(context.Background(), model.Source{
		Type: "rss",
		Name: "Rate Limited",
		URL:  server.URL,
	})
	if err == nil {
		t.Fatal("expected status error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected status code in error, got %v", err)
	}
}

func TestParseAtomFeed(t *testing.T) {
	source := model.Source{ID: 3, Type: "atom", Name: "Atom Feed"}
	items, checkpoint, err := parseFeed([]byte(atomFixture), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 atom item, got %d", len(items))
	}
	if checkpoint != "tag:example.com,2026:sunbreak" {
		t.Fatalf("unexpected checkpoint %q", checkpoint)
	}
	if items[0].URL != "https://example.com/sunbreak" {
		t.Fatalf("unexpected atom URL %q", items[0].URL)
	}
	if items[0].PublishedAt.IsZero() {
		t.Fatal("expected atom published_at to be parsed")
	}
}

func TestParseFeedStopsAtCheckpoint(t *testing.T) {
	source := model.Source{ID: 7, Type: "rss", Name: "Test Feed", Checkpoint: "rss-1"}
	items, checkpoint, err := parseFeed([]byte(rssFixture), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 new item before checkpoint, got %d", len(items))
	}
	if items[0].ExternalID != "rss-2" {
		t.Fatalf("unexpected item before checkpoint: %+v", items[0])
	}
	if checkpoint != "rss-2" {
		t.Fatalf("expected latest checkpoint rss-2, got %q", checkpoint)
	}
}

func TestParseFeedTimeLayouts(t *testing.T) {
	if got := parseFeedTime(time.RFC3339); !got.IsZero() {
		t.Fatalf("expected invalid literal layout to return zero time, got %s", got)
	}
	if got := parseFeedTime("2026-05-17T20:00:00Z"); got.IsZero() {
		t.Fatal("expected RFC3339 timestamp to parse")
	}
	if got := parseFeedTime("Sun, 17 May 2026 20:00:00 GMT"); got.IsZero() {
		t.Fatal("expected RFC1123 timestamp to parse")
	}
}

func TestFeedFallbackHelpers(t *testing.T) {
	rss := rssFeed{}
	rss.Channel.Items = []rssItem{{Title: "Title", Description: "Description", PubDate: "Date"}}
	checkpoint := latestRSSCheckpoint(rss, "fallback")
	if checkpoint == "" || checkpoint == "fallback" {
		t.Fatalf("expected hash checkpoint fallback, got %q", checkpoint)
	}
	if latestRSSCheckpoint(rssFeed{}, "fallback") != "fallback" {
		t.Fatal("expected empty RSS checkpoint fallback")
	}

	atom := atomFeed{Entries: []atomEntry{{Title: "Title", Summary: "Summary", Updated: "Date"}}}
	checkpoint = latestAtomCheckpoint(atom, "fallback")
	if checkpoint == "" || checkpoint == "fallback" {
		t.Fatalf("expected atom hash checkpoint fallback, got %q", checkpoint)
	}
	if latestAtomCheckpoint(atomFeed{}, "fallback") != "fallback" {
		t.Fatal("expected empty atom checkpoint fallback")
	}
	if firstAtomLink([]atomLink{{Rel: "self", Href: "https://example.com/self"}}) != "https://example.com/self" {
		t.Fatal("expected first atom link fallback")
	}
}

func TestParseFeedUnsupported(t *testing.T) {
	items, checkpoint, err := parseFeed([]byte(`<html></html>`), model.Source{Checkpoint: "old"})
	if err == nil {
		t.Fatal("expected unsupported feed error")
	}
	if items != nil || checkpoint != "old" {
		t.Fatalf("expected checkpoint preservation, got items=%+v checkpoint=%q", items, checkpoint)
	}
}

const rssFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Sunbreak Test Feed</title>
    <item>
      <guid>rss-2</guid>
      <title>Second signal</title>
      <link>https://example.com/second</link>
      <description>sqlite monitoring update</description>
      <pubDate>Sun, 17 May 2026 20:05:00 GMT</pubDate>
      <author>editor@example.com</author>
    </item>
    <item>
      <guid>rss-1</guid>
      <title>First signal</title>
      <link>https://example.com/first</link>
      <description>collector checkpoint update</description>
      <pubDate>Sun, 17 May 2026 20:00:00 GMT</pubDate>
      <author>editor@example.com</author>
    </item>
  </channel>
</rss>`

const atomFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Sunbreak Atom Feed</title>
  <entry>
    <id>tag:example.com,2026:sunbreak</id>
    <title>Sunbreak release</title>
    <link href="https://example.com/sunbreak" rel="alternate"></link>
    <summary>signal in the fog</summary>
    <published>2026-05-17T20:00:00Z</published>
    <author><name>Sol Creek</name></author>
  </entry>
</feed>`
