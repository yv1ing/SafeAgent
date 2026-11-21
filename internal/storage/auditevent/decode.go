package auditevent

import "encoding/json"

// Decoded is the typed form of one event payload. Unknown kinds keep the raw payload so a newer
// event can still be listed by an older reader.
type Decoded struct {
	Kind           string
	Request        *Request
	Attempt        *Attempt
	AttemptFailure *AttemptFailure
	FirstToken     *FirstToken
	ToolCall       *ToolCall
	ToolResult     *ToolResult
	Decision       *Decision
	Response       *Response
	Blocked        *Decision
	Cancellation   *Cancellation
	QuotaFailure   *QuotaFailure
	Terminal       *Terminal
	Raw            json.RawMessage
}

func Decode(kind string, payload json.RawMessage) (Decoded, error) {
	decoded := Decoded{Kind: kind, Raw: payload}
	var target any
	switch kind {
	case KindRequest:
		decoded.Request = new(Request)
		target = decoded.Request
	case KindAttempt:
		decoded.Attempt = new(Attempt)
		target = decoded.Attempt
	case KindAttemptFailed:
		decoded.AttemptFailure = new(AttemptFailure)
		target = decoded.AttemptFailure
	case KindFirstToken:
		decoded.FirstToken = new(FirstToken)
		target = decoded.FirstToken
	case KindToolCall:
		decoded.ToolCall = new(ToolCall)
		target = decoded.ToolCall
	case KindToolResult:
		decoded.ToolResult = new(ToolResult)
		target = decoded.ToolResult
	case KindDecision:
		decoded.Decision = new(Decision)
		target = decoded.Decision
	case KindResponse:
		decoded.Response = new(Response)
		target = decoded.Response
	case KindBlocked:
		decoded.Blocked = new(Decision)
		target = decoded.Blocked
	case KindCancellation:
		decoded.Cancellation = new(Cancellation)
		target = decoded.Cancellation
	case KindQuotaFailed:
		decoded.QuotaFailure = new(QuotaFailure)
		target = decoded.QuotaFailure
	case KindTerminal:
		decoded.Terminal = new(Terminal)
		target = decoded.Terminal
	default:
		return decoded, nil
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return decoded, err
	}
	return decoded, nil
}

// LastUserText returns the text of the last user message a request added. It is the metadata-only
// summary the console shows for a conversation node.
func LastUserText(request *Request) string {
	if request == nil {
		return ""
	}
	for index := len(request.Items) - 1; index >= 0; index-- {
		item := request.Items[index]
		if item.Role != "user" || item.Kind != "message" {
			continue
		}
		for partIndex := len(item.Parts) - 1; partIndex >= 0; partIndex-- {
			if text := item.Parts[partIndex].Text; text != "" {
				return text
			}
		}
	}
	return ""
}
