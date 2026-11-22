// Package auditevent is the single contract for the JSON payloads stored in call events. The
// gateway writes it, and every reader decodes it through this package.
package auditevent

import (
	"encoding/json"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/redact"
)

const SchemaVersion = 6

const (
	KindRequest       = "request"
	KindAttempt       = "attempt"
	KindAttemptFailed = "attempt_failed"
	KindFirstToken    = "attempt_first_token"
	KindToolCall      = "tool_call"
	KindToolResult    = "tool_result"
	KindDecision      = "decision"
	KindResponse      = "response"
	KindBlocked       = "blocked"
	KindCancellation  = "cancellation"
	KindQuotaFailed   = "quota_failed"
	KindTerminal      = "terminal"
)

// Principal is the agent-key identity at the time of the call. Analysis replays use this snapshot
// so a later key edit cannot change the verdict of a recorded trace.
type Principal struct {
	AgentKeyID string `json:"agent_key_id"`
	Role       string `json:"role,omitempty"`
	TrustLevel int    `json:"trust_level"`
}

// Request carries the items this call added (not the whole expanded history), the request
// parameters, and the tools when they differ from the parent. Raw text is stored once; the redacted
// view is derived on read from the call's immutable redaction snapshot.
type Request struct {
	Items         []model.Item  `json:"items"`
	Params        model.Request `json:"params"`
	Tools         []model.Tool  `json:"tools,omitempty"`
	Hits          []redact.Hit  `json:"hits,omitempty"`
	Ledger        model.Ledger  `json:"ledger,omitempty"`
	Principal     *Principal    `json:"principal,omitempty"`
	SessionSource string        `json:"session_source,omitempty"`
	// Body is the redacted request of an auxiliary endpoint (embeddings, moderations) that has no
	// canonical items.
	Body json.RawMessage `json:"body,omitempty"`
}

type Attempt struct {
	ChannelID     string `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	ModelID       string `json:"model_id"`
	UpstreamModel string `json:"upstream_model"`
	Format        string `json:"format"`
	KeyID         string `json:"key_id"`
}

type AttemptFailure struct {
	Code      string `json:"code"`
	Class     string `json:"class"`
	ChannelID string `json:"channel_id"`
	KeyID     string `json:"key_id"`
}

type FirstToken struct {
	Milliseconds int64 `json:"milliseconds"`
}

type Response struct {
	Response  model.Response `json:"response"`
	Ledger    model.Ledger   `json:"ledger,omitempty"`
	Committed bool           `json:"committed,omitempty"`
	Partial   bool           `json:"partial,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
	Blocked   bool           `json:"blocked,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Blocked   bool   `json:"blocked,omitempty"`
}

type ToolResult struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

type Decision struct {
	ID            string `json:"id,omitempty"`
	PolicyVersion int64  `json:"policy_version,omitempty"`
	Stage         string `json:"stage"`
	Action        string `json:"action"`
	Blocked       bool   `json:"blocked"`
	RuleID        string `json:"rule_id,omitempty"`
	Code          string `json:"code,omitempty"`
	Severity      string `json:"severity,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type Cancellation struct {
	Code      string `json:"code,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Committed bool   `json:"committed,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type QuotaFailure struct {
	Error string `json:"error"`
}

type Terminal struct {
	State  string       `json:"state"`
	Code   string       `json:"code,omitempty"`
	Ledger model.Ledger `json:"ledger,omitempty"`
}
