package collector

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"radar/internal/model"
)

type FeedCollector struct {
	client *http.Client
}

func NewFeedCollector(client *http.Client) *FeedCollector {
	return &FeedCollector{client: client}
}

func (c *FeedCollector) Collect(ctx context.Context, source model.Source) (Result, error) {
	if source.URL == "" {
		return Result{}, errors.New("feed source url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "radar-monitor/0.1 (+https://example.invalid/radar)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml;q=0.9, */*;q=0.1")
	if source.ETag != "" {
		req.Header.Set("If-None-Match", source.ETag)
	}
	if source.LastModified != "" {
		req.Header.Set("If-Modified-Since", source.LastModified)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return Result{Checkpoint: source.Checkpoint, ETag: source.ETag, LastModified: source.LastModified}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, errors.New("feed request failed: " + resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return Result{}, err
	}
	items, checkpoint, err := parseFeed(body, source)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Items:        items,
		Checkpoint:   checkpoint,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
}

func parseFeed(body []byte, source model.Source) ([]model.Item, string, error) {
	var rss rssFeed
	if err := xml.Unmarshal(body, &rss); err == nil && len(rss.Channel.Items) > 0 {
		return rssItems(rss, source), latestRSSCheckpoint(rss, source.Checkpoint), nil
	}
	var atom atomFeed
	if err := xml.Unmarshal(body, &atom); err == nil && len(atom.Entries) > 0 {
		return atomItems(atom, source), latestAtomCheckpoint(atom, source.Checkpoint), nil
	}
	return nil, source.Checkpoint, errors.New("unsupported or empty feed")
}

func rssItems(feed rssFeed, source model.Source) []model.Item {
	var out []model.Item
	seenCheckpoint := source.Checkpoint
	for _, entry := range feed.Channel.Items {
		externalID := strings.TrimSpace(entry.GUID.Value)
		if externalID == "" {
			externalID = strings.TrimSpace(entry.Link)
		}
		if externalID == "" {
			externalID = hashID(entry.Title + entry.Description + entry.PubDate)
		}
		if seenCheckpoint != "" && externalID == seenCheckpoint {
			break
		}
		published := parseFeedTime(entry.PubDate)
		out = append(out, model.Item{
			SourceID:    source.ID,
			SourceType:  source.Type,
			SourceName:  source.Name,
			ExternalID:  externalID,
			URL:         strings.TrimSpace(entry.Link),
			Title:       strings.TrimSpace(entry.Title),
			Content:     strings.TrimSpace(entry.Description),
			Author:      strings.TrimSpace(firstNonEmpty(entry.Author, entry.Creator)),
			PublishedAt: published,
			FetchedAt:   time.Now().UTC(),
		})
	}
	return out
}

func latestRSSCheckpoint(feed rssFeed, fallback string) string {
	if len(feed.Channel.Items) == 0 {
		return fallback
	}
	entry := feed.Channel.Items[0]
	if entry.GUID.Value != "" {
		return strings.TrimSpace(entry.GUID.Value)
	}
	if entry.Link != "" {
		return strings.TrimSpace(entry.Link)
	}
	return hashID(entry.Title + entry.Description + entry.PubDate)
}

func atomItems(feed atomFeed, source model.Source) []model.Item {
	var out []model.Item
	seenCheckpoint := source.Checkpoint
	for _, entry := range feed.Entries {
		externalID := strings.TrimSpace(entry.ID)
		if externalID == "" {
			externalID = firstAtomLink(entry.Links)
		}
		if externalID == "" {
			externalID = hashID(entry.Title + entry.Summary + entry.Updated)
		}
		if seenCheckpoint != "" && externalID == seenCheckpoint {
			break
		}
		published := parseFeedTime(firstNonEmpty(entry.Published, entry.Updated))
		out = append(out, model.Item{
			SourceID:    source.ID,
			SourceType:  source.Type,
			SourceName:  source.Name,
			ExternalID:  externalID,
			URL:         firstAtomLink(entry.Links),
			Title:       strings.TrimSpace(entry.Title),
			Content:     strings.TrimSpace(firstNonEmpty(entry.Summary, entry.Content)),
			Author:      strings.TrimSpace(entry.Author.Name),
			PublishedAt: published,
			FetchedAt:   time.Now().UTC(),
		})
	}
	return out
}

func latestAtomCheckpoint(feed atomFeed, fallback string) string {
	if len(feed.Entries) == 0 {
		return fallback
	}
	entry := feed.Entries[0]
	if entry.ID != "" {
		return strings.TrimSpace(entry.ID)
	}
	if link := firstAtomLink(entry.Links); link != "" {
		return link
	}
	return hashID(entry.Title + entry.Summary + entry.Updated)
}

func parseFeedTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func firstAtomLink(links []atomLink) string {
	for _, link := range links {
		if link.Rel == "" || link.Rel == "alternate" {
			return strings.TrimSpace(link.Href)
		}
	}
	if len(links) > 0 {
		return strings.TrimSpace(links[0].Href)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func hashID(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	Description string  `xml:"description"`
	PubDate     string  `xml:"pubDate"`
	Author      string  `xml:"author"`
	Creator     string  `xml:"creator"`
	GUID        rssGUID `xml:"guid"`
}

type rssGUID struct {
	Value string `xml:",chardata"`
}

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string     `xml:"id"`
	Title     string     `xml:"title"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
	Updated   string     `xml:"updated"`
	Published string     `xml:"published"`
	Links     []atomLink `xml:"link"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}
