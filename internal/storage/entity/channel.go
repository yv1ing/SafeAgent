package entity

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Provider is the connection profile of one upstream: its endpoints, capabilities and compatibility
// facts per wire format. Built-in rows are seeded from the code catalog at startup and stay
// editable; custom rows are created by operators. The endpoints payload is the serialized form of
// provider.EndpointSpec so the runtime catalog is rebuilt from this table alone.
type Provider struct {
	bun.BaseModel `bun:"table:providers,alias:pr"`
	ID            string          `bun:",pk" json:"id" required:"true"`
	Source        string          `json:"source"`
	Label         string          `json:"label"`
	Endpoints     json.RawMessage `bun:"type:jsonb" json:"endpoints"`
	SeedVersion   int             `json:"seed_version"`
	Enabled       bool            `json:"enabled"`
	Version       int64           `json:"version"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	DeletedAt     *time.Time      `json:"deleted_at,omitempty"`
}

const (
	ProviderBuiltin = "builtin"
	ProviderCustom  = "custom"
)

type Channel struct {
	bun.BaseModel           `bun:"table:channels,alias:ch"`
	ID                      string            `bun:",pk" json:"id" required:"true"`
	ProviderID              string            `json:"provider_id"`
	Name                    string            `json:"name"`
	BaseURL                 string            `json:"base_url"`
	Headers                 map[string]string `bun:"type:jsonb" json:"headers"`
	ForwardHeaders          []string          `bun:"type:jsonb" json:"forward_headers"`
	DefaultMaxOutputTokens  int               `json:"default_max_output_tokens"`
	TimeoutSeconds          int               `json:"timeout_seconds"`
	FirstByteTimeoutSeconds int               `json:"first_byte_timeout_seconds"`
	AllowPrivateNetwork     bool              `json:"allow_private_network"`
	Enabled                 bool              `json:"enabled"`
	Version                 int64             `json:"version"`
	CreatedAt               time.Time         `json:"created_at"`
	UpdatedAt               time.Time         `json:"updated_at"`
	DeletedAt               *time.Time        `json:"deleted_at,omitempty"`
}

// CapabilityOverride records a capability fact one upstream rejected for one channel model format.
// The gateway writes it; operators only read it.
type CapabilityOverride struct {
	bun.BaseModel `bun:"table:channel_capability_overrides,alias:cco"`
	ChannelID     string          `bun:",pk" json:"channel_id"`
	ModelID       string          `bun:",pk" json:"model_id"`
	Format        string          `bun:",pk" json:"format"`
	Field         string          `bun:",pk" json:"field"`
	Value         json.RawMessage `bun:"type:jsonb" json:"value"`
	Reason        string          `json:"reason"`
	CreatedAt     time.Time       `json:"created_at"`
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
