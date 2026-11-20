package format

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

const geminiPlaceholderSignature = "skip_thought_signature_validator"

var serverCallTypes = map[string]bool{
	"server_tool_use": true, "mcp_tool_use": true,
	"web_search_call": true, "file_search_call": true, "code_interpreter_call": true, "image_generation_call": true, "mcp_call": true,
}

func DecodeContent(kind protocol.Format, content any) ([]protocol.Block, error) {
	if content == nil {
		return nil, nil
	}
	if text, ok := content.(string); ok {
		return []protocol.Block{{Kind: protocol.KindText, Text: text}}, nil
	}
	items, ok := content.([]any)
	if !ok {
		return nil, errors.New("content must be text or an array")
	}
	blocks := make([]protocol.Block, 0, len(items))
	for _, item := range items {
		entry := protocol.Object(item)
		if entry == nil {
			return nil, errors.New("content parts must be objects")
		}
		block, err := DecodeBlock(kind, entry)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func DecodeToolResultContent(kind protocol.Format, content any) ([]protocol.Block, error) {
	if content == nil {
		return nil, nil
	}
	if text, ok := content.(string); ok {
		return []protocol.Block{{Kind: protocol.KindText, Text: text}}, nil
	}
	items, ok := content.([]any)
	if !ok {
		return []protocol.Block{{Kind: protocol.KindText, Text: string(protocol.Marshal(content))}}, nil
	}
	blocks := make([]protocol.Block, 0, len(items))
	for _, item := range items {
		entry := protocol.Object(item)
		if entry == nil {
			blocks = append(blocks, protocol.Block{Kind: protocol.KindText, Text: protocol.String(item)})
			continue
		}
		block, err := DecodeBlock(kind, entry)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func DecodeBlock(kind protocol.Format, entry map[string]any) (protocol.Block, error) {
	if kind == protocol.Gemini {
		return decodeGeminiPart(entry)
	}
	blockType := protocol.String(entry["type"])
	if blockType == "" {
		return protocol.Block{}, errors.New("content block is missing its type")
	}
	media, err := decodeMedia(entry)
	if err != nil {
		return protocol.Block{}, err
	}
	if media != nil {
		block := protocol.Block{Kind: protocol.KindMedia, Media: media}
		return finishBlock(block, kind, entry, mediaKeys...), nil
	}
	block, consumed, err := decodeTyped(kind, blockType, entry)
	if err != nil {
		return block, err
	}
	return finishBlock(block, kind, entry, consumed...), nil
}

func finishBlock(block protocol.Block, kind protocol.Format, entry map[string]any, consumed ...string) protocol.Block {
	if block.Kind == protocol.KindNative || block.Kind == protocol.KindRedacted || serverCall(block) {
		block.Extras = nativeExtras(kind, entry)
		if block.Kind == protocol.KindNative {
			return block
		}
	}
	if cache := decodeCacheControl(entry["cache_control"]); cache != nil && block.Kind != protocol.KindNative {
		block.CacheControl = cache
		consumed = append(consumed, "cache_control")
	}
	if serverCall(block) || block.Kind == protocol.KindRedacted {
		return block
	}
	block.Extras = UnmodelledFields(kind, entry, consumed...)
	return block
}

func serverCall(block protocol.Block) bool {
	return block.Kind == protocol.KindToolCall && block.ToolCall != nil && block.ToolCall.Kind == protocol.ToolServer
}

func decodeTyped(kind protocol.Format, blockType string, entry map[string]any) (protocol.Block, []string, error) {
	block := protocol.Block{}
	switch blockType {
	case "refusal":
		block.Kind, block.Text = protocol.KindRefusal, protocol.String(entry["refusal"])
		return block, []string{"type", "refusal"}, nil
	case "text", "input_text", "output_text":
		block.Kind, block.Text = protocol.KindText, protocol.String(entry["text"])
		block.Citations = decodeCitations(entry)
		return block, []string{"type", "text", "citations", "annotations"}, nil
	case "thinking":
		block.Kind, block.Text = protocol.KindReasoning, protocol.String(entry["thinking"])
		block.Reasoning = &protocol.Reasoning{Visibility: "full"}
		if signature := protocol.String(entry["signature"]); signature != "" {
			block.Reasoning.Signature = &protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: signature, TextDigest: protocol.TextDigest(block.Text)}
		}
		return block, []string{"type", "thinking", "signature"}, nil
	case "redacted_thinking":
		block.Kind, block.Text = protocol.KindRedacted, protocol.String(entry["data"])
		return block, nil, nil
	case "reasoning":
		block.Kind, block.Text = protocol.KindReasoning, reasoningSummary(entry)
		block.Reasoning = &protocol.Reasoning{Visibility: "summary", ItemID: protocol.String(entry["id"])}
		if full := reasoningContent(entry); full != "" {
			block.Text, block.Reasoning.Visibility = full, "full"
		}
		if encrypted := protocol.String(entry["encrypted_content"]); encrypted != "" {
			block.Reasoning.Signature = &protocol.Signature{Vendor: protocol.VendorOpenAI, Blob: encrypted, TextDigest: protocol.TextDigest(block.Text)}
		}
		return block, []string{"type", "id", "summary", "content", "encrypted_content"}, nil
	case "custom_tool_call":
		block.Kind = protocol.KindToolCall
		block.ToolCall = &protocol.ToolCall{Kind: protocol.ToolCustom, ID: protocol.String(entry["call_id"]), Name: protocol.String(entry["name"]), Arguments: protocol.String(entry["input"])}
		return block, []string{"type", "call_id", "name", "input"}, nil
	case "function_call":
		arguments := protocol.String(entry["arguments"])
		if arguments != "" && !json.Valid([]byte(arguments)) {
			return block, nil, errors.New("tool arguments must be valid JSON")
		}
		block.Kind = protocol.KindToolCall
		block.ToolCall = &protocol.ToolCall{Kind: protocol.ToolFunction, ID: protocol.String(entry["call_id"]), Name: protocol.String(entry["name"]), Arguments: EmptyObjectIfBlank(arguments)}
		return block, []string{"type", "call_id", "name", "arguments"}, nil
	case "tool_use":
		block.Kind = protocol.KindToolCall
		block.ToolCall = &protocol.ToolCall{Kind: protocol.ToolFunction, ID: protocol.String(entry["id"]), Name: protocol.String(entry["name"]), Arguments: EmptyObjectIfBlank(inputJSON(entry["input"]))}
		return block, []string{"type", "id", "name", "input"}, nil
	case "tool_result", "function_call_output", "custom_tool_call_output":
		return decodeToolResult(kind, blockType, entry)
	}
	if serverCallTypes[blockType] {
		block.Kind = protocol.KindToolCall
		name := protocol.String(entry["name"])
		if name == "" {
			name = blockType
		}
		block.ToolCall = &protocol.ToolCall{Kind: protocol.ToolServer, ID: protocol.String(entry["id"]), Name: name, Arguments: EmptyObjectIfBlank(inputJSON(entry["input"]))}
		return block, nil, nil
	}
	block.Kind = protocol.KindNative
	return block, nil, nil
}

func decodeToolResult(kind protocol.Format, blockType string, entry map[string]any) (protocol.Block, []string, error) {
	block := protocol.Block{Kind: protocol.KindToolResult}
	result := &protocol.ToolResult{}
	content := entry["content"]
	consumed := []string{"type"}
	if blockType == "tool_result" {
		result.CallID = protocol.String(entry["tool_use_id"])
		result.IsError = entry["is_error"] == true
		consumed = append(consumed, "tool_use_id", "content", "is_error")
	} else {
		result.CallID, content = protocol.String(entry["call_id"]), entry["output"]
		consumed = append(consumed, "call_id", "output")
	}
	blocks, err := DecodeToolResultContent(kind, content)
	if err != nil {
		return block, nil, err
	}
	result.Content = blocks
	block.ToolResult = result
	return block, consumed, nil
}

func reasoningSummary(entry map[string]any) string {
	var text strings.Builder
	for _, item := range protocol.Array(entry["summary"]) {
		if part := protocol.Object(item); part != nil {
			text.WriteString(protocol.String(part["text"]))
		}
	}
	return text.String()
}

// reasoningContent is the raw reasoning text of a Responses reasoning item, which open-weight
// models return instead of, or next to, a summary.
func reasoningContent(entry map[string]any) string {
	var text strings.Builder
	for _, item := range protocol.Array(entry["content"]) {
		if part := protocol.Object(item); protocol.String(part["type"]) == "reasoning_text" {
			text.WriteString(protocol.String(part["text"]))
		}
	}
	return text.String()
}

func inputJSON(value any) string {
	if value == nil {
		return ""
	}
	return string(protocol.Marshal(value))
}

func decodeGeminiPart(entry map[string]any) (protocol.Block, error) {
	signature := protocol.String(entry["thoughtSignature"])
	switch {
	case entry["text"] != nil:
		block := protocol.Block{Kind: protocol.KindText, Text: protocol.String(entry["text"])}
		if entry["thought"] == true {
			block.Kind = protocol.KindReasoning
		}
		if entry["thought"] == true || signature != "" {
			block.Reasoning = &protocol.Reasoning{Visibility: "full"}
		}
		if signature != "" {
			block.Reasoning.Signature = &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: signature, TextDigest: protocol.TextDigest(block.Text)}
		}
		block.Extras = UnmodelledFields(protocol.Gemini, entry, "text", "thought", "thoughtSignature")
		return block, nil
	case entry["functionCall"] != nil:
		call := protocol.Object(entry["functionCall"])
		arguments := "{}"
		if call["args"] != nil {
			arguments = string(protocol.Marshal(call["args"]))
		}
		block := protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: protocol.String(call["id"]), Name: protocol.String(call["name"]), Arguments: arguments}}
		if signature != "" {
			block.ToolCall.Signature = &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: signature}
		}
		block.Extras = UnmodelledFields(protocol.Gemini, entry, "functionCall", "thoughtSignature")
		return block, nil
	case entry["functionResponse"] != nil:
		result := protocol.Object(entry["functionResponse"])
		response := protocol.Object(result["response"])
		content, failed := string(protocol.Marshal(result["response"])), false
		switch {
		case len(response) == 1 && response["result"] != nil:
			if text, ok := response["result"].(string); ok {
				content = text
			}
		case len(response) == 1 && response["error"] != nil:
			failed = true
			if text, ok := response["error"].(string); ok {
				content = text
			}
		case response["error"] != nil:
			failed = true
		}
		block := protocol.Block{Kind: protocol.KindToolResult, ToolResult: &protocol.ToolResult{CallID: protocol.String(result["id"]), Name: protocol.String(result["name"]), IsError: failed, Content: []protocol.Block{{Kind: protocol.KindText, Text: content}}}}
		block.Extras = UnmodelledFields(protocol.Gemini, entry, "functionResponse")
		return block, nil
	}
	if media, err := decodeGeminiMedia(entry); err != nil || media != nil {
		return protocol.Block{Kind: protocol.KindMedia, Media: media}, err
	}
	return protocol.Block{Kind: protocol.KindNative, Extras: nativeExtras(protocol.Gemini, entry)}, nil
}
