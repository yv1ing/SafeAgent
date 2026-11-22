package entity

import "github.com/uptrace/bun"

type SystemState struct {
	bun.BaseModel     `bun:"table:system_state,alias:ss"`
	ID                int    `bun:",pk" json:"-"`
	SchemaFingerprint string `json:"-"`
}
