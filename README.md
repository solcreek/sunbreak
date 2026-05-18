# Sunbreak

Sunbreak is a local-first keyword monitoring and research service inspired by
tools like F5Bot. It collects supported public sources, matches keywords and
regular expressions, stores the results locally, and exposes both an HTTP API
and an agent-friendly CLI.

The first target is a cheap single VPS, such as Hetzner. The implementation
therefore favors simple primitives:

- Go single binary
- SQLite WAL for persistence
- SQLite FTS5 for local search
- config-file administration
- local notification outbox
- systemd-friendly long-running process
- optional Docker Compose deployment

Sunbreak is an MVP. It is useful for local monitoring, Hacker News research,
and validating source adapters. It is not a full social listening platform.

## Why Sunbreak?

Some sources already expose useful search APIs. Hacker News is the clearest
example: the public Algolia API is excellent for quick discovery, historical
searches, and ad hoc analysis.

Using a source API directly is often the right choice when you need a one-off
answer:

- fewer moving parts
- no local database to operate
- fast experiments with `curl`, `jq`, or notebooks
- direct access to the source's current search behavior

Sunbreak adds value when the work needs to become repeatable, local, and
operational:

- query plans are explicit and reproducible
- historical probes can split large ranges before pagination caps hide data
- results can be cached in SQLite and searched locally with FTS5
- Hacker News stories can be enriched with complete nested comment trees
- links, relations, matches, digests, and source checkpoints are persisted
- monitoring can continue forward from the backfilled or probed baseline
- automation can use stable JSON output instead of source-specific API shapes

In short:

```text
HN Algolia API = excellent raw discovery/search API
Sunbreak       = local monitoring and research memory built on top of sources
```

Sunbreak should not hide the existence of good source APIs. It should make
them safer to use repeatedly, easier to audit, and more useful for long-running
monitoring and analysis.

## What Works

- RSS and Atom collection with `ETag` and `Last-Modified` support
- Hacker News collection through Algolia discovery and the HN Firebase item API
- Full Hacker News comment tree ingestion with nested relations preserved
- Link extraction from Hacker News items and comments
- Reddit adapter interface with mock mode when credentials are absent
- Keyword and regex rules
- Match persistence
- Digest generation
- Notification outbox with stdout dispatch
- Basic HTTP API
- Vite + React + Tailwind dashboard
- Agent-friendly JSON CLI output
- Read-only Hacker News backfill probe for historical range planning

## Source Policy

Sunbreak should be used with official APIs, RSS/Atom feeds, webhooks, public
archives, or other publisher-supported access paths.

Do not use Sunbreak to bypass rate limits, paywalls, login walls, robots
policies, access controls, platform restrictions, or source terms. Do not add
proxy rotation, credential sharing, stealth browser automation, or similar
evasive behavior.

For every enabled source:

- read the source's current terms, developer policy, and API documentation
- identify the app honestly when credentials or user agents are required
- respect rate limits, `Retry-After`, `429`, `403`, and transient `5xx`
  responses
- prefer API, feed, or push-based access before HTML crawling
- poll conservatively and add jitter when a source is checked repeatedly
- store only what is needed for monitoring, search, and auditability
- avoid collecting private, deleted, gated, or otherwise restricted content
- do not use collected user content for model training unless you have the
  necessary rights and permissions

### Reddit

Reddit support is intentionally conservative. Live Reddit ingestion should use
approved OAuth credentials and Reddit's current Data API rules. The MVP keeps a
Reddit adapter interface and mock path so the rest of the pipeline can be
tested without scraping Reddit.

For Reddit, Sunbreak should prefer:

- explicit subreddit watchlists
- read-only ingestion
- conservative polling
- rate-limit-aware backoff
- official API access once approved
- RSS only as low-frequency discovery, not as a complete historical source

Sunbreak should not use unofficial Reddit scrapers, browser-cookie sidecars, or
HTML scraping as default data sources.

## Quick Start

```sh
cp config.example.yaml config.yaml
mkdir -p data
go mod download
go run -tags sqlite_fts5 ./cmd/sunbreak -config config.yaml
```

Health check:

```sh
curl http://localhost:8080/healthz
```

Run one collection pass:

```sh
go run -tags sqlite_fts5 ./cmd/sunbreak -config config.yaml -collect-once
```

## Dashboard

The dashboard lives in `web/` and uses Vite, React, Tailwind CSS, and
shadcn-style base components.

Run the API:

```sh
go run -tags sqlite_fts5 ./cmd/sunbreak -config config.yaml
```

Run the dashboard:

```sh
cd web
npm install
npm run dev
```

Open `http://localhost:5173/`. The Vite dev server proxies `/api` and
`/healthz` to `http://localhost:8080`.

## CLI

```sh
sunbreak -config config.yaml
sunbreak -config config.yaml -migrate
sunbreak -config config.yaml -collect-once
sunbreak -config config.yaml -digest-once
sunbreak -config config.yaml -dispatch-outbox
sunbreak -describe
sunbreak -config config.yaml -collect-once -output json
```

### Hacker News Backfill Probe

Forward collection is intentionally conservative. If local data is too sparse
for historical analysis, use the read-only backfill probe before calling source
APIs directly:

```sh
sunbreak backfill probe hackernews --query cloudflare --since 1y --output json
sunbreak backfill probe hackernews --keywords cloudflare,workers,pages --from 2024-01-01 --to 2026-05-17 --output json
```

The probe estimates Hacker News Algolia hit counts and returns a time-slice
plan. It does not write local state. A future `backfill run` should reuse the
same slicing strategy, support `--dry-run`, write through SQLite
de-duplication, and optionally enrich results with full HN thread data.

### Agent-Friendly Operation

Sunbreak is designed to be usable by automation and AI agents:

- commands are non-interactive and flag-driven
- `-output json` keeps stdout machine-readable for one-shot commands
- diagnostics and logs go to stderr when JSON output is requested
- `-describe` returns a runtime JSON schema for supported flags and examples
- `backfill probe hackernews` covers the "local data is too sparse" path
- future mutating commands should support `--dry-run`
- large result commands should support limits, pagination, or NDJSON streaming

See [CONTEXT.md](CONTEXT.md) for the short agent operating contract.

## HTTP API

- `GET /healthz`
- `GET /api/sources`
- `GET /api/rules`
- `POST /api/rules`
- `GET /api/items?query=&limit=`
- `GET /api/matches?hours=&limit=`
- `GET /api/digests?limit=`
- `POST /api/collect`
- `POST /api/digest`
- `POST /api/outbox/dispatch`

Create or update a rule:

```sh
curl -X POST http://localhost:8080/api/rules \
  -H 'Content-Type: application/json' \
  -d '{"name":"My Product","type":"keyword","pattern":"my product","enabled":true}'
```

Search ingested items:

```sh
curl 'http://localhost:8080/api/items?query=sqlite&limit=20'
```

List recent matches:

```sh
curl 'http://localhost:8080/api/matches?hours=24&limit=20'
```

## Configuration

Start from [config.example.yaml](config.example.yaml). The example includes:

- one Hacker News source
- one RSS source
- one Reddit adapter source in mock mode
- sample keyword and regex rules
- stdout notification dispatch

Local database files live under `data/` by default and are ignored by git.

## Testing

Run Go tests with SQLite FTS5 enabled:

```sh
go test -tags sqlite_fts5 ./...
```

Run the coverage gate:

```sh
scripts/test/coverage.sh
```

Run dashboard checks:

```sh
cd web
npm run lint
npm run build
```

Run a local Docker smoke test:

```sh
scripts/smoke/docker-local.sh
```

The current test suite covers collectors, source checkpointing, Hacker News
thread expansion, RSS parsing, SQLite persistence, FTS search, matching,
digests, outbox dispatch, HTTP endpoints, and CLI backfill probing.

## Benchmarks

```sh
go test -tags sqlite_fts5 -run '^$' -bench . -benchmem ./internal/matcher ./internal/storage
```

Benchmarks currently cover matcher compilation, keyword/regex matching,
SQLite item insertion, FTS search, and recent match reads.

## Deployment

Build a Linux binary:

```sh
go build -tags sqlite_fts5 -o sunbreak ./cmd/sunbreak
```

Run with systemd using
[deployments/systemd/sunbreak.service](deployments/systemd/sunbreak.service).

Or use Docker Compose:

```sh
docker compose up --build
```

## Roadmap

- `backfill run hackernews` for historical import with dry-run support
- topic aggregation API and dashboard view
- HN opportunity-analysis recipes for recurring pain and market research
- source presets for company blogs and changelogs
- credentialed Reddit API adapter after approval-oriented design work
- richer notification channels

## License

No license file has been added yet.
