package entity

import (
	"time"

	"github.com/uptrace/bun"
)

type Project struct {
	bun.BaseModel          `bun:"table:projects,alias:p"`
	ID                     string    `bun:",pk" json:"id" required:"true"`
	Name                   string    `json:"name" required:"true"`
	RetentionDays          int       `json:"retention_days"`
	RequestsPerMinute      int       `json:"requests_per_minute"`
	Concurrency            int       `json:"concurrency"`
	DailyTokenQuota        int64     `json:"daily_token_quota"`
	DefaultMaxOutputTokens int       `json:"default_max_output_tokens"`
	RulesVersion           int64     `json:"rules_version"`
	ActivePolicyVersion    int64     `json:"active_policy_version"`
	Version                int64     `json:"version"`
	CreatedAt              time.Time `json:"created_at"`
}

type AgentKey struct {
	bun.BaseModel     `bun:"table:agent_keys,alias:ak"`
	ID                string     `bun:",pk" json:"id" required:"true"`
	ProjectID         string     `json:"project_id"`
	Label             string     `json:"label"`
	Hash              string     `bun:",unique" json:"-"`
	Prefix            string     `json:"prefix"`
	RequestsPerMinute int        `json:"requests_per_minute"`
	Concurrency       int        `json:"concurrency"`
	TrustLevel        int        `json:"trust_level"`
	Role              string     `json:"role"`
	Enabled           bool       `json:"enabled"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	LastUsedAt        *time.Time `json:"last_used_at"`
}
