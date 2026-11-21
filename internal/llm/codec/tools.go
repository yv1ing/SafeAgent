package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// EmptyObjectIfBlank normalizes the empty and "null" argument encodings every provider emits for a
// call that takes no parameters.
func EmptyObjectIfBlank(arguments string) string {
	if arguments == "" || arguments == "null" {
		return "{}"
	}
	return arguments
}

const emptySchema = `{"type":"object","properties":{}}`

const customToolSchema = `{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`

var (
	functionToolKeys = map[model.Format][]string{
		model.Chat:      {"type", "function.name", "function.description", "function.parameters", "function.strict"},
		model.Responses: {"type", "name", "description", "parameters", "strict"},
		model.Messages:  {"name", "description", "input_schema", "strict", "cache_control"},
		model.Gemini:    {"name", "description", "parameters", "parametersJsonSchema"},
	}
	geminiServerTools = map[string]string{"googleSearch": "googleSearch", "google_search": "googleSearch", "codeExecution": "codeExecution", "code_execution": "codeExecution", "urlContext": "urlContext", "url_context": "urlContext", "googleSearchRetrieval": "googleSearchRetrieval", "google_search_retrieval": "googleSearchRetrieval", "computerUse": "computerUse", "fileSearch": "fileSearch", "googleMaps": "googleMaps"}
)

// DecodeTools decodes a request's tool list. Gemini lists mix declarations with built-in tools in
// one array, the other formats list every tool separately.
func DecodeTools(kind model.Format, items []any) ([]model.Tool, error) {
	var tools []model.Tool
	for _, item := range items {
		entry := model.Object(item)
		if entry == nil {
			return nil, errors.New("tools must be objects")
		}
		if kind == model.Gemini {
			decoded, err := decodeGeminiTools(entry)
			if err != nil {
				return nil, err
			}
			tools = append(tools, decoded...)
			continue
		}
		tool, err := decodeTool(kind, entry)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func decodeGeminiTools(entry map[string]any) ([]model.Tool, error) {
	var tools []model.Tool
	for _, declaration := range model.Array(entry["functionDeclarations"]) {
		object := model.Object(declaration)
		if object == nil {
			return nil, errors.New("function declarations must be objects")
		}
		tool, err := decodeTool(model.Gemini, object)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	keys := make([]string, 0, len(entry))
	for key := range entry {
		if key != "functionDeclarations" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := entry[key]
		canonical := geminiServerTools[key]
		if canonical == "" {
			canonical = key
		}
		config := model.Marshal(map[string]any{key: value})
		tools = append(tools, model.Tool{Kind: model.ToolServer, Name: canonical, Server: &model.ServerTool{Format: model.Gemini, Type: canonical, Config: config}})
	}
	return tools, nil
}

func decodeTool(kind model.Format, entry map[string]any) (model.Tool, error) {
	declaration, blockType := entry, model.String(entry["type"])
	switch kind {
	case model.Chat:
		switch blockType {
		case "function", "":
			declaration = model.Object(entry["function"])
		case "custom":
			return decodeCustomTool(kind, model.Object(entry["custom"]), entry)
		default:
			return serverTool(kind, blockType, blockType, entry), nil
		}
	case model.Responses:
		switch blockType {
		case "function", "":
		case "custom":
			return decodeCustomTool(kind, entry, entry)
		default:
			return serverTool(kind, blockType, blockType, entry), nil
		}
	case model.Messages:
		if blockType != "" && blockType != "custom" {
			return serverTool(kind, blockType, model.String(entry["name"]), entry), nil
		}
	}
	if declaration == nil {
		return model.Tool{}, errors.New("function tool requires a declaration")
	}
	name := model.String(declaration["name"])
	if name == "" {
		return model.Tool{}, errors.New("tool name is required")
	}
	parameterKey := map[model.Format]string{model.Messages: "input_schema", model.Gemini: "parametersJsonSchema"}[kind]
	if parameterKey == "" {
		parameterKey = "parameters"
	}
	if kind == model.Gemini && declaration[parameterKey] == nil {
		parameterKey = "parameters"
	}
	if declaration[parameterKey] != nil && model.Object(declaration[parameterKey]) == nil {
		return model.Tool{}, errors.New("tool parameters must be a schema object")
	}
	tool := model.Tool{Kind: model.ToolFunction, Name: name, Description: model.String(declaration["description"])}
	if parameters := model.Object(declaration[parameterKey]); parameters != nil {
		if kind == model.Gemini && parameterKey == "parameters" {
			normalized, err := NormalizeGeminiSchema(parameters)
			if err != nil {
				return model.Tool{}, err
			}
			parameters = normalized
		}
		tool.Parameters = model.Marshal(parameters)
	}
	if strict, ok := declaration["strict"].(bool); ok {
		tool.Strict = &strict
	}
	tool.CacheControl = DecodeCacheControl(entry["cache_control"])
	tool.Raw = CaptureRaw(kind, entry, functionToolKeys[kind]...)
	return tool, nil
}

func decodeCustomTool(kind model.Format, definition, entry map[string]any) (model.Tool, error) {
	if definition == nil || model.String(definition["name"]) == "" {
		return model.Tool{}, errors.New("custom tool requires a name")
	}
	tool := model.Tool{Kind: model.ToolCustom, Name: model.String(definition["name"]), Description: model.String(definition["description"])}
	if grammar := model.Object(definition["format"]); grammar != nil {
		tool.Format = model.Marshal(grammar)
	}
	if kind == model.Chat {
		tool.Raw = CaptureRaw(kind, entry, "type", "custom.name", "custom.description", "custom.format")
	} else {
		tool.Raw = CaptureRaw(kind, entry, "type", "name", "description", "format")
	}
	return tool, nil
}

func serverTool(kind model.Format, blockType, name string, entry map[string]any) model.Tool {
	if name == "" {
		name = blockType
	}
	return model.Tool{Kind: model.ToolServer, Name: name, Server: &model.ServerTool{Format: kind, Type: blockType, Config: model.Marshal(entry)}}
}

func encodeServerTool(tool model.Tool, target model.Format, path string) (any, []model.Loss) {
	server := tool.Server
	if server == nil {
		return nil, []model.Loss{dropped(path, "server tool has no definition", target)}
	}
	if server.Format == target {
		return model.DecodeJSONOrNil(server.Config), nil
	}
	var mapped any
	switch model.ServerCapability(server) {
	case "web_search":
		mapped = map[model.Format]any{
			model.Responses: map[string]any{"type": "web_search"},
			model.Messages:  map[string]any{"type": "web_search_20250305", "name": "web_search"},
			model.Gemini:    map[string]any{"googleSearch": map[string]any{}},
		}[target]
	case "code_execution":
		mapped = map[model.Format]any{
			model.Responses: map[string]any{"type": "code_interpreter", "container": map[string]any{"type": "auto"}},
			model.Messages:  map[string]any{"type": "code_execution_20250825", "name": "code_execution"},
			model.Gemini:    map[string]any{"codeExecution": map[string]any{}},
		}[target]
	}
	if mapped == nil {
		return nil, []model.Loss{{Path: path, Code: model.LossFieldDropped, Class: model.LossDegraded, Detail: "provider-executed tool " + server.Type + " has no equivalent in the target format", From: server.Format, To: target}}
	}
	return mapped, []model.Loss{{Path: path, Code: model.LossToolKindMapped, Class: model.LossDegraded, Detail: "provider-executed tool " + server.Type + " mapped by capability; its configuration was not carried over", From: server.Format, To: target}}
}

func parametersOf(tool model.Tool, orEmpty bool) (any, error) {
	if len(bytes.TrimSpace(tool.Parameters)) == 0 || bytes.Equal(bytes.TrimSpace(tool.Parameters), []byte("null")) {
		if !orEmpty {
			return nil, nil
		}
		return model.DecodeJSON([]byte(emptySchema))
	}
	decoded, err := model.DecodeJSON(tool.Parameters)
	if err != nil {
		return nil, errors.New("tool " + tool.Name + ": parameters must be a JSON schema object")
	}
	return decoded, nil
}

// EncodeTools returns the target's tool list. Gemini's is the complete top-level tools array.
func EncodeTools(tools []model.Tool, target model.Format, capability model.ToolCapability) ([]any, []model.Loss, error) {
	var losses []model.Loss
	encoded := make([]any, 0, len(tools))
	var declarations, builtins []any
	for index, tool := range tools {
		path := "tools[" + strconv.Itoa(index) + "]"
		if tool.Name == "" {
			return nil, losses, errors.New(path + ": tool name is required")
		}
		if tool.Kind == model.ToolServer {
			entry, nested := encodeServerTool(tool, target, path)
			losses = append(losses, nested...)
			if entry == nil {
				continue
			}
			if target == model.Gemini {
				builtins = append(builtins, entry)
			} else {
				encoded = append(encoded, entry)
			}
			continue
		}
		entry, nested, err := encodeFunctionTool(tool, target, capability, path)
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		if target == model.Gemini {
			declarations = append(declarations, entry)
		} else {
			encoded = append(encoded, entry)
		}
	}
	if target == model.Gemini {
		if len(declarations) > 0 {
			encoded = append(encoded, map[string]any{"functionDeclarations": declarations})
		}
		encoded = append(encoded, builtins...)
	}
	return encoded, losses, nil
}

func encodeFunctionTool(tool model.Tool, target model.Format, capability model.ToolCapability, path string) (map[string]any, []model.Loss, error) {
	var losses []model.Loss
	custom := tool.Kind == model.ToolCustom
	customSupported := capability.Custom && (target == model.Chat || target == model.Responses)
	if custom && !customSupported {
		losses = append(losses, model.Loss{Path: path, Code: model.LossToolKindMapped, Class: model.LossDegraded, Detail: "a custom tool became a function taking one string input", To: target})
		if tool.Format != nil {
			losses = append(losses, dropped(path+".format", "target has no tool grammar", target))
		}
		custom = false
		tool.Kind = model.ToolFunction
		tool.Parameters, tool.Strict = json.RawMessage(customToolSchema), nil
	}
	if tool.Strict != nil && !capability.Strict {
		tool.Strict = nil
		losses = append(losses, dropped(path+".strict", "the target has no strict tool schema", target))
	}
	var entry map[string]any
	switch target {
	case model.Chat:
		if custom {
			definition := map[string]any{"name": tool.Name, "description": tool.Description}
			if tool.Format != nil {
				definition["format"] = model.DecodeJSONOrNil(tool.Format)
			}
			entry = map[string]any{"type": "custom", "custom": definition}
			break
		}
		parameters, err := parametersOf(tool, false)
		if err != nil {
			return nil, losses, err
		}
		function := map[string]any{"name": tool.Name, "description": tool.Description}
		if parameters != nil {
			function["parameters"] = parameters
		}
		if tool.Strict != nil {
			function["strict"] = *tool.Strict
		}
		entry = map[string]any{"type": "function", "function": function}
	case model.Responses:
		entry = map[string]any{"type": "function", "name": tool.Name, "description": tool.Description}
		if custom {
			entry["type"] = "custom"
			if tool.Format != nil {
				entry["format"] = model.DecodeJSONOrNil(tool.Format)
			}
			break
		}
		parameters, err := parametersOf(tool, true)
		if err != nil {
			return nil, losses, err
		}
		entry["parameters"] = parameters
		if tool.Strict != nil {
			entry["strict"] = *tool.Strict
		} else {
			entry["strict"] = false
		}
	case model.Messages:
		parameters, err := parametersOf(tool, true)
		if err != nil {
			return nil, losses, err
		}
		entry = map[string]any{"name": tool.Name, "input_schema": parameters}
		if tool.Description != "" {
			entry["description"] = tool.Description
		}
		if tool.Strict != nil {
			entry["strict"] = *tool.Strict
		}
		if tool.CacheControl != nil {
			entry["cache_control"] = EncodeCacheControl(tool.CacheControl)
		}
	case model.Gemini:
		parameters, err := parametersOf(tool, false)
		if err != nil {
			return nil, losses, err
		}
		entry = map[string]any{"name": tool.Name, "description": tool.Description}
		if parameters != nil {
			entry["parametersJsonSchema"] = parameters
		}
		if tool.Strict != nil {
			losses = append(losses, dropped(path+".strict", "target has no strict function schemas", target))
		}
	}
	if tool.CacheControl != nil && target != model.Messages {
		losses = append(losses, dropped(path+".cache_control", "target has no prompt cache markers", target))
	}
	losses = append(losses, RawFields(entry, tool.Raw, target, path+".raw")...)
	return entry, losses, nil
}
