package digest

import (
	"strings"
	"testing"
	"time"

	"sunbreak/internal/model"
)

func TestBuildEmptyDigest(t *testing.T) {
	start := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	d := Build(nil, start, end)

	if d.Subject != "Sunbreak digest: no matches" {
		t.Fatalf("unexpected subject: %q", d.Subject)
	}
	if !strings.Contains(d.Body, "Matches: 0") {
		t.Fatalf("unexpected body: %q", d.Body)
	}
	if d.CreatedAt.IsZero() {
		t.Fatal("expected created timestamp")
	}
}

func TestBuildGroupsAndLimitsMatches(t *testing.T) {
	start := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	var matches []model.Match
	for i := 0; i < 12; i++ {
		matches = append(matches, model.Match{
			Rule: model.Rule{Name: "SQLite"},
			Item: model.Item{SourceName: "HN", Title: strings.Repeat("A", 130), URL: "https://example.com/item"},
		})
	}
	matches = append(matches, model.Match{
		Rule: model.Rule{Name: "Go"},
		Item: model.Item{SourceName: "RSS", Content: "Go release notes"},
	})

	d := Build(matches, start, start.Add(time.Hour))
	if d.Subject != "Sunbreak digest: 13 matches" {
		t.Fatalf("unexpected subject: %q", d.Subject)
	}
	if !strings.Contains(d.Body, "Rule: Go (1)") || !strings.Contains(d.Body, "Rule: SQLite (12)") {
		t.Fatalf("expected grouped rules, got body:\n%s", d.Body)
	}
	if !strings.Contains(d.Body, "- ... and 2 more") {
		t.Fatalf("expected overflow marker, got body:\n%s", d.Body)
	}
	if !strings.Contains(d.Body, "AAA...") {
		t.Fatalf("expected long title truncation, got body:\n%s", d.Body)
	}
}
