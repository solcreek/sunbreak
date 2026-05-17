package matcher

import (
	"fmt"
	"strings"
	"testing"

	"radar/internal/model"
)

var benchmarkSink []Result

func BenchmarkMatcherMatch(b *testing.B) {
	cases := []struct {
		name       string
		keywords   int
		regexRules int
	}{
		{name: "keywords_100", keywords: 100},
		{name: "keywords_1000", keywords: 1000},
		{name: "mixed_1000_keywords_50_regex", keywords: 1000, regexRules: 50},
	}

	item := model.Item{
		Title:   "SQLite based keyword monitoring engine",
		Content: benchmarkText(4096) + " radar-term-42 alternative to f5bot sqlite monitoring",
		URL:     "https://example.com/radar-term-42",
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			matcher, err := New(benchmarkRules(tc.keywords, tc.regexRules))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(item.Title) + len(item.Content) + len(item.URL)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkSink = matcher.Match(item)
			}
		})
	}
}

func BenchmarkMatcherCompile(b *testing.B) {
	cases := []struct {
		name       string
		keywords   int
		regexRules int
	}{
		{name: "keywords_1000", keywords: 1000},
		{name: "mixed_1000_keywords_100_regex", keywords: 1000, regexRules: 100},
	}

	for _, tc := range cases {
		rules := benchmarkRules(tc.keywords, tc.regexRules)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m, err := New(rules)
				if err != nil {
					b.Fatal(err)
				}
				if m == nil {
					b.Fatal("nil matcher")
				}
			}
		})
	}
}

func benchmarkRules(keywordCount, regexCount int) []model.Rule {
	rules := make([]model.Rule, 0, keywordCount+regexCount)
	for i := 0; i < keywordCount; i++ {
		pattern := fmt.Sprintf("radar-term-%d", i)
		if i%3 == 0 {
			pattern = fmt.Sprintf("unused-term-%d", i)
		}
		rules = append(rules, model.Rule{
			ID:      int64(i + 1),
			Name:    fmt.Sprintf("keyword-%d", i),
			Type:    "keyword",
			Pattern: pattern,
			Enabled: true,
		})
	}
	for i := 0; i < regexCount; i++ {
		rules = append(rules, model.Rule{
			ID:      int64(keywordCount + i + 1),
			Name:    fmt.Sprintf("regex-%d", i),
			Type:    "regex",
			Pattern: `\b(alternative|monitoring|sqlite)\b`,
			Enabled: true,
		})
	}
	return rules
}

func benchmarkText(size int) string {
	chunk := "distributed collectors normalize public data and checkpoint source cursors "
	var builder strings.Builder
	for builder.Len() < size {
		builder.WriteString(chunk)
	}
	return builder.String()
}
