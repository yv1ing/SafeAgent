package entity

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

type TrajectorySummary struct {
	bun.BaseModel `bun:"table:trajectory_summaries,alias:ts"`
	ID            string     `bun:",pk" json:"id" required:"true"`
	ProjectID     string     `json:"project_id"`
	AgentKeyID    string     `json:"agent_key_id"`
	SessionID     string     `json:"session_id"`
	Goal          string     `json:"goal"`
	State         string     `json:"state"`
	Coverage      float64    `json:"coverage"`
	Risk          float64    `json:"risk"`
	MaxSeverity   string     `json:"max_severity"`
	FirstEventAt  time.Time  `json:"first_event_at"`
	LastEventAt   time.Time  `json:"last_event_at"`
	EventCount    int64      `json:"event_count"`
	CallCount     int64      `json:"call_count"`
	FindingCount  int64      `json:"finding_count"`
	CreatedAt     time.Time  `json:"created_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
}

type TrajectoryEvent struct {
	bun.BaseModel `bun:"table:trajectory_events,alias:te"`
	ID            string          `bun:",pk" json:"id"`
	TrajectoryID  string          `json:"trajectory_id"`
	ProjectID     string          `json:"project_id"`
	Seq           int64           `json:"seq"`
	Kind          string          `json:"kind"`
	Boundary      string          `json:"boundary"`
	Actor         string          `json:"actor"`
	Principal     string          `json:"principal"`
	CallID        string          `json:"call_id"`
	ActionID      string          `json:"action_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Inputs        json.RawMessage `bun:"type:jsonb" json:"inputs"`
	Outputs       json.RawMessage `bun:"type:jsonb" json:"outputs"`
	Auth          json.RawMessage `bun:"type:jsonb" json:"auth,omitempty"`
	Checks        json.RawMessage `bun:"type:jsonb" json:"checks"`
	Obligations   json.RawMessage `bun:"type:jsonb" json:"obligations"`
	Snapshot      json.RawMessage `bun:"type:jsonb" json:"snapshot,omitempty"`
	Capture       json.RawMessage `bun:"type:jsonb" json:"capture"`
	Payload       json.RawMessage `bun:"type:jsonb" json:"payload"`
	PrevHash      string          `json:"prev_hash"`
	Hash          string          `json:"hash"`
}

type Segment struct {
	bun.BaseModel   `bun:"table:segments,alias:s"`
	ID              string    `bun:",pk" json:"id"`
	TrajectoryID    string    `bun:",pk" json:"trajectory_id"`
	MediaType       string    `json:"media_type"`
	BytesRef        string    `json:"bytes_ref"`
	Size            int64     `json:"size"`
	Integrity       string    `json:"integrity"`
	Confidentiality string    `json:"confidentiality"`
	SourceEvent     string    `json:"source_event"`
	CreatedAt       time.Time `json:"created_at"`
}

type Blob struct {
	bun.BaseModel `bun:"table:blobs,alias:b"`
	Hash          string    `bun:",pk" json:"hash"`
	Bytes         []byte    `json:"-"`
	MediaType     string    `json:"media_type"`
	Encrypted     bool      `json:"encrypted"`
	CreatedAt     time.Time `json:"created_at"`
}

type Edge struct {
	bun.BaseModel `bun:"table:edges,alias:e"`
	TrajectoryID  string  `bun:",pk" json:"trajectory_id"`
	From          string  `bun:",pk" json:"src"`
	To            string  `bun:",pk" json:"dst"`
	Relation      string  `bun:",pk" json:"relation"`
	EventID       string  `json:"event_id"`
	Confidence    float64 `json:"confidence"`
}

type Checkpoint struct {
	bun.BaseModel `bun:"table:checkpoints,alias:cp"`
	ID            string    `bun:",pk" json:"id"`
	TrajectoryID  string    `json:"trajectory_id"`
	FromSeq       int64     `json:"from_seq"`
	ToSeq         int64     `json:"to_seq"`
	MerkleRoot    string    `json:"merkle_root"`
	Signature     string    `json:"signature"`
	PrevHash      string    `json:"prev_hash"`
	WitnessRef    string    `json:"witness_ref,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type TrajectoryStateRow struct {
	bun.BaseModel `bun:"table:trajectory_states,alias:tst"`
	TrajectoryID  string          `bun:",pk" json:"trajectory_id"`
	Version       int64           `json:"version"`
	StateHash     string          `json:"state_hash"`
	HeadEvent     string          `json:"head_event"`
	ProjectID     string          `json:"project_id"`
	Goal          string          `json:"goal"`
	Constraints   json.RawMessage `bun:"type:jsonb" json:"constraints"`
	Labels        json.RawMessage `bun:"type:jsonb" json:"labels"`
	Capabilities  json.RawMessage `bun:"type:jsonb" json:"capabilities"`
	Obligations   json.RawMessage `bun:"type:jsonb" json:"obligations"`
	Risk          json.RawMessage `bun:"type:jsonb" json:"risk"`
	Coverage      float64         `json:"coverage"`
	State         json.RawMessage `bun:"type:jsonb" json:"state"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type Finding struct {
	bun.BaseModel   `bun:"table:findings,alias:f"`
	ID              string          `bun:",pk" json:"id" required:"true"`
	ProjectID       string          `json:"project_id"`
	TrajectoryID    string          `json:"trajectory_id"`
	EventID         string          `json:"event_id"`
	CallID          string          `json:"call_id"`
	Fingerprint     string          `json:"fingerprint"`
	Code            string          `json:"code"`
	Origin          string          `json:"origin"`
	Trust           string          `json:"trust"`
	Sink            string          `json:"sink"`
	Severity        string          `json:"severity"`
	Confidence      float64         `json:"confidence"`
	DetectorID      string          `json:"detector_id"`
	DetectorVersion string          `json:"detector_version"`
	Evidence        json.RawMessage `bun:"type:jsonb" json:"evidence"`
	Attribution     json.RawMessage `bun:"type:jsonb" json:"attribution"`
	Invariant       string          `json:"invariant"`
	Transfer        string          `json:"transfer"`
	Control         string          `json:"control"`
	Segments        json.RawMessage `bun:"type:jsonb" json:"segments"`
	CreatedAt       time.Time       `json:"created_at"`
}

type DecisionModelConfig struct {
	bun.BaseModel    `bun:"table:decision_model_configs,alias:dmc"`
	ProjectID        string    `bun:",pk" json:"project_id"`
	Enabled          bool      `json:"enabled"`
	Kind             string    `json:"kind"`
	Endpoint         string    `json:"endpoint"`
	Model            string    `json:"model"`
	APIKey           string    `json:"-"`
	TimeoutSeconds   int       `json:"timeout_seconds"`
	AllowPrivate     bool      `json:"allow_private"`
	UseRaw           bool      `json:"use_raw"`
	DailyTokenBudget int64     `json:"daily_token_budget"`
	AuthorizeBelow   float64   `json:"authorize_below"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type PolicyVersion struct {
	bun.BaseModel `bun:"table:policy_versions,alias:pv"`
	ID            string          `bun:",pk" json:"id" required:"true"`
	ProjectID     string          `json:"project_id"`
	Version       int64           `json:"version"`
	Document      json.RawMessage `bun:"type:jsonb" json:"document"`
	Hash          string          `json:"hash"`
	Note          string          `json:"note"`
	CreatedBy     string          `json:"created_by"`
	CreatedAt     time.Time       `json:"created_at"`
}
