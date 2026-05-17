# Sunbreak

Sunbreak is an MVP F5Bot-like keyword monitoring service for a single cheap VPS.

It is designed around local primitives:

- Go single binary
- SQLite WAL for persistence
- SQLite FTS5 when available
- local source checkpoints
- local notification outbox
- systemd-friendly process model
- Docker Compose for simple deployment

## Features

- RSS/Atom collector with `ETag` and `Last-Modified` conditional requests
- Hacker News collector using Algolia `search_by_date` discovery and the HN Firebase item API for thread fetches
- Reddit collector adapter with mock mode when OAuth credentials are absent
- keyword and regex matching
- persisted items, matches, outbox messages, and digests
- persisted Hacker News thread relations and extracted item links
- basic HTTP API
- config-file based administration

## Source Policy

Sunbreak is intended to be a self-hosted, personal/hobby keyword monitoring tool. It should be used with official APIs, feeds, webhooks, or other publisher-supported access paths whenever they are available.

This project is not intended for bypassing rate limits, access controls, robots policies, paywalls, login walls, or platform restrictions. Do not use proxy rotation, credential sharing, browser automation, or other evasive techniques to access data that a source does not intend to make available to your app.

For every source you enable:

- read and follow the source's terms of service, developer terms, API terms, and documentation
- identify your app honestly with OAuth credentials or an appropriate user agent when required
- respect rate-limit headers, `Retry-After`, backoff responses, and documented polling guidance
- prefer push/feed/API-first access before HTML crawling
- keep polling intervals conservative and add jitter for repeated requests
- store only what you need for monitoring, search, and auditability
- avoid collecting private, deleted, gated, or otherwise restricted content
- avoid using collected user content to train AI/ML models unless you have the rights and permissions required by the source

### Reddit

Reddit is a priority source for Sunbreak, but it has specific developer and data-use requirements. Before enabling Reddit ingestion, review:

- [Reddit Developer Terms](https://redditinc.com/policies/developer-terms)
- [Reddit Data API Terms](https://redditinc.com/policies/data-api-terms)
- [Reddit Data API Wiki](https://support.reddithelp.com/hc/en-us/articles/16160319875092-Reddit-Data-API-Wiki)
- [Reddit API documentation](https://www.reddit.com/dev/api/)

The Reddit Data API Wiki currently states that eligible free Data API use requires OAuth and is rate limited at 100 queries per minute per OAuth client ID. Traffic not using OAuth or login credentials may be blocked, and the default rate limit may not apply.

Sunbreak's Reddit adapter should therefore:

- require user-provided OAuth credentials for live ingestion
- read and obey `X-Ratelimit-*` headers
- back off on `429`, `403`, and transient `5xx` responses
- support scoped polling, such as explicit subreddits or search queries, instead of broad collection
- default to conservative polling intervals
- avoid storing full raw payloads longer than needed
- avoid automated posting, voting, messaging, moderation actions, or user impersonation

If your intended use goes beyond personal self-hosted monitoring, or if you want large-scale Reddit data access, review Reddit's current commercial/data licensing requirements before using Sunbreak.

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

## Dashboard

The local dashboard lives in `web/` and uses Vite, React, Tailwind CSS, and shadcn-style base components.

Run the API in one terminal:

```sh
go run -tags sqlite_fts5 ./cmd/sunbreak -config config.yaml
```

Run the dashboard in another terminal:

```sh
cd web
npm install
npm run dev
```

Open `http://localhost:5173/`. The Vite dev server proxies `/api` and `/healthz` to `http://localhost:8080`.

Trigger collection manually:

```sh
curl -X POST http://localhost:8080/api/collect
```

List matches:

```sh
curl http://localhost:8080/api/matches
```

Search ingested items:

```sh
curl 'http://localhost:8080/api/items?query=sqlite&limit=20'
```

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

### Agent-Friendly CLI

Sunbreak's CLI is designed to be safe for automation and AI agents:

- commands are non-interactive and flag-driven
- `-output json` keeps stdout machine-readable for one-shot commands
- diagnostics and logs go to stderr when JSON output is requested
- `-describe` returns a runtime JSON schema for supported flags and examples
- future mutating commands, including backfill runs, should support `-dry-run`
- future large result commands should support field selection, pagination, or NDJSON streaming
- input validation should reject path traversal, control characters, embedded query strings in IDs, and double-encoded values before touching external APIs or local state
- [CONTEXT.md](CONTEXT.md) captures the short agent operating contract for tools that load repository context

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

## Test Suites

Run the Go test suite with SQLite FTS5 enabled:

```sh
go test -tags sqlite_fts5 ./...
```

The early verification suite covers:

- collector behavior with controlled HTTP status codes, headers, response shape, and item counts
- RSS/Atom parsing, checkpoint handling, and conditional request metadata
- Hacker News query construction and response normalization
- Hacker News full-thread expansion, nested relation preservation, and link extraction
- parse/preprocess -> SQLite persistence -> FTS search -> match/outbox/digest pipeline

Run the dashboard checks:

```sh
cd web
npm run lint
npm run build
```

Run a local Docker deployment smoke test:

```sh
scripts/smoke/docker-local.sh
```

The smoke script builds the image, starts a temporary container on `127.0.0.1:18080`, checks `/healthz`, and removes the container.

## Benchmarks

Run the focused benchmark suite:

```sh
go test -tags sqlite_fts5 -run '^$' -bench . -benchmem ./internal/matcher ./internal/storage
```

The current benchmarks cover:

- matcher rule compilation
- matcher throughput for 100 and 1,000 keyword rules
- mixed keyword and regex matching
- SQLite item inserts
- SQLite FTS item search
- recent match reads over seeded match data

## Deployment

Build a Linux binary:

```sh
go build -tags sqlite_fts5 -o sunbreak ./cmd/sunbreak
```

Run with systemd using [deployments/systemd/sunbreak.service](deployments/systemd/sunbreak.service).

Or use Docker Compose:

```sh
docker compose up --build
```

## Data Retention

This MVP does not yet enforce retention automatically. The intended first policy is:

- raw item payloads: 7-30 days
- normalized items: 30-180 days
- matches: 1-2 years
- digests and aggregates: long-lived

## Current Non-Goals

- no response/reply workflows
- no proxy rotation
- no Kafka/Redis/Elasticsearch/Kubernetes
- no full-world firehose indexing
- no production Reddit OAuth ingestion yet
- no LLM provider integration yet; digest is deterministic and asynchronous
