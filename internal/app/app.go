package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"radar/internal/collector"
	"radar/internal/config"
	"radar/internal/digest"
	"radar/internal/httpapi"
	"radar/internal/matcher"
	"radar/internal/model"
	"radar/internal/notifier"
	"radar/internal/storage"
)

type App struct {
	cfg        config.Config
	store      *storage.Store
	registry   *collector.Registry
	dispatcher *notifier.Dispatcher
	logger     *slog.Logger
}

func New(cfg config.Config, store *storage.Store, logger *slog.Logger) *App {
	if logger == nil {
		logger = slog.Default()
	}
	httpClient := &http.Client{Timeout: time.Duration(cfg.Scheduler.CollectTimeoutSeconds) * time.Second}
	registry := collector.NewRegistry(httpClient, collector.RedditOptions{
		ClientID:     firstNonEmpty(cfg.Reddit.ClientID, os.Getenv("REDDIT_CLIENT_ID")),
		ClientSecret: firstNonEmpty(cfg.Reddit.ClientSecret, os.Getenv("REDDIT_CLIENT_SECRET")),
		UserAgent:    firstNonEmpty(cfg.Reddit.UserAgent, os.Getenv("REDDIT_USER_AGENT")),
	})
	dispatcher := notifier.NewDispatcher(cfg.Notifications.Stdout, os.Stdout, logger)
	return &App{
		cfg:        cfg,
		store:      store,
		registry:   registry,
		dispatcher: dispatcher,
		logger:     logger,
	}
}

func (a *App) Seed(ctx context.Context) error {
	for _, sourceCfg := range a.cfg.Sources {
		sourceType := strings.TrimSpace(sourceCfg.Type)
		if sourceType == "" {
			return errors.New("source type is required")
		}
		sourceName := strings.TrimSpace(sourceCfg.Name)
		if sourceName == "" {
			sourceName = sourceType
		}
		configJSON := "{}"
		if sourceCfg.Config != nil {
			data, err := json.Marshal(sourceCfg.Config)
			if err != nil {
				return err
			}
			configJSON = string(data)
		}
		enabled := true
		if sourceCfg.Enabled != nil {
			enabled = *sourceCfg.Enabled
		}
		_, err := a.store.UpsertSource(ctx, model.Source{
			Type:            sourceType,
			Name:            sourceName,
			URL:             sourceCfg.URL,
			Enabled:         enabled,
			IntervalSeconds: sourceCfg.IntervalSeconds,
			ConfigJSON:      configJSON,
			NextRunAt:       time.Now().UTC(),
		})
		if err != nil {
			return err
		}
	}
	for _, ruleCfg := range a.cfg.Rules {
		if strings.TrimSpace(ruleCfg.Name) == "" {
			return errors.New("rule name is required")
		}
		enabled := true
		if ruleCfg.Enabled != nil {
			enabled = *ruleCfg.Enabled
		}
		_, err := a.store.UpsertRule(ctx, model.Rule{
			Name:          ruleCfg.Name,
			Type:          ruleCfg.Type,
			Pattern:       ruleCfg.Pattern,
			CaseSensitive: ruleCfg.CaseSensitive,
			Enabled:       enabled,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.Seed(ctx); err != nil {
		return err
	}
	server := &http.Server{
		Addr:    a.cfg.Server.Addr,
		Handler: httpapi.New(a.store, a, a.logger),
	}
	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("http server listening", "addr", a.cfg.Server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go a.schedulerLoop(ctx)
	go a.outboxLoop(ctx)
	if a.cfg.Digest.Enabled {
		go a.digestLoop(ctx)
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (a *App) RunOnce(ctx context.Context) error {
	if err := a.Seed(ctx); err != nil {
		return err
	}
	sources, err := a.store.DueSources(ctx, a.cfg.Scheduler.BatchSize)
	if err != nil {
		return err
	}
	for _, source := range sources {
		if err := a.collectSource(ctx, source); err != nil {
			a.logger.Warn("collector failed", "source", source.Name, "type", source.Type, "err", err)
		}
	}
	return nil
}

func (a *App) RunDigest(ctx context.Context) error {
	now := time.Now().UTC()
	windowStart := now.Add(-time.Duration(a.cfg.Digest.WindowHours) * time.Hour)
	matches, err := a.store.RecentMatches(ctx, windowStart, 1000)
	if err != nil {
		return err
	}
	d := digest.Build(matches, windowStart, now)
	if _, err := a.store.SaveDigest(ctx, d); err != nil {
		return err
	}
	_, err = a.store.InsertOutbox(ctx, model.OutboxMessage{
		Channel: "stdout",
		Subject: d.Subject,
		Body:    d.Body,
	})
	return err
}

func (a *App) RunOutbox(ctx context.Context) error {
	messages, err := a.store.PendingOutbox(ctx, 20)
	if err != nil {
		return err
	}
	for _, msg := range messages {
		if err := a.dispatcher.Dispatch(ctx, msg); err != nil {
			delay := retryDelay(msg.Attempts)
			_ = a.store.MarkOutboxRetry(ctx, msg.ID, err.Error(), delay)
			continue
		}
		if err := a.store.MarkOutboxSent(ctx, msg.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(a.cfg.Scheduler.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()
	if err := a.RunOnce(ctx); err != nil {
		a.logger.Warn("initial collection failed", "err", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.RunOnce(ctx); err != nil {
				a.logger.Warn("scheduled collection failed", "err", err)
			}
		}
	}
}

func (a *App) outboxLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.RunOutbox(ctx); err != nil {
				a.logger.Warn("outbox dispatch failed", "err", err)
			}
		}
	}
}

func (a *App) digestLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(a.cfg.Digest.IntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.RunDigest(ctx); err != nil {
				a.logger.Warn("digest job failed", "err", err)
			}
		}
	}
}

func (a *App) collectSource(ctx context.Context, source model.Source) error {
	collector, err := a.registry.Get(source.Type)
	if err != nil {
		next := time.Now().UTC().Add(time.Duration(source.IntervalSeconds) * time.Second)
		_ = a.store.MarkSourceFailure(ctx, source.ID, err.Error(), next)
		return err
	}
	collectCtx, cancel := context.WithTimeout(ctx, time.Duration(a.cfg.Scheduler.CollectTimeoutSeconds)*time.Second)
	defer cancel()
	result, err := collector.Collect(collectCtx, source)
	next := time.Now().UTC().Add(time.Duration(source.IntervalSeconds) * time.Second)
	if err != nil {
		_ = a.store.MarkSourceFailure(ctx, source.ID, err.Error(), next)
		return err
	}
	rules, err := a.store.ActiveRules(ctx)
	if err != nil {
		return err
	}
	m, err := matcher.New(rules)
	if err != nil {
		return err
	}
	insertedCount := 0
	matchCount := 0
	for _, item := range result.Items {
		item.SourceID = source.ID
		item.SourceType = source.Type
		item.SourceName = source.Name
		itemID, inserted, err := a.store.InsertItem(ctx, item)
		if err != nil {
			return err
		}
		if !inserted {
			continue
		}
		insertedCount++
		item.ID = itemID
		for _, found := range m.Match(item) {
			saved, err := a.store.InsertMatch(ctx, model.Match{
				ItemID:      itemID,
				RuleID:      found.Rule.ID,
				MatchedText: found.MatchedText,
				Score:       found.Score,
			})
			if err != nil {
				return err
			}
			if saved {
				matchCount++
				if _, err := a.store.InsertOutbox(ctx, matchOutbox(found.Rule, item, found.MatchedText)); err != nil {
					return err
				}
			}
		}
	}
	if err := a.store.MarkSourceSuccess(ctx, source.ID, result.Checkpoint, result.ETag, result.LastModified, next); err != nil {
		return err
	}
	a.logger.Info("source collected", "source", source.Name, "type", source.Type, "items", len(result.Items), "inserted", insertedCount, "matches", matchCount)
	return nil
}

func matchOutbox(rule model.Rule, item model.Item, matchedText string) model.OutboxMessage {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = strings.TrimSpace(item.Content)
	}
	if len(title) > 160 {
		title = title[:160] + "..."
	}
	body := fmt.Sprintf("Rule: %s\nMatched: %s\nSource: %s\nTitle: %s\nURL: %s\n\n%s",
		rule.Name,
		matchedText,
		item.SourceName,
		title,
		item.URL,
		snippet(item.Content, 500),
	)
	return model.OutboxMessage{
		Channel: "stdout",
		Subject: "Radar match: " + rule.Name,
		Body:    body,
	}
}

func snippet(value string, limit int) string {
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func retryDelay(attempts int) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	delay := time.Duration(1<<min(attempts, 8)) * time.Minute
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
