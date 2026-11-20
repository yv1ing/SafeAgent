// Package responses implements the OpenAI Responses request and response codecs.
package responses

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

func (Codec) Format() protocol.Format { return protocol.Responses }

// store and include are fixed by EncodeRequest: the gateway never lets an upstream keep state, and
// it needs the encrypted reasoning content to replay reasoning.
var requestKeys = []string{"model", "previous_response_id", "stream", "instructions", "input", "max_output_tokens", "temperature", "top_p", "parallel_tool_calls", "tools", "reasoning.effort", "reasoning.summary", "text.format", "text.verbosity", "user", "metadata", "service_tier", "prompt_cache_key", "store", "include", "truncation", "max_tool_calls", "safety_identifier", "prompt_cache_retention", "logprobs", "top_logprobs", "logit_bias", "modalities", "background", "conversation"}

var itemKeys = []string{"type", "role", "content"}

func (Codec) DecodeRequest(body []byte, header http.Header, peer format.Peer) (*protocol.Request, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, errors.New("request must be a JSON object")
	}
	request := &protocol.Request{Model: protocol.String(root["model"]), PreviousResponseID: protocol.String(root["previous_response_id"])}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	if root["background"] == true || root["conversation"] != nil {
		return nil, errors.New("background responses and provider conversation state are unsupported")
	}
	if value, ok := root["store"].(bool); ok {
		request.Store = &value
	}
	for _, item := range protocol.Array(root["include"]) {
		if text := protocol.String(item); text != "" {
			request.Include = append(request.Include, text)
		}
	}
	request.Truncation = protocol.String(root["truncation"])
	request.MaxToolCalls = protocol.OptionalInteger(root["max_tool_calls"])
	request.SafetyIdentifier = protocol.String(root["safety_identifier"])
	request.PromptCacheRetention = protocol.String(root["prompt_cache_retention"])
	if value, ok := root["logprobs"].(bool); ok {
		request.Logprobs = &value
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
	for _, item := range protocol.Array(root["modalities"]) {
		if text := protocol.String(item); text != "" {
			request.Modalities = append(request.Modalities, text)
		}
	}
	if err := format.DecodeScalars(request, root, "max_output_tokens"); err != nil {
		return nil, err
	}
	format.DecodeOpenAISampling(request, root)
	if instructions := protocol.String(root["instructions"]); instructions != "" {
		request.Messages = append(request.Messages, protocol.Message{Role: protocol.RoleSystem, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: instructions}}})
	}
	if text, ok := root["input"].(string); ok {
		request.Messages = append(request.Messages, protocol.Message{Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: text}}})
	} else {
		messages, err := decodeItems(protocol.Array(root["input"]))
		if err != nil {
			return nil, err
		}
		request.Messages = append(request.Messages, messages...)
	}
	if request.Tools, err = format.DecodeTools(protocol.Responses, protocol.Array(root["tools"])); err != nil {
		return nil, err
	}
	choice, err := format.ParseToolChoice(protocol.Responses, root["tool_choice"])
	if err != nil && !namesProviderTool(root["tool_choice"]) {
		return nil, err
	}
	request.ToolChoice = choice
	if reasoning := protocol.Object(root["reasoning"]); reasoning != nil {
		request.Reasoning = &protocol.ReasoningConfig{Effort: protocol.String(reasoning["effort"]), Summary: protocol.String(reasoning["summary"])}
	}
	if text := protocol.Object(root["text"]); text != nil {
		if request.ResponseFormat, err = format.ParseResponseFormat(text["format"]); err != nil {
			return nil, err
		}
		request.Verbosity = protocol.String(text["verbosity"])
	}
	request.Extras = format.UnmodelledFields(protocol.Responses, root, requestKeys...)
	if request.ToolChoice != nil || root["tool_choice"] == nil {
		delete(request.Extras.Fields, "tool_choice")
	} else {
		request.Extras = withField(request.Extras, "tool_choice", root["tool_choice"])
	}
	return request, nil
}

// namesProviderTool reports a tool choice that selects a provider-executed tool; the canonical
// choice can only name functions, so such a choice travels as an extra.
func namesProviderTool(value any) bool {
	switch protocol.String(protocol.Object(value)["type"]) {
	case "", "function", "custom", "allowed_tools":
		return false
	}
	return true
}

func withField(extras protocol.Extras, key string, value any) protocol.Extras {
	if extras.Fields == nil {
		extras = protocol.Extras{Format: protocol.Responses, Fields: map[string]json.RawMessage{}}
	}
	extras.Fields[key] = protocol.Marshal(value)
	return extras
}

// decodeItems turns Responses items into messages: a message item is one message, every other item
// is a single block of an assistant message, or of a tool message for outputs.
func decodeItems(items []any) ([]protocol.Message, error) {
	messages := make([]protocol.Message, 0, len(items))
	// A Responses turn is a flat sequence of items, while every other protocol attaches reasoning
	// and tool calls to one assistant message. Consecutive assistant items are therefore grouped;
	// a tool output ends the group because it belongs to the next user turn.
	assistant := -1
	for _, item := range items {
		entry := protocol.Object(item)
		if entry == nil {
			return nil, errors.New("input items must be objects")
		}
		if kind := protocol.String(entry["type"]); kind == "message" || kind == "" {
			blocks, err := format.DecodeContent(protocol.Responses, entry["content"])
			if err != nil {
				return nil, err
			}
			role := protocol.Role(protocol.String(entry["role"]))
			switch role {
			case protocol.RoleSystem, protocol.RoleDeveloper, protocol.RoleUser, protocol.RoleAssistant:
			default:
				return nil, errors.New("unsupported message role")
			}
			if role == protocol.RoleAssistant && assistant >= 0 {
				messages[assistant].Blocks = append(messages[assistant].Blocks, blocks...)
				continue
			}
			messages = append(messages, protocol.Message{Role: role, Blocks: blocks, Extras: format.UnmodelledFields(protocol.Responses, entry, itemKeys...)})
			if role == protocol.RoleAssistant {
				assistant = len(messages) - 1
			} else {
				assistant = -1
			}
			continue
		}
		block, err := format.DecodeBlock(protocol.Responses, entry)
		if err != nil {
			return nil, err
		}
		if block.Kind == protocol.KindToolResult {
			messages = append(messages, protocol.Message{Role: protocol.RoleTool, Blocks: []protocol.Block{block}})
			assistant = -1
			continue
		}
		if assistant >= 0 {
			messages[assistant].Blocks = append(messages[assistant].Blocks, block)
			continue
		}
		messages = append(messages, protocol.Message{Role: protocol.RoleAssistant, Blocks: []protocol.Block{block}})
		assistant = len(messages) - 1
	}
	return messages, nil
}

func (Codec) EncodeRequest(request *protocol.Request, peer format.Peer) ([]byte, http.Header, []protocol.Loss, error) {
	include := []any{"reasoning.encrypted_content"}
	for _, item := range request.Include {
		if item != "reasoning.encrypted_content" {
			include = append(include, item)
		}
	}
	root := map[string]any{"model": peer.Model, "stream": request.Stream, "store": false, "include": include}
	var stateLosses []protocol.Loss
	if request.Store != nil && *request.Store {
		stateLosses = append(stateLosses, protocol.Loss{Path: "store", Code: protocol.LossFieldDropped, Detail: "the gateway replays history itself and never relies on upstream state", To: protocol.Responses})
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
	context := format.NewPartContext(protocol.Responses, peer)
	input := make([]any, 0, len(request.Messages))
	instructions, messages := leadingInstructions(request)
	for index, message := range messages {
		message.Blocks = format.DropBlank(message.Blocks)
		items, nested, err := encodeItems(message, context, "messages["+strconv.Itoa(index)+"]")
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		input = append(input, items...)
	}
	root["input"] = input
	losses = append(losses, stateLosses...)
	if instructions != "" {
		root["instructions"] = instructions
	}
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
	if request.PreviousResponseID != "" {
		losses = append(losses, protocol.Loss{Path: "previous_response_id", Code: protocol.LossFieldDropped, Detail: "upstream state is never used; the history is replayed in full", To: protocol.Responses})
	}
	nested, err := format.EncodeParams(request, protocol.Responses, root)
	if err != nil {
		return nil, nil, losses, err
	}
	losses = append(losses, nested...)
	losses = append(losses, format.ApplyExtras(root, request.Extras, protocol.Responses, "extras")...)
	return protocol.Marshal(root), nil, losses, nil
}

// leadingInstructions moves the text-only system messages at the head of the request into the
// instructions field, where Responses keeps them, and returns the messages that remain.
func leadingInstructions(request *protocol.Request) (string, []protocol.Message) {
	instructions := request.Instructions
	messages := request.Messages
	for len(messages) > 0 && messages[0].Role == protocol.RoleSystem && len(messages[0].Extras.Fields) == 0 {
		text, plain := plainText(messages[0].Blocks)
		if !plain {
			break
		}
		if instructions != "" && text != "" {
			instructions += "\n\n"
		}
		instructions += text
		messages = messages[1:]
	}
	return instructions, messages
}

func plainText(blocks []protocol.Block) (string, bool) {
	text := ""
	for _, block := range blocks {
		if block.Kind != protocol.KindText || block.CacheControl != nil || len(block.Citations) > 0 || len(block.Extras.Fields) > 0 {
			return "", false
		}
		if text != "" {
			text += "\n\n"
		}
		text += block.Text
	}
	return text, true
}

// encodeItems returns the items of one message: adjacent content parts share a message item, and
// every other block interrupts it.
func encodeItems(message protocol.Message, context format.PartContext, path string) ([]any, []protocol.Loss, error) {
	var items, parts []any
	var losses []protocol.Loss
	context.Role = message.Role
	role := string(message.Role)
	if message.Role == protocol.RoleTool {
		role = string(protocol.RoleUser)
	}
	extras := message.Extras
	flush := func() {
		if len(parts) == 0 {
			return
		}
		item := map[string]any{"type": "message", "role": role, "content": parts}
		losses = append(losses, format.ApplyExtras(item, extras, protocol.Responses, path+".extras")...)
		extras = protocol.Extras{}
		items, parts = append(items, item), nil
	}
	for index, block := range message.Blocks {
		context.Track(block)
		blockPath := path + ".blocks[" + strconv.Itoa(index) + "]"
		part, nested, err := format.EncodePart(block, context)
		if err != nil {
			return nil, losses, errors.New(blockPath + ": " + err.Error())
		}
		losses = append(losses, prefixed(nested, blockPath)...)
		switch {
		case part == nil:
		case isContentPart(block, part):
			parts = append(parts, part)
		default:
			flush()
			items = append(items, part)
		}
	}
	flush()
	return items, losses, nil
}

func isContentPart(block protocol.Block, part map[string]any) bool {
	switch block.Kind {
	case protocol.KindText, protocol.KindRefusal, protocol.KindMedia:
		return true
	case protocol.KindReasoning:
		return part["type"] == "output_text"
	}
	return false
}

func prefixed(losses []protocol.Loss, path string) []protocol.Loss {
	for index := range losses {
		if losses[index].Path == "" {
			losses[index].Path = path
		} else {
			losses[index].Path = path + "." + losses[index].Path
		}
	}
	return losses
}
