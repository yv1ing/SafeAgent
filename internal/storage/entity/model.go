package entity

import (
	"time"

	"github.com/uptrace/bun"
)

type ModelKind string

const (
	ModelGeneration ModelKind = "generation"
	ModelEmbedding  ModelKind = "embedding"
	ModelModeration ModelKind = "moderation"
)

func (kind ModelKind) Valid() bool {
	return kind == ModelGeneration || kind == ModelEmbedding || kind == ModelModeration
}

// Model is the administrator-owned catalog entry. A channel enables model ids; a route target
// selects one channel and one model, and the runtime intersects the model formats with the
// channel endpoints.
type Model struct {
	bun.BaseModel   `bun:"table:models,alias:m"`
	ID              string    `bun:",pk" json:"id" required:"true"`
	ProviderID      string    `json:"provider_id" required:"true"`
	UpstreamName    string    `json:"upstream_name" required:"true"`
	DisplayName     string    `json:"display_name,omitempty"`
	Kind            ModelKind `json:"kind" required:"true" enum:"generation,embedding,moderation"`
	Formats         []string  `bun:"type:jsonb" json:"formats"`
	InputPrice      int64     `json:"input_price"`
	OutputPrice     int64     `json:"output_price"`
	CacheReadPrice  int64     `json:"cache_read_price"`
	CacheWritePrice int64     `json:"cache_write_price"`
	Currency        string    `json:"currency" required:"true"`
	Enabled         bool      `json:"enabled"`
	Version         int64     `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
}

func (model Model) Supports(format string) bool {
	for _, candidate := range model.Formats {
		if candidate == format {
			return true
		}
	}
	return false
}
