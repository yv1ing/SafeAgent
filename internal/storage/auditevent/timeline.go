package auditevent

import (
	"encoding/json"
	"time"
)

type Record struct {
	CallID        string          `json:"call_id"`
	Sequence      int64           `json:"sequence"`
	ProjectID     string          `json:"project_id"`
	Kind          string          `json:"kind"`
	SchemaVersion int             `json:"schema_version"`
	Payload       json.RawMessage `json:"payload"`
	CreatedAt     time.Time       `json:"created_at"`
	PrevHash      string          `json:"prev_hash"`
	Hash          string          `json:"hash"`
}

// Entry is one step of a call timeline. It carries metadata only: raw request, response and tool
// text stay behind the existing role-based views.
type Entry struct {
	Sequence int64     `json:"sequence"`
	At       time.Time `json:"at"`
	Kind     string    `json:"kind"`
	Category string    `json:"category"`
	Label    string    `json:"label"`
	Status   string    `json:"status"`
	Code     string    `json:"code,omitempty"`
	Message  string    `json:"message,omitempty"`
	Decoded  Decoded   `json:"-"`
}

func BuildTimeline(records []Record) ([]Entry, error) {
	entries := make([]Entry, 0, len(records))
	for _, record := range records {
		decoded, err := Decode(record.Kind, record.Payload)
		if err != nil {
			return nil, err
		}
		entries = append(entries, timelineEntry(record, decoded))
	}
	return entries, nil
}

func timelineEntry(record Record, decoded Decoded) Entry {
	entry := Entry{Sequence: record.Sequence, At: record.CreatedAt, Kind: record.Kind, Decoded: decoded}
	switch record.Kind {
	case KindRequest:
		entry.Category, entry.Label, entry.Status = "admission", "call_admitted", "info"
	case KindAttempt:
		entry.Category, entry.Label, entry.Status = "attempt", "upstream_attempt", "info"
		if decoded.Attempt != nil {
			entry.Message = decoded.Attempt.ChannelName
			entry.Code = decoded.Attempt.UpstreamModel
		}
	case KindAttemptFailed:
		entry.Category, entry.Label, entry.Status = "attempt", "attempt_failed", "failed"
		if decoded.AttemptFailure != nil {
			entry.Code = decoded.AttemptFailure.Code
		}
	case KindFirstToken:
		entry.Category, entry.Label, entry.Status = "stream", "first_token", "ok"
	case KindToolCall:
		entry.Category, entry.Label, entry.Status = "tool", "tool_call", "ok"
		if decoded.ToolCall != nil {
			entry.Message = decoded.ToolCall.Name
			if decoded.ToolCall.Blocked {
				entry.Status = "blocked"
			}
		}
	case KindToolResult:
		entry.Category, entry.Label, entry.Status = "tool", "tool_result", "ok"
		if decoded.ToolResult != nil {
			entry.Message = decoded.ToolResult.Name
			if decoded.ToolResult.IsError {
				entry.Status = "failed"
			}
		}
	case KindDecision:
		entry.Category, entry.Label, entry.Status = "policy", "policy_decision", "recorded"
		if decoded.Decision != nil {
			entry.Code = decoded.Decision.RuleID
			if entry.Code == "" {
				entry.Code = decoded.Decision.Code
			}
			entry.Message = decoded.Decision.Reason
			if decoded.Decision.Blocked {
				entry.Status = "blocked"
			}
		}
	case KindBlocked:
		entry.Category, entry.Label, entry.Status = "policy", "call_blocked", "blocked"
		if decoded.Blocked != nil {
			entry.Code = firstNonEmpty(decoded.Blocked.RuleID, decoded.Blocked.Code, decoded.Blocked.Reason)
		}
	case KindResponse:
		entry.Category, entry.Label, entry.Status = "response", "response_summary", "info"
		if decoded.Response != nil {
			if decoded.Response.Blocked {
				entry.Status = "blocked"
			} else if decoded.Response.Response.Status != "" {
				entry.Status = decoded.Response.Response.Status
			}
			entry.Code = string(decoded.Response.Response.Finish.Kind)
		}
	case KindCancellation:
		entry.Category, entry.Label, entry.Status = "terminal", "cancellation", "cancelled"
		if decoded.Cancellation != nil {
			entry.Code = decoded.Cancellation.Code
		}
	case KindQuotaFailed:
		entry.Category, entry.Label, entry.Status = "terminal", "quota_failed", "failed"
		if decoded.QuotaFailure != nil {
			entry.Code = decoded.QuotaFailure.Error
		}
	case KindTerminal:
		entry.Category, entry.Label, entry.Status = "terminal", "terminal", "info"
		if decoded.Terminal != nil {
			entry.Code = decoded.Terminal.Code
			if decoded.Terminal.State != "" {
				entry.Status = decoded.Terminal.State
			}
		}
	default:
		entry.Category, entry.Label, entry.Status = "other", "event", "info"
	}
	return entry
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
