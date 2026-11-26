package codec

import (
	"errors"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type PartContext struct {
	Target    model.Format
	Peer      Peer
	Role      model.Role
	Output    bool
	ToolKinds map[string]model.ToolKind
}

var serverCallTypes = map[string]bool{
	"server_tool_use": true, "mcp_tool_use": true,
	"web_search_call": true, "file_search_call": true, "code_interpreter_call": true,
	"image_generation_call": true, "mcp_call": true,
}

// DecodeContentItems turns one wire message's content into ordered items. Consecutive text and
// media parts share one message item; reasoning, tool and native blocks become their own items and
// split the surrounding message so the original order survives.
func DecodeContentItems(kind model.Format, role model.Role, content any, peer Peer) ([]model.Item, error) {
	var items []model.Item
	var parts []model.Part
	flush := func() {
		if len(parts) == 0 {
			return
		}
		items = append(items, model.Item{Kind: model.ItemMessage, Role: role, Parts: parts})
		parts = nil
	}
	if content == nil {
		return nil, nil
	}
	if text, ok := content.(string); ok {
		if text != "" {
			parts = append(parts, model.Part{Kind: model.PartText, Text: text})
		}
		flush()
		return items, nil
	}
	entries, ok := content.([]any)
	if !ok {
		return nil, errors.New("content must be text or an array")
	}
	for _, value := range entries {
		entry := model.Object(value)
		if entry == nil {
			return nil, errors.New("content parts must be objects")
		}
		if model.String(entry["type"]) == "message" || model.String(entry["type"]) == "" && model.String(entry["role"]) != "" {
			flush()
			nested, err := DecodeContentItems(kind, model.Role(model.String(entry["role"])), entry["content"], peer)
			if err != nil {
				return nil, err
			}
			id := model.String(entry["id"])
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
		if media, err := decodeMedia(entry); err != nil {
			return nil, err
		} else if media != nil {
			part := model.Part{Kind: model.PartMedia, Media: media, CacheControl: DecodeCacheControl(entry["cache_control"])}
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

func decodeBlockPart(kind model.Format, entry map[string]any) (*model.Item, *model.Part, error) {
	blockType := model.String(entry["type"])
	if blockType == "" {
		return nil, nil, errors.New("content block is missing its type")
	}
	cache := DecodeCacheControl(entry["cache_control"])
	switch blockType {
	case "text", "input_text", "output_text":
		part := &model.Part{Kind: model.PartText, Text: model.String(entry["text"]), Citations: DecodeCitations(entry), CacheControl: cache}
		part.Raw = CaptureRaw(kind, entry, "type", "text", "citations", "annotations", "cache_control", "id")
		return nil, part, nil
	case "refusal":
		part := &model.Part{Kind: model.PartRefusal, Text: model.String(entry["refusal"])}
		part.Raw = CaptureRaw(kind, entry, "type", "refusal")
		return nil, part, nil
	case "thinking":
		text := model.String(entry["thinking"])
		item := &model.Item{Kind: model.ItemReasoning, Role: model.RoleAssistant}
		item.Parts = []model.Part{{Kind: model.PartReasoningText, Text: text}}
		item.Reasoning = &model.Reasoning{Visibility: "full"}
		if signature := model.String(entry["signature"]); signature != "" {
			item.Reasoning.Signature = &model.Signature{Vendor: model.VendorAnthropic, Blob: signature, TextDigest: model.TextDigest(text)}
		}
		item.CacheControl = cache
		item.Raw = CaptureRaw(kind, entry, "type", "thinking", "signature", "cache_control")
		return item, nil, nil
	case "redacted_thinking":
		item := &model.Item{Kind: model.ItemReasoning, Role: model.RoleAssistant}
		item.Parts = []model.Part{{Kind: model.PartRedactedReasoning, Text: model.String(entry["data"])}}
		item.Raw = nativeRaw(kind, entry)
		return item, nil, nil
	case "reasoning":
		return decodeReasoningItem(entry), nil, nil
	case "tool_use":
		return &model.Item{
			Kind: model.ItemToolCall, Role: model.RoleAssistant, ID: model.String(entry["id"]),
			ToolCall:     &model.ToolCall{Kind: model.ToolFunction, ID: model.String(entry["id"]), Name: model.String(entry["name"]), Arguments: EmptyObjectIfBlank(inputJSON(entry["input"]))},
			CacheControl: cache,
			Raw:          CaptureRaw(kind, entry, "type", "id", "name", "input", "cache_control"),
		}, nil, nil
	case "server_tool_use", "mcp_tool_use":
		server := &model.ServerInfo{Format: kind, Type: blockType, Capability: model.ServerCapability(&model.ServerTool{Format: kind, Type: blockType}), Config: model.Marshal(entry)}
		return &model.Item{
			Kind: model.ItemServerToolCall, Role: model.RoleAssistant, ID: model.String(entry["id"]),
			ToolCall: &model.ToolCall{Kind: model.ToolServer, ID: model.String(entry["id"]), Name: FirstNonEmpty(model.String(entry["name"]), blockType), Arguments: EmptyObjectIfBlank(inputJSON(entry["input"]))},
			Server:   server,
			Raw:      nativeRaw(kind, entry),
		}, nil, nil
	case "web_search_tool_result", "code_execution_tool_result", "mcp_tool_result":
		server := &model.ServerInfo{Format: kind, Type: blockType, Capability: model.ServerCapability(&model.ServerTool{Format: kind, Type: blockType}), Config: model.Marshal(entry)}
		return &model.Item{
			Kind: model.ItemServerToolResult, Role: model.RoleUser, CallID: model.String(entry["tool_use_id"]),
			IsError: entry["is_error"] == true, Server: server, Raw: nativeRaw(kind, entry),
		}, nil, nil
	case "tool_result":
		result, err := decodeToolResultItem(entry)
		return result, nil, err
	case "function_call":
		arguments := model.String(entry["arguments"])
		if arguments != "" && !ValidJSON(arguments) {
			return nil, nil, errors.New("tool arguments must be valid JSON")
		}
		return &model.Item{
			Kind: model.ItemToolCall, Role: model.RoleAssistant, ID: model.String(entry["id"]),
			ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: model.String(entry["call_id"]), Name: model.String(entry["name"]), Arguments: EmptyObjectIfBlank(arguments)},
			Raw:      CaptureRaw(kind, entry, "type", "id", "call_id", "name", "arguments", "status"),
		}, nil, nil
	case "custom_tool_call":
		return &model.Item{
			Kind: model.ItemToolCall, Role: model.RoleAssistant, ID: model.String(entry["id"]),
			ToolCall: &model.ToolCall{Kind: model.ToolCustom, ID: model.String(entry["call_id"]), Name: model.String(entry["name"]), Arguments: model.String(entry["input"])},
			Raw:      CaptureRaw(kind, entry, "type", "id", "call_id", "name", "input", "status"),
		}, nil, nil
	case "function_call_output", "custom_tool_call_output":
		parts, err := DecodeToolResultParts(kind, entry["output"])
		if err != nil {
			return nil, nil, err
		}
		return &model.Item{
			Kind: model.ItemToolResult, Role: model.RoleTool, ID: model.String(entry["id"]),
			CallID: model.String(entry["call_id"]), Parts: parts,
			Raw: CaptureRaw(kind, entry, "type", "id", "call_id", "output", "status"),
		}, nil, nil
	}
	if serverCallTypes[blockType] {
		id := model.String(entry["id"])
		server := &model.ServerInfo{Format: kind, Type: blockType, Capability: model.ServerCapability(&model.ServerTool{Format: kind, Type: blockType}), Config: model.Marshal(entry)}
		return &model.Item{
			Kind: model.ItemServerToolCall, Role: model.RoleAssistant, ID: id,
			ToolCall: &model.ToolCall{Kind: model.ToolServer, ID: id, Name: FirstNonEmpty(model.String(entry["name"]), blockType), Arguments: EmptyObjectIfBlank(inputJSON(entry["input"]))},
			Server:   server,
			Raw:      nativeRaw(kind, entry),
		}, nil, nil
	}
	return &model.Item{Kind: model.ItemNative, Raw: nativeRaw(kind, entry)}, nil, nil
}

func decodeReasoningItem(entry map[string]any) *model.Item {
	item := &model.Item{Kind: model.ItemReasoning, Role: model.RoleAssistant, ID: model.String(entry["id"])}
	var summary, full strings.Builder
	for _, value := range model.Array(entry["summary"]) {
		if part := model.Object(value); part != nil {
			summary.WriteString(model.String(part["text"]))
		}
	}
	for _, value := range model.Array(entry["content"]) {
		if part := model.Object(value); model.String(part["type"]) == "reasoning_text" {
			full.WriteString(model.String(part["text"]))
		}
	}
	visibility := "summary"
	if full.Len() > 0 {
		item.Parts = append(item.Parts, model.Part{Kind: model.PartReasoningText, Text: full.String()})
		visibility = "full"
	}
	if summary.Len() > 0 {
		item.Parts = append(item.Parts, model.Part{Kind: model.PartReasoningSummary, Text: summary.String()})
	}
	item.Reasoning = &model.Reasoning{Visibility: visibility}
	if encrypted := model.String(entry["encrypted_content"]); encrypted != "" {
		text := item.Text()
		item.Reasoning.Signature = &model.Signature{Vendor: model.VendorOpenAI, Blob: encrypted, TextDigest: model.TextDigest(text)}
	}
	item.Raw = CaptureRaw(model.Responses, entry, "type", "id", "summary", "content", "encrypted_content", "status")
	return item
}

func decodeToolResultItem(entry map[string]any) (*model.Item, error) {
	parts, err := DecodeToolResultParts(model.Messages, entry["content"])
	if err != nil {
		return nil, err
	}
	return &model.Item{
		Kind: model.ItemToolResult, Role: model.RoleTool, CallID: model.String(entry["tool_use_id"]),
		IsError: entry["is_error"] == true, Parts: parts, CacheControl: DecodeCacheControl(entry["cache_control"]),
		Raw: CaptureRaw(model.Messages, entry, "type", "tool_use_id", "content", "is_error", "cache_control"),
	}, nil
}

func DecodeToolResultParts(kind model.Format, content any) ([]model.Part, error) {
	if content == nil {
		return nil, nil
	}
	if text, ok := content.(string); ok {
		return model.TextParts(text), nil
	}
	entries, ok := content.([]any)
	if !ok {
		return []model.Part{{Kind: model.PartText, Text: string(model.Marshal(content))}}, nil
	}
	parts := make([]model.Part, 0, len(entries))
	for _, value := range entries {
		entry := model.Object(value)
		if entry == nil {
			parts = append(parts, model.Part{Kind: model.PartText, Text: model.String(value)})
			continue
		}
		if media, err := decodeMedia(entry); err != nil {
			return nil, err
		} else if media != nil {
			part := model.Part{Kind: model.PartMedia, Media: media}
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
		case item != nil && item.Kind == model.ItemNative:
			parts = append(parts, model.Part{Kind: model.PartNative, Raw: item.Raw})
		default:
			parts = append(parts, model.Part{Kind: model.PartNative, Raw: nativeRaw(kind, entry)})
		}
	}
	return parts, nil
}

// EncodePart renders one part for a target. A part the target cannot carry returns nil and a loss.
func EncodePart(part model.Part, item *model.Item, context PartContext, path string) (any, []model.Loss, error) {
	var encoded map[string]any
	var losses []model.Loss
	switch part.Kind {
	case model.PartText:
		encoded = encodeTextPart(part, context)
	case model.PartRefusal:
		if context.Target == model.Responses && context.Output {
			encoded = map[string]any{"type": "refusal", "refusal": part.Text}
			break
		}
		encoded = encodeTextPart(model.Part{Kind: model.PartText, Text: part.Text, Citations: part.Citations}, context)
	case model.PartReasoningText, model.PartReasoningSummary:
		encoded = encodeReasoningPart(part, item, context)
	case model.PartRedactedReasoning:
		encoded = RawObject(part.Raw, context.Target)
		if encoded == nil {
			losses = append(losses, dropped(path, "redacted reasoning is only meaningful to its originating provider", context.Target))
		}
	case model.PartMedia:
		var err error
		encoded, err = EncodeMedia(part.Media, context.Target)
		if err != nil {
			return nil, losses, errors.New(path + ": " + err.Error())
		}
	case model.PartToolArguments:
		return nil, nil, nil
	case model.PartNative:
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
		if context.Target == model.Messages {
			encoded["cache_control"] = EncodeCacheControl(part.CacheControl)
		} else {
			losses = append(losses, dropped(path+".cache_control", "target has no prompt cache markers", context.Target))
		}
	}
	if len(part.Citations) > 0 {
		switch context.Target {
		case model.Responses:
			if !context.Output {
				losses = append(losses, dropped(path+".citations", "Responses input content carries no annotations", context.Target))
			}
		case model.Gemini:
			if !context.Output {
				losses = append(losses, dropped(path+".citations", "Gemini carries citations on the response candidate", context.Target))
			}
		}
	}
	losses = append(losses, RawFields(encoded, part.Raw, context.Target, path+".raw")...)
	return encoded, losses, nil
}

func encodeTextPart(part model.Part, context PartContext) map[string]any {
	var encoded map[string]any
	switch context.Target {
	case model.Gemini:
		encoded = map[string]any{"text": part.Text}
	case model.Responses:
		kind := "input_text"
		if context.Output || context.Role == model.RoleAssistant {
			kind = "output_text"
		}
		encoded = map[string]any{"type": kind, "text": part.Text}
		if context.Output && len(part.Citations) > 0 {
			annotations, _ := encodeCitations(part.Citations, context.Target)
			encoded["annotations"] = annotations
		} else if context.Output {
			encoded["annotations"] = []any{}
		}
	case model.Messages:
		encoded = map[string]any{"type": "text", "text": part.Text}
	default:
		encoded = map[string]any{"type": "text", "text": part.Text}
	}
	if len(part.Citations) > 0 && context.Target == model.Messages {
		citations, _ := encodeCitations(part.Citations, context.Target)
		encoded["citations"] = citations
	}
	return encoded
}

func encodeReasoningPart(part model.Part, item *model.Item, context PartContext) map[string]any {
	signature := ""
	if item != nil && item.Reasoning != nil && SignatureCoversText(item.Reasoning.Signature, part.Kind, part.Text) {
		signature = context.Peer.WireSignature(item.Reasoning.Signature, part.Text)
	}
	switch context.Target {
	case model.Messages:
		encoded := map[string]any{"type": "thinking", "thinking": part.Text}
		if signature != "" {
			encoded["signature"] = signature
		}
		return encoded
	case model.Gemini:
		encoded := map[string]any{"text": part.Text, "thought": true}
		if signature != "" {
			encoded["thoughtSignature"] = signature
		}
		return encoded
	case model.Responses:
		if part.Kind == model.PartReasoningSummary {
			return map[string]any{"type": "summary_text", "text": part.Text}
		}
		return map[string]any{"type": "reasoning_text", "text": part.Text}
	}
	return map[string]any{"type": "text", "text": part.Text}
}

// SignatureCoversText reports whether a reasoning signature belongs to this part. A signature
// without a digest covers the full reasoning text; a digest-bound signature covers only the part it
// was issued for, which keeps a summary from claiming the full block's signature.
func SignatureCoversText(signature *model.Signature, kind model.PartKind, text string) bool {
	if signature == nil || signature.Blob == "" {
		return false
	}
	if signature.TextDigest == "" {
		return kind == model.PartReasoningText
	}
	return signature.TextDigest == model.TextDigest(text)
}
