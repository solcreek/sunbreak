#!/usr/bin/env bash
set -euo pipefail

image="${IMAGE:-sunbreak:smoke}"
name="${NAME:-sunbreak-smoke-$$}"
port="${PORT:-18080}"

cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker build -t "$image" .
docker run --rm -d --name "$name" -p "${port}:8080" "$image" >/dev/null

for _ in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:${port}/healthz" >/tmp/sunbreak-smoke-health.json; then
    cat /tmp/sunbreak-smoke-health.json
    echo
    exit 0
  fi
  sleep 1
done

docker logs "$name"
echo "sunbreak container did not become healthy on port ${port}" >&2
exit 1
