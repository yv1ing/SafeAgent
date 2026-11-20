package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

type canonicalBlock struct {
	Kind      BlockKind `json:"kind"`
	Text      string    `json:"text,omitempty"`
	ToolName  string    `json:"tool_name,omitempty"`
	Arguments string    `json:"arguments,omitempty"`
	Result    []string  `json:"result,omitempty"`
	IsError   bool      `json:"is_error,omitempty"`
	Media     string    `json:"media,omitempty"`
}

type canonicalMessage struct {
	Role   Role             `json:"role"`
	Blocks []canonicalBlock `json:"blocks"`
}

func canonicalize(messages []Message) []canonicalMessage {
	result := make([]canonicalMessage, 0, len(messages))
	for _, message := range messages {
		entry := canonicalMessage{Role: message.Role}
		for _, block := range message.Blocks {
			switch block.Kind {
			case KindText, KindRefusal:
				if block.Text == "" {
					continue
				}
				if count := len(entry.Blocks); count > 0 && entry.Blocks[count-1].Kind == KindText {
					entry.Blocks[count-1].Text += block.Text
					continue
				}
				entry.Blocks = append(entry.Blocks, canonicalBlock{Kind: KindText, Text: block.Text})
			case KindToolCall:
				if block.ToolCall == nil {
					continue
				}
				entry.Blocks = append(entry.Blocks, canonicalBlock{Kind: KindToolCall, ToolName: block.ToolCall.Name, Arguments: canonicalArguments(block.ToolCall.Arguments)})
			case KindToolResult:
				if block.ToolResult == nil {
					continue
				}
				entry.Blocks = append(entry.Blocks, canonicalBlock{Kind: KindToolResult, Result: resultTexts(block.ToolResult.Content), IsError: block.ToolResult.IsError})
			case KindMedia:
				if block.Media != nil {
					entry.Blocks = append(entry.Blocks, canonicalBlock{Kind: KindMedia, Media: block.Media.MIMEType})
				}
			}
		}
		if len(entry.Blocks) == 0 {
			continue
		}
		if entry.Role == RoleAssistant && len(result) > 0 && result[len(result)-1].Role == RoleAssistant {
			previous := &result[len(result)-1]
			for _, block := range entry.Blocks {
				if count := len(previous.Blocks); count > 0 && previous.Blocks[count-1].Kind == KindText && block.Kind == KindText {
					previous.Blocks[count-1].Text += block.Text
					continue
				}
				previous.Blocks = append(previous.Blocks, block)
			}
			continue
		}
		result = append(result, entry)
	}
	return result
}

func resultTexts(content []Block) []string {
	texts := make([]string, 0, len(content))
	for _, block := range content {
		if block.Kind == KindText && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return texts
}

func canonicalArguments(arguments string) string {
	decoded, err := DecodeJSON([]byte(arguments))
	if err != nil {
		return ""
	}
	return string(Marshal(decoded))
}

func historyBytes(messages []Message) []byte {
	return Marshal(canonicalize(messages))
}

func HistoryFingerprint(secret []byte, messages []Message) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("safeagent/history/v1\x00"))
	mac.Write(historyBytes(messages))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

// HistoryPrefix fingerprints the request up to and including the last assistant turn, which is what
// a follow-up request repeats before appending its new messages.
func HistoryPrefix(secret []byte, messages []Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == RoleAssistant {
			return HistoryFingerprint(secret, messages[:index+1])
		}
	}
	return ""
}

// RepeatsPrefix reports whether messages starts with history. Cache markers, signatures, item ids
// and JSON formatting are ignored because clients do not echo them, while every block and its
// content still has to match so that the offset never cuts a turn the client actually sent.
func RepeatsPrefix(messages, history []Message) bool {
	if len(history) == 0 || len(messages) < len(history) {
		return false
	}
	return string(prefixBytes(messages[:len(history)])) == string(prefixBytes(history))
}

func prefixBytes(messages []Message) []byte {
	normalized := make([]Message, len(messages))
	for index, message := range messages {
		normalized[index] = Message{Role: message.Role, Extras: message.Extras, Blocks: make([]Block, len(message.Blocks))}
		for position, block := range message.Blocks {
			block = CloneBlock(block)
			block.CacheControl = nil
			if block.Reasoning != nil {
				block.Reasoning.Signature = nil
				block.Reasoning.ItemID = ""
			}
			if block.ToolCall != nil {
				block.ToolCall.Signature = nil
				block.ToolCall.Arguments = canonicalArguments(block.ToolCall.Arguments)
			}
			normalized[index].Blocks[position] = block
		}
	}
	return Marshal(normalized)
}

func ConversationFingerprint(secret []byte, request *Request, response *Response) string {
	messages := make([]Message, 0, len(request.Messages)+1)
	messages = append(messages, request.Messages...)
	messages = append(messages, Message{Role: RoleAssistant, Blocks: response.Blocks})
	return HistoryFingerprint(secret, messages)
}
