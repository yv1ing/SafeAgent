package entity

import (
	"time"

	"github.com/uptrace/bun"
)

type Channel struct {
	bun.BaseModel           `bun:"table:channels,alias:ch"`
	ID                      string            `bun:",pk" json:"id" required:"true"`
	Name                    string            `json:"name" required:"true"`
	Format                  string            `json:"format"`
	Vendor                  string            `json:"vendor"`
	Endpoint                string            `json:"endpoint"`
	CountTokensEndpoint     string            `json:"count_tokens_endpoint"`
	Headers                 map[string]string `bun:"type:jsonb" json:"headers"`
	ForwardHeaders          []string          `bun:"type:jsonb" json:"forward_headers"`
	DefaultMaxOutputTokens  int               `json:"default_max_output_tokens"`
	Models                  []ChannelModel    `bun:"type:jsonb" json:"models"`
	Currency                string            `json:"currency"`
	TimeoutSeconds          int               `json:"timeout_seconds"`
	FirstByteTimeoutSeconds int               `json:"first_byte_timeout_seconds"`
	AllowPrivateNetwork     bool              `json:"allow_private_network"`
	Enabled                 bool              `json:"enabled"`
	Version                 int64             `json:"version"`
	CreatedAt               time.Time         `json:"created_at"`
}

type ChannelModel struct {
	Name            string `json:"name"`
	InputPrice      int64  `json:"input_price"`
	OutputPrice     int64  `json:"output_price"`
	CacheReadPrice  int64  `json:"cache_read_price"`
	CacheWritePrice int64  `json:"cache_write_price"`
}

type ChannelKey struct {
	bun.BaseModel `bun:"table:channel_keys,alias:ck"`
	ID            string `bun:",pk" json:"id" required:"true"`
	ChannelID     string `json:"channel_id"`
	Label         string `json:"label"`
	Secret        string `json:"secret,omitempty"`
	Suffix        string `json:"suffix"`
	Enabled       bool   `json:"enabled"`
}
