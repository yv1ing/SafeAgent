package entity

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

type Event struct {
	bun.BaseModel `bun:"table:events,alias:e"`
	CallID        string          `bun:",pk" json:"call_id"`
	Sequence      int64           `bun:",pk" json:"sequence"`
	ProjectID     string          `json:"-"`
	Kind          string          `json:"kind"`
	SchemaVersion int             `json:"schema_version"`
	Payload       json.RawMessage `bun:"type:jsonb" json:"payload"`
	CreatedAt     time.Time       `json:"created_at"`
	PrevHash      string          `json:"prev_hash"`
	Hash          string          `json:"hash"`
}
