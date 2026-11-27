package entity

import "github.com/uptrace/bun"

// Rule is one redaction rule. The rules of a call are snapshotted at admission, so editing this
// table never changes how an old call is displayed.
type Rule struct {
	bun.BaseModel `bun:"table:redaction_rules,alias:rl"`
	ID            string `bun:",pk" json:"id" required:"true"`
	ProjectID     string `json:"project_id"`
	Kind          string `json:"kind"`
	Detector      string `json:"detector"`
	Pattern       string `json:"pattern"`
	Capture       string `json:"capture"`
	Label         string `json:"label"`
	Action        string `json:"action"`
	Enabled       bool   `json:"enabled"`
	Version       int64  `json:"version"`
}
