// Package gemini implements the Google Gemini request and response codecs.
package gemini

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type Codec struct{}

func New() Codec { return Codec{} }

func (Codec) Format() protocol.Format { return protocol.Gemini }

var requestKeys = []string{"contents", "systemInstruction", "tools", "toolConfig.functionCallingConfig", "generationConfig.maxOutputTokens", "generationConfig.temperature", "generationConfig.topP", "generationConfig.topK", "generationConfig.stopSequences", "generationConfig.seed", "generationConfig.frequencyPenalty", "generationConfig.presencePenalty", "generationConfig.responseMimeType", "generationConfig.responseJsonSchema", "generationConfig.responseSchema", "generationConfig.thinkingConfig.thinkingLevel", "generationConfig.thinkingConfig.thinkingBudget", "generationConfig.thinkingConfig.includeThoughts"}

func (Codec) DecodeRequest(body []byte, header http.Header, peer format.Peer) (*protocol.Request, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, errors.New("request must be a JSON object")
	}
	request := &protocol.Request{Model: peer.Model}
	if instruction := protocol.Object(root["systemInstruction"]); instruction != nil {
		blocks, err := format.DecodeContent(protocol.Gemini, instruction["parts"])
		if err != nil {
			return nil, err
		}
		request.Messages = append(request.Messages, protocol.Message{Role: protocol.RoleSystem, Blocks: blocks, Extras: format.UnmodelledFields(protocol.Gemini, instruction, "parts")})
	}
	ids := format.NewCallIDs(peer)
	for _, item := range protocol.Array(root["contents"]) {
		entry := protocol.Object(item)
		role := protocol.Role(protocol.String(entry["role"]))
		switch role {
		case "model":
			role = protocol.RoleAssistant
		case "", protocol.RoleUser:
			role = protocol.RoleUser
		default:
			return nil, errors.New("unsupported message role")
		}
		blocks, err := format.DecodeContent(protocol.Gemini, entry["parts"])
		if err != nil {
			return nil, err
		}
		assignCallIDs(blocks, ids)
		request.Messages = append(request.Messages, protocol.Message{Role: role, Blocks: blocks, Extras: format.UnmodelledFields(protocol.Gemini, entry, "role", "parts")})
	}
	if config := protocol.Object(root["generationConfig"]); config != nil {
		if err := decodeConfig(request, config); err != nil {
			return nil, err
		}
	}
	if request.Tools, err = format.DecodeTools(protocol.Gemini, protocol.Array(root["tools"])); err != nil {
		return nil, err
	}
	if calling := protocol.Object(protocol.Object(root["toolConfig"])["functionCallingConfig"]); calling != nil {
		if request.ToolChoice, err = format.ParseToolChoice(protocol.Gemini, calling); err != nil {
			return nil, err
		}
	}
	matchResults(request.Messages)
	request.Extras = format.UnmodelledFields(protocol.Gemini, root, requestKeys...)
	return request, nil
}

// assignCallIDs names calls and results that Gemini left anonymous; results pair with calls by
// name and order, which is how Gemini itself resolves them.
func assignCallIDs(blocks []protocol.Block, ids *format.CallIDs) {
	for index := range blocks {
		if call := blocks[index].ToolCall; call != nil && call.ID == "" {
			call.ID = ids.Assign(call.Name, call.Arguments)
		}
	}
}

func matchResults(messages []protocol.Message) {
	pending := map[string][]string{}
	for _, message := range messages {
		for _, block := range message.Blocks {
			if block.ToolCall != nil {
				pending[block.ToolCall.Name] = append(pending[block.ToolCall.Name], block.ToolCall.ID)
			}
		}
	}
	taken := map[string]int{}
	for messageIndex := range messages {
		for blockIndex := range messages[messageIndex].Blocks {
			result := messages[messageIndex].Blocks[blockIndex].ToolResult
			if result == nil || result.CallID != "" {
				continue
			}
			if queue := pending[result.Name]; taken[result.Name] < len(queue) {
				result.CallID = queue[taken[result.Name]]
				taken[result.Name]++
			}
		}
	}
}

func decodeConfig(request *protocol.Request, config map[string]any) error {
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

// reasoningEffort maps Gemini's protobuf-JSON enum to the canonical effort tiers.
func reasoningEffort(value string) string {
	switch strings.ToUpper(value) {
	case "THINKING_LEVEL_UNSPECIFIED":
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

func (Codec) EncodeRequest(request *protocol.Request, peer format.Peer) ([]byte, http.Header, []protocol.Loss, error) {
	root := map[string]any{}
	var losses []protocol.Loss
	context := format.NewPartContext(protocol.Gemini, peer)
	var system []any
	var systemExtras protocol.Extras
	if request.Instructions != "" {
		system = append(system, map[string]any{"text": request.Instructions})
	}
	contents := make([]any, 0, len(request.Messages))
	for index, message := range request.Messages {
		path := "messages[" + strconv.Itoa(index) + "]"
		context.Role = message.Role
		if message.Role == protocol.RoleDeveloper {
			losses = append(losses, protocol.Loss{Path: path + ".role", Code: protocol.LossRoleMapped, Detail: "developer role represented as system instruction", To: protocol.Gemini})
		}
		parts, nested, err := format.EncodeBlocks(message.Blocks, context, path+".blocks")
		if err != nil {
			return nil, nil, losses, err
		}
		losses = append(losses, nested...)
		if message.Role == protocol.RoleSystem || message.Role == protocol.RoleDeveloper {
			system = append(system, parts...)
			if systemExtras.Fields == nil {
				systemExtras = message.Extras
			}
			continue
		}
		if len(parts) == 0 {
			continue
		}
		role := "user"
		if message.Role == protocol.RoleAssistant {
			role = "model"
		}
		contents = appendContent(contents, role, parts, message.Extras, path, &losses)
	}
	root["contents"] = contents
	if len(system) > 0 {
		instruction := map[string]any{"parts": system}
		losses = append(losses, format.ApplyExtras(instruction, systemExtras, protocol.Gemini, "systemInstruction.extras")...)
		root["systemInstruction"] = instruction
	}
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
	losses = append(losses, format.ApplyExtras(root, request.Extras, protocol.Gemini, "extras")...)
	return protocol.Marshal(root), nil, losses, nil
}

// appendContent merges consecutive turns of one role: Gemini requires every functionResponse of a
// model turn to share a single user turn.
func appendContent(contents []any, role string, parts []any, extras protocol.Extras, path string, losses *[]protocol.Loss) []any {
	if count := len(contents); count > 0 {
		previous := contents[count-1].(map[string]any)
		if previous["role"] == role {
			previous["parts"] = append(previous["parts"].([]any), parts...)
			*losses = append(*losses, format.ApplyExtras(previous, extras, protocol.Gemini, path+".extras")...)
			return contents
		}
	}
	entry := map[string]any{"role": role, "parts": parts}
	*losses = append(*losses, format.ApplyExtras(entry, extras, protocol.Gemini, path+".extras")...)
	return append(contents, entry)
}
