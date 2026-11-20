// Package messages implements the Anthropic Messages request and response codecs.
package messages

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type Codec struct{}

func New() Codec { return Codec{} }

func (Codec) Format() protocol.Format { return protocol.Messages }

var requestKeys = []string{"model", "system", "messages", "stream", "max_tokens", "temperature", "top_p", "top_k", "stop_sequences", "tools", "tool_choice", "thinking.type", "thinking.budget_tokens", "thinking.display", "thinking.between_tools", "output_config.effort", "output_config.format", "metadata.user_id"}

func (Codec) DecodeRequest(body []byte, header http.Header, peer format.Peer) (*protocol.Request, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, errors.New("request must be a JSON object")
	}
	request := &protocol.Request{Model: protocol.String(root["model"])}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	if err := format.DecodeScalars(request, root, "max_tokens"); err != nil {
		return nil, err
	}
	request.TopK = protocol.OptionalInteger(root["top_k"])
	if root["system"] != nil {
		blocks, err := format.DecodeContent(protocol.Messages, root["system"])
		if err != nil {
			return nil, err
		}
		request.Messages = append(request.Messages, protocol.Message{Role: protocol.RoleSystem, Blocks: blocks})
	}
	for _, item := range protocol.Array(root["messages"]) {
		entry := protocol.Object(item)
		role := protocol.Role(protocol.String(entry["role"]))
		switch role {
		case protocol.RoleUser, protocol.RoleAssistant, protocol.RoleSystem, protocol.RoleDeveloper:
		default:
			return nil, errors.New("unsupported message role")
		}
		blocks, err := format.DecodeContent(protocol.Messages, entry["content"])
		if err != nil {
			return nil, err
		}
		request.Messages = append(request.Messages, protocol.Message{Role: role, Blocks: blocks, Extras: format.UnmodelledFields(protocol.Messages, entry, "role", "content")})
	}
	if request.Stop, err = format.ParseStop(root["stop_sequences"]); err != nil {
		return nil, err
	}
	if request.Tools, err = format.DecodeTools(protocol.Messages, protocol.Array(root["tools"])); err != nil {
		return nil, err
	}
	if err := decodeToolChoice(request, root["tool_choice"]); err != nil {
		return nil, err
	}
	if err := decodeOutput(request, root); err != nil {
		return nil, err
	}
	request.User = protocol.String(protocol.Object(root["metadata"])["user_id"])
	request.Extras = format.UnmodelledFields(protocol.Messages, root, requestKeys...)
	return request, nil
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
	context := format.NewPartContext(protocol.Messages, peer)
	var system []any
	if request.Instructions != "" {
		system = append(system, map[string]any{"type": "text", "text": request.Instructions})
	}
	messages := make([]any, 0, len(request.Messages))
	for index, message := range request.Messages {
		path := "messages[" + strconv.Itoa(index) + "]"
		role := message.Role
		context.Role = role
		if role == protocol.RoleDeveloper {
			losses = append(losses, protocol.Loss{Path: path + ".role", Code: protocol.LossRoleMapped, Detail: "developer role represented as system prompt", To: protocol.Messages})
		}
		parts, nested, err := format.EncodeBlocks(message.Blocks, context, path+".blocks")
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if role == protocol.RoleSystem || role == protocol.RoleDeveloper {
			kept, dropped := textParts(parts)
			system = append(system, kept...)
			for range dropped {
				losses = append(losses, protocol.Loss{Path: path, Code: protocol.LossFieldDropped, Detail: "Messages system prompts only carry text", To: protocol.Messages})
			}
			continue
		}
		if len(parts) == 0 {
			continue
		}
		if role != protocol.RoleAssistant {
			role = protocol.RoleUser
		}
		messages = appendMessage(messages, role, parts, message.Extras, protocol.Messages, path, &losses)
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
	losses = append(losses, format.ApplyExtras(root, request.Extras, protocol.Messages, "extras")...)
	return protocol.Marshal(root), nil, losses, nil
}

func textParts(parts []any) (kept []any, dropped []any) {
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
func appendMessage(messages []any, role protocol.Role, parts []any, extras protocol.Extras, target protocol.Format, path string, losses *[]protocol.Loss) []any {
	if count := len(messages); count > 0 {
		previous := messages[count-1].(map[string]any)
		if previous["role"] == string(role) {
			previous["content"] = append(previous["content"].([]any), parts...)
			*losses = append(*losses, format.ApplyExtras(previous, extras, target, path+".extras")...)
			return messages
		}
	}
	entry := map[string]any{"role": string(role), "content": parts}
	*losses = append(*losses, format.ApplyExtras(entry, extras, target, path+".extras")...)
	return append(messages, entry)
}
