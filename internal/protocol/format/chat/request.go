// Package chat implements OpenAI Chat Completions request and response codecs.
package chat

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type Codec struct{}

func New() Codec { return Codec{} }

func (Codec) Format() protocol.Format { return protocol.Chat }

var requestKeys = []string{"model", "messages", "stream", "stream_options", "n", "max_completion_tokens", "max_tokens", "temperature", "top_p", "stop", "tools", "tool_choice", "parallel_tool_calls", "reasoning_effort", "metadata", "user", "service_tier", "prompt_cache_key", "seed", "frequency_penalty", "presence_penalty", "response_format"}

var messageKeys = []string{"role", "content", "reasoning_content", "reasoning", "refusal", "tool_calls", "tool_call_id"}

func (Codec) DecodeRequest(body []byte, header http.Header, peer format.Peer) (*protocol.Request, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, errors.New("request must be a JSON object")
	}
	request := &protocol.Request{Model: protocol.String(root["model"])}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	if root["n"] != nil {
		if count, ok := protocol.Integer(root["n"]); !ok || count != 1 {
			return nil, errors.New("multiple candidates are not supported")
		}
	}
	limitKey := "max_completion_tokens"
	if root[limitKey] == nil {
		limitKey = "max_tokens"
	}
	if err := format.DecodeScalars(request, root, limitKey); err != nil {
		return nil, err
	}
	format.DecodeOpenAISampling(request, root)
	for _, item := range protocol.Array(root["messages"]) {
		message, err := decodeMessage(protocol.Object(item))
		if err != nil {
			return nil, err
		}
		request.Messages = append(request.Messages, message)
	}
	if len(request.Messages) == 0 {
		return nil, errors.New("at least one message is required")
	}
	if request.Stop, err = format.ParseStop(root["stop"]); err != nil {
		return nil, err
	}
	if request.Tools, err = format.DecodeTools(protocol.Chat, protocol.Array(root["tools"])); err != nil {
		return nil, err
	}
	if request.ToolChoice, err = format.ParseToolChoice(protocol.Chat, root["tool_choice"]); err != nil {
		return nil, err
	}
	if request.ResponseFormat, err = format.ParseResponseFormat(root["response_format"]); err != nil {
		return nil, err
	}
	if effort := protocol.String(root["reasoning_effort"]); effort != "" {
		request.Reasoning = &protocol.ReasoningConfig{Effort: effort}
	}
	request.Extras = format.UnmodelledFields(protocol.Chat, root, requestKeys...)
	return request, nil
}

func decodeMessage(entry map[string]any) (protocol.Message, error) {
	if entry == nil {
		return protocol.Message{}, errors.New("messages must be objects")
	}
	role := protocol.Role(protocol.String(entry["role"]))
	message := protocol.Message{Role: role, Extras: format.UnmodelledFields(protocol.Chat, entry, messageKeys...)}
	switch role {
	case protocol.RoleSystem, protocol.RoleDeveloper, protocol.RoleUser, protocol.RoleAssistant:
	case protocol.RoleTool:
		content, err := format.DecodeToolResultContent(protocol.Chat, entry["content"])
		if err != nil {
			return message, err
		}
		message.Blocks = []protocol.Block{{Kind: protocol.KindToolResult, ToolResult: &protocol.ToolResult{CallID: protocol.String(entry["tool_call_id"]), Content: content}}}
		return message, nil
	default:
		return message, errors.New("unsupported message role")
	}
	blocks, err := format.DecodeContent(protocol.Chat, entry["content"])
	if err != nil {
		return message, err
	}
	reasoning := protocol.String(entry["reasoning_content"])
	if reasoning == "" {
		reasoning = protocol.String(entry["reasoning"])
	}
	if reasoning != "" {
		blocks = append([]protocol.Block{{Kind: protocol.KindReasoning, Text: reasoning, Reasoning: &protocol.Reasoning{Visibility: "full"}}}, blocks...)
	}
	if refusal := protocol.String(entry["refusal"]); refusal != "" {
		blocks = append(blocks, protocol.Block{Kind: protocol.KindRefusal, Text: refusal})
	}
	for _, item := range protocol.Array(entry["tool_calls"]) {
		block, err := decodeToolCall(protocol.Object(item), true)
		if err != nil {
			return message, err
		}
		blocks = append(blocks, block)
	}
	if role == protocol.RoleAssistant {
		blocks = attachMessageAnnotations(blocks, entry)
	}
	message.Blocks = blocks
	return message, nil
}

// attachMessageAnnotations lifts Chat's message-level web-search annotations onto the last text
// block, which is where every other protocol stores citations.
func attachMessageAnnotations(blocks []protocol.Block, entry map[string]any) []protocol.Block {
	annotations := protocol.Array(entry["annotations"])
	if len(annotations) == 0 {
		return blocks
	}
	position := -1
	for index := len(blocks) - 1; index >= 0; index-- {
		if blocks[index].Kind == protocol.KindText {
			position = index
			break
		}
	}
	if position < 0 {
		return blocks
	}
	for _, item := range annotations {
		if object := protocol.Object(item); object != nil {
			blocks[position].Citations = append(blocks[position].Citations, format.DecodeCitation(protocol.Chat, object))
		}
	}
	return blocks
}

func decodeToolCall(call map[string]any, validate bool) (protocol.Block, error) {
	switch protocol.String(call["type"]) {
	case "custom":
		custom := protocol.Object(call["custom"])
		return protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolCustom, ID: protocol.String(call["id"]), Name: protocol.String(custom["name"]), Arguments: protocol.String(custom["input"])}}, nil
	case "function", "":
		function := protocol.Object(call["function"])
		arguments := protocol.String(function["arguments"])
		if validate && arguments != "" && !json.Valid([]byte(arguments)) {
			return protocol.Block{}, errors.New("tool arguments must be valid JSON")
		}
		return protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: protocol.String(call["id"]), Name: protocol.String(function["name"]), Arguments: format.EmptyObjectIfBlank(arguments)}}, nil
	}
	return protocol.Block{}, errors.New("unsupported tool call type")
}

func (codec Codec) EncodeRequest(request *protocol.Request, peer format.Peer) ([]byte, http.Header, []protocol.Loss, error) {
	root := map[string]any{"model": peer.Model, "stream": request.Stream}
	var losses []protocol.Loss
	context := format.NewPartContext(protocol.Chat, peer)
	messages := make([]any, 0, len(request.Messages)+1)
	if request.Instructions != "" {
		messages = append(messages, map[string]any{"role": "system", "content": request.Instructions})
	}
	for index, message := range request.Messages {
		path := "messages[" + strconv.Itoa(index) + "]"
		encoded, nested, err := encodeMessage(message, context, path)
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		messages = append(messages, encoded...)
	}
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
	nested, err := format.EncodeParams(request, protocol.Chat, root)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	if request.Stream {
		root["stream_options"] = map[string]any{"include_usage": true}
	}
	losses = append(losses, format.ApplyExtras(root, request.Extras, protocol.Chat, "extras")...)
	return protocol.Marshal(root), nil, losses, nil
}

// encodeMessage returns the Chat messages for one canonical message: tool results become their own
// "tool" messages placed before the remaining content.
func encodeMessage(message protocol.Message, context format.PartContext, path string) ([]any, []protocol.Loss, error) {
	var losses []protocol.Loss
	var results, parts, toolCalls []any
	var reasoning, refusal string
	context.Role = message.Role
	for index, block := range message.Blocks {
		blockPath := path + ".blocks[" + strconv.Itoa(index) + "]"
		context.Track(block)
		if format.Blank(block) {
			continue
		}
		switch {
		case block.Kind == protocol.KindReasoning && message.Role == protocol.RoleAssistant:
			reasoning += block.Text
			if block.Reasoning != nil && block.Reasoning.Signature != nil {
				losses = append(losses, protocol.Loss{Path: blockPath + ".signature", Code: protocol.LossSignatureStripped, Detail: "Chat has no field for reasoning signatures", To: protocol.Chat})
			}
			continue
		case block.Kind == protocol.KindRefusal && message.Role == protocol.RoleAssistant:
			refusal += block.Text
			continue
		}
		part, nested, err := format.EncodePart(block, context)
		if err != nil {
			return nil, losses, errors.New(blockPath + ": " + err.Error())
		}
		losses = append(losses, prefixed(nested, blockPath)...)
		switch {
		case part == nil:
		case block.Kind == protocol.KindToolResult:
			results = append(results, part)
		case block.Kind == protocol.KindToolCall:
			toolCalls = append(toolCalls, part)
		default:
			parts = append(parts, part)
		}
	}
	encoded := results
	if message.Role == protocol.RoleTool && len(results) == 1 {
		if result, ok := results[0].(map[string]any); ok {
			losses = append(losses, format.ApplyExtras(result, message.Extras, protocol.Chat, path+".extras")...)
		}
	}
	if len(parts) == 0 && len(toolCalls) == 0 && reasoning == "" && refusal == "" {
		return encoded, losses, nil
	}
	role := string(message.Role)
	if message.Role == protocol.RoleTool {
		role = "user"
	}
	entry := map[string]any{"role": role, "content": contentOf(parts)}
	if len(toolCalls) > 0 {
		entry["tool_calls"] = toolCalls
	}
	if reasoning != "" {
		entry["reasoning_content"] = reasoning
	}
	if refusal != "" {
		entry["refusal"] = refusal
	}
	if message.Role != protocol.RoleTool {
		losses = append(losses, format.ApplyExtras(entry, message.Extras, protocol.Chat, path+".extras")...)
	}
	return append(encoded, entry), losses, nil
}

// contentOf keeps a lone plain text part as a string, the one shape every Chat-compatible provider
// accepts; nil content is only legal next to tool calls.
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

func prefixed(losses []protocol.Loss, path string) []protocol.Loss {
	for index := range losses {
		losses[index].Path = path + "." + losses[index].Path
	}
	return losses
}
