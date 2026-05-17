package config

import (
	"errors"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server        ServerConfig       `yaml:"server"`
	Database      DatabaseConfig     `yaml:"database"`
	Scheduler     SchedulerConfig    `yaml:"scheduler"`
	Digest        DigestConfig       `yaml:"digest"`
	Notifications NotificationConfig `yaml:"notifications"`
	Sources       []SourceConfig     `yaml:"sources"`
	Rules         []RuleConfig       `yaml:"rules"`
	Reddit        RedditConfig       `yaml:"reddit"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type SchedulerConfig struct {
	PollIntervalSeconds   int `yaml:"poll_interval_seconds"`
	BatchSize             int `yaml:"batch_size"`
	CollectTimeoutSeconds int `yaml:"collect_timeout_seconds"`
}

type DigestConfig struct {
	Enabled         bool `yaml:"enabled"`
	IntervalSeconds int  `yaml:"interval_seconds"`
	WindowHours     int  `yaml:"window_hours"`
}

type NotificationConfig struct {
	Stdout bool `yaml:"stdout"`
}

type SourceConfig struct {
	Type            string         `yaml:"type"`
	Name            string         `yaml:"name"`
	URL             string         `yaml:"url"`
	Enabled         *bool          `yaml:"enabled"`
	IntervalSeconds int            `yaml:"interval_seconds"`
	Config          map[string]any `yaml:"config"`
}

type RuleConfig struct {
	Name          string `yaml:"name"`
	Type          string `yaml:"type"`
	Pattern       string `yaml:"pattern"`
	CaseSensitive bool   `yaml:"case_sensitive"`
	Enabled       *bool  `yaml:"enabled"`
}

type RedditConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	UserAgent    string `yaml:"user_agent"`
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if len(data) == 0 {
		return Config{}, errors.New("config file is empty")
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

func Default() Config {
	cfg := Config{
		Server: ServerConfig{
			Addr: ":8080",
		},
		Database: DatabaseConfig{
			Path: "radar.db",
		},
		Scheduler: SchedulerConfig{
			PollIntervalSeconds:   30,
			BatchSize:             10,
			CollectTimeoutSeconds: 20,
		},
		Digest: DigestConfig{
			Enabled:         true,
			IntervalSeconds: int((6 * time.Hour).Seconds()),
			WindowHours:     24,
		},
		Notifications: NotificationConfig{
			Stdout: true,
		},
	}
	return cfg
}

func (c *Config) ApplyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Database.Path == "" {
		c.Database.Path = "radar.db"
	}
	if c.Scheduler.PollIntervalSeconds <= 0 {
		c.Scheduler.PollIntervalSeconds = 30
	}
	if c.Scheduler.BatchSize <= 0 {
		c.Scheduler.BatchSize = 10
	}
	if c.Scheduler.CollectTimeoutSeconds <= 0 {
		c.Scheduler.CollectTimeoutSeconds = 20
	}
	if c.Digest.IntervalSeconds <= 0 {
		c.Digest.IntervalSeconds = int((6 * time.Hour).Seconds())
	}
	if c.Digest.WindowHours <= 0 {
		c.Digest.WindowHours = 24
	}
	for i := range c.Sources {
		if c.Sources[i].IntervalSeconds <= 0 {
			c.Sources[i].IntervalSeconds = 300
		}
		if c.Sources[i].Config == nil {
			c.Sources[i].Config = map[string]any{}
		}
	}
	for i := range c.Rules {
		if c.Rules[i].Type == "" {
			c.Rules[i].Type = "keyword"
		}
	}
}
