package entity

import "time"

type Totals struct {
	Calls           int64
	Failures        int64
	Blocked         int64
	InputTokens     int64
	OutputTokens    int64
	RedactionValues int64
	HighSeverity    int64
	FirstTokenP50   int64
	FirstTokenP95   int64
	DurationP50     int64
	DurationP95     int64
}

// Cost is a spend in one currency; amounts in different currencies are never added together.
type Cost struct {
	Currency   string
	CostMicros int64
}

type Group struct {
	Name         string
	Label        string
	Calls        int64
	Failures     int64
	InputTokens  int64
	OutputTokens int64
	Costs        []Cost `bun:"-"`
}

type Statistics struct {
	Window string
	Totals Totals
	Groups []Group
	Costs  []Cost
}

type SeriesBucket struct {
	Start           time.Time
	Requests        int64
	Errors          int64
	Blocked         int64
	InputTokens     int64
	OutputTokens    int64
	DurationP95     int64 `bun:"duration_p95"`
	RedactionValues int64
	FindingsHigh    int64
}

type Redaction struct {
	Label string
	Hits  int64
}

type SeverityCount struct {
	Severity string
	Count    int64
}

type CodeCount struct {
	Code  string
	Count int64
}

type ChannelTraffic struct {
	ChannelID   string
	ChannelName string
	Requests    int64
	Errors      int64
	DurationP95 int64 `bun:"duration_p95"`
}

type Series struct {
	Buckets      []SeriesBucket
	Costs        []Cost
	Redactions   []Redaction
	Findings     []SeverityCount
	FindingCodes []CodeCount
	Channels     []ChannelTraffic
}

// ProjectSummary holds 24h traffic aggregates for a project card.
type ProjectSummary struct {
	ProjectID       string `bun:"project_id"`
	Calls24h        int64  `bun:"calls_24h"`
	Blocked24h      int64  `bun:"blocked_24h"`
	Failed24h       int64  `bun:"failed_24h"`
	LastActivityAt  int64  `bun:"last_activity_at"`
	InputTokens24h  int64  `bun:"input_tokens_24h"`
	OutputTokens24h int64  `bun:"output_tokens_24h"`
}
