package chat

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
	request := &protocol.Request{Source: protocol.Chat, Model: protocol.String(root["model"])}
	if request.Model == "" {
		return nil, nil, errors.New("model is required")
	}
	if root["n"] != nil {
		if count, ok := protocol.Integer(root["n"]); !ok || count != 1 {
			return nil, nil, protocol.UnsupportedFeature("unsupported_multi_choice", "Multiple candidates are not supported.")
		}
	}
	limitKey := "max_completion_tokens"
	if root[limitKey] == nil {
		limitKey = "max_tokens"
	}
	if err := format.DecodeScalars(request, root, limitKey); err != nil {
		return nil, nil, err
	}
	format.DecodeOpenAISampling(request, root)
	if options := protocol.Object(root["stream_options"]); options != nil {
		request.StreamOptions = &protocol.StreamOptions{IncludeUsage: options["include_usage"] == true}
	}
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
	for _, modality := range protocol.Array(root["modalities"]) {
		request.Modalities = append(request.Modalities, protocol.String(modality))
	}
	request.Store, err = booleanField(root, "store")
	if err != nil {
		return nil, nil, err
	}
	request.Background, err = booleanField(root, "background")
	if err != nil {
		return nil, nil, err
	}
	request.Conversation = protocol.String(root["conversation"])
	request.PreviousResponseID = protocol.String(root["previous_response_id"])
	request.SafetyIdentifier = protocol.String(root["safety_identifier"])

	for _, value := range protocol.Array(root["messages"]) {
		items, err := decodeMessage(protocol.Object(value), peer)
		if err != nil {
			return nil, nil, err
		}
		request.Items = append(request.Items, items...)
	}
	if len(request.Items) == 0 {
		return nil, nil, errors.New("at least one message is required")
	}
	if request.Stop, err = format.ParseStop(root["stop"]); err != nil {
		return nil, nil, err
	}
	if request.Tools, err = format.DecodeTools(protocol.Chat, protocol.Array(root["tools"])); err != nil {
		return nil, nil, err
	}
	if request.ToolChoice, err = format.ParseToolChoice(protocol.Chat, root["tool_choice"]); err != nil {
		return nil, nil, err
	}
	if request.ResponseFormat, err = format.ParseResponseFormat(root["response_format"]); err != nil {
		return nil, nil, err
	}
	if effort := protocol.String(root["reasoning_effort"]); effort != "" {
		request.Reasoning = &protocol.ReasoningConfig{Effort: effort}
	}
	if thinking := protocol.Object(root["thinking"]); thinking != nil {
		switch protocol.String(thinking["type"]) {
		case "disabled":
			request.Reasoning = &protocol.ReasoningConfig{Effort: "none"}
		case "enabled":
			if request.Reasoning == nil {
				request.Reasoning = &protocol.ReasoningConfig{Effort: "high"}
			}
		}
	}
	request.Raw = format.CaptureRaw(protocol.Chat, root, requestKeys...)
	format.AssignCallIDs(request.Items, format.NewCallIDs(peer))
	format.PairAnonymousResults(request.Items)
	normalizeOrdinals(request.Items)
	return request, nil, nil
}

func normalizeOrdinals(items []protocol.Item) {
	for index := range items {
		items[index].Ordinal = index
	}
}

func decodeMessage(entry map[string]any, peer format.Peer) ([]protocol.Item, error) {
	if entry == nil {
		return nil, errors.New("messages must be objects")
	}
	role := protocol.Role(protocol.String(entry["role"]))
	switch role {
	case protocol.RoleSystem, protocol.RoleDeveloper, protocol.RoleUser, protocol.RoleAssistant:
	case protocol.RoleTool, "function":
		parts, err := format.DecodeToolResultParts(protocol.Chat, entry["content"])
		if err != nil {
			return nil, err
		}
		return []protocol.Item{{
			Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: protocol.String(entry["tool_call_id"]), Name: protocol.String(entry["name"]), Parts: parts,
			Raw: format.CaptureRaw(protocol.Chat, entry, messageKeys...),
		}}, nil
	default:
		return nil, errors.New("unsupported message role")
	}
	items, err := format.DecodeContentItems(protocol.Chat, role, entry["content"], peer)
	if err != nil {
		return nil, err
	}
	raw := format.CaptureRaw(protocol.Chat, entry, messageKeys...)
	if reasoning := firstNonEmpty(protocol.String(entry["reasoning_content"]), protocol.String(entry["reasoning"])); reasoning != "" {
		items = append([]protocol.Item{{
			Kind: protocol.ItemReasoning, Role: role, Parts: []protocol.Part{{Kind: protocol.PartReasoningText, Text: reasoning}},
			Reasoning: &protocol.Reasoning{Visibility: "full"},
		}}, items...)
	}
	if refusal := protocol.String(entry["refusal"]); refusal != "" {
		items = append(items, protocol.Item{Kind: protocol.ItemMessage, Role: role, Parts: []protocol.Part{{Kind: protocol.PartRefusal, Text: refusal}}})
	}
	for _, value := range protocol.Array(entry["tool_calls"]) {
		item, err := decodeToolCall(protocol.Object(value), true)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if legacy := protocol.Object(entry["function_call"]); legacy != nil {
		arguments := protocol.String(legacy["arguments"])
		if arguments != "" && !validJSON(arguments) {
			return nil, errors.New("tool arguments must be valid JSON")
		}
		items = append(items, protocol.Item{
			Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant,
			ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, Name: protocol.String(legacy["name"]), Arguments: format.EmptyObjectIfBlank(arguments)},
		})
	}
	if len(protocol.Array(entry["annotations"])) > 0 {
		attachAnnotations(items, entry)
	}
	return attachMessageRaw(items, raw, role), nil
}

// attachMessageRaw keeps wire fields of the enclosing Chat message. A message whose content is
// empty but whose tool calls are modeled separately still needs a container for those fields.
func attachMessageRaw(items []protocol.Item, raw *protocol.Raw, role protocol.Role) []protocol.Item {
	if raw == nil {
		return items
	}
	for index := range items {
		if items[index].Kind == protocol.ItemMessage {
			items[index].Raw = raw
			return items
		}
	}
	return append([]protocol.Item{{Kind: protocol.ItemMessage, Role: role, Raw: raw}}, items...)
}

func decodeToolCall(call map[string]any, validate bool) (protocol.Item, error) {
	switch protocol.String(call["type"]) {
	case "custom":
		custom := protocol.Object(call["custom"])
		return protocol.Item{
			Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ID: protocol.String(call["id"]),
			ToolCall: &protocol.ToolCall{Kind: protocol.ToolCustom, ID: protocol.String(call["id"]), Name: protocol.String(custom["name"]), Arguments: protocol.String(custom["input"])},
			Raw:      format.CaptureRaw(protocol.Chat, call, "id", "type", "custom.name", "custom.input"),
		}, nil
	case "function", "":
		function := protocol.Object(call["function"])
		arguments := protocol.String(function["arguments"])
		if validate && arguments != "" && !validJSON(arguments) {
			return protocol.Item{}, errors.New("tool arguments must be valid JSON")
		}
		return protocol.Item{
			Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ID: protocol.String(call["id"]),
			ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: protocol.String(call["id"]), Name: protocol.String(function["name"]), Arguments: format.EmptyObjectIfBlank(arguments)},
			Raw:      format.CaptureRaw(protocol.Chat, call, "id", "type", "function.name", "function.arguments"),
		}, nil
	}
	return protocol.Item{}, errors.New("unsupported tool call type")
}

func attachAnnotations(items []protocol.Item, entry map[string]any) {
	for itemIndex := len(items) - 1; itemIndex >= 0; itemIndex-- {
		item := &items[itemIndex]
		if item.Kind != protocol.ItemMessage {
			continue
		}
		for partIndex := len(item.Parts) - 1; partIndex >= 0; partIndex-- {
			if item.Parts[partIndex].Kind != protocol.PartText {
				continue
			}
			for _, value := range protocol.Array(entry["annotations"]) {
				if object := protocol.Object(value); object != nil {
					item.Parts[partIndex].Citations = append(item.Parts[partIndex].Citations, format.DecodeCitation(protocol.Chat, object))
				}
			}
			return
		}
	}
}

func (codec Codec) EncodeRequest(request *protocol.Request, peer format.Peer) ([]byte, http.Header, []protocol.Loss, error) {
	root := map[string]any{"model": peer.Model, "stream": request.Stream}
	context := format.PartContext{Target: protocol.Chat, Peer: peer}
	var losses []protocol.Loss
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
		tools, nested, err := format.EncodeTools(request.Tools, protocol.Chat)
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if len(tools) > 0 {
			root["tools"] = tools
		}
	}
	nested, err = format.EncodeParams(request, protocol.Chat, root)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	if request.Stream {
		root["stream_options"] = map[string]any{"include_usage": true}
	}
	losses = append(losses, format.RawFields(root, request.Raw, protocol.Chat, "raw")...)
	return protocol.Marshal(root), nil, losses, nil
}

type chatMessage struct {
	role       protocol.Role
	parts      []any
	toolCalls  []any
	reasoning  string
	refusal    string
	citations  []protocol.Citation
	raw        *protocol.Raw
	hasContent bool
}

func (message *chatMessage) addPart(part any) {
	message.parts = append(message.parts, part)
	message.hasContent = true
}

func encodeMessages(items []protocol.Item, context format.PartContext) ([]any, []protocol.Loss, error) {
	var losses []protocol.Loss
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
		losses = append(losses, format.RawFields(entry, current.raw, protocol.Chat, "messages["+strconv.Itoa(len(out))+"]")...)
		out = append(out, entry)
		current = nil
	}
	start := func(role protocol.Role) {
		if current != nil && current.role == role {
			return
		}
		flush()
		current = &chatMessage{role: role}
	}

	for index := range items {
		item := &items[index]
		path := "items[" + strconv.Itoa(index) + "]"
		switch item.Kind {
		case protocol.ItemMessage:
			end := index
			for end < len(items) && items[end].Kind == protocol.ItemMessage && items[end].Role == item.Role {
				end++
			}
			role := item.Role
			if role == "" {
				role = protocol.RoleUser
			}
			start(role)
			if current.raw == nil && item.Raw != nil {
				current.raw = item.Raw
			}
			for position := index; position < end; position++ {
				entry := &items[position]
				for partIndex := range entry.Parts {
					part := &entry.Parts[partIndex]
					if context.Output && part.Kind == protocol.PartMedia {
						losses = append(losses, format.Degraded(path, "Chat cannot return generated media", protocol.Chat))
						continue
					}
					switch part.Kind {
					case protocol.PartRefusal:
						if entry.Role == protocol.RoleAssistant {
							current.refusal += part.Text
							continue
						}
					case protocol.PartReasoningText, protocol.PartReasoningSummary:
						losses = append(losses, format.Degraded(path, "Chat messages carry no reasoning field for this role", protocol.Chat))
						continue
					case protocol.PartRedactedReasoning:
						losses = append(losses, format.Degraded(path, "redacted reasoning cannot be replayed by Chat", protocol.Chat))
						continue
					}
					partContext := context
					partContext.Role = entry.Role
					encoded, nested, err := format.EncodePart(*part, entry, partContext, path)
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
		case protocol.ItemReasoning:
			start(protocol.RoleAssistant)
			for partIndex := range item.Parts {
				part := &item.Parts[partIndex]
				if part.Text != "" {
					current.reasoning += part.Text
				}
			}
			if item.Reasoning != nil && item.Reasoning.Signature != nil {
				losses = append(losses, protocol.Loss{Path: path + ".signature", Code: protocol.LossSignatureStripped, Class: protocol.LossDegraded, Detail: "Chat has no field for reasoning signatures", To: protocol.Chat})
			}
		case protocol.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == protocol.ToolServer {
				losses = append(losses, format.Degraded(path, "provider-executed tool calls cannot be represented by Chat", protocol.Chat))
				continue
			}
			start(protocol.RoleAssistant)
			entry := map[string]any{"id": item.ToolCall.ID, "type": "function", "function": map[string]any{"name": item.ToolCall.Name, "arguments": item.ToolCall.Arguments}}
			if item.ToolCall.Kind == protocol.ToolCustom {
				entry = map[string]any{"id": item.ToolCall.ID, "type": "custom", "custom": map[string]any{"name": item.ToolCall.Name, "input": item.ToolCall.Arguments}}
			}
			losses = append(losses, format.RawFields(entry, item.Raw, protocol.Chat, path+".raw")...)
			if item.ToolCall.Signature != nil {
				losses = append(losses, protocol.Loss{Path: path + ".signature", Code: protocol.LossSignatureStripped, Class: protocol.LossDegraded, Detail: "Chat has no field for tool call signatures", To: protocol.Chat})
			}
			current.toolCalls = append(current.toolCalls, entry)
		case protocol.ItemToolResult:
			flush()
			if item.IsError {
				losses = append(losses, format.Degraded(path, "Chat cannot mark a tool result as an error", protocol.Chat))
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
			losses = append(losses, format.RawFields(entry, item.Raw, protocol.Chat, path+".raw")...)
			out = append(out, entry)
		case protocol.ItemServerToolCall, protocol.ItemServerToolResult, protocol.ItemNative:
			losses = append(losses, format.Degraded(path, "provider-private item cannot be replayed by Chat", protocol.Chat))
		}
	}
	flush()
	return out, losses, nil
}

func encodeToolResultContent(parts []protocol.Part, path string) (any, []protocol.Loss, error) {
	var losses []protocol.Loss
	if len(parts) == 0 {
		return "", nil, nil
	}
	if len(parts) == 1 && parts[0].Kind == protocol.PartText {
		return parts[0].Text, nil, nil
	}
	encoded := make([]any, 0, len(parts))
	for _, part := range parts {
		entry, nested, err := format.EncodePart(part, nil, format.PartContext{Target: protocol.Chat, Role: protocol.RoleTool}, path)
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

func encodeChatCitations(citations []protocol.Citation) ([]any, []protocol.Loss) {
	var losses []protocol.Loss
	var encoded []any
	for index, citation := range citations {
		entry, nested := format.EncodeCitation(citation, protocol.Chat)
		losses = append(losses, prefix(nested, "citations["+strconv.Itoa(index)+"]")...)
		if entry != nil {
			encoded = append(encoded, entry)
		}
	}
	return encoded, losses
}

func prefix(losses []protocol.Loss, path string) []protocol.Loss {
	for index := range losses {
		losses[index].Path = path + "." + losses[index].Path
	}
	return losses
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func validJSON(value string) bool {
	_, err := protocol.DecodeJSON([]byte(value))
	return err == nil
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
