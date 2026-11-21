package messages

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func (Codec) DecodeRequest(body []byte, header http.Header, peer codec.Peer) (*model.Request, []model.Loss, error) {
	root, err := model.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("request must be a JSON object")
	}
	request := &model.Request{Source: model.Messages, Model: model.String(root["model"])}
	if request.Model == "" {
		return nil, nil, errors.New("model is required")
	}
	if err := codec.DecodeScalars(request, root, "max_tokens"); err != nil {
		return nil, nil, err
	}
	if topK := model.OptionalInteger(root["top_k"]); topK != nil {
		if *topK < 0 {
			return nil, nil, errors.New("top_k must not be negative")
		}
		request.TopK = topK
	}
	if root["system"] != nil {
		items, err := codec.DecodeContentItems(model.Messages, model.RoleSystem, root["system"], peer)
		if err != nil {
			return nil, nil, err
		}
		request.Items = append(request.Items, items...)
	}
	for _, value := range model.Array(root["messages"]) {
		entry := model.Object(value)
		role := model.Role(model.String(entry["role"]))
		switch role {
		case model.RoleUser, model.RoleAssistant, model.RoleSystem, model.RoleDeveloper:
		default:
			return nil, nil, errors.New("unsupported message role")
		}
		items, err := codec.DecodeContentItems(model.Messages, role, entry["content"], peer)
		if err != nil {
			return nil, nil, err
		}
		raw := codec.CaptureRaw(model.Messages, entry, messageKeys...)
		for index := range items {
			if index == 0 {
				items[index].Raw = raw
			}
			items[index].CacheControl = codec.DecodeCacheControl(entry["cache_control"])
		}
		request.Items = append(request.Items, items...)
	}
	if len(request.Items) == 0 {
		return nil, nil, errors.New("at least one message is required")
	}
	if request.Stop, err = codec.ParseStop(root["stop_sequences"]); err != nil {
		return nil, nil, err
	}
	if request.Tools, err = codec.DecodeTools(model.Messages, model.Array(root["tools"])); err != nil {
		return nil, nil, err
	}
	if err := decodeToolChoice(request, root["tool_choice"]); err != nil {
		return nil, nil, err
	}
	if err := decodeOutput(request, root); err != nil {
		return nil, nil, err
	}
	request.User = model.String(model.Object(root["metadata"])["user_id"])
	request.ServiceTier = model.String(root["service_tier"])
	request.Raw = codec.CaptureRaw(model.Messages, root, requestKeys...)
	codec.AssignCallIDs(request.Items, codec.NewCallIDs(peer))
	codec.PairAnonymousResults(request.Items)
	for index := range request.Items {
		request.Items[index].Ordinal = index
	}
	return request, nil, nil
}

func decodeToolChoice(request *model.Request, value any) error {
	choice, err := codec.ParseToolChoice(model.Messages, value)
	if err != nil {
		return err
	}
	request.ToolChoice = choice
	if disable, ok := model.Object(value)["disable_parallel_tool_use"].(bool); ok {
		parallel := !disable
		request.ParallelToolCalls = &parallel
	}
	return nil
}

func decodeOutput(request *model.Request, root map[string]any) error {
	output := model.Object(root["output_config"])
	if output != nil {
		responseFormat, err := codec.ParseResponseFormat(output["format"])
		if err != nil {
			return err
		}
		request.ResponseFormat = responseFormat
	}
	thinking := model.Object(root["thinking"])
	effort := model.String(output["effort"])
	if thinking == nil && effort == "" {
		return nil
	}
	config := &model.ReasoningConfig{Effort: effort}
	switch model.String(thinking["type"]) {
	case "disabled":
		config.Effort = "none"
	case "adaptive", "":
		if config.Effort == "" {
			config.Effort = "auto"
		}
	case "enabled":
		if budget, ok := model.Integer(thinking["budget_tokens"]); ok {
			config.BudgetTokens = &budget
		} else if config.Effort == "" {
			config.Effort = "auto"
		}
	}
	config.Display = model.String(thinking["display"])
	if enabled, ok := thinking["between_tools"].(bool); ok {
		config.BetweenTools = enabled
	}
	request.Reasoning = config
	return nil
}

func (coder Codec) EncodeRequest(request *model.Request, peer codec.Peer) ([]byte, http.Header, []model.Loss, error) {
	root := map[string]any{"model": peer.Model, "stream": request.Stream}
	var losses []model.Loss
	context := codec.PartContext{Target: model.Messages, Peer: peer}
	var system []any
	if request.Instructions != "" {
		system = append(system, map[string]any{"type": "text", "text": request.Instructions})
	}
	messages := make([]any, 0, len(request.Items))
	for index := range request.Items {
		item := &request.Items[index]
		path := "items[" + strconv.Itoa(index) + "]"
		switch item.Kind {
		case model.ItemMessage, model.ItemReasoning:
			role := item.Role
			if item.Kind == model.ItemReasoning {
				role = model.RoleAssistant
			}
			if role == "" {
				role = model.RoleUser
			}
			if role == model.RoleSystem || role == model.RoleDeveloper {
				if role == model.RoleDeveloper {
					losses = append(losses, model.Loss{Path: path + ".role", Code: model.LossRoleMapped, Class: model.LossDegraded, Detail: "developer role represented as system prompt", To: model.Messages})
				}
				parts, nested, err := encodeContent(item, context, path)
				if err != nil {
					return nil, nil, losses, err
				}
				losses = append(losses, nested...)
				kept, dropped := textBlocks(parts)
				system = append(system, kept...)
				for range dropped {
					losses = append(losses, model.Loss{Path: path, Code: model.LossFieldDropped, Class: model.LossDegraded, Detail: "Messages system prompts only carry text", To: model.Messages})
				}
				continue
			}
			if role != model.RoleAssistant {
				role = model.RoleUser
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
		case model.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == model.ToolServer {
				losses = append(losses, codec.Degraded(path, "provider-executed tool calls cannot be replayed as client tools", model.Messages))
				continue
			}
			entry, nested := encodeToolUse(item, context, path)
			losses = append(losses, nested...)
			messages = appendMessage(messages, model.RoleAssistant, []any{entry}, nil, path, &losses)
		case model.ItemToolResult:
			entry, nested, err := encodeToolResult(item, context, path)
			if err != nil {
				return nil, nil, losses, err
			}
			losses = append(losses, nested...)
			messages = appendMessage(messages, model.RoleUser, []any{entry}, nil, path, &losses)
		case model.ItemServerToolCall, model.ItemServerToolResult, model.ItemNative:
			if native := codec.RawObject(item.Raw, model.Messages); native != nil {
				role := model.RoleUser
				if item.Role == model.RoleAssistant || item.Kind.IsTool() {
					role = model.RoleAssistant
				}
				messages = appendMessage(messages, role, []any{native}, nil, path, &losses)
				continue
			}
			losses = append(losses, codec.Degraded(path, "provider-private item cannot be replayed by Messages", model.Messages))
		}
	}
	if len(system) > 0 {
		root["system"] = system
	}
	root["messages"] = messages
	tools := codec.RestrictTools(request.Tools, request.ToolChoice)
	if len(tools) > 0 {
		encoded, nested, err := codec.EncodeTools(tools, model.Messages, peer.Profile.Capability.Tools)
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if len(encoded) > 0 {
			root["tools"] = encoded
		}
	}
	nested, err := codec.EncodeParams(request, model.Messages, root, peer.Profile)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	losses = append(losses, codec.RawFields(root, request.Raw, model.Messages, "raw")...)
	return model.Marshal(root), nil, losses, nil
}

func encodeContent(item *model.Item, context codec.PartContext, path string) ([]any, []model.Loss, error) {
	var losses []model.Loss
	parts := make([]any, 0, len(item.Parts))
	context.Role = item.Role
	if item.Kind == model.ItemReasoning {
		context.Role = model.RoleAssistant
	}
	cache := item.CacheControl
	appendPart := func(encoded any) {
		if encoded == nil {
			return
		}
		if cache != nil {
			if object, ok := encoded.(map[string]any); ok {
				object["cache_control"] = codec.EncodeCacheControl(cache)
				cache = nil
			}
		}
		parts = append(parts, encoded)
	}
	if item.Kind == model.ItemReasoning && len(item.Parts) == 0 && item.Reasoning != nil && item.Reasoning.Signature != nil {
		entry, nested, err := codec.EncodePart(model.Part{Kind: model.PartReasoningText}, item, context, "reasoning")
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		appendPart(entry)
	}
	for index := range item.Parts {
		part := &item.Parts[index]
		if part.Kind == model.PartToolArguments {
			continue
		}
		if context.Output && part.Kind == model.PartMedia {
			losses = append(losses, codec.Degraded("parts", "Messages cannot return generated media", model.Messages))
			continue
		}
		encoded, nested, err := codec.EncodePart(*part, item, context, "parts")
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		appendPart(encoded)
	}
	if cache != nil {
		losses = append(losses, codec.Degraded("cache_control", "the item has no content block for its cache marker", model.Messages))
	}
	return parts, losses, nil
}

func encodeToolUse(item *model.Item, context codec.PartContext, path string) (map[string]any, []model.Loss) {
	call := item.ToolCall
	input, err := toolInput(call.Arguments)
	if call.Kind == model.ToolCustom {
		input, err = map[string]any{"input": call.Arguments}, nil
	}
	entry := map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": input}
	var losses []model.Loss
	if err != nil {
		losses = append(losses, codec.Degraded(path+".input", "tool arguments were not valid JSON and were sent as an empty object", model.Messages))
	}
	if item.CacheControl != nil {
		entry["cache_control"] = codec.EncodeCacheControl(item.CacheControl)
	}
	if call.Signature != nil {
		losses = append(losses, codec.Degraded(path+".signature", "Messages has no tool call signature", model.Messages))
	}
	losses = append(losses, codec.RawFields(entry, item.Raw, model.Messages, path+".raw")...)
	return entry, losses
}

func toolInput(arguments string) (any, error) {
	arguments = codec.EmptyObjectIfBlank(arguments)
	decoded, err := model.DecodeJSON([]byte(arguments))
	if err != nil {
		return map[string]any{}, err
	}
	if object, ok := decoded.(map[string]any); ok {
		return object, nil
	}
	return map[string]any{"input": decoded}, nil
}

func encodeToolResult(item *model.Item, context codec.PartContext, path string) (map[string]any, []model.Loss, error) {
	entry := map[string]any{"type": "tool_result", "tool_use_id": item.CallID}
	var losses []model.Loss
	if len(item.Parts) == 1 && item.Parts[0].Kind == model.PartText {
		entry["content"] = item.Parts[0].Text
	} else if len(item.Parts) > 0 {
		parts := make([]any, 0, len(item.Parts))
		for index := range item.Parts {
			part := &item.Parts[index]
			encoded, nested, err := codec.EncodePart(*part, item, codec.PartContext{Target: model.Messages, Peer: context.Peer, Role: model.RoleTool}, "parts")
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
		entry["cache_control"] = codec.EncodeCacheControl(item.CacheControl)
	}
	losses = append(losses, codec.RawFields(entry, item.Raw, model.Messages, path+".raw")...)
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
func appendMessage(messages []any, role model.Role, parts []any, raw *model.Raw, path string, losses *[]model.Loss) []any {
	if count := len(messages); count > 0 {
		previous := messages[count-1].(map[string]any)
		if previous["role"] == string(role) {
			previous["content"] = append(previous["content"].([]any), parts...)
			*losses = append(*losses, codec.RawFields(previous, raw, model.Messages, path+".raw")...)
			return messages
		}
	}
	entry := map[string]any{"role": string(role), "content": parts}
	*losses = append(*losses, codec.RawFields(entry, raw, model.Messages, path+".raw")...)
	return append(messages, entry)
}
