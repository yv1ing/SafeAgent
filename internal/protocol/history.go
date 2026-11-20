package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

type canonicalPart struct {
	Kind  PartKind `json:"kind"`
	Text  string   `json:"text,omitempty"`
	Media string   `json:"media,omitempty"`
}

type canonicalItem struct {
	Kind      ItemKind        `json:"kind"`
	Role      Role            `json:"role,omitempty"`
	Parts     []canonicalPart `json:"parts,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Input     string          `json:"input,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

func canonicalize(items []Item) []canonicalItem {
	result := make([]canonicalItem, 0, len(items))
	for _, item := range items {
		entry := canonicalItem{Kind: item.Kind, Role: item.Role, CallID: item.CallID, Name: item.Name, IsError: item.IsError}
		switch item.Kind {
		case ItemNative:
			continue
		case ItemToolCall, ItemServerToolCall:
			if item.ToolCall != nil {
				entry.CallID, entry.Name = item.ToolCall.ID, item.ToolCall.Name
				if item.ToolCall.Kind == ToolCustom {
					entry.Input = item.ToolCall.Arguments
				} else {
					entry.Arguments = canonicalArguments(item.ToolCall.Arguments)
				}
			}
		case ItemToolResult, ItemServerToolResult:
			entry.CallID = item.CallID
		}
		appendPart := func(part canonicalPart) {
			if part.Kind == PartText || part.Kind == PartRefusal || part.Kind == PartReasoningText || part.Kind == PartReasoningSummary {
				if part.Text == "" {
					return
				}
				if count := len(entry.Parts); count > 0 && entry.Parts[count-1].Kind == part.Kind {
					entry.Parts[count-1].Text += part.Text
					return
				}
			}
			entry.Parts = append(entry.Parts, part)
		}
		for _, part := range item.Parts {
			if part.Kind == PartToolArguments {
				continue
			}
			switch part.Kind {
			case PartMedia:
				if part.Media != nil {
					appendPart(canonicalPart{Kind: PartMedia, Media: mediaFingerprint(part.Media)})
				}
			case PartNative:
			default:
				appendPart(canonicalPart{Kind: part.Kind, Text: part.Text})
			}
		}
		if len(entry.Parts) == 0 && entry.Kind != ItemToolCall && entry.Kind != ItemServerToolCall && entry.Kind != ItemToolResult && entry.Kind != ItemServerToolResult {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func mediaFingerprint(media *Media) string {
	sum := sha256.Sum256([]byte(media.MIMEType + "\x00" + media.Data + "\x00" + media.URL + "\x00" + media.FileID))
	return media.MIMEType + ":" + hex.EncodeToString(sum[:8])
}

func canonicalArguments(arguments string) string {
	decoded, err := DecodeJSON([]byte(arguments))
	if err != nil {
		return ""
	}
	return string(Marshal(decoded))
}

func HistoryFingerprint(secret []byte, items []Item) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("safeagent/history/v1\x00"))
	mac.Write(Marshal(canonicalize(items)))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

// HistoryPrefix fingerprints the request up to and including the last assistant turn, which is what
// a follow-up request repeats before appending its new items.
func HistoryPrefix(secret []byte, items []Item) string {
	for index := len(items) - 1; index >= 0; index-- {
		if items[index].Role == RoleAssistant {
			return HistoryFingerprint(secret, items[:index+1])
		}
	}
	return ""
}

// RepeatsPrefix reports whether items starts with history. Cache markers, signatures, item ids and
// JSON formatting are ignored because clients do not echo them, while every part and its content
// still has to match so that the offset never cuts a turn the client actually sent.
func RepeatsPrefix(items, history []Item) bool {
	if len(history) == 0 || len(items) < len(history) {
		return false
	}
	return string(Marshal(canonicalize(items[:len(history)]))) == string(Marshal(canonicalize(history)))
}

func ConversationFingerprint(secret []byte, request *Request, response *Response) string {
	items := make([]Item, 0, len(request.Items)+len(response.Items))
	items = append(items, request.Items...)
	for _, item := range response.Items {
		item.Role = RoleAssistant
		items = append(items, item)
	}
	return HistoryFingerprint(secret, items)
}

func itemKey(index int) string { return "items[" + strconv.Itoa(index) + "]" }
