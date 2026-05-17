# Sunbreak Agent Context

Sunbreak is frequently operated by automation and AI agents.

Use these defaults:

- Prefer `sunbreak -describe` before constructing commands from memory.
- Prefer `-output json` for one-shot commands.
- Treat stdout as data and stderr as diagnostics when `-output json` is used.
- Do not rely on interactive prompts; commands must be fully specified by flags or JSON payloads.
- Use probe or dry-run modes before commands that can write data, crawl large ranges, or dispatch notifications.
- Keep result sets bounded. Prefer explicit limits, fields, pagination, or NDJSON streaming for large reads.
- Validate generated inputs before invoking commands: reject control characters, path traversal, embedded query strings in resource IDs, and pre-encoded values that could be double-encoded.

Backfill-specific guidance:

- Start with probe mode before run mode.
- Use narrow keywords first; broad keywords like `ai` can produce millions of Hacker News hits.
- Prefer time slicing for historical imports because public APIs may cap direct pagination.
- Respect source terms, rate limits, and conservative request pacing.
