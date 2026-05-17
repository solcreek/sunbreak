package backfill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	DefaultHackerNewsEndpoint     = "https://hn.algolia.com/api/v1/search_by_date"
	DefaultHackerNewsMaxSliceHits = 800
	DefaultHackerNewsMaxSlices    = 128
)

type HackerNewsProbeOptions struct {
	Endpoint     string
	Query        string
	From         time.Time
	To           time.Time
	MaxSliceHits int
	MaxSlices    int
}

type HackerNewsProbeResult struct {
	OK             bool                  `json:"ok"`
	Source         string                `json:"source"`
	Query          string                `json:"query"`
	From           string                `json:"from"`
	To             string                `json:"to"`
	TotalHits      int                   `json:"total_hits"`
	Slices         []HackerNewsTimeSlice `json:"slices"`
	Truncated      bool                  `json:"truncated"`
	Warnings       []string              `json:"warnings"`
	Recommendation ProbeRecommendation   `json:"recommendation"`
}

type HackerNewsTimeSlice struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Hits        int    `json:"hits"`
	FetchPages  int    `json:"fetch_pages"`
	TooLarge    bool   `json:"too_large"`
	DurationSec int64  `json:"duration_seconds"`
}

type ProbeRecommendation struct {
	Mode         string `json:"mode"`
	MaxSliceHits int    `json:"max_slice_hits"`
	MaxSlices    int    `json:"max_slices"`
	Reason       string `json:"reason"`
}

func ProbeHackerNews(ctx context.Context, client *http.Client, opts HackerNewsProbeOptions) (HackerNewsProbeResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if opts.Endpoint == "" {
		opts.Endpoint = DefaultHackerNewsEndpoint
	}
	if opts.MaxSliceHits <= 0 {
		opts.MaxSliceHits = DefaultHackerNewsMaxSliceHits
	}
	if opts.MaxSlices <= 0 {
		opts.MaxSlices = DefaultHackerNewsMaxSlices
	}
	if err := validateProbeOptions(opts); err != nil {
		return HackerNewsProbeResult{}, err
	}

	prober := hackerNewsProber{client: client, endpoint: opts.Endpoint}
	total, err := prober.count(ctx, opts.Query, opts.From, opts.To)
	if err != nil {
		return HackerNewsProbeResult{}, err
	}
	result := HackerNewsProbeResult{
		OK:        true,
		Source:    "hackernews",
		Query:     opts.Query,
		From:      opts.From.UTC().Format(time.RFC3339),
		To:        opts.To.UTC().Format(time.RFC3339),
		TotalHits: total,
		Slices:    []HackerNewsTimeSlice{},
		Warnings:  []string{},
		Recommendation: ProbeRecommendation{
			Mode:         "time_sliced",
			MaxSliceHits: opts.MaxSliceHits,
			MaxSlices:    opts.MaxSlices,
			Reason:       "Algolia pagination is capped, so historical fetches should split ranges until each slice is below the page cap.",
		},
	}
	if total == 0 {
		return result, nil
	}
	if err := prober.split(ctx, opts, opts.From, opts.To, total, &result); err != nil {
		return HackerNewsProbeResult{}, err
	}
	if result.Truncated {
		result.Warnings = append(result.Warnings, "slice planning stopped before the range was fully split; narrow the query or range, or raise --max-slices")
	}
	for _, slice := range result.Slices {
		if slice.TooLarge {
			result.Warnings = append(result.Warnings, "one or more slices are still above --max-slice-hits; use smaller time ranges before fetching")
			break
		}
	}
	return result, nil
}

type hackerNewsProber struct {
	client   *http.Client
	endpoint string
}

func (p hackerNewsProber) split(ctx context.Context, opts HackerNewsProbeOptions, from, to time.Time, knownHits int, result *HackerNewsProbeResult) error {
	if len(result.Slices) >= opts.MaxSlices {
		result.Truncated = true
		return nil
	}
	duration := to.Sub(from)
	if knownHits <= opts.MaxSliceHits || duration <= time.Hour {
		result.Slices = append(result.Slices, HackerNewsTimeSlice{
			From:        from.UTC().Format(time.RFC3339),
			To:          to.UTC().Format(time.RFC3339),
			Hits:        knownHits,
			FetchPages:  fetchPages(knownHits),
			TooLarge:    knownHits > opts.MaxSliceHits,
			DurationSec: int64(duration.Seconds()),
		})
		return nil
	}
	mid := from.Add(duration / 2).UTC()
	leftHits, err := p.count(ctx, opts.Query, from, mid)
	if err != nil {
		return err
	}
	if err := p.split(ctx, opts, from, mid, leftHits, result); err != nil {
		return err
	}
	if result.Truncated {
		return nil
	}
	rightHits, err := p.count(ctx, opts.Query, mid, to)
	if err != nil {
		return err
	}
	return p.split(ctx, opts, mid, to, rightHits, result)
}

func (p hackerNewsProber) count(ctx context.Context, query string, from, to time.Time) (int, error) {
	values := url.Values{}
	values.Set("query", query)
	values.Set("tags", "(story,comment)")
	values.Set("hitsPerPage", "1")
	values.Add("numericFilters", "created_at_i>="+strconv.FormatInt(from.UTC().Unix(), 10))
	values.Add("numericFilters", "created_at_i<"+strconv.FormatInt(to.UTC().Unix(), 10))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "sunbreak-monitor/0.1 (+https://example.invalid/sunbreak)")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("hackernews backfill probe failed: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return 0, err
	}
	var payload struct {
		NBHits int `json:"nbHits"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, err
	}
	return payload.NBHits, nil
}

func validateProbeOptions(opts HackerNewsProbeOptions) error {
	if strings.TrimSpace(opts.Query) == "" {
		return errors.New("query is required")
	}
	for _, r := range opts.Query {
		if unicode.IsControl(r) {
			return errors.New("query contains control characters")
		}
	}
	if opts.From.IsZero() {
		return errors.New("from time is required")
	}
	if opts.To.IsZero() {
		return errors.New("to time is required")
	}
	if !opts.From.Before(opts.To) {
		return errors.New("from time must be before to time")
	}
	return nil
}

func fetchPages(hits int) int {
	if hits <= 0 {
		return 0
	}
	return (hits + 99) / 100
}
