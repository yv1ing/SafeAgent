package responses

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func (Codec) DecodeRequest(body []byte, header http.Header, peer codec.Peer) (*model.Request, []model.Loss, error) {
	root, err := model.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("request must be a JSON object")
	}
	request := &model.Request{
		Source: model.Responses, Model: model.String(root["model"]),
		PreviousResponseID: model.String(root["previous_response_id"]), Instructions: model.String(root["instructions"]),
	}
	if request.Model == "" {
		return nil, nil, errors.New("model is required")
	}
	if err := codec.DecodeScalars(request, root, "max_output_tokens"); err != nil {
		return nil, nil, err
	}
	codec.DecodeOpenAISampling(request, root)
	request.Store, err = codec.BooleanField(root, "store")
	if err != nil {
		return nil, nil, err
	}
	request.Background, err = codec.BooleanField(root, "background")
	if err != nil {
		return nil, nil, err
	}
	request.Conversation = model.String(root["conversation"])
	for _, value := range model.Array(root["include"]) {
		if text := model.String(value); text != "" {
			request.Include = append(request.Include, text)
		}
	}
	request.Truncation = model.String(root["truncation"])
	request.MaxToolCalls = model.OptionalInteger(root["max_tool_calls"])
	request.SafetyIdentifier = model.String(root["safety_identifier"])
	request.PromptCacheRetention = model.String(root["prompt_cache_retention"])
	request.Logprobs, err = codec.BooleanField(root, "logprobs")
	if err != nil {
		return nil, nil, err
	}
	request.TopLogprobs = model.OptionalInteger(root["top_logprobs"])
	if bias := model.Object(root["logit_bias"]); bias != nil {
		request.LogitBias = map[string]float64{}
		for key, value := range bias {
			if number, ok := model.Float(value); ok {
				request.LogitBias[key] = number
			}
		}
	}
	for _, value := range model.Array(root["modalities"]) {
		if text := model.String(value); text != "" {
			request.Modalities = append(request.Modalities, text)
		}
	}
	if text, ok := root["input"].(string); ok {
		if text != "" {
			request.Items = append(request.Items, model.MessageItem(model.RoleUser, text))
		}
	} else {
		items, err := codec.DecodeContentItems(model.Responses, model.RoleUser, root["input"], peer)
		if err != nil {
			return nil, nil, err
		}
		request.Items = items
	}
	codec.AssignCallIDs(request.Items, codec.NewCallIDs(peer))
	if len(request.Items) == 0 && request.Instructions == "" {
		return nil, nil, errors.New("input is required")
	}
	if request.Tools, err = codec.DecodeTools(model.Responses, model.Array(root["tools"])); err != nil {
		return nil, nil, err
	}
	choice, choiceErr := codec.ParseToolChoice(model.Responses, root["tool_choice"])
	if choiceErr != nil && !namesProviderTool(root["tool_choice"]) {
		return nil, nil, choiceErr
	}
	request.ToolChoice = choice
	if reasoning := model.Object(root["reasoning"]); reasoning != nil {
		request.Reasoning = &model.ReasoningConfig{Effort: model.String(reasoning["effort"]), Summary: model.String(reasoning["summary"])}
	}
	if text := model.Object(root["text"]); text != nil {
		if request.ResponseFormat, err = codec.ParseResponseFormat(text["format"]); err != nil {
			return nil, nil, err
		}
		request.Verbosity = model.String(text["verbosity"])
	}
	request.Raw = codec.CaptureRaw(model.Responses, root, requestKeys...)
	if choiceErr != nil {
		if request.Raw == nil {
			request.Raw = &model.Raw{Format: model.Responses}
		}
		if request.Raw.Fields == nil {
			request.Raw.Fields = map[string]json.RawMessage{}
		}
		request.Raw.Fields["tool_choice"] = model.Marshal(root["tool_choice"])
	}
	for index := range request.Items {
		request.Items[index].Ordinal = index
	}
	return request, nil, nil
}

func namesProviderTool(value any) bool {
	switch model.String(model.Object(value)["type"]) {
	case "", "function", "custom", "allowed_tools":
		return false
	}
	return true
}

func (Codec) EncodeRequest(request *model.Request, peer codec.Peer) ([]byte, http.Header, []model.Loss, error) {
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
	var losses []model.Loss
	toolKinds := map[string]model.ToolKind{}
	for index := range request.Items {
		item := &request.Items[index]
		if item.ToolCall != nil {
			toolKinds[item.ToolCall.ID] = item.ToolCall.Kind
		}
	}
	input := make([]any, 0, len(request.Items))
	for index := range request.Items {
		item := &request.Items[index]
		encoded, nested, err := encodeItem(item, index, codec.PartContext{Target: model.Responses, Peer: peer, Role: item.Role, ToolKinds: toolKinds})
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
		tools, nested, err := codec.EncodeTools(request.Tools, model.Responses, peer.Profile.Capability.Tools)
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if len(tools) > 0 {
			root["tools"] = tools
		}
	}
	nested, err := codec.EncodeParams(request, model.Responses, root, peer.Profile)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	losses = append(losses, codec.RawFields(root, request.Raw, model.Responses, "raw")...)
	return model.Marshal(root), nil, losses, nil
}

// encodeItem writes one canonical item in Responses' flat input shape.
func encodeItem(item *model.Item, ordinal int, context codec.PartContext) (map[string]any, []model.Loss, error) {
	var losses []model.Loss
	switch item.Kind {
	case model.ItemMessage:
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
		losses = append(losses, codec.RawFields(entry, item.Raw, model.Responses, "raw")...)
		return entry, losses, nil
	case model.ItemReasoning:
		return encodeReasoningItem(item, context)
	case model.ItemToolCall:
		if item.ToolCall == nil || item.ToolCall.Kind == model.ToolServer {
			return nil, []model.Loss{codec.Degraded("item", "provider-executed tool call cannot be replayed as a client tool call", model.Responses)}, nil
		}
		entry := map[string]any{"type": "function_call", "call_id": item.ToolCall.ID, "name": item.ToolCall.Name, "arguments": codec.EmptyObjectIfBlank(item.ToolCall.Arguments)}
		if item.ToolCall.Kind == model.ToolCustom {
			entry = map[string]any{"type": "custom_tool_call", "call_id": item.ToolCall.ID, "name": item.ToolCall.Name, "input": item.ToolCall.Arguments}
		}
		if applyItemID(entry, item, model.ItemToolCall, ordinal) {
			losses = append(losses, model.Loss{Path: "id", Code: model.LossItemIDMapped, Class: model.LossDegraded, Detail: "the source item id was replaced by a Responses id", To: model.Responses})
		}
		losses = append(losses, codec.RawFields(entry, item.Raw, model.Responses, "raw")...)
		if item.ToolCall.Signature != nil {
			losses = append(losses, codec.Degraded("signature", "Responses has no tool call signature", model.Responses))
		}
		return entry, losses, nil
	case model.ItemToolResult:
		if item.IsError {
			losses = append(losses, codec.Degraded("item", "Responses cannot mark a function output as an error", model.Responses))
		}
		output, outputLosses, err := encodeToolOutput(item, context)
		if err != nil {
			return nil, outputLosses, err
		}
		losses = append(losses, outputLosses...)
		outputType := "function_call_output"
		if context.ToolKinds[item.CallID] == model.ToolCustom {
			outputType = "custom_tool_call_output"
			var text string
			for _, part := range item.Parts {
				if part.Kind != model.PartText {
					losses = append(losses, codec.Degraded("item", "custom tool output carries text only", model.Responses))
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
		if applyItemID(entry, item, model.ItemToolResult, ordinal) {
			losses = append(losses, model.Loss{Path: "id", Code: model.LossItemIDMapped, Class: model.LossDegraded, Detail: "the source item id was replaced by a Responses id", To: model.Responses})
		}
		losses = append(losses, codec.RawFields(entry, item.Raw, model.Responses, "raw")...)
		return entry, losses, nil
	case model.ItemServerToolCall, model.ItemServerToolResult, model.ItemNative:
		if native := codec.RawObject(item.Raw, model.Responses); native != nil {
			return native, nil, nil
		}
		return nil, []model.Loss{codec.Degraded("item", "provider-private item cannot be replayed across protocols", model.Responses)}, nil
	}
	return nil, nil, errors.New("unsupported item kind " + string(item.Kind))
}

func applyItemID(entry map[string]any, item *model.Item, kind model.ItemKind, ordinal int) bool {
	material := item.ID
	if material == "" && item.ToolCall != nil {
		material = item.ToolCall.ID
	}
	id, changed := model.ItemID(model.Responses, kind, item.ID, material, ordinal)
	if id != "" {
		entry["id"] = id
	}
	return changed
}

func encodeMessageParts(item *model.Item, input bool, context codec.PartContext) ([]any, []model.Loss, error) {
	var losses []model.Loss
	parts := make([]any, 0, len(item.Parts))
	context.Output = !input
	for index := range item.Parts {
		part := &item.Parts[index]
		if !input && part.Kind == model.PartMedia {
			losses = append(losses, codec.Degraded("parts", "Responses cannot return generated media", model.Responses))
			continue
		}
		encoded, nested, err := codec.EncodePart(*part, item, context, "parts")
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

func encodeReasoningItem(item *model.Item, context codec.PartContext) (map[string]any, []model.Loss, error) {
	entry := map[string]any{"type": "reasoning"}
	var summary, content []any
	var losses []model.Loss
	for index := range item.Parts {
		part := &item.Parts[index]
		switch part.Kind {
		case model.PartReasoningSummary:
			summary = append(summary, map[string]any{"type": "summary_text", "text": part.Text})
		case model.PartReasoningText:
			content = append(content, map[string]any{"type": "reasoning_text", "text": part.Text})
		case model.PartRedactedReasoning:
			losses = append(losses, codec.Degraded("parts", "redacted reasoning cannot be replayed by Responses", model.Responses))
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
	if applyItemID(entry, item, model.ItemReasoning, item.Ordinal) {
		losses = append(losses, model.Loss{Path: "id", Code: model.LossItemIDMapped, Class: model.LossDegraded, Detail: "the source item id was replaced by a Responses id", To: model.Responses})
	}
	losses = append(losses, codec.RawFields(entry, item.Raw, model.Responses, "raw")...)
	return entry, losses, nil
}

func encodeToolOutput(item *model.Item, context codec.PartContext) (any, []model.Loss, error) {
	var losses []model.Loss
	if len(item.Parts) == 1 && item.Parts[0].Kind == model.PartText {
		return item.Parts[0].Text, nil, nil
	}
	if len(item.Parts) == 0 {
		return "", nil, nil
	}
	parts := make([]any, 0, len(item.Parts))
	for index := range item.Parts {
		part := &item.Parts[index]
		encoded, nested, err := codec.EncodePart(*part, item, codec.PartContext{Target: model.Responses, Peer: context.Peer, Role: model.RoleTool}, "parts")
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
