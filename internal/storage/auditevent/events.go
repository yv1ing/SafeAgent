// Package auditevent is the single contract for the JSON payloads stored in call events. The
// gateway writes it, and the observation adapter reads it; neither side defines its own shape.
package auditevent

import (
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/redact"
)

const SchemaVersion = 2

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

// Request carries the messages this call added (not the whole expanded history), the request
// parameters, and the tools when they differ from the parent. Raw text is stored once; the redacted
// view is derived on read from the call's immutable redaction snapshot.
type Request struct {
	Messages []protocol.Message `json:"messages"`
	Params   protocol.Request   `json:"params"`
	Tools    []protocol.Tool    `json:"tools,omitempty"`
	Hits     []redact.Hit       `json:"hits,omitempty"`
	Losses   []protocol.Loss    `json:"losses,omitempty"`
}

type Response struct {
	Response protocol.Response `json:"response"`
	Losses   []protocol.Loss   `json:"losses,omitempty"`
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
	Losses []protocol.Loss `json:"losses,omitempty"`
}

type Decision struct {
	Stage   string `json:"stage"`
	Action  string `json:"action"`
	Blocked bool   `json:"blocked"`
	RuleID  string `json:"rule_id,omitempty"`
	Code    string `json:"code,omitempty"`
	Reason  string `json:"reason,omitempty"`
}
