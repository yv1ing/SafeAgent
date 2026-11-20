package format

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

// PartContext carries what encoding one block needs to know about its surroundings. Output marks a
// block that is part of a response rather than a request, which some formats shape differently.
type PartContext struct {
	Target    protocol.Format
	Peer      Peer
	Role      protocol.Role
	Output    bool
	ToolNames map[string]string
	ToolKinds map[string]protocol.ToolKind
}

func NewPartContext(target protocol.Format, peer Peer) PartContext {
	return PartContext{Target: target, Peer: peer, ToolNames: map[string]string{}, ToolKinds: map[string]protocol.ToolKind{}}
}

// Track remembers a tool call so that its result can be encoded with the call's name and kind.
func (context PartContext) Track(block protocol.Block) {
	if block.Kind == protocol.KindToolCall && block.ToolCall != nil {
		context.ToolNames[block.ToolCall.ID] = block.ToolCall.Name
		context.ToolKinds[block.ToolCall.ID] = block.ToolCall.Kind
	}
}

// Blank reports a plain text block without content, which Messages and Gemini reject and which
// carries nothing a target could use.
func Blank(block protocol.Block) bool {
	return block.Kind == protocol.KindText && block.Text == "" && len(block.Citations) == 0 && block.Reasoning == nil
}

// DropBlank returns the blocks that carry content.
func DropBlank(blocks []protocol.Block) []protocol.Block {
	kept := make([]protocol.Block, 0, len(blocks))
	for _, block := range blocks {
		if !Blank(block) {
			kept = append(kept, block)
		}
	}
	return kept
}

// EncodeBlocks encodes blocks in order and skips the ones the target cannot represent; every skip
// is reported as a loss, so the result never contains a null part. Blank text blocks are skipped
// silently.
func EncodeBlocks(blocks []protocol.Block, context PartContext, path string) ([]any, []protocol.Loss, error) {
	var losses []protocol.Loss
	parts := make([]any, 0, len(blocks))
	for index, block := range blocks {
		context.Track(block)
		if Blank(block) {
			continue
		}
		blockPath := path + "[" + strconv.Itoa(index) + "]"
		part, nested, err := EncodePart(block, context)
		if err != nil {
			return nil, losses, errors.New(blockPath + ": " + err.Error())
		}
		losses = append(losses, prefixLosses(nested, blockPath)...)
		if part != nil {
			parts = append(parts, part)
		}
	}
	return parts, losses, nil
}

func prefixLosses(losses []protocol.Loss, path string) []protocol.Loss {
	for index := range losses {
		losses[index].Path = joinPath(path, losses[index].Path)
	}
	return losses
}

// EncodePart returns a nil part, with the reason in the losses, when the target cannot carry the
// block.
func EncodePart(block protocol.Block, context PartContext) (map[string]any, []protocol.Loss, error) {
	part, losses, err := encodePart(block, context)
	if err != nil || part == nil {
		return nil, losses, err
	}
	if block.CacheControl != nil {
		if context.Target == protocol.Messages {
			part["cache_control"] = encodeCacheControl(block.CacheControl)
		} else {
			losses = append(losses, dropped("cache_control", "target has no prompt cache markers", context.Target))
		}
	}
	if block.Kind != protocol.KindNative && block.Kind != protocol.KindRedacted && !serverCall(block) {
		losses = append(losses, ApplyExtras(part, block.Extras, context.Target, "extras")...)
	}
	return part, losses, nil
}

func encodePart(block protocol.Block, context PartContext) (map[string]any, []protocol.Loss, error) {
	switch block.Kind {
	case protocol.KindNative:
		return encodeNative(block, context.Target, "block")
	case protocol.KindRedacted:
		return encodeNative(block, context.Target, "reasoning")
	case protocol.KindMedia:
		part, err := encodeMedia(block, context.Target)
		return part, nil, err
	case protocol.KindText:
		return encodeText(block, context)
	case protocol.KindRefusal:
		return encodeRefusal(block, context)
	case protocol.KindReasoning:
		return encodeReasoning(block, context)
	case protocol.KindToolCall:
		if block.ToolCall == nil {
			return nil, nil, errors.New("tool call block has no call")
		}
		return encodeToolCall(block, context)
	case protocol.KindToolResult:
		if block.ToolResult == nil {
			return nil, nil, errors.New("tool result block has no result")
		}
		return encodeToolResult(block, context)
	}
	return nil, nil, fmt.Errorf("unsupported block kind %q", block.Kind)
}

func encodeNative(block protocol.Block, target protocol.Format, path string) (map[string]any, []protocol.Loss, error) {
	if native := nativeObject(block.Extras, target); native != nil {
		return native, nil, nil
	}
	return nil, []protocol.Loss{{Path: path, Code: protocol.LossFieldDropped, Detail: "the provider block cannot be represented in the target format", From: block.Extras.Format, To: target}}, nil
}

func encodeText(block protocol.Block, context PartContext) (map[string]any, []protocol.Loss, error) {
	var losses []protocol.Loss
	var part map[string]any
	switch context.Target {
	case protocol.Gemini:
		part = map[string]any{"text": block.Text}
		if block.Reasoning != nil {
			if signature := context.Peer.WireSignature(block.Reasoning.Signature, block.Text); signature != "" {
				part["thoughtSignature"] = signature
			}
		}
	case protocol.Responses:
		kind := "input_text"
		if context.Role == protocol.RoleAssistant {
			kind = "output_text"
		}
		part = map[string]any{"type": kind, "text": block.Text}
		if context.Output {
			part["annotations"] = []any{}
		}
	default:
		part = map[string]any{"type": "text", "text": block.Text}
	}
	if block.Reasoning != nil && block.Reasoning.Signature != nil && context.Target != protocol.Gemini {
		losses = append(losses, protocol.Loss{Path: "signature", Code: protocol.LossSignatureStripped, Detail: "target has no signature on text", To: context.Target})
	}
	if len(block.Citations) > 0 {
		annotations, nested := encodeCitations(block.Citations, context.Target)
		losses = append(losses, nested...)
		switch {
		case len(annotations) == 0:
		case context.Target == protocol.Messages:
			part["citations"] = annotations
		case context.Target == protocol.Responses && context.Role == protocol.RoleAssistant:
			part["annotations"] = annotations
		}
	}
	return part, losses, nil
}

func encodeRefusal(block protocol.Block, context PartContext) (map[string]any, []protocol.Loss, error) {
	switch {
	case context.Target == protocol.Responses && context.Role == protocol.RoleAssistant:
		return map[string]any{"type": "refusal", "refusal": block.Text}, nil, nil
	case context.Target == protocol.Gemini:
		return map[string]any{"text": block.Text}, nil, nil
	}
	return encodeText(protocol.Block{Kind: protocol.KindText, Text: block.Text}, context)
}

func encodeReasoning(block protocol.Block, context PartContext) (map[string]any, []protocol.Loss, error) {
	signature := ""
	if block.Reasoning != nil {
		signature = context.Peer.WireSignature(block.Reasoning.Signature, block.Text)
	}
	hasItem := block.Reasoning != nil && block.Reasoning.ItemID != "" && context.Target == protocol.Responses
	if signature == "" && block.Text == "" && context.Target != protocol.Gemini && !hasItem {
		return nil, []protocol.Loss{{Path: "reasoning", Code: protocol.LossFieldDropped, Detail: "reasoning without text or signature carries nothing", To: context.Target}}, nil
	}
	switch context.Target {
	case protocol.Messages:
		part := map[string]any{"type": "thinking", "thinking": block.Text}
		if signature != "" {
			part["signature"] = signature
		}
		return part, nil, nil
	case protocol.Gemini:
		part := map[string]any{"text": block.Text, "thought": true}
		if signature != "" {
			part["thoughtSignature"] = signature
		}
		return part, nil, nil
	case protocol.Responses:
		item := map[string]any{"type": "reasoning", "summary": []any{}}
		if block.Text != "" {
			if block.Reasoning != nil && block.Reasoning.Visibility == "full" {
				item["content"] = []any{map[string]any{"type": "reasoning_text", "text": block.Text}}
			} else {
				item["summary"] = []any{map[string]any{"type": "summary_text", "text": block.Text}}
			}
		}
		if block.Reasoning != nil && block.Reasoning.ItemID != "" && (signature != "" || context.Peer.Side == Client) {
			item["id"] = block.Reasoning.ItemID
		}
		if signature != "" {
			item["encrypted_content"] = signature
		}
		return item, nil, nil
	}
	return nil, nil, errors.New("target cannot represent reasoning content")
}

// toolArguments returns the call arguments as the JSON object Messages and Gemini require; custom
// tool input that is not an object travels under "input".
func toolArguments(call *protocol.ToolCall) (any, error) {
	arguments := EmptyObjectIfBlank(call.Arguments)
	decoded, err := protocol.DecodeJSON([]byte(arguments))
	if err == nil {
		if _, isObject := decoded.(map[string]any); isObject {
			return decoded, nil
		}
	}
	if call.Kind == protocol.ToolCustom {
		return map[string]any{"input": call.Arguments}, nil
	}
	return nil, errors.New("tool arguments must be a JSON object")
}

func encodeToolCall(block protocol.Block, context PartContext) (map[string]any, []protocol.Loss, error) {
	call := block.ToolCall
	if call.Kind == protocol.ToolServer {
		return encodeServerCall(block, context.Target)
	}
	var losses []protocol.Loss
	mapped := protocol.Loss{Path: "tool_call.kind", Code: protocol.LossToolKindMapped, Detail: "a custom tool became a function call", To: context.Target}
	switch context.Target {
	case protocol.Responses:
		if call.Kind == protocol.ToolCustom {
			return map[string]any{"type": "custom_tool_call", "call_id": call.ID, "name": call.Name, "input": call.Arguments}, nil, nil
		}
		return map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": EmptyObjectIfBlank(call.Arguments)}, nil, nil
	case protocol.Messages, protocol.Gemini:
		arguments, err := toolArguments(call)
		if err != nil {
			return nil, nil, err
		}
		if call.Kind == protocol.ToolCustom {
			losses = append(losses, mapped)
		}
		if context.Target == protocol.Messages {
			return map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": arguments}, losses, nil
		}
		return encodeGeminiCall(call, arguments, context.Peer, losses)
	}
	if call.Kind == protocol.ToolCustom {
		return map[string]any{"id": call.ID, "type": "custom", "custom": map[string]any{"name": call.Name, "input": call.Arguments}}, nil, nil
	}
	return map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": EmptyObjectIfBlank(call.Arguments)}}, nil, nil
}

func encodeGeminiCall(call *protocol.ToolCall, arguments any, peer Peer, losses []protocol.Loss) (map[string]any, []protocol.Loss, error) {
	function := map[string]any{"name": call.Name, "args": arguments}
	if call.ID != "" {
		function["id"] = call.ID
	}
	part := map[string]any{"functionCall": function}
	if signature := peer.WireSignature(call.Signature, ""); signature != "" {
		part["thoughtSignature"] = signature
	} else if peer.Side == Upstream {
		part["thoughtSignature"] = geminiPlaceholderSignature
		losses = append(losses, protocol.Loss{Path: "tool_call.signature", Code: protocol.LossGeminiDummySignature, Detail: "the original thought signature was unavailable", To: protocol.Gemini})
	}
	return part, losses, nil
}

func encodeServerCall(block protocol.Block, target protocol.Format) (map[string]any, []protocol.Loss, error) {
	native := nativeObject(block.Extras, target)
	if native == nil {
		return nil, []protocol.Loss{{Path: "tool_call", Code: protocol.LossFieldDropped, Detail: "provider-executed tool call cannot cross protocols", From: block.Extras.Format, To: target}}, nil
	}
	if _, hasInput := native["input"]; hasInput && block.ToolCall.Arguments != "" {
		if input, err := protocol.DecodeJSON([]byte(block.ToolCall.Arguments)); err == nil {
			native["input"] = input
		}
	}
	return native, nil, nil
}

func encodeToolResult(block protocol.Block, context PartContext) (map[string]any, []protocol.Loss, error) {
	result := block.ToolResult
	var losses []protocol.Loss
	text, hasMedia := "", false
	for _, content := range result.Content {
		switch content.Kind {
		case protocol.KindText:
			text += content.Text
		default:
			hasMedia = true
		}
	}
	if result.IsError && context.Target != protocol.Messages && context.Target != protocol.Gemini {
		losses = append(losses, dropped("tool_result.is_error", "target has no tool error flag", context.Target))
	}
	switch context.Target {
	case protocol.Responses:
		kind := "function_call_output"
		if context.ToolKinds[result.CallID] == protocol.ToolCustom {
			kind = "custom_tool_call_output"
		}
		var output any = text
		if hasMedia {
			inner := context
			inner.Role = protocol.RoleUser
			parts, nested, err := EncodeBlocks(result.Content, inner, "tool_result.content")
			if err != nil {
				return nil, nil, err
			}
			output, losses = parts, append(losses, nested...)
		}
		return map[string]any{"type": kind, "call_id": result.CallID, "output": output}, losses, nil
	case protocol.Messages:
		var content any = text
		if hasMedia || len(result.Content) > 1 || annotated(result.Content) {
			parts, nested, err := EncodeBlocks(result.Content, context, "tool_result.content")
			if err != nil {
				return nil, nil, err
			}
			content, losses = parts, append(losses, nested...)
		}
		part := map[string]any{"type": "tool_result", "tool_use_id": result.CallID, "content": content}
		if result.IsError {
			part["is_error"] = true
		}
		return part, losses, nil
	case protocol.Gemini:
		name := result.Name
		if name == "" {
			name = context.ToolNames[result.CallID]
		}
		if name == "" {
			return nil, nil, fmt.Errorf("tool result %s has no corresponding tool call name", result.CallID)
		}
		if hasMedia {
			losses = append(losses, dropped("tool_result.content", "non-text tool output is not representable in Gemini", context.Target))
		}
		response := geminiResponse(text, result.IsError)
		function := map[string]any{"name": name, "response": response}
		if result.CallID != "" {
			function["id"] = result.CallID
		}
		return map[string]any{"functionResponse": function}, losses, nil
	}
	if hasMedia {
		losses = append(losses, dropped("tool_result.content", "non-text tool output is only representable in Messages and Responses", context.Target))
	}
	return map[string]any{"role": "tool", "tool_call_id": result.CallID, "content": text}, losses, nil
}

func annotated(blocks []protocol.Block) bool {
	for _, block := range blocks {
		if block.CacheControl != nil || len(block.Extras.Fields) > 0 {
			return true
		}
	}
	return false
}

func geminiResponse(text string, failed bool) map[string]any {
	if !failed {
		if object, err := protocol.DecodeObject([]byte(text)); err == nil {
			return object
		}
	}
	if failed {
		return map[string]any{"error": text}
	}
	return map[string]any{"result": text}
}
