package collector

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sunbreak/internal/model"
)

const (
	defaultHNHitLimit            = 50
	defaultHNMaxPages            = 2
	defaultHNMaxStoriesPerRun    = 5
	defaultHNMaxCommentsPerStory = 1000
	defaultHNMaxDepth            = 64
	defaultHNOverlapSeconds      = 900
)

type HNCollector struct {
	client         *http.Client
	searchEndpoint string
	itemEndpoint   string
}

func NewHNCollector(client *http.Client) *HNCollector {
	return &HNCollector{
		client:         client,
		searchEndpoint: "https://hn.algolia.com/api/v1/search_by_date",
		itemEndpoint:   "https://hacker-news.firebaseio.com/v0/item/%d.json",
	}
}

func (c *HNCollector) Collect(ctx context.Context, source model.Source) (Result, error) {
	opts, err := parseHNConfig(source.ConfigJSON)
	if err != nil {
		return Result{}, err
	}
	hits, checkpoint, err := c.search(ctx, source, opts)
	if err != nil {
		return Result{}, err
	}
	if !opts.IncludeComments {
		return Result{
			Items:        algoliaItems(hits, source),
			Checkpoint:   checkpoint,
			ETag:         source.ETag,
			LastModified: source.LastModified,
		}, nil
	}

	rootIDs := rootStoryIDs(hits, opts.MaxStoriesPerRun)
	thread := hnThreadResult{}
	seenItems := map[string]struct{}{}
	for _, rootID := range rootIDs {
		root, err := c.fetchItem(ctx, rootID)
		if err != nil {
			return Result{}, err
		}
		if root.ID == 0 {
			continue
		}
		crawler := hnThreadCrawler{
			collector:           c,
			source:              source,
			root:                root,
			maxDepth:            opts.MaxDepth,
			maxCommentsPerStory: opts.MaxCommentsPerStory,
			seenItems:           seenItems,
		}
		if err := crawler.visit(ctx, root, 0, 0, nil); err != nil {
			return Result{}, err
		}
		thread.Items = append(thread.Items, crawler.items...)
		thread.Relations = append(thread.Relations, crawler.relations...)
		thread.Links = append(thread.Links, crawler.links...)
	}
	return Result{
		Items:        thread.Items,
		Relations:    thread.Relations,
		Links:        thread.Links,
		Checkpoint:   checkpoint,
		ETag:         source.ETag,
		LastModified: source.LastModified,
	}, nil
}

func (c *HNCollector) search(ctx context.Context, source model.Source, opts hnOptions) ([]hnHit, string, error) {
	var hits []hnHit
	maxCheckpoint := source.Checkpoint
	for page := 0; page < opts.MaxPages; page++ {
		values := url.Values{}
		values.Set("hitsPerPage", strconv.Itoa(opts.HitsPerPage))
		values.Set("page", strconv.Itoa(page))
		values.Set("tags", opts.Tags)
		if opts.Query != "" {
			values.Set("query", opts.Query)
		}
		if source.Checkpoint != "" {
			checkpoint, err := strconv.ParseInt(source.Checkpoint, 10, 64)
			if err == nil {
				lowerBound := checkpoint - int64(opts.OverlapSeconds)
				if lowerBound < 0 {
					lowerBound = 0
				}
				values.Add("numericFilters", "created_at_i>"+strconv.FormatInt(lowerBound, 10))
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.searchEndpoint+"?"+values.Encode(), nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("User-Agent", "sunbreak-monitor/0.1 (+https://example.invalid/sunbreak)")
		req.Header.Set("Accept", "application/json")
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, "", err
		}
		body, err := readJSONResponse(resp, "hackernews request failed")
		if err != nil {
			return nil, "", err
		}
		var payload hnResponse
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, "", err
		}
		if len(payload.Hits) == 0 {
			break
		}
		for _, hit := range payload.Hits {
			if hit.CreatedAtI > 0 && (maxCheckpoint == "" || hit.CreatedAtI > parseInt64(maxCheckpoint)) {
				maxCheckpoint = strconv.FormatInt(hit.CreatedAtI, 10)
			}
			hits = append(hits, hit)
		}
		if payload.NbPages > 0 && page+1 >= payload.NbPages {
			break
		}
	}
	return hits, maxCheckpoint, nil
}

func (c *HNCollector) fetchItem(ctx context.Context, id int64) (hnAPIItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.itemURL(id), nil)
	if err != nil {
		return hnAPIItem{}, err
	}
	req.Header.Set("User-Agent", "sunbreak-monitor/0.1 (+https://example.invalid/sunbreak)")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return hnAPIItem{}, err
	}
	body, err := readJSONResponse(resp, "hackernews item request failed")
	if err != nil {
		return hnAPIItem{}, err
	}
	if strings.TrimSpace(string(body)) == "null" {
		return hnAPIItem{}, nil
	}
	var item hnAPIItem
	if err := json.Unmarshal(body, &item); err != nil {
		return hnAPIItem{}, err
	}
	return item, nil
}

func (c *HNCollector) itemURL(id int64) string {
	if strings.Contains(c.itemEndpoint, "%") {
		return fmtItemEndpoint(c.itemEndpoint, id)
	}
	return strings.TrimRight(c.itemEndpoint, "/") + "/" + strconv.FormatInt(id, 10) + ".json"
}

type hnThreadCrawler struct {
	collector           *HNCollector
	source              model.Source
	root                hnAPIItem
	maxDepth            int
	maxCommentsPerStory int
	commentCount        int
	seenItems           map[string]struct{}
	items               []model.Item
	relations           []model.ItemRelation
	links               []model.ItemLink
}

func (c *hnThreadCrawler) visit(ctx context.Context, item hnAPIItem, parentID int64, depth int, ancestors []int64) error {
	if item.ID == 0 {
		return nil
	}
	if item.Type == "comment" {
		if c.maxCommentsPerStory > 0 && c.commentCount >= c.maxCommentsPerStory {
			return nil
		}
		c.commentCount++
	}

	externalID := strconv.FormatInt(item.ID, 10)
	pathIDs := append(append([]int64{}, ancestors...), item.ID)
	path := hnPath(pathIDs)
	if _, ok := c.seenItems[externalID]; !ok {
		c.seenItems[externalID] = struct{}{}
		normalized, itemLinks := normalizeHNItem(c.source, c.root, item)
		c.items = append(c.items, normalized)
		c.links = append(c.links, itemLinks...)
	}

	rootExternalID := strconv.FormatInt(c.root.ID, 10)
	parentExternalID := ""
	relationType := "root"
	if parentID != 0 {
		parentExternalID = strconv.FormatInt(parentID, 10)
		relationType = "comment"
	}
	c.relations = append(c.relations, model.ItemRelation{
		SourceID:         c.source.ID,
		RootExternalID:   rootExternalID,
		ParentExternalID: parentExternalID,
		ChildExternalID:  externalID,
		RelationType:     relationType,
		Depth:            depth,
		Path:             path,
	})

	if depth >= c.maxDepth {
		return nil
	}
	for _, kidID := range item.Kids {
		if c.maxCommentsPerStory > 0 && c.commentCount >= c.maxCommentsPerStory {
			return nil
		}
		kid, err := c.collector.fetchItem(ctx, kidID)
		if err != nil {
			return err
		}
		if err := c.visit(ctx, kid, item.ID, depth+1, pathIDs); err != nil {
			return err
		}
	}
	return nil
}

func normalizeHNItem(source model.Source, root, item hnAPIItem) (model.Item, []model.ItemLink) {
	content := htmlToText(item.Text)
	if item.Deleted {
		content = "[deleted]"
	}
	if item.Dead {
		if content == "" {
			content = "[dead]"
		} else {
			content = "[dead] " + content
		}
	}
	title := strings.TrimSpace(item.Title)
	if title == "" && item.Type == "comment" {
		title = strings.TrimSpace(root.Title)
	}
	itemURL := strings.TrimSpace(item.URL)
	if itemURL == "" || item.Type == "comment" {
		itemURL = hnItemWebURL(item.ID)
	}
	raw, _ := json.Marshal(item)
	normalized := model.Item{
		SourceID:    source.ID,
		SourceType:  source.Type,
		SourceName:  source.Name,
		ExternalID:  strconv.FormatInt(item.ID, 10),
		URL:         itemURL,
		Title:       title,
		Content:     content,
		Author:      item.By,
		PublishedAt: time.Unix(item.Time, 0).UTC(),
		FetchedAt:   time.Now().UTC(),
		RawJSON:     string(raw),
	}
	links := extractHNLinks(normalized.ExternalID, item.URL, item.Text)
	for i := range links {
		links[i].SourceID = source.ID
	}
	return normalized, links
}

func algoliaItems(hits []hnHit, source model.Source) []model.Item {
	now := time.Now().UTC()
	out := make([]model.Item, 0, len(hits))
	for _, hit := range hits {
		title := firstNonEmpty(hit.Title, hit.StoryTitle)
		link := firstNonEmpty(hit.URL, hit.StoryURL)
		content := htmlToText(hit.CommentText)
		if content == "" {
			content = strings.TrimSpace(title)
		}
		published := time.Unix(hit.CreatedAtI, 0).UTC()
		if hit.CreatedAtI == 0 {
			published = parseFeedTime(hit.CreatedAt)
		}
		raw, _ := json.Marshal(hit)
		out = append(out, model.Item{
			SourceID:    source.ID,
			SourceType:  source.Type,
			SourceName:  source.Name,
			ExternalID:  hit.ObjectID,
			URL:         link,
			Title:       strings.TrimSpace(title),
			Content:     content,
			Author:      hit.Author,
			PublishedAt: published,
			FetchedAt:   now,
			RawJSON:     string(raw),
		})
	}
	return out
}

func rootStoryIDs(hits []hnHit, limit int) []int64 {
	if limit <= 0 {
		limit = defaultHNMaxStoriesPerRun
	}
	seen := map[int64]struct{}{}
	var out []int64
	for _, hit := range hits {
		id := hit.StoryID
		if id == 0 && hit.ObjectID != "" {
			id = parseInt64(hit.ObjectID)
		}
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func parseHNConfig(raw string) (hnOptions, error) {
	opts := hnOptions{
		IncludeComments:     true,
		HitsPerPage:         defaultHNHitLimit,
		MaxPages:            defaultHNMaxPages,
		MaxStoriesPerRun:    defaultHNMaxStoriesPerRun,
		MaxCommentsPerStory: defaultHNMaxCommentsPerStory,
		MaxDepth:            defaultHNMaxDepth,
		OverlapSeconds:      defaultHNOverlapSeconds,
		Tags:                "(story,comment)",
	}
	if strings.TrimSpace(raw) == "" {
		return opts, nil
	}
	var input hnConfig
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return hnOptions{}, err
	}
	if input.IncludeComments != nil {
		opts.IncludeComments = *input.IncludeComments
	}
	if input.HitsPerPage > 0 {
		opts.HitsPerPage = input.HitsPerPage
	}
	if opts.HitsPerPage > 100 {
		opts.HitsPerPage = 100
	}
	if input.MaxPages > 0 {
		opts.MaxPages = input.MaxPages
	}
	if input.MaxStoriesPerRun > 0 {
		opts.MaxStoriesPerRun = input.MaxStoriesPerRun
	}
	if input.MaxCommentsPerStory > 0 {
		opts.MaxCommentsPerStory = input.MaxCommentsPerStory
	}
	if input.MaxDepth > 0 {
		opts.MaxDepth = input.MaxDepth
	}
	if input.OverlapSeconds != nil && *input.OverlapSeconds >= 0 {
		opts.OverlapSeconds = *input.OverlapSeconds
	}
	if strings.TrimSpace(input.Tags) != "" {
		opts.Tags = strings.TrimSpace(input.Tags)
	}
	opts.Query = strings.TrimSpace(input.Query)
	return opts, nil
}

func readJSONResponse(resp *http.Response, label string) ([]byte, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errStatus(label, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20))
}

func fmtItemEndpoint(format string, id int64) string {
	return strings.Replace(format, "%d", strconv.FormatInt(id, 10), 1)
}

func hnPath(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, "/")
}

func hnItemWebURL(id int64) string {
	return "https://news.ycombinator.com/item?id=" + strconv.FormatInt(id, 10)
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

var (
	anchorRE   = regexp.MustCompile(`(?is)<a\s+[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	tagRE      = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRE    = regexp.MustCompile(`\s+`)
	plainURLRE = regexp.MustCompile(`https?://[^\s<>"']+`)
)

func htmlToText(value string) string {
	value = strings.ReplaceAll(value, "<p>", "\n\n")
	value = strings.ReplaceAll(value, "<P>", "\n\n")
	value = strings.ReplaceAll(value, "<br>", "\n")
	value = strings.ReplaceAll(value, "<br/>", "\n")
	value = strings.ReplaceAll(value, "<br />", "\n")
	value = tagRE.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = spaceRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func extractHNLinks(itemExternalID, storyURL, text string) []model.ItemLink {
	seen := map[string]struct{}{}
	var out []model.ItemLink
	add := func(rawURL, anchor string) {
		rawURL = strings.TrimSpace(html.UnescapeString(rawURL))
		if rawURL == "" {
			return
		}
		normalized := normalizeHNURL(rawURL)
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		out = append(out, model.ItemLink{
			ItemExternalID: itemExternalID,
			URL:            rawURL,
			NormalizedURL:  normalized,
			AnchorText:     htmlToText(anchor),
		})
	}
	add(storyURL, "")
	for _, match := range anchorRE.FindAllStringSubmatch(text, -1) {
		if len(match) >= 3 {
			add(match[1], match[2])
		}
	}
	for _, match := range plainURLRE.FindAllString(text, -1) {
		add(strings.TrimRight(match, ".,);]"), "")
	}
	return out
}

func normalizeHNURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" && parsed.Host == "" {
		base, _ := url.Parse("https://news.ycombinator.com/")
		parsed = base.ResolveReference(parsed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

type hnOptions struct {
	IncludeComments     bool
	HitsPerPage         int
	MaxPages            int
	MaxStoriesPerRun    int
	MaxCommentsPerStory int
	MaxDepth            int
	OverlapSeconds      int
	Tags                string
	Query               string
}

type hnConfig struct {
	IncludeComments     *bool  `json:"include_comments"`
	HitsPerPage         int    `json:"hits_per_page"`
	MaxPages            int    `json:"max_pages"`
	MaxStoriesPerRun    int    `json:"max_stories_per_run"`
	MaxCommentsPerStory int    `json:"max_comments_per_story"`
	MaxDepth            int    `json:"max_depth"`
	OverlapSeconds      *int   `json:"overlap_seconds"`
	Tags                string `json:"tags"`
	Query               string `json:"query"`
}

type hnThreadResult struct {
	Items     []model.Item
	Relations []model.ItemRelation
	Links     []model.ItemLink
}

type hnResponse struct {
	Hits    []hnHit `json:"hits"`
	NbPages int     `json:"nbPages"`
}

type hnHit struct {
	ObjectID    string   `json:"objectID"`
	Title       string   `json:"title"`
	StoryTitle  string   `json:"story_title"`
	URL         string   `json:"url"`
	StoryURL    string   `json:"story_url"`
	Author      string   `json:"author"`
	CommentText string   `json:"comment_text"`
	CreatedAt   string   `json:"created_at"`
	CreatedAtI  int64    `json:"created_at_i"`
	StoryID     int64    `json:"story_id"`
	Tags        []string `json:"_tags"`
}

type hnAPIItem struct {
	ID      int64   `json:"id"`
	Deleted bool    `json:"deleted"`
	Dead    bool    `json:"dead"`
	Type    string  `json:"type"`
	By      string  `json:"by"`
	Time    int64   `json:"time"`
	Text    string  `json:"text"`
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Parent  int64   `json:"parent"`
	Kids    []int64 `json:"kids"`
}
