package entity

import (
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/classify"
)

// Call is the operational row for one gateway request. Analysis state here is a materialized
// projection of the latest analysis; analyses and analysis_findings are the authority.
type Call struct {
	bun.BaseModel          `bun:"table:calls,alias:c"`
	ID                     string                 `bun:",pk" json:"id" required:"true"`
	ProjectID              string                 `json:"project_id"`
	AgentKeyID             string                 `json:"agent_key_id"`
	ClientFormat           string                 `json:"client_format"`
	Operation              classify.Operation     `json:"operation"`
	Purpose                classify.Purpose       `json:"purpose"`
	PurposeSource          classify.PurposeSource `json:"purpose_source"`
	Lane                   classify.Lane          `json:"lane"`
	ClientTransport        string                 `json:"client_transport"`
	UpstreamFormat         string                 `json:"upstream_format"`
	PublicModel            string                 `json:"public_model"`
	ModelID                string                 `bun:"column:model_id" json:"model_id"`
	UpstreamModel          string                 `json:"upstream_model"`
	ChannelID              string                 `json:"channel_id"`
	RouteID                string                 `json:"route_id"`
	Resolution             string                 `json:"resolution"`
	State                  string                 `json:"state"`
	Outcome                classify.Outcome       `json:"outcome"`
	ErrorCode              string                 `json:"error_code"`
	Stream                 bool                   `json:"stream"`
	FirstTokenMilliseconds int                    `json:"first_token_ms"`
	DurationMilliseconds   int                    `json:"duration_ms"`
	InputTokens            int64                  `json:"input_tokens"`
	OutputTokens           int64                  `json:"output_tokens"`
	CacheReadTokens        int64                  `json:"cache_read_tokens"`
	CacheWriteTokens       int64                  `json:"cache_write_tokens"`
	ReasoningTokens        int64                  `json:"reasoning_tokens"`
	UsageKnown             bool                   `json:"usage_known"`
	CostMicros             *int64                 `json:"cost_micros"`
	Currency               string                 `json:"currency"`
	RedactionValues        int                    `json:"redaction_values"`
	RedactionLabels        []string               `bun:"type:jsonb" json:"redaction_labels"`
	MaxSeverity            string                 `json:"max_severity"`
	Losses                 []Loss                 `bun:"type:jsonb" json:"losses"`
	Repairs                []Repair               `bun:"type:jsonb" json:"repairs"`
	Attempts               int                    `json:"attempts"`
	ResponseID             string                 `json:"response_id"`
	ResponseStore          bool                   `json:"response_store"`
	ResponseDeletedAt      *time.Time             `json:"response_deleted_at,omitempty"`
	CancelledAt            *time.Time             `json:"cancelled_at,omitempty"`
	SessionID              string                 `json:"session_id"`
	ParentCallID           string                 `json:"parent_call_id"`
	ConversationHash       string                 `json:"conversation_hash"`
	MessageOffset          int                    `json:"message_offset"`
	AuditHead              string                 `json:"audit_head"`
	AuditEvents            int64                  `json:"audit_events"`
	AnalysisState          string                 `json:"analysis_state"`
	LatestAnalysisID       string                 `json:"latest_analysis_id"`
	FindingCount           int                    `json:"finding_count"`
	CreatedAt              time.Time              `json:"created_at"`
	UpdatedAt              time.Time              `json:"updated_at"`
	TerminatedAt           *time.Time             `json:"terminated_at,omitempty"`
}

type Repair struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Detail  string `json:"detail,omitempty"`
	ItemKey string `json:"item_key,omitempty"`
}

type Loss struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Class   string `json:"class,omitempty"`
	Detail  string `json:"detail,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	ItemKey string `json:"item_key,omitempty"`
}

// Conversation is the console's unit of listing: the calls that share one session id.
type Conversation struct {
	bun.BaseModel `bun:"table:conversations,alias:cv"`
	ID            string    `bun:",pk" json:"id" required:"true"`
	ProjectID     string    `json:"project_id"`
	FirstCallID   string    `json:"first_call_id"`
	LastCallID    string    `json:"last_call_id"`
	CreatedAt     time.Time `json:"created_at"`
	LastAt        time.Time `json:"last_at"`
	Calls         int       `bun:",scanonly" json:"calls"`
	MaxSeverity   string    `bun:",scanonly" json:"max_severity"`
}

// CallTool maps a tool call id to the call that issued it, so a tool result finds its parent by index.
type CallTool struct {
	bun.BaseModel `bun:"table:call_tools,alias:ct"`
	ProjectID     string `bun:",pk"`
	ToolCallID    string `bun:",pk"`
	CallID        string
	CreatedAt     time.Time
}
