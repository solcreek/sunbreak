package model

import "time"

type Source struct {
	ID              int64
	Type            string
	Name            string
	URL             string
	Enabled         bool
	IntervalSeconds int
	ConfigJSON      string
	Checkpoint      string
	ETag            string
	LastModified    string
	NextRunAt       time.Time
	LastRunAt       time.Time
	LastError       string
	ErrorCount      int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Item struct {
	ID          int64
	SourceID    int64
	SourceType  string
	SourceName  string
	ExternalID  string
	URL         string
	Title       string
	Content     string
	Author      string
	PublishedAt time.Time
	FetchedAt   time.Time
	RawJSON     string
}

type Rule struct {
	ID            int64
	Name          string
	Type          string
	Pattern       string
	CaseSensitive bool
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Match struct {
	ID          int64
	ItemID      int64
	RuleID      int64
	MatchedText string
	Score       float64
	CreatedAt   time.Time
	Item        Item
	Rule        Rule
}

type OutboxMessage struct {
	ID          int64
	Channel     string
	Destination string
	Subject     string
	Body        string
	Status      string
	Attempts    int
	AvailableAt time.Time
	LastError   string
	CreatedAt   time.Time
	SentAt      time.Time
}

type Digest struct {
	ID          int64
	WindowStart time.Time
	WindowEnd   time.Time
	Subject     string
	Body        string
	CreatedAt   time.Time
}
