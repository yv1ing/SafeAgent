package entity

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// PolicyVersion is one saved, immediately active policy. The newest version is the one that runs;
// older versions are kept only for review and rollback.
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

// Analysis is one diagnosis of one call. Failures are stored as rows so a missing result is never
// mistaken for a clean one.
type Analysis struct {
	bun.BaseModel    `bun:"table:analyses,alias:a"`
	ID               string          `bun:",pk" json:"id" required:"true"`
	ProjectID        string          `json:"project_id"`
	CallID           string          `json:"call_id"`
	AgentKeyID       string          `json:"agent_key_id"`
	BatchID          string          `json:"batch_id"`
	State            string          `json:"state"`
	AdapterVersion   string          `json:"adapter_version"`
	DetectorVersion  string          `json:"detector_version"`
	CatalogueVersion string          `json:"catalogue_version"`
	TaxonomyVersion  string          `json:"taxonomy_version"`
	DecisionModel    string          `json:"decision_model"`
	InputHash        string          `json:"input_hash"`
	Verdict          json.RawMessage `bun:"type:jsonb" json:"verdict"`
	Coverage         json.RawMessage `bun:"type:jsonb" json:"coverage"`
	Model            json.RawMessage `bun:"type:jsonb" json:"model"`
	ModelState       string          `json:"model_state"`
	Error            string          `json:"error"`
	StartedAt        time.Time       `json:"started_at"`
	FinishedAt       *time.Time      `json:"finished_at,omitempty"`
}

type AnalysisFinding struct {
	bun.BaseModel      `bun:"table:analysis_findings,alias:af"`
	ID                 string          `bun:",pk" json:"id" required:"true"`
	AnalysisID         string          `json:"analysis_id"`
	ProjectID          string          `json:"project_id"`
	CallID             string          `json:"call_id"`
	Fingerprint        string          `json:"fingerprint"`
	Code               string          `json:"code"`
	Origin             string          `json:"origin"`
	Trust              string          `json:"trust"`
	Sink               string          `json:"sink"`
	Source             string          `json:"source"`
	Mode               string          `json:"mode"`
	Harm               string          `json:"harm"`
	Severity           string          `json:"severity"`
	Confidence         float64         `json:"confidence"`
	DetectorID         string          `json:"detector_id"`
	DetectorVersion    string          `json:"detector_version"`
	Evidence           json.RawMessage `bun:"type:jsonb" json:"evidence"`
	Attribution        json.RawMessage `bun:"type:jsonb" json:"attribution"`
	FlowSourceTurn     int             `json:"flow_source_turn"`
	FlowSourceItem     int             `json:"flow_source_item"`
	FlowSourceOrigin   string          `json:"flow_source_origin"`
	FlowRelation       string          `json:"flow_relation"`
	FlowSinkToolCallID string          `json:"flow_sink_tool_call_id"`
	CreatedAt          time.Time       `json:"created_at"`
}

type AnalysisFlow struct {
	bun.BaseModel  `bun:"table:analysis_flows,alias:afl"`
	ID             string    `bun:",pk" json:"id" required:"true"`
	AnalysisID     string    `json:"analysis_id"`
	ProjectID      string    `json:"project_id"`
	CallID         string    `json:"call_id"`
	AgentKeyID     string    `json:"agent_key_id"`
	SourceTurn     int       `json:"source_turn"`
	SourceItem     int       `json:"source_item"`
	SourceOrigin   string    `json:"source_origin"`
	SourcePath     string    `json:"source_path"`
	SinkToolCallID string    `json:"sink_tool_call_id"`
	Sink           string    `json:"sink"`
	Relation       string    `json:"relation"`
	ContentHash    string    `json:"content_hash"`
	Confidence     float64   `json:"confidence"`
	CreatedAt      time.Time `json:"created_at"`
}

type AnalysisBatch struct {
	bun.BaseModel `bun:"table:analysis_batches,alias:ab"`
	ID            string     `bun:",pk" json:"id" required:"true"`
	ProjectID     string     `json:"project_id"`
	Kind          string     `json:"kind"`
	State         string     `json:"state"`
	Total         int        `json:"total"`
	Completed     int        `json:"completed"`
	Failed        int        `json:"failed"`
	CreatedAt     time.Time  `json:"created_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

type Decision struct {
	bun.BaseModel `bun:"table:decisions,alias:d"`
	ID            string          `bun:",pk" json:"id" required:"true"`
	ProjectID     string          `json:"project_id"`
	CallID        string          `json:"call_id"`
	PolicyVersion int64           `json:"policy_version"`
	Phase         string          `json:"phase"`
	Action        string          `json:"action"`
	Blocked       bool            `json:"blocked"`
	RuleID        string          `json:"rule_id"`
	Code          string          `json:"code"`
	Severity      string          `json:"severity"`
	Reason        string          `json:"reason"`
	Attributes    json.RawMessage `bun:"type:jsonb" json:"attributes"`
	CreatedAt     time.Time       `json:"created_at"`
}

type AnalysisJob struct {
	bun.BaseModel  `bun:"table:analysis_jobs,alias:aj"`
	ID             string          `bun:",pk" json:"id" required:"true"`
	ProjectID      string          `json:"project_id"`
	Kind           string          `json:"kind"`
	Scope          json.RawMessage `bun:"type:jsonb" json:"scope"`
	State          string          `json:"state"`
	Attempts       int             `json:"attempts"`
	MaxAttempts    int             `json:"max_attempts"`
	Priority       int             `json:"priority"`
	RunAfter       time.Time       `json:"run_after"`
	LockedBy       string          `json:"locked_by"`
	LeaseExpiresAt *time.Time      `json:"lease_expires_at,omitempty"`
	LastError      string          `json:"last_error"`
	DedupeKey      string          `json:"dedupe_key"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// SealAnalysis is what the call seal writes with the call. Online decisions and the asynchronous
// job are the only analysis-adjacent rows created at admission time.
type SealAnalysis struct {
	Decisions []Decision
	Job       *AnalysisJob
	Snapshot  *RedactionSnapshot
}

// DecisionModelConfig is one project's optional decision model. The API key is stored sealed; only
// the service that calls the provider decrypts it.
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
