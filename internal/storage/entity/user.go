package entity

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

type User struct {
	bun.BaseModel      `bun:"table:users,alias:u"`
	ID                 string    `bun:",pk" json:"id" required:"true"`
	Username           string    `bun:",unique" json:"username" required:"true"`
	PasswordHash       string    `json:"-"`
	Administrator      bool      `json:"administrator"`
	MustChangePassword bool      `json:"must_change_password"`
	Disabled           bool      `json:"disabled"`
	CreatedAt          time.Time `json:"created_at"`
}

type Session struct {
	bun.BaseModel `bun:"table:sessions,alias:s"`
	ID            string    `bun:",pk" json:"-"`
	UserID        string    `json:"user_id"`
	CreatedAt     time.Time `json:"created_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type Member struct {
	bun.BaseModel `bun:"table:project_members,alias:m"`
	ProjectID     string `bun:",pk" json:"project_id"`
	UserID        string `bun:",pk" json:"user_id"`
}

type OperationLog struct {
	bun.BaseModel `bun:"table:operation_logs,alias:ol"`
	ID            string          `bun:",pk" json:"id"`
	UserID        string          `bun:",nullzero" json:"user_id"`
	Username      string          `json:"username"`
	Action        string          `json:"action"`
	ProjectID     string          `json:"project_id"`
	Detail        json.RawMessage `bun:"type:jsonb" json:"detail"`
	CreatedAt     time.Time       `json:"created_at"`
}
