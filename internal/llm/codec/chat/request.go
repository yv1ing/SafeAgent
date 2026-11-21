package chat

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
	request := &model.Request{Source: model.Chat, Model: model.String(root["model"])}
	if request.Model == "" {
		return nil, nil, errors.New("model is required")
	}
	if root["n"] != nil {
		count, ok := model.Integer(root["n"])
		if !ok || count < 1 {
			return nil, nil, errors.New("n must be a positive integer")
		}
		request.ChoiceCount = int(count)
	}
	limitKey := "max_completion_tokens"
	if root[limitKey] == nil {
		limitKey = "max_tokens"
	}
	if err := codec.DecodeScalars(request, root, limitKey); err != nil {
		return nil, nil, err
	}
	codec.DecodeOpenAISampling(request, root)
	if options := model.Object(root["stream_options"]); options != nil {
		request.StreamOptions = &model.StreamOptions{IncludeUsage: options["include_usage"] == true}
	}
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
	for _, modality := range model.Array(root["modalities"]) {
		request.Modalities = append(request.Modalities, model.String(modality))
	}
	request.Store, err = codec.BooleanField(root, "store")
	if err != nil {
		return nil, nil, err
	}
	request.Background, err = codec.BooleanField(root, "background")
	if err != nil {
		return nil, nil, err
	}
	request.Conversation = model.String(root["conversation"])
	request.PreviousResponseID = model.String(root["previous_response_id"])
	request.SafetyIdentifier = model.String(root["safety_identifier"])

	for _, value := range model.Array(root["messages"]) {
		items, err := decodeMessage(model.Object(value), peer)
		if err != nil {
			return nil, nil, err
		}
		request.Items = append(request.Items, items...)
	}
	if len(request.Items) == 0 {
		return nil, nil, errors.New("at least one message is required")
	}
	if request.Stop, err = codec.ParseStop(root["stop"]); err != nil {
		return nil, nil, err
	}
	if request.Tools, err = codec.DecodeTools(model.Chat, model.Array(root["tools"])); err != nil {
		return nil, nil, err
	}
	if request.ToolChoice, err = codec.ParseToolChoice(model.Chat, root["tool_choice"]); err != nil {
		return nil, nil, err
	}
	if request.ResponseFormat, err = codec.ParseResponseFormat(root["response_format"]); err != nil {
		return nil, nil, err
	}
	if effort := model.String(root["reasoning_effort"]); effort != "" {
		request.Reasoning = &model.ReasoningConfig{Effort: effort}
	}
	if thinking := model.Object(root["thinking"]); thinking != nil {
		switch model.String(thinking["type"]) {
		case "disabled":
			request.Reasoning = &model.ReasoningConfig{Effort: "none"}
		case "enabled":
			if request.Reasoning == nil {
				request.Reasoning = &model.ReasoningConfig{Effort: "high"}
			}
		}
	}
	request.Raw = codec.CaptureRaw(model.Chat, root, requestKeys...)
	codec.AssignCallIDs(request.Items, codec.NewCallIDs(peer))
	codec.PairAnonymousResults(request.Items)
	normalizeOrdinals(request.Items)
	return request, nil, nil
}

func normalizeOrdinals(items []model.Item) {
	for index := 0; index < len(items); index++ {
		items[index].Ordinal = index
	}
}

func decodeMessage(entry map[string]any, peer codec.Peer) ([]model.Item, error) {
	if entry == nil {
		return nil, errors.New("messages must be objects")
	}
	role := model.Role(model.String(entry["role"]))
	switch role {
	case model.RoleSystem, model.RoleDeveloper, model.RoleUser, model.RoleAssistant:
	case model.RoleTool, "function":
		parts, err := codec.DecodeToolResultParts(model.Chat, entry["content"])
		if err != nil {
			return nil, err
		}
		return []model.Item{{
			Kind: model.ItemToolResult, Role: model.RoleTool, CallID: model.String(entry["tool_call_id"]), Name: model.String(entry["name"]), Parts: parts,
			Raw: codec.CaptureRaw(model.Chat, entry, messageKeys...),
		}}, nil
	default:
		return nil, errors.New("unsupported message role")
	}
	items, err := codec.DecodeContentItems(model.Chat, role, entry["content"], peer)
	if err != nil {
		return nil, err
	}
	if cache := codec.DecodeCacheControl(entry["cache_control"]); cache != nil {
		if len(items) == 0 {
			items = append(items, model.Item{Kind: model.ItemMessage, Role: role})
		}
		items[0].CacheControl = cache
	}
	raw := codec.CaptureRaw(model.Chat, entry, messageKeys...)
	if reasoning := codec.FirstNonEmpty(model.String(entry["reasoning_content"]), model.String(entry["reasoning"])); reasoning != "" {
		items = append([]model.Item{{
			Kind: model.ItemReasoning, Role: role, Parts: []model.Part{{Kind: model.PartReasoningText, Text: reasoning}},
			Reasoning: &model.Reasoning{Visibility: "full"},
		}}, items...)
	}
	if refusal := model.String(entry["refusal"]); refusal != "" {
		items = append(items, model.Item{Kind: model.ItemMessage, Role: role, Parts: []model.Part{{Kind: model.PartRefusal, Text: refusal}}})
	}
	for _, value := range model.Array(entry["tool_calls"]) {
		item, err := decodeToolCall(model.Object(value), true)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if legacy := model.Object(entry["function_call"]); legacy != nil {
		arguments := model.String(legacy["arguments"])
		if arguments != "" && !codec.ValidJSON(arguments) {
			return nil, errors.New("tool arguments must be valid JSON")
		}
		items = append(items, model.Item{
			Kind: model.ItemToolCall, Role: model.RoleAssistant,
			ToolCall: &model.ToolCall{Kind: model.ToolFunction, Name: model.String(legacy["name"]), Arguments: codec.EmptyObjectIfBlank(arguments)},
		})
	}
	if len(model.Array(entry["annotations"])) > 0 {
		attachAnnotations(items, entry)
	}
	return attachMessageRaw(items, raw, role), nil
}

// attachMessageRaw keeps wire fields of the enclosing Chat message. A message whose content is
// empty but whose tool calls are modeled separately still needs a container for those fields.
func attachMessageRaw(items []model.Item, raw *model.Raw, role model.Role) []model.Item {
	if raw == nil {
		return items
	}
	for index := range items {
		if items[index].Kind == model.ItemMessage {
			items[index].Raw = raw
			return items
		}
	}
	return append([]model.Item{{Kind: model.ItemMessage, Role: role, Raw: raw}}, items...)
}

func decodeToolCall(call map[string]any, validate bool) (model.Item, error) {
	switch model.String(call["type"]) {
	case "custom":
		custom := model.Object(call["custom"])
		return model.Item{
			Kind: model.ItemToolCall, Role: model.RoleAssistant, ID: model.String(call["id"]),
			ToolCall: &model.ToolCall{Kind: model.ToolCustom, ID: model.String(call["id"]), Name: model.String(custom["name"]), Arguments: model.String(custom["input"])},
			Raw:      codec.CaptureRaw(model.Chat, call, "id", "type", "custom.name", "custom.input"),
		}, nil
	case "function", "":
		function := model.Object(call["function"])
		arguments := model.String(function["arguments"])
		if validate && arguments != "" && !codec.ValidJSON(arguments) {
			return model.Item{}, errors.New("tool arguments must be valid JSON")
		}
		return model.Item{
			Kind: model.ItemToolCall, Role: model.RoleAssistant, ID: model.String(call["id"]),
			ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: model.String(call["id"]), Name: model.String(function["name"]), Arguments: codec.EmptyObjectIfBlank(arguments)},
			Raw:      codec.CaptureRaw(model.Chat, call, "id", "type", "function.name", "function.arguments"),
		}, nil
	}
	return model.Item{}, errors.New("unsupported tool call type")
}

func attachAnnotations(items []model.Item, entry map[string]any) {
	for itemIndex := len(items) - 1; itemIndex >= 0; itemIndex-- {
		item := &items[itemIndex]
		if item.Kind != model.ItemMessage {
			continue
		}
		for partIndex := len(item.Parts) - 1; partIndex >= 0; partIndex-- {
			if item.Parts[partIndex].Kind != model.PartText {
				continue
			}
			for _, value := range model.Array(entry["annotations"]) {
				if object := model.Object(value); object != nil {
					item.Parts[partIndex].Citations = append(item.Parts[partIndex].Citations, codec.DecodeCitation(model.Chat, object))
				}
			}
			return
		}
	}
}

func (coder Codec) EncodeRequest(request *model.Request, peer codec.Peer) ([]byte, http.Header, []model.Loss, error) {
	root := map[string]any{"model": peer.Model, "stream": request.Stream}
	context := codec.PartContext{Target: model.Chat, Peer: peer}
	var losses []model.Loss
	messages := make([]any, 0, len(request.Items)+1)
	if request.Instructions != "" {
		messages = append(messages, map[string]any{"role": "system", "content": request.Instructions})
	}
	encoded, nested, err := encodeMessages(request.Items, context)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	messages = append(messages, encoded...)
	root["messages"] = messages
	if len(request.Tools) > 0 {
		tools, nested, err := codec.EncodeTools(request.Tools, model.Chat, peer.Profile.Capability.Tools)
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if len(tools) > 0 {
			root["tools"] = tools
		}
	}
	nested, err = codec.EncodeParams(request, model.Chat, root, peer.Profile)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	if request.Stream {
		root["stream_options"] = map[string]any{"include_usage": true}
	}
	losses = append(losses, codec.RawFields(root, request.Raw, model.Chat, "raw")...)
	return model.Marshal(root), nil, losses, nil
}

type chatMessage struct {
	role       model.Role
	parts      []any
	toolCalls  []any
	reasoning  string
	refusal    string
	citations  []model.Citation
	raw        *model.Raw
	hasContent bool
}

func (message *chatMessage) addPart(part any) {
	message.parts = append(message.parts, part)
	message.hasContent = true
}

func encodeMessages(items []model.Item, context codec.PartContext) ([]any, []model.Loss, error) {
	var losses []model.Loss
	out := make([]any, 0, len(items))
	var current *chatMessage
	flush := func() {
		if current == nil {
			return
		}
		entry := map[string]any{"role": string(current.role)}
		entry["content"] = contentOf(current.parts)
		if len(current.toolCalls) > 0 {
			entry["tool_calls"] = current.toolCalls
		}
		if current.reasoning != "" {
			entry["reasoning_content"] = current.reasoning
		}
		if current.refusal != "" {
			entry["refusal"] = current.refusal
		}
		if len(current.citations) > 0 {
			annotations, _ := encodeChatCitations(current.citations)
			entry["annotations"] = annotations
		}
		losses = append(losses, codec.RawFields(entry, current.raw, model.Chat, "messages["+strconv.Itoa(len(out))+"]")...)
		out = append(out, entry)
		current = nil
	}
	start := func(role model.Role) {
		if current != nil && current.role == role {
			return
		}
		flush()
		current = &chatMessage{role: role}
	}

	for index := 0; index < len(items); index++ {
		item := &items[index]
		path := "items[" + strconv.Itoa(index) + "]"
		switch item.Kind {
		case model.ItemMessage:
			end := index
			for end < len(items) && items[end].Kind == model.ItemMessage && items[end].Role == item.Role {
				end++
			}
			role := item.Role
			if role == "" {
				role = model.RoleUser
			}
			start(role)
			if current.raw == nil && item.Raw != nil {
				current.raw = item.Raw
			}
			for position := index; position < end; position++ {
				entry := &items[position]
				for partIndex := range entry.Parts {
					part := &entry.Parts[partIndex]
					if context.Output && part.Kind == model.PartMedia {
						losses = append(losses, codec.Degraded(path, "Chat cannot return generated media", model.Chat))
						continue
					}
					switch part.Kind {
					case model.PartRefusal:
						if entry.Role == model.RoleAssistant {
							current.refusal += part.Text
							continue
						}
					case model.PartReasoningText, model.PartReasoningSummary:
						losses = append(losses, codec.Degraded(path, "Chat messages carry no reasoning field for this role", model.Chat))
						continue
					case model.PartRedactedReasoning:
						losses = append(losses, codec.Degraded(path, "redacted reasoning cannot be replayed by Chat", model.Chat))
						continue
					}
					partContext := context
					partContext.Role = entry.Role
					encoded, nested, err := codec.EncodePart(*part, entry, partContext, path)
					if err != nil {
						return nil, losses, err
					}
					losses = append(losses, nested...)
					if encoded != nil {
						current.addPart(encoded)
					}
					current.citations = append(current.citations, part.Citations...)
				}
			}
			index = end - 1
		case model.ItemReasoning:
			start(model.RoleAssistant)
			for partIndex := range item.Parts {
				part := &item.Parts[partIndex]
				if part.Text != "" {
					current.reasoning += part.Text
				}
			}
			if item.Reasoning != nil && item.Reasoning.Signature != nil {
				losses = append(losses, model.Loss{Path: path + ".signature", Code: model.LossSignatureStripped, Class: model.LossDegraded, Detail: "Chat has no field for reasoning signatures", To: model.Chat})
			}
		case model.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == model.ToolServer {
				losses = append(losses, codec.Degraded(path, "provider-executed tool calls cannot be represented by Chat", model.Chat))
				continue
			}
			start(model.RoleAssistant)
			entry := map[string]any{"id": item.ToolCall.ID, "type": "function", "function": map[string]any{"name": item.ToolCall.Name, "arguments": item.ToolCall.Arguments}}
			if item.ToolCall.Kind == model.ToolCustom {
				entry = map[string]any{"id": item.ToolCall.ID, "type": "custom", "custom": map[string]any{"name": item.ToolCall.Name, "input": item.ToolCall.Arguments}}
			}
			losses = append(losses, codec.RawFields(entry, item.Raw, model.Chat, path+".raw")...)
			if item.ToolCall.Signature != nil {
				losses = append(losses, model.Loss{Path: path + ".signature", Code: model.LossSignatureStripped, Class: model.LossDegraded, Detail: "Chat has no field for tool call signatures", To: model.Chat})
			}
			current.toolCalls = append(current.toolCalls, entry)
		case model.ItemToolResult:
			flush()
			if item.IsError {
				losses = append(losses, codec.Degraded(path, "Chat cannot mark a tool result as an error", model.Chat))
			}
			entry := map[string]any{"role": "tool", "tool_call_id": item.CallID}
			content, nested, err := encodeToolResultContent(item.Parts, path)
			if err != nil {
				return nil, losses, err
			}
			losses = append(losses, nested...)
			entry["content"] = content
			if item.Name != "" {
				entry["name"] = item.Name
			}
			losses = append(losses, codec.RawFields(entry, item.Raw, model.Chat, path+".raw")...)
			out = append(out, entry)
		case model.ItemServerToolCall, model.ItemServerToolResult, model.ItemNative:
			losses = append(losses, codec.Degraded(path, "provider-private item cannot be replayed by Chat", model.Chat))
		}
	}
	flush()
	return out, losses, nil
}

func encodeToolResultContent(parts []model.Part, path string) (any, []model.Loss, error) {
	var losses []model.Loss
	if len(parts) == 0 {
		return "", nil, nil
	}
	if len(parts) == 1 && parts[0].Kind == model.PartText {
		return parts[0].Text, nil, nil
	}
	encoded := make([]any, 0, len(parts))
	for _, part := range parts {
		entry, nested, err := codec.EncodePart(part, nil, codec.PartContext{Target: model.Chat, Role: model.RoleTool}, path)
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		if entry != nil {
			encoded = append(encoded, entry)
		}
	}
	return encoded, losses, nil
}

func contentOf(parts []any) any {
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		if part, ok := parts[0].(map[string]any); ok && part["type"] == "text" && len(part) == 2 {
			return part["text"]
		}
	}
	return parts
}

func encodeChatCitations(citations []model.Citation) ([]any, []model.Loss) {
	var losses []model.Loss
	var encoded []any
	for index, citation := range citations {
		entry, nested := codec.EncodeCitation(citation, model.Chat)
		losses = append(losses, codec.PrefixLosses(nested, "citations["+strconv.Itoa(index)+"]")...)
		if entry != nil {
			encoded = append(encoded, entry)
		}
	}
	return encoded, losses
}
