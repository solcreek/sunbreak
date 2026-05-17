package collector

import (
	"context"
	"errors"
	"net/http"

	"radar/internal/model"
)

type Result struct {
	Items        []model.Item
	Checkpoint   string
	ETag         string
	LastModified string
}

type Collector interface {
	Collect(ctx context.Context, source model.Source) (Result, error)
}

type Registry struct {
	collectors map[string]Collector
}

func NewRegistry(httpClient *http.Client, reddit RedditOptions) *Registry {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Registry{
		collectors: map[string]Collector{
			"rss":        NewFeedCollector(httpClient),
			"atom":       NewFeedCollector(httpClient),
			"hackernews": NewHNCollector(httpClient),
			"reddit":     NewRedditCollector(httpClient, reddit),
		},
	}
}

func (r *Registry) Get(sourceType string) (Collector, error) {
	collector, ok := r.collectors[sourceType]
	if !ok {
		return nil, errors.New("unsupported source type: " + sourceType)
	}
	return collector, nil
}
