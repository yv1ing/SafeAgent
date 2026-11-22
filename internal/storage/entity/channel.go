package entity

import (
	"time"

	"github.com/uptrace/bun"
)

// ChannelEndpoint enables one provider format. The order is the fallback order when the client
// format is unavailable; an empty base URL or path means the provider catalog value.
type ChannelEndpoint struct {
	Format  string `json:"format" required:"true"`
	BaseURL string `json:"base_url,omitempty"`
	Path    string `json:"path,omitempty"`
}

type Channel struct {
	bun.BaseModel           `bun:"table:channels,alias:ch"`
	ID                      string               `bun:",pk" json:"id" required:"true"`
	Name                    string               `json:"name" required:"true"`
	ProviderID              string               `json:"provider_id" required:"true"`
	Endpoints               []ChannelEndpoint    `bun:"type:jsonb" json:"endpoints"`
	ModelIDs                []string             `bun:"column:model_ids,type:jsonb" json:"model_ids"`
	CapabilityOverrides     []CapabilityOverride `bun:"type:jsonb" json:"capability_overrides"`
	Headers                 map[string]string    `bun:"type:jsonb" json:"headers"`
	ForwardHeaders          []string             `bun:"type:jsonb" json:"forward_headers"`
	DefaultMaxOutputTokens  int                  `json:"default_max_output_tokens"`
	TimeoutSeconds          int                  `json:"timeout_seconds"`
	FirstByteTimeoutSeconds int                  `json:"first_byte_timeout_seconds"`
	AllowPrivateNetwork     bool                 `json:"allow_private_network"`
	Enabled                 bool                 `json:"enabled"`
	Version                 int64                `json:"version"`
	CreatedAt               time.Time            `json:"created_at"`
}

// CapabilityOverride records a field the upstream rejected for one channel model format. It is
// written by the gateway and is read-only for operators.
type CapabilityOverride struct {
	ModelID   string    `json:"model_id"`
	Format    string    `json:"format"`
	Field     string    `json:"field"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (channel Channel) HasFormat(format string) bool {
	for _, endpoint := range channel.Endpoints {
		if endpoint.Format == format {
			return true
		}
	}
	return false
}

func (channel Channel) HasModel(modelID string) bool {
	for _, candidate := range channel.ModelIDs {
		if candidate == modelID {
			return true
		}
	}
	return false
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
