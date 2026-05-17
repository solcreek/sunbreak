package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sunbreak/internal/backfill"
)

func runBackfillCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		writeBackfillError(stderr, "usage: sunbreak backfill probe hackernews --query <term> --since 365d --output json")
		return 2
	}
	mode := args[0]
	source := args[1]
	switch mode {
	case "probe":
		if source != "hackernews" {
			writeBackfillError(stderr, "unsupported backfill source: "+source)
			return 2
		}
		if err := runHackerNewsProbe(ctx, args[2:], stdout, stderr); err != nil {
			writeBackfillError(stderr, err.Error())
			return 1
		}
		return 0
	default:
		writeBackfillError(stderr, "unsupported backfill mode: "+mode)
		return 2
	}
}

func runHackerNewsProbe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sunbreak backfill probe hackernews", flag.ContinueOnError)
	fs.SetOutput(stderr)
	query := fs.String("query", "", "Keyword or query to probe")
	keywords := fs.String("keywords", "", "Comma-separated keywords to probe")
	fromRaw := fs.String("from", "", "Start date/time, YYYY-MM-DD or RFC3339")
	toRaw := fs.String("to", "", "End date/time, YYYY-MM-DD or RFC3339. Defaults to now")
	sinceRaw := fs.String("since", "", "Relative window such as 24h, 30d, 52w, or 1y")
	output := fs.String("output", "json", "Output format: json or text")
	maxSliceHits := fs.Int("max-slice-hits", backfill.DefaultHackerNewsMaxSliceHits, "Target maximum hits per planned time slice")
	maxSlices := fs.Int("max-slices", backfill.DefaultHackerNewsMaxSlices, "Maximum planned time slices before truncating")
	endpoint := fs.String("endpoint", backfill.DefaultHackerNewsEndpoint, "Hacker News Algolia endpoint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	queries := splitKeywords(*query, *keywords)
	if len(queries) == 0 {
		return errors.New("--query or --keywords is required")
	}
	from, to, err := parseBackfillWindow(*fromRaw, *toRaw, *sinceRaw, time.Now().UTC())
	if err != nil {
		return err
	}

	results := make([]backfill.HackerNewsProbeResult, 0, len(queries))
	for _, q := range queries {
		result, err := backfill.ProbeHackerNews(ctx, http.DefaultClient, backfill.HackerNewsProbeOptions{
			Endpoint:     *endpoint,
			Query:        q,
			From:         from,
			To:           to,
			MaxSliceHits: *maxSliceHits,
			MaxSlices:    *maxSlices,
		})
		if err != nil {
			return err
		}
		results = append(results, result)
	}
	payload := map[string]any{
		"ok":      true,
		"command": "backfill probe",
		"source":  "hackernews",
		"queries": results,
	}
	if *output == "json" {
		writeJSON(stdout, payload)
		return nil
	}
	for _, result := range results {
		fmt.Fprintf(stdout, "%s: %d hits, %d planned slices", result.Query, result.TotalHits, len(result.Slices))
		if result.Truncated {
			fmt.Fprint(stdout, " (truncated)")
		}
		fmt.Fprintln(stdout)
		for _, warning := range result.Warnings {
			fmt.Fprintf(stdout, "warning: %s\n", warning)
		}
	}
	return nil
}

func splitKeywords(query, keywords string) []string {
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range out {
			if existing == value {
				return
			}
		}
		out = append(out, value)
	}
	add(query)
	for _, part := range strings.Split(keywords, ",") {
		add(part)
	}
	return out
}

func parseBackfillWindow(fromRaw, toRaw, sinceRaw string, now time.Time) (time.Time, time.Time, error) {
	to := now.UTC()
	var err error
	if strings.TrimSpace(toRaw) != "" {
		to, err = parseBackfillTime(toRaw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if strings.TrimSpace(sinceRaw) != "" {
		duration, err := parseBackfillDuration(sinceRaw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		return to.Add(-duration).UTC(), to, nil
	}
	if strings.TrimSpace(fromRaw) == "" {
		return time.Time{}, time.Time{}, errors.New("--from or --since is required")
	}
	from, err := parseBackfillTime(fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, errors.New("--from must be before --to")
	}
	return from.UTC(), to, nil
}

func parseBackfillTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("time value is required")
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q; use YYYY-MM-DD or RFC3339", value)
}

func parseBackfillDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("duration is required")
	}
	if d, err := time.ParseDuration(value); err == nil {
		return d, nil
	}
	units := map[string]time.Duration{
		"d":  24 * time.Hour,
		"w":  7 * 24 * time.Hour,
		"mo": 30 * 24 * time.Hour,
		"y":  365 * 24 * time.Hour,
	}
	for suffix, unit := range units {
		if strings.HasSuffix(value, suffix) {
			raw := strings.TrimSuffix(value, suffix)
			var count int
			if _, err := fmt.Sscanf(raw, "%d", &count); err != nil || count <= 0 {
				return 0, fmt.Errorf("invalid duration %q", value)
			}
			return time.Duration(count) * unit, nil
		}
	}
	return 0, fmt.Errorf("invalid duration %q; use 24h, 30d, 52w, 12mo, or 1y", value)
}

func writeBackfillError(stderr io.Writer, message string) {
	fmt.Fprintln(stderr, "backfill:", message)
}
