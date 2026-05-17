package storage

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"radar/internal/model"
)

type Store struct {
	db     *sql.DB
	hasFTS bool
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		path = "radar.db"
	}
	dsn := path
	if !strings.Contains(dsn, "?") {
		dsn += "?_busy_timeout=5000&_foreign_keys=on"
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA foreign_keys=ON;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, baseSchema); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, ftsSchema); err != nil {
		s.hasFTS = false
		return nil
	}
	s.hasFTS = true
	return nil
}

func (s *Store) HasFTS() bool {
	return s.hasFTS
}

func (s *Store) UpsertSource(ctx context.Context, source model.Source) (int64, error) {
	now := dbTime(time.Now())
	if source.IntervalSeconds <= 0 {
		source.IntervalSeconds = 300
	}
	if source.ConfigJSON == "" {
		source.ConfigJSON = "{}"
	}
	nextRun := source.NextRunAt
	if nextRun.IsZero() {
		nextRun = time.Now().UTC()
	}
	enabled := boolInt(source.Enabled)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sources (type, name, url, enabled, interval_seconds, config_json, next_run_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(type, name, url) DO UPDATE SET
	enabled = excluded.enabled,
	interval_seconds = excluded.interval_seconds,
	config_json = excluded.config_json,
	updated_at = excluded.updated_at
`, source.Type, source.Name, source.URL, enabled, source.IntervalSeconds, source.ConfigJSON, dbTime(nextRun), now, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM sources WHERE type = ? AND name = ? AND url = ?`, source.Type, source.Name, source.URL).Scan(&id)
	return id, err
}

func (s *Store) ListSources(ctx context.Context) ([]model.Source, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, type, name, url, enabled, interval_seconds, config_json, checkpoint, etag, last_modified, next_run_at, COALESCE(last_run_at, ''), last_error, error_count, created_at, updated_at FROM sources ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Source
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, source)
	}
	return out, rows.Err()
}

func (s *Store) DueSources(ctx context.Context, limit int) ([]model.Source, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, type, name, url, enabled, interval_seconds, config_json, checkpoint, etag, last_modified, next_run_at, COALESCE(last_run_at, ''), last_error, error_count, created_at, updated_at FROM sources WHERE enabled = 1 AND next_run_at <= ? ORDER BY next_run_at LIMIT ?`, dbTime(time.Now()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Source
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, source)
	}
	return out, rows.Err()
}

func (s *Store) MarkSourceSuccess(ctx context.Context, id int64, checkpoint, etag, lastModified string, nextRun time.Time) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE sources
SET checkpoint = ?, etag = ?, last_modified = ?, next_run_at = ?, last_run_at = ?, last_error = '', error_count = 0, updated_at = ?
WHERE id = ?
`, checkpoint, etag, lastModified, dbTime(nextRun), dbTime(time.Now()), dbTime(time.Now()), id)
	return err
}

func (s *Store) MarkSourceFailure(ctx context.Context, id int64, message string, nextRun time.Time) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE sources
SET next_run_at = ?, last_run_at = ?, last_error = ?, error_count = error_count + 1, updated_at = ?
WHERE id = ?
`, dbTime(nextRun), dbTime(time.Now()), message, dbTime(time.Now()), id)
	return err
}

func (s *Store) InsertItem(ctx context.Context, item model.Item) (int64, bool, error) {
	if item.SourceID == 0 {
		return 0, false, errors.New("item source_id is required")
	}
	if item.ExternalID == "" {
		item.ExternalID = stableID(item.SourceName + "\n" + item.URL + "\n" + item.Title + "\n" + item.Content)
	}
	if item.FetchedAt.IsZero() {
		item.FetchedAt = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO items (source_id, source_type, source_name, external_id, url, title, content, author, published_at, fetched_at, raw_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, item.SourceID, item.SourceType, item.SourceName, item.ExternalID, item.URL, item.Title, item.Content, item.Author, dbTime(item.PublishedAt), dbTime(item.FetchedAt), item.RawJSON)
	if err != nil {
		return 0, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM items WHERE source_id = ? AND external_id = ?`, item.SourceID, item.ExternalID).Scan(&id)
	return id, affected > 0, err
}

func (s *Store) SearchItems(ctx context.Context, query string, limit int) ([]model.Item, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return s.queryItems(ctx, `SELECT id, source_id, source_type, source_name, external_id, url, title, content, author, published_at, fetched_at, raw_json FROM items ORDER BY fetched_at DESC LIMIT ?`, limit)
	}
	if s.hasFTS {
		items, err := s.queryItems(ctx, `SELECT i.id, i.source_id, i.source_type, i.source_name, i.external_id, i.url, i.title, i.content, i.author, i.published_at, i.fetched_at, i.raw_json FROM items_fts f JOIN items i ON i.id = f.rowid WHERE items_fts MATCH ? ORDER BY i.fetched_at DESC LIMIT ?`, query, limit)
		if err == nil {
			return items, nil
		}
	}
	like := "%" + query + "%"
	return s.queryItems(ctx, `SELECT id, source_id, source_type, source_name, external_id, url, title, content, author, published_at, fetched_at, raw_json FROM items WHERE title LIKE ? OR content LIKE ? OR url LIKE ? ORDER BY fetched_at DESC LIMIT ?`, like, like, like, limit)
}

func (s *Store) queryItems(ctx context.Context, query string, args ...any) ([]model.Item, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertRule(ctx context.Context, rule model.Rule) (int64, error) {
	if rule.Type == "" {
		rule.Type = "keyword"
	}
	now := dbTime(time.Now())
	_, err := s.db.ExecContext(ctx, `
INSERT INTO rules (name, type, pattern, case_sensitive, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
	type = excluded.type,
	pattern = excluded.pattern,
	case_sensitive = excluded.case_sensitive,
	enabled = excluded.enabled,
	updated_at = excluded.updated_at
`, rule.Name, rule.Type, rule.Pattern, boolInt(rule.CaseSensitive), boolInt(rule.Enabled), now, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM rules WHERE name = ?`, rule.Name).Scan(&id)
	return id, err
}

func (s *Store) ActiveRules(ctx context.Context) ([]model.Rule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, type, pattern, case_sensitive, enabled, created_at, updated_at FROM rules WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRules(rows)
}

func (s *Store) ListRules(ctx context.Context) ([]model.Rule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, type, pattern, case_sensitive, enabled, created_at, updated_at FROM rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRules(rows)
}

func (s *Store) InsertMatch(ctx context.Context, match model.Match) (bool, error) {
	if match.Score == 0 {
		match.Score = 1
	}
	res, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO matches (item_id, rule_id, matched_text, score, created_at)
VALUES (?, ?, ?, ?, ?)
`, match.ItemID, match.RuleID, match.MatchedText, match.Score, dbTime(time.Now()))
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

func (s *Store) RecentMatches(ctx context.Context, since time.Time, limit int) ([]model.Match, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT
	m.id, m.item_id, m.rule_id, m.matched_text, m.score, m.created_at,
	i.id, i.source_id, i.source_type, i.source_name, i.external_id, i.url, i.title, i.content, i.author, i.published_at, i.fetched_at, i.raw_json,
	r.id, r.name, r.type, r.pattern, r.case_sensitive, r.enabled, r.created_at, r.updated_at
FROM matches m
JOIN items i ON i.id = m.item_id
JOIN rules r ON r.id = m.rule_id
WHERE m.created_at >= ?
ORDER BY m.created_at DESC
LIMIT ?
`, dbTime(since), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Match
	for rows.Next() {
		var match model.Match
		var matchCreated, itemPublished, itemFetched, ruleCreated, ruleUpdated string
		var caseSensitive, ruleEnabled int
		if err := rows.Scan(
			&match.ID, &match.ItemID, &match.RuleID, &match.MatchedText, &match.Score, &matchCreated,
			&match.Item.ID, &match.Item.SourceID, &match.Item.SourceType, &match.Item.SourceName, &match.Item.ExternalID, &match.Item.URL, &match.Item.Title, &match.Item.Content, &match.Item.Author, &itemPublished, &itemFetched, &match.Item.RawJSON,
			&match.Rule.ID, &match.Rule.Name, &match.Rule.Type, &match.Rule.Pattern, &caseSensitive, &ruleEnabled, &ruleCreated, &ruleUpdated,
		); err != nil {
			return nil, err
		}
		match.CreatedAt = parseDBTime(matchCreated)
		match.Item.PublishedAt = parseDBTime(itemPublished)
		match.Item.FetchedAt = parseDBTime(itemFetched)
		match.Rule.CaseSensitive = caseSensitive == 1
		match.Rule.Enabled = ruleEnabled == 1
		match.Rule.CreatedAt = parseDBTime(ruleCreated)
		match.Rule.UpdatedAt = parseDBTime(ruleUpdated)
		out = append(out, match)
	}
	return out, rows.Err()
}

func (s *Store) InsertOutbox(ctx context.Context, msg model.OutboxMessage) (int64, error) {
	if msg.Channel == "" {
		msg.Channel = "stdout"
	}
	if msg.Status == "" {
		msg.Status = "pending"
	}
	if msg.AvailableAt.IsZero() {
		msg.AvailableAt = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO notification_outbox (channel, destination, subject, body, status, attempts, available_at, last_error, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, msg.Channel, msg.Destination, msg.Subject, msg.Body, msg.Status, msg.Attempts, dbTime(msg.AvailableAt), msg.LastError, dbTime(time.Now()))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) PendingOutbox(ctx context.Context, limit int) ([]model.OutboxMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, channel, destination, subject, body, status, attempts, available_at, last_error, created_at, COALESCE(sent_at, '') FROM notification_outbox WHERE status = 'pending' AND available_at <= ? ORDER BY available_at LIMIT ?`, dbTime(time.Now()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.OutboxMessage
	for rows.Next() {
		var msg model.OutboxMessage
		var available, created, sent string
		if err := rows.Scan(&msg.ID, &msg.Channel, &msg.Destination, &msg.Subject, &msg.Body, &msg.Status, &msg.Attempts, &available, &msg.LastError, &created, &sent); err != nil {
			return nil, err
		}
		msg.AvailableAt = parseDBTime(available)
		msg.CreatedAt = parseDBTime(created)
		msg.SentAt = parseDBTime(sent)
		out = append(out, msg)
	}
	return out, rows.Err()
}

func (s *Store) MarkOutboxSent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notification_outbox SET status = 'sent', sent_at = ?, last_error = '' WHERE id = ?`, dbTime(time.Now()), id)
	return err
}

func (s *Store) MarkOutboxRetry(ctx context.Context, id int64, message string, delay time.Duration) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE notification_outbox SET attempts = attempts + 1, available_at = ?, last_error = ? WHERE id = ?`, dbTime(time.Now().Add(delay)), message, id)
	return err
}

func (s *Store) SaveDigest(ctx context.Context, digest model.Digest) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO digests (window_start, window_end, subject, body, created_at)
VALUES (?, ?, ?, ?, ?)
`, dbTime(digest.WindowStart), dbTime(digest.WindowEnd), digest.Subject, digest.Body, dbTime(time.Now()))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) RecentDigests(ctx context.Context, limit int) ([]model.Digest, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, window_start, window_end, subject, body, created_at FROM digests ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Digest
	for rows.Next() {
		var digest model.Digest
		var start, end, created string
		if err := rows.Scan(&digest.ID, &start, &end, &digest.Subject, &digest.Body, &created); err != nil {
			return nil, err
		}
		digest.WindowStart = parseDBTime(start)
		digest.WindowEnd = parseDBTime(end)
		digest.CreatedAt = parseDBTime(created)
		out = append(out, digest)
	}
	return out, rows.Err()
}

func scanSource(scanner interface{ Scan(dest ...any) error }) (model.Source, error) {
	var source model.Source
	var enabled int
	var nextRun, lastRun, created, updated string
	err := scanner.Scan(&source.ID, &source.Type, &source.Name, &source.URL, &enabled, &source.IntervalSeconds, &source.ConfigJSON, &source.Checkpoint, &source.ETag, &source.LastModified, &nextRun, &lastRun, &source.LastError, &source.ErrorCount, &created, &updated)
	if err != nil {
		return model.Source{}, err
	}
	source.Enabled = enabled == 1
	source.NextRunAt = parseDBTime(nextRun)
	source.LastRunAt = parseDBTime(lastRun)
	source.CreatedAt = parseDBTime(created)
	source.UpdatedAt = parseDBTime(updated)
	return source, nil
}

func scanItem(scanner interface{ Scan(dest ...any) error }) (model.Item, error) {
	var item model.Item
	var published, fetched string
	err := scanner.Scan(&item.ID, &item.SourceID, &item.SourceType, &item.SourceName, &item.ExternalID, &item.URL, &item.Title, &item.Content, &item.Author, &published, &fetched, &item.RawJSON)
	if err != nil {
		return model.Item{}, err
	}
	item.PublishedAt = parseDBTime(published)
	item.FetchedAt = parseDBTime(fetched)
	return item, nil
}

func scanRules(rows *sql.Rows) ([]model.Rule, error) {
	var out []model.Rule
	for rows.Next() {
		var rule model.Rule
		var caseSensitive, enabled int
		var created, updated string
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Type, &rule.Pattern, &caseSensitive, &enabled, &created, &updated); err != nil {
			return nil, err
		}
		rule.CaseSensitive = caseSensitive == 1
		rule.Enabled = enabled == 1
		rule.CreatedAt = parseDBTime(created)
		rule.UpdatedAt = parseDBTime(updated)
		out = append(out, rule)
	}
	return out, rows.Err()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func dbTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseDBTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	return time.Time{}
}

func stableID(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func FormatError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T: %v", err, err)
}

const baseSchema = `
CREATE TABLE IF NOT EXISTS sources (
	id INTEGER PRIMARY KEY,
	type TEXT NOT NULL,
	name TEXT NOT NULL,
	url TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	interval_seconds INTEGER NOT NULL DEFAULT 300,
	config_json TEXT NOT NULL DEFAULT '{}',
	checkpoint TEXT NOT NULL DEFAULT '',
	etag TEXT NOT NULL DEFAULT '',
	last_modified TEXT NOT NULL DEFAULT '',
	next_run_at TEXT NOT NULL DEFAULT '',
	last_run_at TEXT,
	last_error TEXT NOT NULL DEFAULT '',
	error_count INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT '',
	UNIQUE(type, name, url)
);

CREATE TABLE IF NOT EXISTS items (
	id INTEGER PRIMARY KEY,
	source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
	source_type TEXT NOT NULL,
	source_name TEXT NOT NULL,
	external_id TEXT NOT NULL,
	url TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	author TEXT NOT NULL DEFAULT '',
	published_at TEXT NOT NULL DEFAULT '',
	fetched_at TEXT NOT NULL,
	raw_json TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(source_id, external_id)
);

CREATE INDEX IF NOT EXISTS items_source_idx ON items(source_id, fetched_at);
CREATE INDEX IF NOT EXISTS items_fetched_idx ON items(fetched_at);
CREATE INDEX IF NOT EXISTS items_published_idx ON items(published_at);

CREATE TABLE IF NOT EXISTS rules (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	type TEXT NOT NULL CHECK(type IN ('keyword', 'regex')),
	pattern TEXT NOT NULL,
	case_sensitive INTEGER NOT NULL DEFAULT 0,
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS matches (
	id INTEGER PRIMARY KEY,
	item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	rule_id INTEGER NOT NULL REFERENCES rules(id) ON DELETE CASCADE,
	matched_text TEXT NOT NULL DEFAULT '',
	score REAL NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT '',
	UNIQUE(item_id, rule_id, matched_text)
);

CREATE INDEX IF NOT EXISTS matches_rule_idx ON matches(rule_id, created_at);
CREATE INDEX IF NOT EXISTS matches_item_idx ON matches(item_id);
CREATE INDEX IF NOT EXISTS matches_created_idx ON matches(created_at);

CREATE TABLE IF NOT EXISTS notification_outbox (
	id INTEGER PRIMARY KEY,
	channel TEXT NOT NULL DEFAULT 'stdout',
	destination TEXT NOT NULL DEFAULT '',
	subject TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	attempts INTEGER NOT NULL DEFAULT 0,
	available_at TEXT NOT NULL DEFAULT '',
	last_error TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT '',
	sent_at TEXT
);

CREATE INDEX IF NOT EXISTS outbox_pending_idx ON notification_outbox(status, available_at);

CREATE TABLE IF NOT EXISTS digests (
	id INTEGER PRIMARY KEY,
	window_start TEXT NOT NULL,
	window_end TEXT NOT NULL,
	subject TEXT NOT NULL,
	body TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS digests_created_idx ON digests(created_at);
`

const ftsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(title, content, url, content='items', content_rowid='id');

CREATE TRIGGER IF NOT EXISTS items_ai AFTER INSERT ON items BEGIN
	INSERT INTO items_fts(rowid, title, content, url) VALUES (new.id, new.title, new.content, new.url);
END;

CREATE TRIGGER IF NOT EXISTS items_ad AFTER DELETE ON items BEGIN
	INSERT INTO items_fts(items_fts, rowid, title, content, url) VALUES ('delete', old.id, old.title, old.content, old.url);
END;

CREATE TRIGGER IF NOT EXISTS items_au AFTER UPDATE ON items BEGIN
	INSERT INTO items_fts(items_fts, rowid, title, content, url) VALUES ('delete', old.id, old.title, old.content, old.url);
	INSERT INTO items_fts(rowid, title, content, url) VALUES (new.id, new.title, new.content, new.url);
END;
`
