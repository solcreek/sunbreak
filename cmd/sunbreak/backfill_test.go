package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunBackfillProbeCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"nbHits": 42})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	code := runBackfillCommand(context.Background(), []string{
		"probe",
		"hackernews",
		"--query", "cloudflare",
		"--from", "2026-05-01",
		"--to", "2026-05-02",
		"--endpoint", server.URL,
		"--output", "json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected success code, got %d stderr %s", code, stderr.String())
	}
	var payload struct {
		OK      bool `json:"ok"`
		Queries []struct {
			Query     string `json:"query"`
			TotalHits int    `json:"total_hits"`
		} `json:"queries"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || len(payload.Queries) != 1 || payload.Queries[0].Query != "cloudflare" || payload.Queries[0].TotalHits != 42 {
		t.Fatalf("unexpected payload: %s", stdout.String())
	}
}

func TestRunBackfillProbeCommandValidatesArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runBackfillCommand(context.Background(), []string{"probe", "hackernews", "--query", "cloudflare"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected validation failure")
	}
	if stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("unexpected output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestParseBackfillWindow(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	from, to, err := parseBackfillWindow("", "", "7d", now)
	if err != nil {
		t.Fatal(err)
	}
	if !to.Equal(now) || !from.Equal(now.Add(-7*24*time.Hour)) {
		t.Fatalf("unexpected since window: %s %s", from, to)
	}
	from, to, err = parseBackfillWindow("2026-05-01", "2026-05-02", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if from.Format("2006-01-02") != "2026-05-01" || to.Format("2006-01-02") != "2026-05-02" {
		t.Fatalf("unexpected absolute window: %s %s", from, to)
	}
}

func TestSplitKeywords(t *testing.T) {
	got := splitKeywords("sqlite", "postgres, sqlite,cloudflare")
	want := []string{"sqlite", "postgres", "cloudflare"}
	if len(got) != len(want) {
		t.Fatalf("unexpected keywords: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected keywords: %+v", got)
		}
	}
}
