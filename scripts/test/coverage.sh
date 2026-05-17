#!/usr/bin/env sh
set -eu

threshold="${COVERAGE_THRESHOLD:-60.0}"
profile="${COVERAGE_PROFILE:-/tmp/sunbreak-cover.out}"

go test -tags sqlite_fts5 -coverprofile="$profile" ./...
total="$(go tool cover -func="$profile" | awk '/^total:/ { sub(/%/, "", $3); print $3 }')"

awk -v total="$total" -v threshold="$threshold" 'BEGIN {
  if (total + 0 < threshold + 0) {
    printf("coverage %.1f%% is below threshold %.1f%%\n", total, threshold) > "/dev/stderr"
    exit 1
  }
  printf("coverage %.1f%% meets threshold %.1f%%\n", total, threshold)
}'
