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

// Model is the administrator-owned catalog entry. A route target selects a channel and a catalog
// model; the formats a model supports are derived from the provider endpoints and the model kind,
// never stored here.
type Model struct {
	bun.BaseModel   `bun:"table:models,alias:m"`
	ID              string     `bun:",pk" json:"id" required:"true"`
	ProviderID      string     `json:"provider_id"`
	UpstreamName    string     `json:"upstream_name"`
	DisplayName     string     `json:"display_name,omitempty"`
	Kind            ModelKind  `json:"kind" enum:"generation,embedding,moderation"`
	InputPrice      int64      `json:"input_price"`
	OutputPrice     int64      `json:"output_price"`
	CacheReadPrice  int64      `json:"cache_read_price"`
	CacheWritePrice int64      `json:"cache_write_price"`
	Currency        string     `json:"currency" required:"true"`
	Enabled         bool       `json:"enabled"`
	Version         int64      `json:"version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
}
