package collector

import (
	"context"
	"errors"
	"net/http"
	"time"

	"radar/internal/model"
)

type RedditOptions struct {
	ClientID     string
	ClientSecret string
	UserAgent    string
}

type RedditCollector struct {
	client  *http.Client
	options RedditOptions
}

func NewRedditCollector(client *http.Client, options RedditOptions) *RedditCollector {
	if options.UserAgent == "" {
		options.UserAgent = "radar-monitor/0.1"
	}
	return &RedditCollector{client: client, options: options}
}

func (c *RedditCollector) Collect(ctx context.Context, source model.Source) (Result, error) {
	_ = ctx
	_ = c.client
	if c.options.ClientID == "" || c.options.ClientSecret == "" {
		if source.Checkpoint != "" {
			return Result{Checkpoint: source.Checkpoint, ETag: source.ETag, LastModified: source.LastModified}, nil
		}
		item := model.Item{
			SourceID:    source.ID,
			SourceType:  source.Type,
			SourceName:  source.Name,
			ExternalID:  "reddit-adapter-mock-v1",
			URL:         "https://www.reddit.com/",
			Title:       "Reddit collector adapter is ready",
			Content:     "Provide Reddit OAuth credentials to enable live Reddit ingestion. This mock item proves the source pipeline, matcher, and outbox path.",
			Author:      "radar",
			PublishedAt: time.Now().UTC(),
			FetchedAt:   time.Now().UTC(),
		}
		return Result{Items: []model.Item{item}, Checkpoint: item.ExternalID, ETag: source.ETag, LastModified: source.LastModified}, nil
	}
	return Result{}, errors.New("credentialed Reddit collector is not implemented in the MVP yet")
}

type statusError string

func errStatus(prefix, status string) error {
	return statusError(prefix + ": " + status)
}

func (e statusError) Error() string {
	return string(e)
}
