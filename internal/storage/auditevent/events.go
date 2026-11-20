// Package auditevent is the single contract for the JSON payloads stored in call events. The
// gateway writes it, and the observation adapter reads it; neither side defines its own shape.
package auditevent

import (
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/redact"
)

const SchemaVersion = 3

const (
	KindRequest       = "request"
	KindAttempt       = "attempt"
	KindAttemptFailed = "attempt_failed"
	KindFirstToken    = "attempt_first_token"
	KindToolCall      = "tool_call"
	KindToolResult    = "tool_result"
	KindResponse      = "response"
	KindBlocked       = "blocked"
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
	Items     []protocol.Item  `json:"items"`
	Params    protocol.Request `json:"params"`
	Tools     []protocol.Tool  `json:"tools,omitempty"`
	Hits      []redact.Hit     `json:"hits,omitempty"`
	Ledger    protocol.Ledger  `json:"ledger,omitempty"`
	Principal *Principal       `json:"principal,omitempty"`
}

type Response struct {
	Response protocol.Response `json:"response"`
	Ledger   protocol.Ledger   `json:"ledger,omitempty"`
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

type Terminal struct {
	State  string          `json:"state"`
	Code   string          `json:"code,omitempty"`
	Ledger protocol.Ledger `json:"ledger,omitempty"`
}

type Decision struct {
	Stage   string `json:"stage"`
	Action  string `json:"action"`
	Blocked bool   `json:"blocked"`
	RuleID  string `json:"rule_id,omitempty"`
	Code    string `json:"code,omitempty"`
	Reason  string `json:"reason,omitempty"`
}
