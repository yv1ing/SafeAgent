package entity

import (
	"time"

	"github.com/uptrace/bun"
)

// Call is the operational row for one gateway request. Analysis state here is a materialized
// projection of the latest analysis; analyses and analysis_findings are the authority.
type Call struct {
	bun.BaseModel          `bun:"table:calls,alias:c"`
	ID                     string     `bun:",pk" json:"id" required:"true"`
	ProjectID              string     `json:"project_id"`
	AgentKeyID             string     `json:"agent_key_id"`
	ClientFormat           string     `json:"client_format"`
	PublicModel            string     `json:"public_model"`
	UpstreamModel          string     `json:"upstream_model"`
	ChannelID              string     `json:"channel_id"`
	State                  string     `json:"state"`
	ErrorCode              string     `json:"error_code"`
	Stream                 bool       `json:"stream"`
	FirstTokenMilliseconds int        `json:"first_token_ms"`
	DurationMilliseconds   int        `json:"duration_ms"`
	InputTokens            int64      `json:"input_tokens"`
	OutputTokens           int64      `json:"output_tokens"`
	CacheReadTokens        int64      `json:"cache_read_tokens"`
	CacheWriteTokens       int64      `json:"cache_write_tokens"`
	ReasoningTokens        int64      `json:"reasoning_tokens"`
	UsageKnown             bool       `json:"usage_known"`
	CostMicros             *int64     `json:"cost_micros"`
	Currency               string     `json:"currency"`
	RedactionHits          int        `json:"redaction_hits"`
	MaxSeverity            string     `json:"max_severity"`
	Losses                 []Loss     `bun:"type:jsonb" json:"losses"`
	Repairs                []Repair   `bun:"type:jsonb" json:"repairs"`
	Attempts               int        `json:"attempts"`
	ResponseID             string     `json:"response_id"`
	SessionID              string     `json:"session_id"`
	ParentCallID           string     `json:"parent_call_id"`
	ConversationHash       string     `json:"conversation_hash"`
	MessageOffset          int        `json:"message_offset"`
	AnalysisState          string     `json:"analysis_state"`
	LatestAnalysisID       string     `json:"latest_analysis_id"`
	FindingCount           int        `json:"finding_count"`
	CreatedAt              time.Time  `json:"created_at"`
	TerminatedAt           *time.Time `json:"terminated_at,omitempty"`
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
