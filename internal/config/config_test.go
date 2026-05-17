package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAndApplyDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Server.Addr != ":8080" {
		t.Fatalf("unexpected default addr: %q", cfg.Server.Addr)
	}
	if cfg.Database.Path != "sunbreak.db" {
		t.Fatalf("unexpected default db path: %q", cfg.Database.Path)
	}

	cfg.Server.Addr = ""
	cfg.Database.Path = ""
	cfg.Scheduler.PollIntervalSeconds = 0
	cfg.Scheduler.BatchSize = 0
	cfg.Scheduler.CollectTimeoutSeconds = 0
	cfg.Digest.IntervalSeconds = 0
	cfg.Digest.WindowHours = 0
	cfg.Sources = []SourceConfig{{Type: "rss"}}
	cfg.Rules = []RuleConfig{{Name: "SQLite"}}
	cfg.ApplyDefaults()

	if cfg.Server.Addr != ":8080" || cfg.Database.Path != "sunbreak.db" {
		t.Fatalf("defaults were not applied: %+v", cfg)
	}
	if cfg.Sources[0].IntervalSeconds != 300 {
		t.Fatalf("expected source interval default, got %d", cfg.Sources[0].IntervalSeconds)
	}
	if cfg.Sources[0].Config == nil {
		t.Fatal("expected source config map default")
	}
	if cfg.Rules[0].Type != "keyword" {
		t.Fatalf("expected keyword rule default, got %q", cfg.Rules[0].Type)
	}
}

func TestLoadMergesYAMLWithDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(`
server:
  addr: ":9090"
database:
  path: ""
sources:
  - type: "hackernews"
    name: "HN"
rules:
  - name: "SQLite"
    pattern: "sqlite"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":9090" {
		t.Fatalf("expected configured addr, got %q", cfg.Server.Addr)
	}
	if cfg.Database.Path != "sunbreak.db" {
		t.Fatalf("expected db default, got %q", cfg.Database.Path)
	}
	if cfg.Sources[0].IntervalSeconds != 300 || cfg.Rules[0].Type != "keyword" {
		t.Fatalf("expected nested defaults, got %+v", cfg)
	}
}

func TestLoadRejectsEmptyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.yaml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected empty config error")
	}
}
