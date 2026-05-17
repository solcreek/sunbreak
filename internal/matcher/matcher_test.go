package matcher

import (
	"strings"
	"testing"

	"sunbreak/internal/model"
)

func TestMatcherMatchesKeywordAndRegex(t *testing.T) {
	m, err := New([]model.Rule{
		{ID: 1, Name: "SQLite", Type: "keyword", Pattern: "sqlite", Enabled: true},
		{ID: 2, Name: "Version", Type: "regex", Pattern: `v\d+\.\d+`, Enabled: true},
		{ID: 3, Name: "Disabled", Type: "keyword", Pattern: "hidden", Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	results := m.Match(model.Item{
		Title:   "SQLite release",
		Content: "Version v3.45 improves monitoring",
		URL:     "https://example.com/sqlite",
	})
	if len(results) != 2 {
		t.Fatalf("expected two matches, got %+v", results)
	}
	if results[0].Rule.Name != "SQLite" || results[0].MatchedText != "sqlite" {
		t.Fatalf("unexpected keyword match: %+v", results[0])
	}
	if results[1].Rule.Name != "Version" || results[1].MatchedText != "v3.45" {
		t.Fatalf("unexpected regex match: %+v", results[1])
	}
}

func TestMatcherCaseSensitiveKeyword(t *testing.T) {
	m, err := New([]model.Rule{
		{ID: 1, Name: "Go", Type: "keyword", Pattern: "Go", CaseSensitive: true, Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Match(model.Item{Title: "go release"}); len(got) != 0 {
		t.Fatalf("expected no lowercase match, got %+v", got)
	}
	if got := m.Match(model.Item{Title: "Go release"}); len(got) != 1 {
		t.Fatalf("expected case-sensitive match, got %+v", got)
	}
}

func TestMatcherRejectsInvalidRules(t *testing.T) {
	tests := []model.Rule{
		{Name: "Empty", Type: "keyword", Pattern: " ", Enabled: true},
		{Name: "BadRegex", Type: "regex", Pattern: "(", Enabled: true},
		{Name: "BadType", Type: "semantic", Pattern: "x", Enabled: true},
	}
	for _, rule := range tests {
		_, err := New([]model.Rule{rule})
		if err == nil {
			t.Fatalf("expected error for %+v", rule)
		}
		if strings.TrimSpace(err.Error()) == "" {
			t.Fatalf("expected useful error for %+v", rule)
		}
	}
}
