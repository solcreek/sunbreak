# Radar

Radar is an MVP F5Bot-like keyword monitoring service for a single cheap VPS.

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
- Hacker News collector using Algolia `search_by_date`
- Reddit collector adapter with mock mode when OAuth credentials are absent
- keyword and regex matching
- persisted items, matches, outbox messages, and digests
- basic HTTP API
- config-file based administration

## Quick Start

```sh
cp config.example.yaml config.yaml
mkdir -p data
go mod download
go run -tags sqlite_fts5 ./cmd/radar -config config.yaml
```

Health check:

```sh
curl http://localhost:8080/healthz
```

## Dashboard

The local dashboard lives in `web/` and uses Vite, React, Tailwind CSS, and shadcn-style base components.

Run the API in one terminal:

```sh
go run -tags sqlite_fts5 ./cmd/radar -config config.yaml
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
radar -config config.yaml
radar -config config.yaml -migrate
radar -config config.yaml -collect-once
radar -config config.yaml -digest-once
radar -config config.yaml -dispatch-outbox
```

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
go build -tags sqlite_fts5 -o radar ./cmd/radar
```

Run with systemd using [deployments/systemd/radar.service](deployments/systemd/radar.service).

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
