package entity

import "github.com/uptrace/bun"

type QuotaUsage struct {
	bun.BaseModel `bun:"table:quota_usage,alias:q"`
	ProjectID     string `bun:",pk" json:"project_id"`
	Day           string `bun:",pk" json:"day"`
	Used          int64  `json:"used"`
	Reserved      int64  `json:"reserved"`
}
