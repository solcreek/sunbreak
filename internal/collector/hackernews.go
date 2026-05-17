package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"radar/internal/model"
)

type HNCollector struct {
	client *http.Client
}

func NewHNCollector(client *http.Client) *HNCollector {
	return &HNCollector{client: client}
}

func (c *HNCollector) Collect(ctx context.Context, source model.Source) (Result, error) {
	endpoint := "https://hn.algolia.com/api/v1/search_by_date"
	values := url.Values{}
	values.Set("hitsPerPage", "50")
	values.Set("tags", "story")
	if source.Checkpoint != "" {
		if _, err := strconv.ParseInt(source.Checkpoint, 10, 64); err == nil {
			values.Add("numericFilters", "created_at_i>"+source.Checkpoint)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "radar-monitor/0.1 (+https://example.invalid/radar)")
	resp, err := c.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, errStatus("hackernews request failed", resp.Status)
	}
	var payload hnResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Result{}, err
	}
	var items []model.Item
	maxCheckpoint := source.Checkpoint
	for _, hit := range payload.Hits {
		if hit.CreatedAtI > 0 && (maxCheckpoint == "" || strconv.FormatInt(hit.CreatedAtI, 10) > maxCheckpoint) {
			maxCheckpoint = strconv.FormatInt(hit.CreatedAtI, 10)
		}
		title := firstNonEmpty(hit.Title, hit.StoryTitle)
		link := firstNonEmpty(hit.URL, hit.StoryURL)
		content := strings.TrimSpace(hit.CommentText)
		if content == "" {
			content = strings.TrimSpace(hit.Title)
		}
		published := time.Unix(hit.CreatedAtI, 0).UTC()
		if hit.CreatedAtI == 0 {
			published = parseFeedTime(hit.CreatedAt)
		}
		items = append(items, model.Item{
			SourceID:    source.ID,
			SourceType:  source.Type,
			SourceName:  source.Name,
			ExternalID:  hit.ObjectID,
			URL:         link,
			Title:       strings.TrimSpace(title),
			Content:     content,
			Author:      hit.Author,
			PublishedAt: published,
			FetchedAt:   time.Now().UTC(),
		})
	}
	return Result{Items: items, Checkpoint: maxCheckpoint, ETag: source.ETag, LastModified: source.LastModified}, nil
}

type hnResponse struct {
	Hits []hnHit `json:"hits"`
}

type hnHit struct {
	ObjectID    string `json:"objectID"`
	Title       string `json:"title"`
	StoryTitle  string `json:"story_title"`
	URL         string `json:"url"`
	StoryURL    string `json:"story_url"`
	Author      string `json:"author"`
	CommentText string `json:"comment_text"`
	CreatedAt   string `json:"created_at"`
	CreatedAtI  int64  `json:"created_at_i"`
}
