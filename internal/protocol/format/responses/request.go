package responses

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

func (Codec) DecodeRequest(body []byte, header http.Header, peer format.Peer) (*protocol.Request, []protocol.Loss, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("request must be a JSON object")
	}
	request := &protocol.Request{
		Source: protocol.Responses, Model: protocol.String(root["model"]),
		PreviousResponseID: protocol.String(root["previous_response_id"]), Instructions: protocol.String(root["instructions"]),
	}
	if request.Model == "" {
		return nil, nil, errors.New("model is required")
	}
	if err := format.DecodeScalars(request, root, "max_output_tokens"); err != nil {
		return nil, nil, err
	}
	format.DecodeOpenAISampling(request, root)
	request.Store, err = booleanField(root, "store")
	if err != nil {
		return nil, nil, err
	}
	request.Background, err = booleanField(root, "background")
	if err != nil {
		return nil, nil, err
	}
	request.Conversation = protocol.String(root["conversation"])
	for _, value := range protocol.Array(root["include"]) {
		if text := protocol.String(value); text != "" {
			request.Include = append(request.Include, text)
		}
	}
	request.Truncation = protocol.String(root["truncation"])
	request.MaxToolCalls = protocol.OptionalInteger(root["max_tool_calls"])
	request.SafetyIdentifier = protocol.String(root["safety_identifier"])
	request.PromptCacheRetention = protocol.String(root["prompt_cache_retention"])
	request.Logprobs, err = booleanField(root, "logprobs")
	if err != nil {
		return nil, nil, err
	}
	request.TopLogprobs = protocol.OptionalInteger(root["top_logprobs"])
	if bias := protocol.Object(root["logit_bias"]); bias != nil {
		request.LogitBias = map[string]float64{}
		for key, value := range bias {
			if number, ok := protocol.Float(value); ok {
				request.LogitBias[key] = number
			}
		}
	}
	for _, value := range protocol.Array(root["modalities"]) {
		if text := protocol.String(value); text != "" {
			request.Modalities = append(request.Modalities, text)
		}
	}
	if text, ok := root["input"].(string); ok {
		if text != "" {
			request.Items = append(request.Items, protocol.MessageItem(protocol.RoleUser, text))
		}
	} else {
		items, err := format.DecodeContentItems(protocol.Responses, protocol.RoleUser, root["input"], peer)
		if err != nil {
			return nil, nil, err
		}
		request.Items = items
	}
	format.AssignCallIDs(request.Items, format.NewCallIDs(peer))
	if len(request.Items) == 0 && request.Instructions == "" {
		return nil, nil, errors.New("input is required")
	}
	if request.Tools, err = format.DecodeTools(protocol.Responses, protocol.Array(root["tools"])); err != nil {
		return nil, nil, err
	}
	choice, choiceErr := format.ParseToolChoice(protocol.Responses, root["tool_choice"])
	if choiceErr != nil && !namesProviderTool(root["tool_choice"]) {
		return nil, nil, choiceErr
	}
	request.ToolChoice = choice
	if reasoning := protocol.Object(root["reasoning"]); reasoning != nil {
		request.Reasoning = &protocol.ReasoningConfig{Effort: protocol.String(reasoning["effort"]), Summary: protocol.String(reasoning["summary"])}
	}
	if text := protocol.Object(root["text"]); text != nil {
		if request.ResponseFormat, err = format.ParseResponseFormat(text["format"]); err != nil {
			return nil, nil, err
		}
		request.Verbosity = protocol.String(text["verbosity"])
	}
	request.Raw = format.CaptureRaw(protocol.Responses, root, requestKeys...)
	if choiceErr != nil {
		if request.Raw == nil {
			request.Raw = &protocol.Raw{Format: protocol.Responses}
		}
		if request.Raw.Fields == nil {
			request.Raw.Fields = map[string]json.RawMessage{}
		}
		request.Raw.Fields["tool_choice"] = protocol.Marshal(root["tool_choice"])
	}
	for index := range request.Items {
		request.Items[index].Ordinal = index
	}
	return request, nil, nil
}

func namesProviderTool(value any) bool {
	switch protocol.String(protocol.Object(value)["type"]) {
	case "", "function", "custom", "allowed_tools":
		return false
	}
	return true
}

func (Codec) EncodeRequest(request *protocol.Request, peer format.Peer) ([]byte, http.Header, []protocol.Loss, error) {
	include := []any{"reasoning.encrypted_content"}
	for _, item := range request.Include {
		if item != "reasoning.encrypted_content" {
			include = append(include, item)
		}
	}
	root := map[string]any{"model": peer.Model, "stream": request.Stream, "store": false, "include": include}
	if request.Instructions != "" {
		root["instructions"] = request.Instructions
	}
	if request.Truncation != "" {
		root["truncation"] = request.Truncation
	}
	if request.MaxToolCalls != nil {
		root["max_tool_calls"] = *request.MaxToolCalls
	}
	if request.SafetyIdentifier != "" {
		root["safety_identifier"] = request.SafetyIdentifier
	}
	if request.PromptCacheRetention != "" {
		root["prompt_cache_retention"] = request.PromptCacheRetention
	}
	var losses []protocol.Loss
	toolKinds := map[string]protocol.ToolKind{}
	for index := range request.Items {
		item := &request.Items[index]
		if item.ToolCall != nil {
			toolKinds[item.ToolCall.ID] = item.ToolCall.Kind
		}
	}
	input := make([]any, 0, len(request.Items))
	for index := range request.Items {
		item := &request.Items[index]
		encoded, nested, err := encodeItem(item, index, format.PartContext{Target: protocol.Responses, Peer: peer, Role: item.Role, ToolKinds: toolKinds})
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if encoded != nil {
			input = append(input, encoded)
		}
	}
	root["input"] = input
	if len(request.Tools) > 0 {
		tools, nested, err := format.EncodeTools(request.Tools, protocol.Responses)
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if len(tools) > 0 {
			root["tools"] = tools
		}
	}
	nested, err := format.EncodeParams(request, protocol.Responses, root)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	losses = append(losses, format.RawFields(root, request.Raw, protocol.Responses, "raw")...)
	return protocol.Marshal(root), nil, losses, nil
}

// encodeItem writes one canonical item in Responses' flat input shape.
func encodeItem(item *protocol.Item, ordinal int, context format.PartContext) (map[string]any, []protocol.Loss, error) {
	var losses []protocol.Loss
	switch item.Kind {
	case protocol.ItemMessage:
		parts, losses, err := encodeMessageParts(item, true, context)
		if err != nil {
			return nil, losses, err
		}
		if len(parts) == 0 {
			return nil, losses, nil
		}
		role := string(item.Role)
		if role == "" {
			role = "user"
		}
		entry := map[string]any{"type": "message", "role": role, "content": parts}
		if item.ID != "" {
			entry["id"] = item.ID
		}
		if item.Status != "" {
			entry["status"] = item.Status
		}
		losses = append(losses, format.RawFields(entry, item.Raw, protocol.Responses, "raw")...)
		return entry, losses, nil
	case protocol.ItemReasoning:
		return encodeReasoningItem(item, context)
	case protocol.ItemToolCall:
		if item.ToolCall == nil || item.ToolCall.Kind == protocol.ToolServer {
			return nil, []protocol.Loss{format.Degraded("item", "provider-executed tool call cannot be replayed as a client tool call", protocol.Responses)}, nil
		}
		entry := map[string]any{"type": "function_call", "call_id": item.ToolCall.ID, "name": item.ToolCall.Name, "arguments": format.EmptyObjectIfBlank(item.ToolCall.Arguments)}
		if item.ToolCall.Kind == protocol.ToolCustom {
			entry = map[string]any{"type": "custom_tool_call", "call_id": item.ToolCall.ID, "name": item.ToolCall.Name, "input": item.ToolCall.Arguments}
		}
		if applyItemID(entry, item, protocol.ItemToolCall, ordinal) {
			losses = append(losses, protocol.Loss{Path: "id", Code: protocol.LossItemIDMapped, Class: protocol.LossDegraded, Detail: "the source item id was replaced by a Responses id", To: protocol.Responses})
		}
		losses = append(losses, format.RawFields(entry, item.Raw, protocol.Responses, "raw")...)
		if item.ToolCall.Signature != nil {
			losses = append(losses, format.Degraded("signature", "Responses has no tool call signature", protocol.Responses))
		}
		return entry, losses, nil
	case protocol.ItemToolResult:
		if item.IsError {
			losses = append(losses, format.Degraded("item", "Responses cannot mark a function output as an error", protocol.Responses))
		}
		output, outputLosses, err := encodeToolOutput(item, context)
		if err != nil {
			return nil, outputLosses, err
		}
		losses = append(losses, outputLosses...)
		outputType := "function_call_output"
		if context.ToolKinds[item.CallID] == protocol.ToolCustom {
			outputType = "custom_tool_call_output"
			var text string
			for _, part := range item.Parts {
				if part.Kind != protocol.PartText {
					losses = append(losses, format.Degraded("item", "custom tool output carries text only", protocol.Responses))
					continue
				}
				text += part.Text
			}
			output = text
		}
		entry := map[string]any{"type": outputType, "call_id": item.CallID, "output": output}
		if item.Name != "" {
			entry["name"] = item.Name
		}
		if applyItemID(entry, item, protocol.ItemToolResult, ordinal) {
			losses = append(losses, protocol.Loss{Path: "id", Code: protocol.LossItemIDMapped, Class: protocol.LossDegraded, Detail: "the source item id was replaced by a Responses id", To: protocol.Responses})
		}
		losses = append(losses, format.RawFields(entry, item.Raw, protocol.Responses, "raw")...)
		return entry, losses, nil
	case protocol.ItemServerToolCall, protocol.ItemServerToolResult, protocol.ItemNative:
		if native := format.RawObject(item.Raw, protocol.Responses); native != nil {
			return native, nil, nil
		}
		return nil, []protocol.Loss{format.Degraded("item", "provider-private item cannot be replayed across protocols", protocol.Responses)}, nil
	}
	return nil, nil, errors.New("unsupported item kind " + string(item.Kind))
}

func applyItemID(entry map[string]any, item *protocol.Item, kind protocol.ItemKind, ordinal int) bool {
	material := item.ID
	if material == "" && item.ToolCall != nil {
		material = item.ToolCall.ID
	}
	id, changed := protocol.ItemID(protocol.Responses, kind, item.ID, material, ordinal)
	if id != "" {
		entry["id"] = id
	}
	return changed
}

func encodeMessageParts(item *protocol.Item, input bool, context format.PartContext) ([]any, []protocol.Loss, error) {
	var losses []protocol.Loss
	parts := make([]any, 0, len(item.Parts))
	context.Output = !input
	for index := range item.Parts {
		part := &item.Parts[index]
		if !input && part.Kind == protocol.PartMedia {
			losses = append(losses, format.Degraded("parts", "Responses cannot return generated media", protocol.Responses))
			continue
		}
		encoded, nested, err := format.EncodePart(*part, item, context, "parts")
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		if encoded != nil {
			parts = append(parts, encoded)
		}
	}
	return parts, losses, nil
}

func encodeReasoningItem(item *protocol.Item, context format.PartContext) (map[string]any, []protocol.Loss, error) {
	entry := map[string]any{"type": "reasoning"}
	var summary, content []any
	var losses []protocol.Loss
	for index := range item.Parts {
		part := &item.Parts[index]
		switch part.Kind {
		case protocol.PartReasoningSummary:
			summary = append(summary, map[string]any{"type": "summary_text", "text": part.Text})
		case protocol.PartReasoningText:
			content = append(content, map[string]any{"type": "reasoning_text", "text": part.Text})
		case protocol.PartRedactedReasoning:
			losses = append(losses, format.Degraded("parts", "redacted reasoning cannot be replayed by Responses", protocol.Responses))
		}
	}
	if summary != nil {
		entry["summary"] = summary
	} else {
		entry["summary"] = []any{}
	}
	if content != nil {
		entry["content"] = content
	}
	if item.Reasoning != nil && item.Reasoning.Signature != nil {
		if blob := context.Peer.WireSignature(item.Reasoning.Signature, item.Text()); blob != "" {
			entry["encrypted_content"] = blob
		}
	}
	if applyItemID(entry, item, protocol.ItemReasoning, item.Ordinal) {
		losses = append(losses, protocol.Loss{Path: "id", Code: protocol.LossItemIDMapped, Class: protocol.LossDegraded, Detail: "the source item id was replaced by a Responses id", To: protocol.Responses})
	}
	losses = append(losses, format.RawFields(entry, item.Raw, protocol.Responses, "raw")...)
	return entry, losses, nil
}

func encodeToolOutput(item *protocol.Item, context format.PartContext) (any, []protocol.Loss, error) {
	var losses []protocol.Loss
	if len(item.Parts) == 1 && item.Parts[0].Kind == protocol.PartText {
		return item.Parts[0].Text, nil, nil
	}
	if len(item.Parts) == 0 {
		return "", nil, nil
	}
	parts := make([]any, 0, len(item.Parts))
	for index := range item.Parts {
		part := &item.Parts[index]
		encoded, nested, err := format.EncodePart(*part, item, format.PartContext{Target: protocol.Responses, Peer: context.Peer, Role: protocol.RoleTool}, "parts")
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		if encoded != nil {
			parts = append(parts, encoded)
		}
	}
	return parts, losses, nil
}

func booleanField(root map[string]any, key string) (*bool, error) {
	value, exists := root[key]
	if !exists || value == nil {
		return nil, nil
	}
	flag, ok := value.(bool)
	if !ok {
		return nil, errors.New(key + " must be boolean")
	}
	return &flag, nil
}
