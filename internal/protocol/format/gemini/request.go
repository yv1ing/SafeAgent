package gemini

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

func (Codec) DecodeRequest(body []byte, header http.Header, peer format.Peer) (*protocol.Request, []protocol.Loss, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("request must be a JSON object")
	}
	request := &protocol.Request{Source: protocol.Gemini, Model: peer.Model}
	if request.Model == "" {
		request.Model = protocol.String(root["model"])
	}
	if config := protocol.Object(root["generationConfig"]); config != nil {
		if err := decodeConfig(request, config); err != nil {
			return nil, nil, err
		}
	}
	if instruction := protocol.Object(root["systemInstruction"]); instruction != nil {
		items, err := decodeParts(protocol.RoleSystem, protocol.Array(instruction["parts"]), format.NewCallIDs(peer), nil)
		if err != nil {
			return nil, nil, err
		}
		raw := format.CaptureRaw(protocol.Gemini, instruction, "parts")
		if len(items) > 0 {
			items[0].Raw = raw
		}
		request.Items = append(request.Items, items...)
	}
	ids := format.NewCallIDs(peer)
	pending := map[string][]string{}
	for _, value := range protocol.Array(root["contents"]) {
		entry := protocol.Object(value)
		role := protocol.Role(protocol.String(entry["role"]))
		switch role {
		case "model":
			role = protocol.RoleAssistant
		case "", protocol.RoleUser:
			role = protocol.RoleUser
		default:
			return nil, nil, errors.New("unsupported message role")
		}
		items, err := decodeParts(role, protocol.Array(entry["parts"]), ids, pending)
		if err != nil {
			return nil, nil, err
		}
		raw := format.CaptureRaw(protocol.Gemini, entry, "role", "parts")
		if len(items) > 0 {
			items[0].Raw = raw
		}
		request.Items = append(request.Items, items...)
	}
	if len(request.Items) == 0 {
		return nil, nil, errors.New("at least one content turn is required")
	}
	if request.Tools, err = format.DecodeTools(protocol.Gemini, protocol.Array(root["tools"])); err != nil {
		return nil, nil, err
	}
	if calling := protocol.Object(protocol.Object(root["toolConfig"])["functionCallingConfig"]); calling != nil {
		if request.ToolChoice, err = format.ParseToolChoice(protocol.Gemini, calling); err != nil {
			return nil, nil, err
		}
	}
	request.Raw = format.CaptureRaw(protocol.Gemini, root, requestKeys...)
	for index := range request.Items {
		request.Items[index].Ordinal = index
	}
	return request, nil, nil
}

func decodeConfig(request *protocol.Request, config map[string]any) error {
	if count, ok := protocol.Integer(config["candidateCount"]); ok && count > 1 {
		return protocol.UnsupportedFeature("unsupported_multi_choice", "Multiple candidates are not supported.")
	}
	request.MaxOutputTokens = protocol.OptionalInteger(config["maxOutputTokens"])
	request.Temperature, request.TopP = optionalFloat(config["temperature"]), optionalFloat(config["topP"])
	request.TopK = protocol.OptionalInteger(config["topK"])
	request.Seed = protocol.OptionalInteger(config["seed"])
	request.FrequencyPenalty, request.PresencePenalty = optionalFloat(config["frequencyPenalty"]), optionalFloat(config["presencePenalty"])
	var err error
	if request.Stop, err = format.ParseStop(config["stopSequences"]); err != nil {
		return err
	}
	switch protocol.String(config["responseMimeType"]) {
	case "":
	case "text/plain":
		request.ResponseFormat = &protocol.ResponseFormat{Kind: "text"}
	case "application/json":
		schema := protocol.Object(config["responseJsonSchema"])
		if schema == nil {
			schema = protocol.Object(config["responseSchema"])
		}
		if schema == nil {
			request.ResponseFormat = &protocol.ResponseFormat{Kind: "json_object"}
			break
		}
		if protocol.Object(config["responseJsonSchema"]) == nil {
			if schema, err = format.NormalizeGeminiSchema(schema); err != nil {
				return err
			}
		}
		request.ResponseFormat = &protocol.ResponseFormat{Kind: "json_schema", Name: "response", Schema: protocol.Marshal(schema)}
	default:
		return errors.New("unsupported response MIME type")
	}
	if thinking := protocol.Object(config["thinkingConfig"]); thinking != nil {
		reasoning := &protocol.ReasoningConfig{Effort: reasoningEffort(protocol.String(thinking["thinkingLevel"])), BudgetTokens: protocol.OptionalInteger(thinking["thinkingBudget"])}
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

func optionalFloat(value any) *float64 {
	if number, ok := protocol.Float(value); ok {
		return &number
	}
	return nil
}

func decodeParts(role protocol.Role, values []any, ids *format.CallIDs, pending map[string][]string) ([]protocol.Item, error) {
	var items []protocol.Item
	var messageParts []protocol.Part
	flush := func() {
		if len(messageParts) == 0 {
			return
		}
		items = append(items, protocol.Item{Kind: protocol.ItemMessage, Role: role, Parts: messageParts})
		messageParts = nil
	}
	for _, value := range values {
		entry := protocol.Object(value)
		if entry == nil {
			return nil, errors.New("content parts must be objects")
		}
		if media, err := format.DecodeGeminiMedia(entry); err != nil {
			return nil, err
		} else if media != nil {
			messageParts = append(messageParts, protocol.Part{Kind: protocol.PartMedia, Media: media, Raw: format.CaptureRaw(protocol.Gemini, entry, partKeys...)})
			continue
		}
		if entry["functionCall"] != nil {
			flush()
			call := protocol.Object(entry["functionCall"])
			name := protocol.String(call["name"])
			arguments := "{}"
			if call["args"] != nil {
				arguments = string(protocol.Marshal(call["args"]))
			}
			id := protocol.String(call["id"])
			if id == "" {
				id = ids.Assign(name, arguments)
			}
			tool := &protocol.ToolCall{Kind: protocol.ToolFunction, ID: id, Name: name, Arguments: arguments}
			if signature := protocol.String(entry["thoughtSignature"]); signature != "" {
				tool.Signature = &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: signature}
			}
			if pending != nil {
				pending[name] = append(pending[name], id)
			}
			items = append(items, protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: tool, Raw: format.CaptureRaw(protocol.Gemini, entry, partKeys...)})
			continue
		}
		if entry["functionResponse"] != nil {
			flush()
			result := protocol.Object(entry["functionResponse"])
			name := protocol.String(result["name"])
			id := protocol.String(result["id"])
			if id == "" && pending != nil {
				if queue := pending[name]; len(queue) > 0 {
					id, pending[name] = queue[0], queue[1:]
				}
			}
			if id == "" {
				id = ids.Assign(name, "")
			}
			response := protocol.Object(result["response"])
			text, isError := toolResultText(response)
			if response == nil && result["response"] != nil {
				text = string(protocol.Marshal(result["response"]))
			}
			parts := protocol.TextParts(text)
			if rawParts := protocol.Array(result["parts"]); len(rawParts) > 0 {
				for _, value := range rawParts {
					entry := protocol.Object(value)
					if media, err := format.DecodeGeminiMedia(entry); err != nil {
						return nil, err
					} else if media != nil {
						parts = append(parts, protocol.Part{Kind: protocol.PartMedia, Media: media})
					}
				}
			}
			items = append(items, protocol.Item{
				Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: id, Name: name, IsError: isError, Parts: parts,
				Raw: format.CaptureRaw(protocol.Gemini, entry, partKeys...),
			})
			continue
		}
		text := protocol.String(entry["text"])
		thought := entry["thought"] == true
		signature := protocol.String(entry["thoughtSignature"])
		if thought || (signature != "" && text != "") {
			flush()
			item := protocol.Item{Kind: protocol.ItemReasoning, Role: role, Parts: []protocol.Part{{Kind: protocol.PartReasoningText, Text: text}}, Reasoning: &protocol.Reasoning{Visibility: "full"}}
			if signature != "" {
				item.Reasoning.Signature = &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: signature, TextDigest: protocol.TextDigest(text)}
			}
			item.Raw = format.CaptureRaw(protocol.Gemini, entry, partKeys...)
			items = append(items, item)
			continue
		}
		if entry["text"] != nil {
			messageParts = append(messageParts, protocol.Part{Kind: protocol.PartText, Text: text, Raw: format.CaptureRaw(protocol.Gemini, entry, partKeys...)})
			continue
		}
		if signature != "" {
			flush()
			items = append(items, protocol.Item{Kind: protocol.ItemReasoning, Role: role, Reasoning: &protocol.Reasoning{Visibility: "full", Signature: &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: signature}}, Raw: format.CaptureRaw(protocol.Gemini, entry, partKeys...)})
			continue
		}
		flush()
		items = append(items, protocol.Item{Kind: protocol.ItemNative, Role: role, Raw: &protocol.Raw{Format: protocol.Gemini, Fields: map[string]json.RawMessage{"native": protocol.Marshal(entry)}}})
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
			return string(protocol.Marshal(response["error"])), true
		}
	}
	return string(protocol.Marshal(response)), false
}

func (Codec) EncodeRequest(request *protocol.Request, peer format.Peer) ([]byte, http.Header, []protocol.Loss, error) {
	root := map[string]any{}
	var losses []protocol.Loss
	var system []any
	var systemRaw *protocol.Raw
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
		case protocol.ItemMessage, protocol.ItemReasoning:
			role := item.Role
			if item.Kind == protocol.ItemReasoning {
				role = protocol.RoleAssistant
			}
			parts, nested, err := encodeItemParts(item, path, peer, false)
			if err != nil {
				return nil, nil, losses, err
			}
			losses = append(losses, nested...)
			if role == protocol.RoleSystem || role == protocol.RoleDeveloper {
				if role == protocol.RoleDeveloper {
					losses = append(losses, protocol.Loss{Path: path + ".role", Code: protocol.LossRoleMapped, Class: protocol.LossDegraded, Detail: "developer role represented as a system instruction", To: protocol.Gemini})
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
		case protocol.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == protocol.ToolServer {
				losses = append(losses, format.Degraded(path, "provider-executed tool calls cannot be replayed as client tools", protocol.Gemini))
				continue
			}
			entry, nested := encodeFunctionCall(item, peer, path)
			losses = append(losses, nested...)
			contents = appendContent(contents, "model", []any{entry}, item.Raw, path, &losses)
		case protocol.ItemToolResult:
			entry, nested, err := encodeFunctionResponse(item, callNames, path, peer)
			if err != nil {
				return nil, nil, losses, err
			}
			losses = append(losses, nested...)
			contents = appendContent(contents, "user", []any{entry}, item.Raw, path, &losses)
		case protocol.ItemServerToolCall, protocol.ItemServerToolResult, protocol.ItemNative:
			if native := format.RawObject(item.Raw, protocol.Gemini); native != nil {
				role := "user"
				if item.Role == protocol.RoleAssistant || item.Kind.IsTool() {
					role = "model"
				}
				contents = appendContent(contents, role, []any{native}, nil, path, &losses)
				continue
			}
			losses = append(losses, format.Degraded(path, "provider-private item cannot be replayed by Gemini", protocol.Gemini))
		}
	}
	if len(system) > 0 {
		instruction := map[string]any{"parts": system}
		losses = append(losses, format.RawFields(instruction, systemRaw, protocol.Gemini, "systemInstruction.raw")...)
		root["systemInstruction"] = instruction
	}
	root["contents"] = contents
	if len(request.Tools) > 0 {
		tools, nested, err := format.EncodeTools(request.Tools, protocol.Gemini)
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if len(tools) > 0 {
			root["tools"] = tools
		}
	}
	nested, err := format.EncodeParams(request, protocol.Gemini, root)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	losses = append(losses, format.RawFields(root, request.Raw, protocol.Gemini, "raw")...)
	return protocol.Marshal(root), nil, losses, nil
}

// geminiIDs reports a model generation whose functionCall schema carries ids. Older Gemini models
// pair results by name and order and reject an unknown id field.
func geminiIDs(model string) bool {
	return strings.Contains(model, "gemini-3")
}

func geminiRole(role protocol.Role) string {
	if role == protocol.RoleAssistant {
		return "model"
	}
	return "user"
}

func encodeItemParts(item *protocol.Item, path string, peer format.Peer, output bool) ([]any, []protocol.Loss, error) {
	var losses []protocol.Loss
	parts := make([]any, 0, len(item.Parts))
	context := format.PartContext{Target: protocol.Gemini, Peer: peer, Role: item.Role, Output: output}
	if item.Kind == protocol.ItemReasoning && len(item.Parts) == 0 && item.Reasoning != nil && item.Reasoning.Signature != nil {
		encoded, nested, err := format.EncodePart(protocol.Part{Kind: protocol.PartReasoningText}, item, context, path+".reasoning")
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
		if part.Kind == protocol.PartToolArguments {
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

func encodeFunctionCall(item *protocol.Item, peer format.Peer, path string) (map[string]any, []protocol.Loss) {
	call := item.ToolCall
	args, err := toolArguments(call.Arguments)
	if call.Kind == protocol.ToolCustom {
		args, err = map[string]any{"input": call.Arguments}, nil
	}
	var losses []protocol.Loss
	if err != nil {
		losses = append(losses, format.Degraded(path+".args", "tool arguments were not a JSON object and were wrapped under input", protocol.Gemini))
	}
	entry := map[string]any{"functionCall": map[string]any{"name": call.Name, "args": args}}
	if call.ID != "" && geminiIDs(peer.Model) {
		entry["functionCall"].(map[string]any)["id"] = call.ID
	}
	if call.Signature != nil {
		if signature := peer.WireSignature(call.Signature, ""); signature != "" {
			entry["thoughtSignature"] = signature
		}
	}
	losses = append(losses, format.RawFields(entry, item.Raw, protocol.Gemini, path+".raw")...)
	return entry, losses
}

func toolArguments(arguments string) (any, error) {
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

func encodeFunctionResponse(item *protocol.Item, callNames map[string]string, path string, peer format.Peer) (map[string]any, []protocol.Loss, error) {
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
		case protocol.PartText:
			text += part.Text
		case protocol.PartMedia:
			encoded, err := format.EncodeMedia(part.Media, protocol.Gemini)
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
		if decoded, err := protocol.DecodeJSON([]byte(text)); err == nil {
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
	losses := format.RawFields(entry, item.Raw, protocol.Gemini, path+".raw")
	return entry, losses, nil
}

func appendContent(contents []any, role string, parts []any, raw *protocol.Raw, path string, losses *[]protocol.Loss) []any {
	if count := len(contents); count > 0 {
		previous := contents[count-1].(map[string]any)
		if previous["role"] == role {
			previous["parts"] = append(previous["parts"].([]any), parts...)
			*losses = append(*losses, format.RawFields(previous, raw, protocol.Gemini, path+".raw")...)
			return contents
		}
	}
	entry := map[string]any{"role": role, "parts": parts}
	*losses = append(*losses, format.RawFields(entry, raw, protocol.Gemini, path+".raw")...)
	return append(contents, entry)
}
