package entity

import (
	"time"

	"github.com/uptrace/bun"
)

type Route struct {
	bun.BaseModel `bun:"table:model_routes,alias:r"`
	ID            string    `bun:",pk" json:"id" required:"true"`
	ProjectID     string    `json:"project_id"`
	Name          string    `json:"name"`
	Targets       []Target  `bun:"type:jsonb" json:"targets"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
}

// Target selects one channel and one catalog model. The runtime chooses the concrete format from
// the channel endpoints and the model formats, so no format is pinned here.
type Target struct {
	ChannelID       string `json:"channel_id"`
	ModelID         string `json:"model_id"`
	Priority        int    `json:"priority"`
	Weight          int    `json:"weight"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
}
