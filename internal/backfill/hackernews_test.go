package backfill

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestProbeHackerNewsSplitsLargeRanges(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		from := numericFilterUnix(t, r, "created_at_i>=")
		to := numericFilterUnix(t, r, "created_at_i<")
		if r.URL.Query().Get("query") != "sqlite" {
			t.Fatalf("unexpected query: %q", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("tags") != "(story,comment)" {
			t.Fatalf("unexpected tags: %q", r.URL.Query().Get("tags"))
		}
		hits := int((to - from) / 3600 * 100)
		_ = json.NewEncoder(w).Encode(map[string]int{"nbHits": hits})
	}))
	defer server.Close()

	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(4 * time.Hour)
	result, err := ProbeHackerNews(context.Background(), server.Client(), HackerNewsProbeOptions{
		Endpoint:     server.URL,
		Query:        "sqlite",
		From:         from,
		To:           to,
		MaxSliceHits: 150,
		MaxSlices:    16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.TotalHits != 400 {
		t.Fatalf("unexpected probe result: %+v", result)
	}
	if len(result.Slices) != 4 {
		t.Fatalf("expected hourly slices, got %+v", result.Slices)
	}
	for _, slice := range result.Slices {
		if slice.Hits != 100 || slice.TooLarge {
			t.Fatalf("unexpected slice: %+v", slice)
		}
	}
	if calls < 3 {
		t.Fatalf("expected recursive range calls, got %d", calls)
	}
}

func TestProbeHackerNewsReportsTruncatedPlan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"nbHits": 10000})
	}))
	defer server.Close()

	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	result, err := ProbeHackerNews(context.Background(), server.Client(), HackerNewsProbeOptions{
		Endpoint:     server.URL,
		Query:        "ai",
		From:         from,
		To:           from.Add(24 * time.Hour),
		MaxSliceHits: 100,
		MaxSlices:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(result.Warnings) == 0 {
		t.Fatalf("expected truncated warning, got %+v", result)
	}
}

func TestProbeHackerNewsValidatesInput(t *testing.T) {
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	tests := []HackerNewsProbeOptions{
		{Query: "", From: from, To: from.Add(time.Hour)},
		{Query: "bad\nquery", From: from, To: from.Add(time.Hour)},
		{Query: "sqlite", To: from.Add(time.Hour)},
		{Query: "sqlite", From: from, To: from},
	}
	for _, tt := range tests {
		if _, err := ProbeHackerNews(context.Background(), nil, tt); err == nil {
			t.Fatalf("expected validation error for %+v", tt)
		}
	}
}

func TestFetchPages(t *testing.T) {
	if fetchPages(0) != 0 {
		t.Fatal("expected zero pages for zero hits")
	}
	if fetchPages(1) != 1 || fetchPages(100) != 1 || fetchPages(101) != 2 {
		t.Fatal("unexpected fetch page calculation")
	}
}

func numericFilterUnix(t *testing.T, r *http.Request, prefix string) int64 {
	t.Helper()
	for _, value := range r.URL.Query()["numericFilters"] {
		if len(value) > len(prefix) && value[:len(prefix)] == prefix {
			parsed, err := strconv.ParseInt(value[len(prefix):], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return parsed
		}
	}
	t.Fatalf("missing numeric filter %s in %s", prefix, r.URL.RawQuery)
	return 0
}
