package entity

import (
	"time"

	"github.com/uptrace/bun"
)

// Route is the project-facing model name. Targets are rows, so a channel or model can be reused by
// many routes and a deleted target leaves no trace inside the route row.
type Route struct {
	bun.BaseModel `bun:"table:model_routes,alias:r"`
	ID            string     `bun:",pk" json:"id" required:"true"`
	ProjectID     string     `json:"project_id"`
	Name          string     `json:"name"`
	Enabled       bool       `json:"enabled"`
	Version       int64      `json:"version"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

// RouteAlias is an additional model name that resolves to a route. Renaming a route records the old
// name so clients keep working; operators may add their own aliases.
type RouteAlias struct {
	bun.BaseModel `bun:"table:route_aliases,alias:ra"`
	ProjectID     string    `bun:",pk" json:"project_id"`
	Alias         string    `bun:",pk" json:"alias"`
	RouteID       string    `json:"route_id"`
	Source        string    `json:"source"`
	CreatedAt     time.Time `json:"created_at"`
}

const (
	AliasManual  = "manual"
	AliasRenamed = "renamed"
)

// RouteTarget is one upstream candidate of a route. Format is empty when the runtime may pick any
// format the channel and model share, which is the normal case.
type RouteTarget struct {
	bun.BaseModel   `bun:"table:route_targets,alias:rt"`
	ID              string    `bun:",pk" json:"id" required:"true"`
	RouteID         string    `json:"route_id"`
	ChannelID       string    `json:"channel_id"`
	ModelID         string    `json:"model_id"`
	Format          string    `json:"format"`
	Priority        int       `json:"priority"`
	Weight          int       `json:"weight"`
	MaxOutputTokens int       `json:"max_output_tokens"`
	Enabled         bool      `json:"enabled"`
	DisabledReason  string    `json:"disabled_reason"`
	CreatedAt       time.Time `json:"created_at"`
}
