package messages

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

func (Codec) DecodeRequest(body []byte, header http.Header, peer format.Peer) (*protocol.Request, []protocol.Loss, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("request must be a JSON object")
	}
	request := &protocol.Request{Source: protocol.Messages, Model: protocol.String(root["model"])}
	if request.Model == "" {
		return nil, nil, errors.New("model is required")
	}
	if err := format.DecodeScalars(request, root, "max_tokens"); err != nil {
		return nil, nil, err
	}
	request.TopK = protocol.OptionalInteger(root["top_k"])
	if root["system"] != nil {
		items, err := format.DecodeContentItems(protocol.Messages, protocol.RoleSystem, root["system"], peer)
		if err != nil {
			return nil, nil, err
		}
		request.Items = append(request.Items, items...)
	}
	for _, value := range protocol.Array(root["messages"]) {
		entry := protocol.Object(value)
		role := protocol.Role(protocol.String(entry["role"]))
		switch role {
		case protocol.RoleUser, protocol.RoleAssistant, protocol.RoleSystem, protocol.RoleDeveloper:
		default:
			return nil, nil, errors.New("unsupported message role")
		}
		items, err := format.DecodeContentItems(protocol.Messages, role, entry["content"], peer)
		if err != nil {
			return nil, nil, err
		}
		raw := format.CaptureRaw(protocol.Messages, entry, messageKeys...)
		for index := range items {
			if index == 0 {
				items[index].Raw = raw
			}
			items[index].CacheControl = format.DecodeCacheControl(entry["cache_control"])
		}
		request.Items = append(request.Items, items...)
	}
	if len(request.Items) == 0 {
		return nil, nil, errors.New("at least one message is required")
	}
	if request.Stop, err = format.ParseStop(root["stop_sequences"]); err != nil {
		return nil, nil, err
	}
	if request.Tools, err = format.DecodeTools(protocol.Messages, protocol.Array(root["tools"])); err != nil {
		return nil, nil, err
	}
	if err := decodeToolChoice(request, root["tool_choice"]); err != nil {
		return nil, nil, err
	}
	if err := decodeOutput(request, root); err != nil {
		return nil, nil, err
	}
	request.User = protocol.String(protocol.Object(root["metadata"])["user_id"])
	request.ServiceTier = protocol.String(root["service_tier"])
	request.Raw = format.CaptureRaw(protocol.Messages, root, requestKeys...)
	format.AssignCallIDs(request.Items, format.NewCallIDs(peer))
	format.PairAnonymousResults(request.Items)
	for index := range request.Items {
		request.Items[index].Ordinal = index
	}
	return request, nil, nil
}

func decodeToolChoice(request *protocol.Request, value any) error {
	choice, err := format.ParseToolChoice(protocol.Messages, value)
	if err != nil {
		return err
	}
	request.ToolChoice = choice
	if disable, ok := protocol.Object(value)["disable_parallel_tool_use"].(bool); ok {
		parallel := !disable
		request.ParallelToolCalls = &parallel
	}
	return nil
}

func decodeOutput(request *protocol.Request, root map[string]any) error {
	output := protocol.Object(root["output_config"])
	if output != nil {
		responseFormat, err := format.ParseResponseFormat(output["format"])
		if err != nil {
			return err
		}
		request.ResponseFormat = responseFormat
	}
	thinking := protocol.Object(root["thinking"])
	effort := protocol.String(output["effort"])
	if thinking == nil && effort == "" {
		return nil
	}
	config := &protocol.ReasoningConfig{Effort: effort}
	switch protocol.String(thinking["type"]) {
	case "disabled":
		config.Effort = "none"
	case "adaptive", "":
		if config.Effort == "" {
			config.Effort = "auto"
		}
	case "enabled":
		if budget, ok := protocol.Integer(thinking["budget_tokens"]); ok {
			config.BudgetTokens = &budget
		} else if config.Effort == "" {
			config.Effort = "auto"
		}
	}
	config.Display = protocol.String(thinking["display"])
	if enabled, ok := thinking["between_tools"].(bool); ok {
		config.BetweenTools = enabled
	}
	request.Reasoning = config
	return nil
}

func (codec Codec) EncodeRequest(request *protocol.Request, peer format.Peer) ([]byte, http.Header, []protocol.Loss, error) {
	root := map[string]any{"model": peer.Model, "stream": request.Stream}
	var losses []protocol.Loss
	context := format.PartContext{Target: protocol.Messages, Peer: peer}
	var system []any
	if request.Instructions != "" {
		system = append(system, map[string]any{"type": "text", "text": request.Instructions})
	}
	messages := make([]any, 0, len(request.Items))
	for index := range request.Items {
		item := &request.Items[index]
		path := "items[" + strconv.Itoa(index) + "]"
		switch item.Kind {
		case protocol.ItemMessage, protocol.ItemReasoning:
			role := item.Role
			if item.Kind == protocol.ItemReasoning {
				role = protocol.RoleAssistant
			}
			if role == "" {
				role = protocol.RoleUser
			}
			if role == protocol.RoleSystem || role == protocol.RoleDeveloper {
				if role == protocol.RoleDeveloper {
					losses = append(losses, protocol.Loss{Path: path + ".role", Code: protocol.LossRoleMapped, Class: protocol.LossDegraded, Detail: "developer role represented as system prompt", To: protocol.Messages})
				}
				parts, nested, err := encodeContent(item, context, path)
				if err != nil {
					return nil, nil, losses, err
				}
				losses = append(losses, nested...)
				kept, dropped := textBlocks(parts)
				system = append(system, kept...)
				for range dropped {
					losses = append(losses, protocol.Loss{Path: path, Code: protocol.LossFieldDropped, Class: protocol.LossDegraded, Detail: "Messages system prompts only carry text", To: protocol.Messages})
				}
				continue
			}
			if role != protocol.RoleAssistant {
				role = protocol.RoleUser
			}
			parts, nested, err := encodeContent(item, context, path)
			if err != nil {
				return nil, nil, losses, err
			}
			losses = append(losses, nested...)
			if len(parts) == 0 {
				continue
			}
			messages = appendMessage(messages, role, parts, item.Raw, path, &losses)
		case protocol.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == protocol.ToolServer {
				losses = append(losses, format.Degraded(path, "provider-executed tool calls cannot be replayed as client tools", protocol.Messages))
				continue
			}
			entry, nested := encodeToolUse(item, context, path)
			losses = append(losses, nested...)
			messages = appendMessage(messages, protocol.RoleAssistant, []any{entry}, nil, path, &losses)
		case protocol.ItemToolResult:
			entry, nested, err := encodeToolResult(item, context, path)
			if err != nil {
				return nil, nil, losses, err
			}
			losses = append(losses, nested...)
			messages = appendMessage(messages, protocol.RoleUser, []any{entry}, nil, path, &losses)
		case protocol.ItemServerToolCall, protocol.ItemServerToolResult, protocol.ItemNative:
			if native := format.RawObject(item.Raw, protocol.Messages); native != nil {
				role := protocol.RoleUser
				if item.Role == protocol.RoleAssistant || item.Kind.IsTool() {
					role = protocol.RoleAssistant
				}
				messages = appendMessage(messages, role, []any{native}, nil, path, &losses)
				continue
			}
			losses = append(losses, format.Degraded(path, "provider-private item cannot be replayed by Messages", protocol.Messages))
		}
	}
	if len(system) > 0 {
		root["system"] = system
	}
	root["messages"] = messages
	tools := format.RestrictTools(request.Tools, request.ToolChoice)
	if len(tools) > 0 {
		encoded, nested, err := format.EncodeTools(tools, protocol.Messages)
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if len(encoded) > 0 {
			root["tools"] = encoded
		}
	}
	nested, err := format.EncodeParams(request, protocol.Messages, root)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	losses = append(losses, format.RawFields(root, request.Raw, protocol.Messages, "raw")...)
	return protocol.Marshal(root), nil, losses, nil
}

func encodeContent(item *protocol.Item, context format.PartContext, path string) ([]any, []protocol.Loss, error) {
	var losses []protocol.Loss
	parts := make([]any, 0, len(item.Parts))
	context.Role = item.Role
	if item.Kind == protocol.ItemReasoning {
		context.Role = protocol.RoleAssistant
	}
	cache := item.CacheControl
	appendPart := func(encoded any) {
		if encoded == nil {
			return
		}
		if cache != nil {
			if object, ok := encoded.(map[string]any); ok {
				object["cache_control"] = format.EncodeCacheControl(cache)
				cache = nil
			}
		}
		parts = append(parts, encoded)
	}
	if item.Kind == protocol.ItemReasoning && len(item.Parts) == 0 && item.Reasoning != nil && item.Reasoning.Signature != nil {
		entry, nested, err := format.EncodePart(protocol.Part{Kind: protocol.PartReasoningText}, item, context, "reasoning")
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		appendPart(entry)
	}
	for index := range item.Parts {
		part := &item.Parts[index]
		if part.Kind == protocol.PartToolArguments {
			continue
		}
		if context.Output && part.Kind == protocol.PartMedia {
			losses = append(losses, format.Degraded("parts", "Messages cannot return generated media", protocol.Messages))
			continue
		}
		encoded, nested, err := format.EncodePart(*part, item, context, "parts")
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		appendPart(encoded)
	}
	if cache != nil {
		losses = append(losses, format.Degraded("cache_control", "the item has no content block for its cache marker", protocol.Messages))
	}
	return parts, losses, nil
}

func encodeToolUse(item *protocol.Item, context format.PartContext, path string) (map[string]any, []protocol.Loss) {
	call := item.ToolCall
	input, err := toolInput(call.Arguments)
	if call.Kind == protocol.ToolCustom {
		input, err = map[string]any{"input": call.Arguments}, nil
	}
	entry := map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": input}
	var losses []protocol.Loss
	if err != nil {
		losses = append(losses, format.Degraded(path+".input", "tool arguments were not valid JSON and were sent as an empty object", protocol.Messages))
	}
	if item.CacheControl != nil {
		entry["cache_control"] = format.EncodeCacheControl(item.CacheControl)
	}
	if call.Signature != nil {
		losses = append(losses, format.Degraded(path+".signature", "Messages has no tool call signature", protocol.Messages))
	}
	losses = append(losses, format.RawFields(entry, item.Raw, protocol.Messages, path+".raw")...)
	return entry, losses
}

func toolInput(arguments string) (any, error) {
	arguments = format.EmptyObjectIfBlank(arguments)
	decoded, err := protocol.DecodeJSON([]byte(arguments))
	if err != nil {
		return map[string]any{}, err
	}
	if object, ok := decoded.(map[string]any); ok {
		return object, nil
	}
	return map[string]any{"input": decoded}, nil
}

func encodeToolResult(item *protocol.Item, context format.PartContext, path string) (map[string]any, []protocol.Loss, error) {
	entry := map[string]any{"type": "tool_result", "tool_use_id": item.CallID}
	var losses []protocol.Loss
	if len(item.Parts) == 1 && item.Parts[0].Kind == protocol.PartText {
		entry["content"] = item.Parts[0].Text
	} else if len(item.Parts) > 0 {
		parts := make([]any, 0, len(item.Parts))
		for index := range item.Parts {
			part := &item.Parts[index]
			encoded, nested, err := format.EncodePart(*part, item, format.PartContext{Target: protocol.Messages, Peer: context.Peer, Role: protocol.RoleTool}, "parts")
			if err != nil {
				return nil, losses, err
			}
			losses = append(losses, nested...)
			if encoded != nil {
				parts = append(parts, encoded)
			}
		}
		entry["content"] = parts
	}
	if item.IsError {
		entry["is_error"] = true
	}
	if item.CacheControl != nil {
		entry["cache_control"] = format.EncodeCacheControl(item.CacheControl)
	}
	losses = append(losses, format.RawFields(entry, item.Raw, protocol.Messages, path+".raw")...)
	return entry, losses, nil
}

func textBlocks(parts []any) (kept []any, dropped []any) {
	for _, part := range parts {
		if object, ok := part.(map[string]any); ok && object["type"] == "text" {
			kept = append(kept, part)
		} else {
			dropped = append(dropped, part)
		}
	}
	return kept, dropped
}

// appendMessage merges consecutive messages of one role, because Messages alternates strictly.
func appendMessage(messages []any, role protocol.Role, parts []any, raw *protocol.Raw, path string, losses *[]protocol.Loss) []any {
	if count := len(messages); count > 0 {
		previous := messages[count-1].(map[string]any)
		if previous["role"] == string(role) {
			previous["content"] = append(previous["content"].([]any), parts...)
			*losses = append(*losses, format.RawFields(previous, raw, protocol.Messages, path+".raw")...)
			return messages
		}
	}
	entry := map[string]any{"role": string(role), "content": parts}
	*losses = append(*losses, format.RawFields(entry, raw, protocol.Messages, path+".raw")...)
	return append(messages, entry)
}
