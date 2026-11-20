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

type Target struct {
	ChannelID     string `json:"channel_id"`
	UpstreamModel string `json:"upstream_model"`
	Priority      int    `json:"priority"`
	Weight        int    `json:"weight"`
}
