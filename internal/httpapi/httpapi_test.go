package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"sunbreak/internal/model"
	"sunbreak/internal/storage"
)

type fakeRunner struct {
	collects int
	digests  int
	outbox   int
	err      error
}

func (r *fakeRunner) RunOnce(context.Context) error {
	r.collects++
	return r.err
}

func (r *fakeRunner) RunDigest(context.Context) error {
	r.digests++
	return r.err
}

func (r *fakeRunner) RunOutbox(context.Context) error {
	r.outbox++
	return r.err
}

func TestAPIReadEndpointsAndCreateRule(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sourceID, err := store.UpsertSource(ctx, model.Source{Type: "rss", Name: "Feed", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	itemID, _, err := store.InsertItem(ctx, model.Item{
		SourceID:    sourceID,
		SourceType:  "rss",
		SourceName:  "Feed",
		ExternalID:  "item-1",
		Title:       "SQLite item",
		Content:     "content",
		FetchedAt:   time.Now().UTC(),
		PublishedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := store.UpsertRule(ctx, model.Rule{Name: "SQLite", Type: "keyword", Pattern: "sqlite", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertMatch(ctx, model.Match{ItemID: itemID, RuleID: ruleID, MatchedText: "sqlite"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveDigest(ctx, model.Digest{WindowStart: time.Now().Add(-time.Hour), WindowEnd: time.Now(), Subject: "Digest", Body: "Body"}); err != nil {
		t.Fatal(err)
	}

	runner := &fakeRunner{}
	handler := New(store, runner, slog.New(slog.NewTextHandler(io.Discard, nil)))

	assertStatus(t, handler, http.MethodGet, "/healthz", nil, http.StatusOK)
	assertArrayEndpoint(t, handler, "/api/sources")
	assertArrayEndpoint(t, handler, "/api/rules")
	assertArrayEndpoint(t, handler, "/api/items?query=sqlite&limit=5")
	assertArrayEndpoint(t, handler, "/api/matches?hours=24&limit=5")
	assertArrayEndpoint(t, handler, "/api/digests?limit=5")

	req := httptest.NewRequest(http.MethodPost, "/api/rules", bytes.NewBufferString(`{"name":"Go","pattern":"go","enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected created rule, got status %d body %s", rec.Code, rec.Body.String())
	}
	var created model.Rule
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "Go" || created.Type != "keyword" {
		t.Fatalf("unexpected created rule payload: %+v", created)
	}
}

func TestAPIRunnerEndpoints(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "api-runner.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := &fakeRunner{}
	handler := New(store, runner, slog.New(slog.NewTextHandler(io.Discard, nil)))

	assertStatus(t, handler, http.MethodPost, "/api/collect", nil, http.StatusAccepted)
	assertStatus(t, handler, http.MethodPost, "/api/digest", nil, http.StatusAccepted)
	assertStatus(t, handler, http.MethodPost, "/api/outbox/dispatch", nil, http.StatusAccepted)
	if runner.collects != 1 || runner.digests != 1 || runner.outbox != 1 {
		t.Fatalf("runner was not called correctly: %+v", runner)
	}
}

func TestAPIRejectsInvalidRulePayload(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "api-invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := New(store, &fakeRunner{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	assertStatus(t, handler, http.MethodPost, "/api/rules", bytes.NewBufferString("{"), http.StatusBadRequest)
}

func assertArrayEndpoint(t *testing.T, handler http.Handler, path string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s returned status %d body %s", path, rec.Code, rec.Body.String())
	}
	var decoded []any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("%s returned non-array JSON: %v", path, err)
	}
	if decoded == nil {
		t.Fatalf("%s returned nil array", path)
	}
}

func assertStatus(t *testing.T, handler http.Handler, method, path string, body io.Reader, status int) {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != status {
		t.Fatalf("%s %s returned status %d body %s", method, path, rec.Code, rec.Body.String())
	}
}
