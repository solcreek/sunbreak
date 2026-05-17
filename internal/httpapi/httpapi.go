package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"radar/internal/model"
	"radar/internal/storage"
)

type Runner interface {
	RunOnce(ctx context.Context) error
	RunDigest(ctx context.Context) error
	RunOutbox(ctx context.Context) error
}

type API struct {
	store  *storage.Store
	runner Runner
	logger *slog.Logger
	mux    *http.ServeMux
}

func New(store *storage.Store, runner Runner, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	api := &API{
		store:  store,
		runner: runner,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	api.routes()
	return api.mux
}

func (a *API) routes() {
	a.mux.HandleFunc("GET /healthz", a.health)
	a.mux.HandleFunc("GET /api/sources", a.sources)
	a.mux.HandleFunc("GET /api/rules", a.rules)
	a.mux.HandleFunc("POST /api/rules", a.createRule)
	a.mux.HandleFunc("GET /api/items", a.items)
	a.mux.HandleFunc("GET /api/matches", a.matches)
	a.mux.HandleFunc("GET /api/digests", a.digests)
	a.mux.HandleFunc("POST /api/collect", a.collect)
	a.mux.HandleFunc("POST /api/digest", a.digest)
	a.mux.HandleFunc("POST /api/outbox/dispatch", a.dispatchOutbox)
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"fts":      a.store.HasFTS(),
		"time_utc": time.Now().UTC().Format(time.RFC3339),
	})
}

func (a *API) sources(w http.ResponseWriter, r *http.Request) {
	sources, err := a.store.ListSources(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (a *API) rules(w http.ResponseWriter, r *http.Request) {
	rules, err := a.store.ListRules(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (a *API) createRule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name          string `json:"name"`
		Type          string `json:"type"`
		Pattern       string `json:"pattern"`
		CaseSensitive bool   `json:"case_sensitive"`
		Enabled       *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	rule := model.Rule{
		Name:          input.Name,
		Type:          input.Type,
		Pattern:       input.Pattern,
		CaseSensitive: input.CaseSensitive,
		Enabled:       enabled,
	}
	id, err := a.store.UpsertRule(r.Context(), rule)
	if err != nil {
		writeError(w, err)
		return
	}
	rule.ID = id
	writeJSON(w, http.StatusCreated, rule)
}

func (a *API) items(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 50)
	query := r.URL.Query().Get("query")
	items, err := a.store.SearchItems(r.Context(), query, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) matches(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 100)
	hours := intParam(r, "hours", 24)
	matches, err := a.store.RecentMatches(r.Context(), time.Now().UTC().Add(-time.Duration(hours)*time.Hour), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, matches)
}

func (a *API) digests(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 20)
	digests, err := a.store.RecentDigests(r.Context(), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, digests)
}

func (a *API) collect(w http.ResponseWriter, r *http.Request) {
	if err := a.runner.RunOnce(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": false, "ran": true})
}

func (a *API) digest(w http.ResponseWriter, r *http.Request) {
	if err := a.runner.RunDigest(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": false, "ran": true})
}

func (a *API) dispatchOutbox(w http.ResponseWriter, r *http.Request) {
	if err := a.runner.RunOutbox(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": false, "ran": true})
}

func intParam(r *http.Request, name string, fallback int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
