package gemini

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func (Codec) DecodeRequest(body []byte, header http.Header, peer codec.Peer) (*model.Request, []model.Loss, error) {
	root, err := model.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("request must be a JSON object")
	}
	request := &model.Request{Source: model.Gemini, Model: peer.Model}
	if request.Model == "" {
		request.Model = model.String(root["model"])
	}
	if config := model.Object(root["generationConfig"]); config != nil {
		if err := decodeConfig(request, config); err != nil {
			return nil, nil, err
		}
	}
	for _, value := range model.Array(root["safetySettings"]) {
		entry := model.Object(value)
		if entry == nil {
			continue
		}
		request.SafetySettings = append(request.SafetySettings, model.SafetySetting{Category: model.String(entry["category"]), Threshold: model.String(entry["threshold"])})
	}
	if instruction := model.Object(root["systemInstruction"]); instruction != nil {
		items, err := decodeParts(model.RoleSystem, model.Array(instruction["parts"]), codec.NewCallIDs(peer), nil)
		if err != nil {
			return nil, nil, err
		}
		raw := codec.CaptureRaw(model.Gemini, instruction, "parts")
		if len(items) > 0 {
			items[0].Raw = raw
		}
		request.Items = append(request.Items, items...)
	}
	ids := codec.NewCallIDs(peer)
	pending := map[string][]string{}
	for _, value := range model.Array(root["contents"]) {
		entry := model.Object(value)
		role := model.Role(model.String(entry["role"]))
		switch role {
		case "model":
			role = model.RoleAssistant
		case "", model.RoleUser:
			role = model.RoleUser
		default:
			return nil, nil, errors.New("unsupported message role")
		}
		items, err := decodeParts(role, model.Array(entry["parts"]), ids, pending)
		if err != nil {
			return nil, nil, err
		}
		raw := codec.CaptureRaw(model.Gemini, entry, "role", "parts")
		if len(items) > 0 {
			items[0].Raw = raw
		}
		request.Items = append(request.Items, items...)
	}
	if len(request.Items) == 0 {
		return nil, nil, errors.New("at least one content turn is required")
	}
	if request.Tools, err = codec.DecodeTools(model.Gemini, model.Array(root["tools"])); err != nil {
		return nil, nil, err
	}
	if calling := model.Object(model.Object(root["toolConfig"])["functionCallingConfig"]); calling != nil {
		if request.ToolChoice, err = codec.ParseToolChoice(model.Gemini, calling); err != nil {
			return nil, nil, err
		}
	}
	request.Raw = codec.CaptureRaw(model.Gemini, root, requestKeys...)
	for index := range request.Items {
		request.Items[index].Ordinal = index
	}
	return request, nil, nil
}

func decodeConfig(request *model.Request, config map[string]any) error {
	if count, ok := model.Integer(config["candidateCount"]); ok {
		if count < 1 {
			return errors.New("candidateCount must be a positive integer")
		}
		if count > 1 {
			request.ChoiceCount = int(count)
		}
	}
	if limit := model.OptionalInteger(config["maxOutputTokens"]); limit != nil {
		if *limit < 1 {
			return errors.New("maxOutputTokens must be a positive integer")
		}
		request.MaxOutputTokens = limit
	}
	request.Temperature, request.TopP = codec.OptionalFloat(config, "temperature"), codec.OptionalFloat(config, "topP")
	if topK := model.OptionalInteger(config["topK"]); topK != nil {
		if *topK < 0 {
			return errors.New("topK must not be negative")
		}
		request.TopK = topK
	}
	request.Seed = model.OptionalInteger(config["seed"])
	request.FrequencyPenalty, request.PresencePenalty = codec.OptionalFloat(config, "frequencyPenalty"), codec.OptionalFloat(config, "presencePenalty")
	var err error
	if request.Stop, err = codec.ParseStop(config["stopSequences"]); err != nil {
		return err
	}
	switch model.String(config["responseMimeType"]) {
	case "":
	case "text/plain":
		request.ResponseFormat = &model.ResponseFormat{Kind: "text"}
	case "application/json":
		schema := model.Object(config["responseJsonSchema"])
		if schema == nil {
			schema = model.Object(config["responseSchema"])
		}
		if schema == nil {
			request.ResponseFormat = &model.ResponseFormat{Kind: "json_object"}
			break
		}
		if model.Object(config["responseJsonSchema"]) == nil {
			if schema, err = codec.NormalizeGeminiSchema(schema); err != nil {
				return err
			}
		}
		request.ResponseFormat = &model.ResponseFormat{Kind: "json_schema", Name: "response", Schema: model.Marshal(schema)}
	default:
		return errors.New("unsupported response MIME type")
	}
	if thinking := model.Object(config["thinkingConfig"]); thinking != nil {
		reasoning := &model.ReasoningConfig{Effort: reasoningEffort(model.String(thinking["thinkingLevel"])), BudgetTokens: model.OptionalInteger(thinking["thinkingBudget"])}
		if include, ok := thinking["includeThoughts"].(bool); ok {
			reasoning.IncludeThoughts = &include
		}
		request.Reasoning = reasoning
	}
	return nil
}

func reasoningEffort(value string) string {
	switch strings.ToUpper(value) {
	case "THINKING_LEVEL_UNSPECIFIED", "":
		return ""
	case "LOW", "HIGH", "MEDIUM", "MINIMAL":
		return strings.ToLower(value)
	}
	return strings.ToLower(value)
}

func decodeParts(role model.Role, values []any, ids *codec.CallIDs, pending map[string][]string) ([]model.Item, error) {
	var items []model.Item
	var messageParts []model.Part
	flush := func() {
		if len(messageParts) == 0 {
			return
		}
		items = append(items, model.Item{Kind: model.ItemMessage, Role: role, Parts: messageParts})
		messageParts = nil
	}
	for _, value := range values {
		entry := model.Object(value)
		if entry == nil {
			return nil, errors.New("content parts must be objects")
		}
		if item, ok := serverPart(role, entry); ok {
			flush()
			items = append(items, item)
			continue
		}
		if media, err := codec.DecodeGeminiMedia(entry); err != nil {
			return nil, err
		} else if media != nil {
			messageParts = append(messageParts, model.Part{Kind: model.PartMedia, Media: media, Raw: codec.CaptureRaw(model.Gemini, entry, partKeys...)})
			continue
		}
		if entry["functionCall"] != nil {
			flush()
			call := model.Object(entry["functionCall"])
			name := model.String(call["name"])
			arguments := "{}"
			if call["args"] != nil {
				arguments = string(model.Marshal(call["args"]))
			}
			id := model.String(call["id"])
			if id == "" {
				id = ids.Assign(name, arguments)
			}
			tool := &model.ToolCall{Kind: model.ToolFunction, ID: id, Name: name, Arguments: arguments}
			if signature := model.String(entry["thoughtSignature"]); signature != "" {
				tool.Signature = &model.Signature{Vendor: model.VendorGoogle, Blob: signature}
			}
			if pending != nil {
				pending[name] = append(pending[name], id)
			}
			items = append(items, model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: tool, Raw: codec.CaptureRaw(model.Gemini, entry, partKeys...)})
			continue
		}
		if entry["functionResponse"] != nil {
			flush()
			result := model.Object(entry["functionResponse"])
			name := model.String(result["name"])
			id := model.String(result["id"])
			if id == "" && pending != nil {
				if queue := pending[name]; len(queue) > 0 {
					id, pending[name] = queue[0], queue[1:]
				}
			}
			if id == "" {
				id = ids.Assign(name, "")
			}
			response := model.Object(result["response"])
			text, isError := toolResultText(response)
			if response == nil && result["response"] != nil {
				text = string(model.Marshal(result["response"]))
			}
			parts := model.TextParts(text)
			if rawParts := model.Array(result["parts"]); len(rawParts) > 0 {
				for _, value := range rawParts {
					entry := model.Object(value)
					if media, err := codec.DecodeGeminiMedia(entry); err != nil {
						return nil, err
					} else if media != nil {
						parts = append(parts, model.Part{Kind: model.PartMedia, Media: media})
					}
				}
			}
			items = append(items, model.Item{
				Kind: model.ItemToolResult, Role: model.RoleTool, CallID: id, Name: name, IsError: isError, Parts: parts,
				Raw: codec.CaptureRaw(model.Gemini, entry, partKeys...),
			})
			continue
		}
		text := model.String(entry["text"])
		thought := entry["thought"] == true
		signature := model.String(entry["thoughtSignature"])
		if thought || (signature != "" && text != "") {
			flush()
			item := model.Item{Kind: model.ItemReasoning, Role: role, Parts: []model.Part{{Kind: model.PartReasoningText, Text: text}}, Reasoning: &model.Reasoning{Visibility: "full"}}
			if signature != "" {
				item.Reasoning.Signature = &model.Signature{Vendor: model.VendorGoogle, Blob: signature, TextDigest: model.TextDigest(text)}
			}
			item.Raw = codec.CaptureRaw(model.Gemini, entry, partKeys...)
			items = append(items, item)
			continue
		}
		if entry["text"] != nil {
			messageParts = append(messageParts, model.Part{Kind: model.PartText, Text: text, Raw: codec.CaptureRaw(model.Gemini, entry, partKeys...)})
			continue
		}
		if signature != "" {
			flush()
			items = append(items, model.Item{Kind: model.ItemReasoning, Role: role, Reasoning: &model.Reasoning{Visibility: "full", Signature: &model.Signature{Vendor: model.VendorGoogle, Blob: signature}}, Raw: codec.CaptureRaw(model.Gemini, entry, partKeys...)})
			continue
		}
		flush()
		items = append(items, model.Item{Kind: model.ItemNative, Role: role, Raw: &model.Raw{Format: model.Gemini, Fields: map[string]json.RawMessage{"native": model.Marshal(entry)}}})
	}
	flush()
	return items, nil
}

func toolResultText(response map[string]any) (string, bool) {
	if response == nil {
		return "", false
	}
	if len(response) == 1 {
		if text, ok := response["result"].(string); ok {
			return text, false
		}
		if text, ok := response["error"].(string); ok {
			return text, true
		}
		if response["error"] != nil {
			return string(model.Marshal(response["error"])), true
		}
	}
	return string(model.Marshal(response)), false
}

func (Codec) EncodeRequest(request *model.Request, peer codec.Peer) ([]byte, http.Header, []model.Loss, error) {
	root := map[string]any{}
	var losses []model.Loss
	var system []any
	var systemRaw *model.Raw
	contents := make([]any, 0, len(request.Items))
	if request.Instructions != "" {
		system = append(system, map[string]any{"text": request.Instructions})
	}
	callNames := map[string]string{}
	for _, item := range request.Items {
		if item.ToolCall != nil {
			callNames[item.ToolCall.ID] = item.ToolCall.Name
		}
	}
	for index := range request.Items {
		item := &request.Items[index]
		path := "items[" + strconv.Itoa(index) + "]"
		switch item.Kind {
		case model.ItemMessage, model.ItemReasoning:
			role := item.Role
			if item.Kind == model.ItemReasoning {
				role = model.RoleAssistant
			}
			parts, nested, err := encodeItemParts(item, path, peer, false)
			if err != nil {
				return nil, nil, losses, err
			}
			losses = append(losses, nested...)
			if role == model.RoleSystem || role == model.RoleDeveloper {
				if role == model.RoleDeveloper {
					losses = append(losses, model.Loss{Path: path + ".role", Code: model.LossRoleMapped, Class: model.LossDegraded, Detail: "developer role represented as a system instruction", To: model.Gemini})
				}
				system = append(system, parts...)
				if systemRaw == nil {
					systemRaw = item.Raw
				}
				continue
			}
			if len(parts) == 0 {
				continue
			}
			contents = appendContent(contents, geminiRole(role), parts, item.Raw, path, &losses)
		case model.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == model.ToolServer {
				losses = append(losses, codec.Degraded(path, "provider-executed tool calls cannot be replayed as client tools", model.Gemini))
				continue
			}
			entry, nested := encodeFunctionCall(item, peer, path)
			losses = append(losses, nested...)
			contents = appendContent(contents, "model", []any{entry}, item.Raw, path, &losses)
		case model.ItemToolResult:
			entry, nested, err := encodeFunctionResponse(item, callNames, path, peer)
			if err != nil {
				return nil, nil, losses, err
			}
			losses = append(losses, nested...)
			contents = appendContent(contents, "user", []any{entry}, item.Raw, path, &losses)
		case model.ItemServerToolCall, model.ItemServerToolResult, model.ItemNative:
			if native := codec.RawObject(item.Raw, model.Gemini); native != nil {
				role := "user"
				if item.Role == model.RoleAssistant || item.Kind.IsTool() {
					role = "model"
				}
				contents = appendContent(contents, role, []any{native}, nil, path, &losses)
				continue
			}
			losses = append(losses, codec.Degraded(path, "provider-private item cannot be replayed by Gemini", model.Gemini))
		}
	}
	if len(system) > 0 {
		instruction := map[string]any{"parts": system}
		losses = append(losses, codec.RawFields(instruction, systemRaw, model.Gemini, "systemInstruction.raw")...)
		root["systemInstruction"] = instruction
	}
	root["contents"] = contents
	if len(request.SafetySettings) > 0 {
		settings := make([]any, 0, len(request.SafetySettings))
		for _, setting := range request.SafetySettings {
			settings = append(settings, map[string]any{"category": setting.Category, "threshold": setting.Threshold})
		}
		root["safetySettings"] = settings
	}
	if len(request.Tools) > 0 {
		tools, nested, err := codec.EncodeTools(request.Tools, model.Gemini, peer.Profile.Capability.Tools)
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if len(tools) > 0 {
			root["tools"] = tools
		}
	}
	nested, err := codec.EncodeParams(request, model.Gemini, root, peer.Profile)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	losses = append(losses, codec.RawFields(root, request.Raw, model.Gemini, "raw")...)
	return model.Marshal(root), nil, losses, nil
}

// geminiIDs reports a model generation whose functionCall schema carries ids. Older Gemini models
// pair results by name and order and reject an unknown id field.
func geminiIDs(modelName string) bool {
	return strings.Contains(modelName, "gemini-3")
}

func geminiRole(role model.Role) string {
	if role == model.RoleAssistant {
		return "model"
	}
	return "user"
}

func encodeItemParts(item *model.Item, path string, peer codec.Peer, output bool) ([]any, []model.Loss, error) {
	var losses []model.Loss
	parts := make([]any, 0, len(item.Parts))
	context := codec.PartContext{Target: model.Gemini, Peer: peer, Role: item.Role, Output: output}
	if item.Kind == model.ItemReasoning && len(item.Parts) == 0 && item.Reasoning != nil && item.Reasoning.Signature != nil {
		encoded, nested, err := codec.EncodePart(model.Part{Kind: model.PartReasoningText}, item, context, path+".reasoning")
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		if encoded != nil {
			parts = append(parts, encoded)
		}
	}
	for index := range item.Parts {
		part := &item.Parts[index]
		if part.Kind == model.PartToolArguments {
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

func encodeFunctionCall(item *model.Item, peer codec.Peer, path string) (map[string]any, []model.Loss) {
	call := item.ToolCall
	args, err := toolArguments(call.Arguments)
	if call.Kind == model.ToolCustom {
		args, err = map[string]any{"input": call.Arguments}, nil
	}
	var losses []model.Loss
	if err != nil {
		losses = append(losses, codec.Degraded(path+".args", "tool arguments were not a JSON object and were wrapped under input", model.Gemini))
	}
	entry := map[string]any{"functionCall": map[string]any{"name": call.Name, "args": args}}
	if call.ID != "" && geminiIDs(peer.Model) {
		entry["functionCall"].(map[string]any)["id"] = call.ID
	}
	if call.Signature != nil {
		if signature := peer.WireToolSignature(call.Signature, call.Name, call.Arguments); signature != "" {
			entry["thoughtSignature"] = signature
		}
	}
	losses = append(losses, codec.RawFields(entry, item.Raw, model.Gemini, path+".raw")...)
	return entry, losses
}

func toolArguments(arguments string) (any, error) {
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

func encodeFunctionResponse(item *model.Item, callNames map[string]string, path string, peer codec.Peer) (map[string]any, []model.Loss, error) {
	name := item.Name
	if name == "" {
		name = callNames[item.CallID]
	}
	if name == "" {
		return nil, nil, errors.New(path + ": Gemini function responses require the tool name")
	}
	var text string
	var media []any
	for index := range item.Parts {
		part := &item.Parts[index]
		switch part.Kind {
		case model.PartText:
			text += part.Text
		case model.PartMedia:
			encoded, err := codec.EncodeMedia(part.Media, model.Gemini)
			if err != nil {
				return nil, nil, err
			}
			media = append(media, encoded)
		default:
			return nil, nil, errors.New(path + ": unsupported tool result content")
		}
	}
	var response any = map[string]any{}
	if text != "" {
		if decoded, err := model.DecodeJSON([]byte(text)); err == nil {
			if object, ok := decoded.(map[string]any); ok {
				response = object
			} else {
				response = map[string]any{"result": decoded}
			}
		} else {
			response = map[string]any{"result": text}
		}
	}
	if item.IsError {
		response = map[string]any{"error": text}
	}
	fr := map[string]any{"name": name, "response": response}
	if item.CallID != "" && geminiIDs(peer.Model) {
		fr["id"] = item.CallID
	}
	if len(media) > 0 {
		fr["parts"] = media
	}
	entry := map[string]any{"functionResponse": fr}
	losses := codec.RawFields(entry, item.Raw, model.Gemini, path+".raw")
	return entry, losses, nil
}

func appendContent(contents []any, role string, parts []any, raw *model.Raw, path string, losses *[]model.Loss) []any {
	if count := len(contents); count > 0 {
		previous := contents[count-1].(map[string]any)
		if previous["role"] == role {
			previous["parts"] = append(previous["parts"].([]any), parts...)
			*losses = append(*losses, codec.RawFields(previous, raw, model.Gemini, path+".raw")...)
			return contents
		}
	}
	entry := map[string]any{"role": role, "parts": parts}
	*losses = append(*losses, codec.RawFields(entry, raw, model.Gemini, path+".raw")...)
	return append(contents, entry)
}
