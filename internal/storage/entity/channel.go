package entity

import (
	"time"

	"github.com/uptrace/bun"
)

type Channel struct {
	bun.BaseModel           `bun:"table:channels,alias:ch"`
	ID                      string               `bun:",pk" json:"id" required:"true"`
	Name                    string               `json:"name" required:"true"`
	ProviderID              string               `json:"provider_id" required:"true"`
	Custom                  bool                 `json:"custom"`
	Endpoints               []ChannelEndpoint    `bun:"type:jsonb" json:"endpoints"`
	CapabilityOverrides     []CapabilityOverride `bun:"type:jsonb" json:"capability_overrides"`
	Headers                 map[string]string    `bun:"type:jsonb" json:"headers"`
	ForwardHeaders          []string             `bun:"type:jsonb" json:"forward_headers"`
	DefaultMaxOutputTokens  int                  `json:"default_max_output_tokens"`
	Models                  []ChannelModel       `bun:"type:jsonb" json:"models"`
	Currency                string               `json:"currency"`
	TimeoutSeconds          int                  `json:"timeout_seconds"`
	FirstByteTimeoutSeconds int                  `json:"first_byte_timeout_seconds"`
	AllowPrivateNetwork     bool                 `json:"allow_private_network"`
	Enabled                 bool                 `json:"enabled"`
	Version                 int64                `json:"version"`
	CreatedAt               time.Time            `json:"created_at"`
}

// ChannelEndpoint overrides one format of the provider catalog. An absent entry means the catalog
// default; a base URL or path replaces that part of the request, and a disabled entry hides the
// format.
type ChannelEndpoint struct {
	Format   string `json:"format"`
	BaseURL  string `json:"base_url,omitempty"`
	Path     string `json:"path,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

// CapabilityOverride records a field the upstream rejected. It is written by the gateway and is
// read-only for operators.
type CapabilityOverride struct {
	Format    string    `json:"format"`
	Field     string    `json:"field"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
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
