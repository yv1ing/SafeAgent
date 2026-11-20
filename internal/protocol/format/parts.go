package format

import (
	"errors"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

type PartContext struct {
	Target    protocol.Format
	Peer      Peer
	Role      protocol.Role
	Output    bool
	ToolKinds map[string]protocol.ToolKind
}

var serverCallTypes = map[string]bool{
	"server_tool_use": true, "mcp_tool_use": true,
	"web_search_call": true, "file_search_call": true, "code_interpreter_call": true,
	"image_generation_call": true, "mcp_call": true,
}

// DecodeContentItems turns one wire message's content into ordered items. Consecutive text and
// media parts share one message item; reasoning, tool and native blocks become their own items and
// split the surrounding message so the original order survives.
func DecodeContentItems(kind protocol.Format, role protocol.Role, content any, peer Peer) ([]protocol.Item, error) {
	var items []protocol.Item
	var parts []protocol.Part
	flush := func() {
		if len(parts) == 0 {
			return
		}
		items = append(items, protocol.Item{Kind: protocol.ItemMessage, Role: role, Parts: parts})
		parts = nil
	}
	if content == nil {
		return nil, nil
	}
	if text, ok := content.(string); ok {
		if text != "" {
			parts = append(parts, protocol.Part{Kind: protocol.PartText, Text: text})
		}
		flush()
		return items, nil
	}
	entries, ok := content.([]any)
	if !ok {
		return nil, errors.New("content must be text or an array")
	}
	for _, value := range entries {
		entry := protocol.Object(value)
		if entry == nil {
			return nil, errors.New("content parts must be objects")
		}
		if protocol.String(entry["type"]) == "message" || protocol.String(entry["type"]) == "" && protocol.String(entry["role"]) != "" {
			flush()
			nested, err := DecodeContentItems(kind, protocol.Role(protocol.String(entry["role"])), entry["content"], peer)
			if err != nil {
				return nil, err
			}
			id := protocol.String(entry["id"])
			raw := CaptureRaw(kind, entry, "type", "id", "role", "content", "status")
			for index := range nested {
				nested[index].ID = id
				if index == 0 {
					nested[index].Raw = raw
				}
			}
			items = append(items, nested...)
			continue
		}
		if media, err := DecodeMedia(entry); err != nil {
			return nil, err
		} else if media != nil {
			part := protocol.Part{Kind: protocol.PartMedia, Media: media, CacheControl: DecodeCacheControl(entry["cache_control"])}
			part.Raw = CaptureRaw(kind, entry, partMediaKeys...)
			parts = append(parts, part)
			continue
		}
		item, part, err := decodeBlockPart(kind, entry)
		if err != nil {
			return nil, err
		}
		if item != nil {
			flush()
			items = append(items, *item)
			continue
		}
		if part != nil {
			parts = append(parts, *part)
		}
	}
	flush()
	return items, nil
}

var partMediaKeys = []string{"type", "image_url", "source", "input_audio", "file", "data", "media_type", "detail", "file_id", "filename", "file_data", "file_url", "cache_control"}

func decodeBlockPart(kind protocol.Format, entry map[string]any) (*protocol.Item, *protocol.Part, error) {
	blockType := protocol.String(entry["type"])
	if blockType == "" {
		return nil, nil, errors.New("content block is missing its type")
	}
	cache := DecodeCacheControl(entry["cache_control"])
	switch blockType {
	case "text", "input_text", "output_text":
		part := &protocol.Part{Kind: protocol.PartText, Text: protocol.String(entry["text"]), Citations: DecodeCitations(entry), CacheControl: cache}
		part.Raw = CaptureRaw(kind, entry, "type", "text", "citations", "annotations", "cache_control", "id")
		return nil, part, nil
	case "refusal":
		part := &protocol.Part{Kind: protocol.PartRefusal, Text: protocol.String(entry["refusal"])}
		part.Raw = CaptureRaw(kind, entry, "type", "refusal")
		return nil, part, nil
	case "thinking":
		text := protocol.String(entry["thinking"])
		item := &protocol.Item{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant}
		item.Parts = []protocol.Part{{Kind: protocol.PartReasoningText, Text: text}}
		item.Reasoning = &protocol.Reasoning{Visibility: "full"}
		if signature := protocol.String(entry["signature"]); signature != "" {
			item.Reasoning.Signature = &protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: signature, TextDigest: protocol.TextDigest(text)}
		}
		item.CacheControl = cache
		item.Raw = CaptureRaw(kind, entry, "type", "thinking", "signature", "cache_control")
		return item, nil, nil
	case "redacted_thinking":
		item := &protocol.Item{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant}
		item.Parts = []protocol.Part{{Kind: protocol.PartRedactedReasoning, Text: protocol.String(entry["data"])}}
		item.Raw = nativeRaw(kind, entry)
		return item, nil, nil
	case "reasoning":
		return decodeReasoningItem(entry), nil, nil
	case "tool_use":
		return &protocol.Item{
			Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ID: protocol.String(entry["id"]),
			ToolCall:     &protocol.ToolCall{Kind: protocol.ToolFunction, ID: protocol.String(entry["id"]), Name: protocol.String(entry["name"]), Arguments: EmptyObjectIfBlank(inputJSON(entry["input"]))},
			CacheControl: cache,
			Raw:          CaptureRaw(kind, entry, "type", "id", "name", "input", "cache_control"),
		}, nil, nil
	case "server_tool_use", "mcp_tool_use":
		return &protocol.Item{
			Kind: protocol.ItemServerToolCall, Role: protocol.RoleAssistant, ID: protocol.String(entry["id"]),
			ToolCall: &protocol.ToolCall{Kind: protocol.ToolServer, ID: protocol.String(entry["id"]), Name: firstNonEmpty(protocol.String(entry["name"]), blockType), Arguments: EmptyObjectIfBlank(inputJSON(entry["input"]))},
			Raw:      nativeRaw(kind, entry),
		}, nil, nil
	case "tool_result":
		result, err := decodeToolResultItem(entry)
		return result, nil, err
	case "function_call":
		arguments := protocol.String(entry["arguments"])
		if arguments != "" && !validJSON(arguments) {
			return nil, nil, errors.New("tool arguments must be valid JSON")
		}
		return &protocol.Item{
			Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ID: protocol.String(entry["id"]),
			ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: protocol.String(entry["call_id"]), Name: protocol.String(entry["name"]), Arguments: EmptyObjectIfBlank(arguments)},
			Raw:      CaptureRaw(kind, entry, "type", "id", "call_id", "name", "arguments", "status"),
		}, nil, nil
	case "custom_tool_call":
		return &protocol.Item{
			Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ID: protocol.String(entry["id"]),
			ToolCall: &protocol.ToolCall{Kind: protocol.ToolCustom, ID: protocol.String(entry["call_id"]), Name: protocol.String(entry["name"]), Arguments: protocol.String(entry["input"])},
			Raw:      CaptureRaw(kind, entry, "type", "id", "call_id", "name", "input", "status"),
		}, nil, nil
	case "function_call_output", "custom_tool_call_output":
		parts, err := DecodeToolResultParts(kind, entry["output"])
		if err != nil {
			return nil, nil, err
		}
		return &protocol.Item{
			Kind: protocol.ItemToolResult, Role: protocol.RoleTool, ID: protocol.String(entry["id"]),
			CallID: protocol.String(entry["call_id"]), Parts: parts,
			Raw: CaptureRaw(kind, entry, "type", "id", "call_id", "output", "status"),
		}, nil, nil
	}
	if serverCallTypes[blockType] {
		id := protocol.String(entry["id"])
		return &protocol.Item{
			Kind: protocol.ItemServerToolCall, Role: protocol.RoleAssistant, ID: id,
			ToolCall: &protocol.ToolCall{Kind: protocol.ToolServer, ID: id, Name: firstNonEmpty(protocol.String(entry["name"]), blockType), Arguments: EmptyObjectIfBlank(inputJSON(entry["input"]))},
			Raw:      nativeRaw(kind, entry),
		}, nil, nil
	}
	return &protocol.Item{Kind: protocol.ItemNative, Raw: nativeRaw(kind, entry)}, nil, nil
}

func decodeReasoningItem(entry map[string]any) *protocol.Item {
	item := &protocol.Item{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, ID: protocol.String(entry["id"])}
	var summary, full strings.Builder
	for _, value := range protocol.Array(entry["summary"]) {
		if part := protocol.Object(value); part != nil {
			summary.WriteString(protocol.String(part["text"]))
		}
	}
	for _, value := range protocol.Array(entry["content"]) {
		if part := protocol.Object(value); protocol.String(part["type"]) == "reasoning_text" {
			full.WriteString(protocol.String(part["text"]))
		}
	}
	visibility := "summary"
	if full.Len() > 0 {
		item.Parts = append(item.Parts, protocol.Part{Kind: protocol.PartReasoningText, Text: full.String()})
		visibility = "full"
	}
	if summary.Len() > 0 {
		item.Parts = append(item.Parts, protocol.Part{Kind: protocol.PartReasoningSummary, Text: summary.String()})
	}
	item.Reasoning = &protocol.Reasoning{Visibility: visibility}
	if encrypted := protocol.String(entry["encrypted_content"]); encrypted != "" {
		text := item.Text()
		item.Reasoning.Signature = &protocol.Signature{Vendor: protocol.VendorOpenAI, Blob: encrypted, TextDigest: protocol.TextDigest(text)}
	}
	item.Raw = CaptureRaw(protocol.Responses, entry, "type", "id", "summary", "content", "encrypted_content", "status")
	return item
}

func decodeToolResultItem(entry map[string]any) (*protocol.Item, error) {
	parts, err := DecodeToolResultParts(protocol.Messages, entry["content"])
	if err != nil {
		return nil, err
	}
	return &protocol.Item{
		Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: protocol.String(entry["tool_use_id"]),
		IsError: entry["is_error"] == true, Parts: parts, CacheControl: DecodeCacheControl(entry["cache_control"]),
		Raw: CaptureRaw(protocol.Messages, entry, "type", "tool_use_id", "content", "is_error", "cache_control"),
	}, nil
}

func DecodeToolResultParts(kind protocol.Format, content any) ([]protocol.Part, error) {
	if content == nil {
		return nil, nil
	}
	if text, ok := content.(string); ok {
		return protocol.TextParts(text), nil
	}
	entries, ok := content.([]any)
	if !ok {
		return []protocol.Part{{Kind: protocol.PartText, Text: string(protocol.Marshal(content))}}, nil
	}
	parts := make([]protocol.Part, 0, len(entries))
	for _, value := range entries {
		entry := protocol.Object(value)
		if entry == nil {
			parts = append(parts, protocol.Part{Kind: protocol.PartText, Text: protocol.String(value)})
			continue
		}
		if media, err := DecodeMedia(entry); err != nil {
			return nil, err
		} else if media != nil {
			part := protocol.Part{Kind: protocol.PartMedia, Media: media}
			part.Raw = CaptureRaw(kind, entry, partMediaKeys...)
			parts = append(parts, part)
			continue
		}
		item, part, err := decodeBlockPart(kind, entry)
		if err != nil {
			return nil, err
		}
		switch {
		case part != nil:
			parts = append(parts, *part)
		case item != nil && item.Kind == protocol.ItemNative:
			parts = append(parts, protocol.Part{Kind: protocol.PartNative, Raw: item.Raw})
		default:
			parts = append(parts, protocol.Part{Kind: protocol.PartNative, Raw: nativeRaw(kind, entry)})
		}
	}
	return parts, nil
}

// EncodePart renders one part for a target. A part the target cannot carry returns nil and a loss.
func EncodePart(part protocol.Part, item *protocol.Item, context PartContext, path string) (any, []protocol.Loss, error) {
	var encoded map[string]any
	var losses []protocol.Loss
	switch part.Kind {
	case protocol.PartText:
		encoded = encodeTextPart(part, context)
	case protocol.PartRefusal:
		if context.Target == protocol.Responses && context.Output {
			encoded = map[string]any{"type": "refusal", "refusal": part.Text}
			break
		}
		encoded = encodeTextPart(protocol.Part{Kind: protocol.PartText, Text: part.Text, Citations: part.Citations}, context)
	case protocol.PartReasoningText, protocol.PartReasoningSummary:
		encoded = encodeReasoningPart(part, item, context)
	case protocol.PartRedactedReasoning:
		encoded = RawObject(part.Raw, context.Target)
		if encoded == nil {
			losses = append(losses, dropped(path, "redacted reasoning is only meaningful to its originating provider", context.Target))
		}
	case protocol.PartMedia:
		var err error
		encoded, err = EncodeMedia(part.Media, context.Target)
		if err != nil {
			return nil, losses, errors.New(path + ": " + err.Error())
		}
	case protocol.PartToolArguments:
		return nil, nil, nil
	case protocol.PartNative:
		encoded = RawObject(part.Raw, context.Target)
		if encoded == nil {
			losses = append(losses, dropped(path, "the provider part cannot be represented in the target format", context.Target))
		}
	default:
		return nil, nil, errors.New(path + ": unsupported part kind " + string(part.Kind))
	}
	if encoded == nil {
		return nil, losses, nil
	}
	if part.CacheControl != nil {
		if context.Target == protocol.Messages {
			encoded["cache_control"] = EncodeCacheControl(part.CacheControl)
		} else {
			losses = append(losses, dropped(path+".cache_control", "target has no prompt cache markers", context.Target))
		}
	}
	if len(part.Citations) > 0 {
		switch context.Target {
		case protocol.Responses:
			if !context.Output {
				losses = append(losses, dropped(path+".citations", "Responses input content carries no annotations", context.Target))
			}
		case protocol.Gemini:
			if !context.Output {
				losses = append(losses, dropped(path+".citations", "Gemini carries citations on the response candidate", context.Target))
			}
		}
	}
	losses = append(losses, RawFields(encoded, part.Raw, context.Target, path+".raw")...)
	return encoded, losses, nil
}

func encodeTextPart(part protocol.Part, context PartContext) map[string]any {
	var encoded map[string]any
	switch context.Target {
	case protocol.Gemini:
		encoded = map[string]any{"text": part.Text}
	case protocol.Responses:
		kind := "input_text"
		if context.Output || context.Role == protocol.RoleAssistant {
			kind = "output_text"
		}
		encoded = map[string]any{"type": kind, "text": part.Text}
		if context.Output && len(part.Citations) > 0 {
			annotations, _ := encodeCitations(part.Citations, context.Target)
			encoded["annotations"] = annotations
		} else if context.Output {
			encoded["annotations"] = []any{}
		}
	case protocol.Messages:
		encoded = map[string]any{"type": "text", "text": part.Text}
	default:
		encoded = map[string]any{"type": "text", "text": part.Text}
	}
	if len(part.Citations) > 0 && context.Target == protocol.Messages {
		citations, _ := encodeCitations(part.Citations, context.Target)
		encoded["citations"] = citations
	}
	return encoded
}

func encodeReasoningPart(part protocol.Part, item *protocol.Item, context PartContext) map[string]any {
	signature := ""
	if item != nil && item.Reasoning != nil {
		signature = context.Peer.WireSignature(item.Reasoning.Signature, item.Text())
	}
	switch context.Target {
	case protocol.Messages:
		encoded := map[string]any{"type": "thinking", "thinking": part.Text}
		if signature != "" {
			encoded["signature"] = signature
		}
		return encoded
	case protocol.Gemini:
		encoded := map[string]any{"text": part.Text, "thought": true}
		if signature != "" {
			encoded["thoughtSignature"] = signature
		}
		return encoded
	case protocol.Responses:
		if part.Kind == protocol.PartReasoningSummary {
			return map[string]any{"type": "summary_text", "text": part.Text}
		}
		return map[string]any{"type": "reasoning_text", "text": part.Text}
	}
	return map[string]any{"type": "text", "text": part.Text}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func validJSON(value string) bool {
	_, err := protocol.DecodeJSON([]byte(value))
	return err == nil
}

func inputJSON(value any) string {
	if value == nil {
		return ""
	}
	return string(protocol.Marshal(value))
}
