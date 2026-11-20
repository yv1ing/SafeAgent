package format

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

// EmptyObjectIfBlank normalizes the empty and "null" argument encodings every provider emits for a call that
// takes no parameters.
func EmptyObjectIfBlank(arguments string) string {
	if arguments == "" || arguments == "null" {
		return "{}"
	}
	return arguments
}

const emptySchema = `{"type":"object","properties":{}}`

const customToolSchema = `{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`

var (
	functionToolKeys = map[protocol.Format][]string{
		protocol.Chat:      {"type", "function.name", "function.description", "function.parameters", "function.strict"},
		protocol.Responses: {"type", "name", "description", "parameters", "strict"},
		protocol.Messages:  {"name", "description", "input_schema", "strict", "cache_control"},
		protocol.Gemini:    {"name", "description", "parameters", "parametersJsonSchema"},
	}
	geminiServerTools = map[string]string{"googleSearch": "googleSearch", "google_search": "googleSearch", "codeExecution": "codeExecution", "code_execution": "codeExecution", "urlContext": "urlContext", "url_context": "urlContext", "googleSearchRetrieval": "googleSearchRetrieval", "google_search_retrieval": "googleSearchRetrieval", "computerUse": "computerUse", "fileSearch": "fileSearch", "googleMaps": "googleMaps"}
)

// DecodeTools decodes a request's tool list. Gemini lists mix declarations with built-in tools in
// one array, the other formats list every tool separately.
func DecodeTools(kind protocol.Format, items []any) ([]protocol.Tool, error) {
	var tools []protocol.Tool
	for _, item := range items {
		entry := protocol.Object(item)
		if entry == nil {
			return nil, errors.New("tools must be objects")
		}
		if kind == protocol.Gemini {
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

func decodeGeminiTools(entry map[string]any) ([]protocol.Tool, error) {
	var tools []protocol.Tool
	for _, declaration := range protocol.Array(entry["functionDeclarations"]) {
		object := protocol.Object(declaration)
		if object == nil {
			return nil, errors.New("function declarations must be objects")
		}
		tool, err := decodeTool(protocol.Gemini, object)
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
		config := protocol.Marshal(map[string]any{key: value})
		tools = append(tools, protocol.Tool{Kind: protocol.ToolServer, Name: canonical, Server: &protocol.ServerTool{Format: protocol.Gemini, Type: canonical, Config: config}})
	}
	return tools, nil
}

func decodeTool(kind protocol.Format, entry map[string]any) (protocol.Tool, error) {
	declaration, blockType := entry, protocol.String(entry["type"])
	switch kind {
	case protocol.Chat:
		switch blockType {
		case "function", "":
			declaration = protocol.Object(entry["function"])
		case "custom":
			return decodeCustomTool(kind, protocol.Object(entry["custom"]), entry)
		default:
			return serverTool(kind, blockType, blockType, entry), nil
		}
	case protocol.Responses:
		switch blockType {
		case "function", "":
		case "custom":
			return decodeCustomTool(kind, entry, entry)
		default:
			return serverTool(kind, blockType, blockType, entry), nil
		}
	case protocol.Messages:
		if blockType != "" && blockType != "custom" {
			return serverTool(kind, blockType, protocol.String(entry["name"]), entry), nil
		}
	}
	if declaration == nil {
		return protocol.Tool{}, errors.New("function tool requires a declaration")
	}
	name := protocol.String(declaration["name"])
	if name == "" {
		return protocol.Tool{}, errors.New("tool name is required")
	}
	parameterKey := map[protocol.Format]string{protocol.Messages: "input_schema", protocol.Gemini: "parametersJsonSchema"}[kind]
	if parameterKey == "" {
		parameterKey = "parameters"
	}
	if kind == protocol.Gemini && declaration[parameterKey] == nil {
		parameterKey = "parameters"
	}
	if declaration[parameterKey] != nil && protocol.Object(declaration[parameterKey]) == nil {
		return protocol.Tool{}, errors.New("tool parameters must be a schema object")
	}
	tool := protocol.Tool{Kind: protocol.ToolFunction, Name: name, Description: protocol.String(declaration["description"])}
	if parameters := protocol.Object(declaration[parameterKey]); parameters != nil {
		if kind == protocol.Gemini && parameterKey == "parameters" {
			normalized, err := NormalizeGeminiSchema(parameters)
			if err != nil {
				return protocol.Tool{}, err
			}
			parameters = normalized
		}
		tool.Parameters = protocol.Marshal(parameters)
	}
	if strict, ok := declaration["strict"].(bool); ok {
		tool.Strict = &strict
	}
	tool.CacheControl = decodeCacheControl(entry["cache_control"])
	known := functionToolKeys[kind]
	tool.Extras = UnmodelledFields(kind, entry, known...)
	return tool, nil
}

func decodeCustomTool(kind protocol.Format, definition, entry map[string]any) (protocol.Tool, error) {
	if definition == nil || protocol.String(definition["name"]) == "" {
		return protocol.Tool{}, errors.New("custom tool requires a name")
	}
	tool := protocol.Tool{Kind: protocol.ToolCustom, Name: protocol.String(definition["name"]), Description: protocol.String(definition["description"])}
	if grammar := protocol.Object(definition["format"]); grammar != nil {
		tool.Format = protocol.Marshal(grammar)
	}
	if kind == protocol.Chat {
		tool.Extras = UnmodelledFields(kind, entry, "type", "custom.name", "custom.description", "custom.format")
	} else {
		tool.Extras = UnmodelledFields(kind, entry, "type", "name", "description", "format")
	}
	return tool, nil
}

func serverTool(kind protocol.Format, blockType, name string, entry map[string]any) protocol.Tool {
	if name == "" {
		name = blockType
	}
	return protocol.Tool{Kind: protocol.ToolServer, Name: name, Server: &protocol.ServerTool{Format: kind, Type: blockType, Config: protocol.Marshal(entry)}}
}

// Provider-executed tools are portable by capability only; everything else needs the same format.
type capability string

const (
	capabilityWebSearch     capability = "web_search"
	capabilityCodeExecution capability = "code_execution"
)

func serverCapability(server *protocol.ServerTool) capability {
	switch server.Format {
	case protocol.Responses:
		switch {
		case strings.HasPrefix(server.Type, "web_search"):
			return capabilityWebSearch
		case server.Type == "code_interpreter":
			return capabilityCodeExecution
		}
	case protocol.Messages:
		switch {
		case strings.HasPrefix(server.Type, "web_search_"):
			return capabilityWebSearch
		case strings.HasPrefix(server.Type, "code_execution_"):
			return capabilityCodeExecution
		}
	case protocol.Gemini:
		switch server.Type {
		case "googleSearch":
			return capabilityWebSearch
		case "codeExecution":
			return capabilityCodeExecution
		}
	}
	return ""
}

func encodeServerTool(tool protocol.Tool, target protocol.Format, path string) (any, []protocol.Loss) {
	server := tool.Server
	if server == nil {
		return nil, []protocol.Loss{dropped(path, "server tool has no definition", target)}
	}
	if server.Format == target {
		return protocol.DecodeJSONOrNil(server.Config), nil
	}
	var mapped any
	switch serverCapability(server) {
	case capabilityWebSearch:
		mapped = map[protocol.Format]any{
			protocol.Responses: map[string]any{"type": "web_search"},
			protocol.Messages:  map[string]any{"type": "web_search_20250305", "name": "web_search"},
			protocol.Gemini:    map[string]any{"googleSearch": map[string]any{}},
		}[target]
	case capabilityCodeExecution:
		mapped = map[protocol.Format]any{
			protocol.Responses: map[string]any{"type": "code_interpreter", "container": map[string]any{"type": "auto"}},
			protocol.Messages:  map[string]any{"type": "code_execution_20250825", "name": "code_execution"},
			protocol.Gemini:    map[string]any{"codeExecution": map[string]any{}},
		}[target]
	}
	if mapped == nil {
		return nil, []protocol.Loss{{Path: path, Code: protocol.LossFieldDropped, Detail: "provider-executed tool " + server.Type + " has no equivalent in the target format", From: server.Format, To: target}}
	}
	return mapped, []protocol.Loss{{Path: path, Code: protocol.LossToolKindMapped, Detail: "provider-executed tool " + server.Type + " mapped by capability; its configuration was not carried over", From: server.Format, To: target}}
}

func parametersOf(tool protocol.Tool, orEmpty bool) (any, error) {
	if len(bytes.TrimSpace(tool.Parameters)) == 0 || bytes.Equal(bytes.TrimSpace(tool.Parameters), []byte("null")) {
		if !orEmpty {
			return nil, nil
		}
		return protocol.DecodeJSON([]byte(emptySchema))
	}
	decoded, err := protocol.DecodeJSON(tool.Parameters)
	if err != nil {
		return nil, errors.New("tool " + tool.Name + ": parameters must be a JSON schema object")
	}
	return decoded, nil
}

// EncodeTools returns the target's tool list. Gemini's is the complete top-level tools array.
func EncodeTools(tools []protocol.Tool, target protocol.Format) ([]any, []protocol.Loss, error) {
	var losses []protocol.Loss
	encoded := make([]any, 0, len(tools))
	var declarations, builtins []any
	for index, tool := range tools {
		path := "tools[" + strconv.Itoa(index) + "]"
		if tool.Name == "" {
			return nil, losses, errors.New(path + ": tool name is required")
		}
		if tool.Kind == protocol.ToolServer {
			entry, nested := encodeServerTool(tool, target, path)
			losses = append(losses, nested...)
			if entry == nil {
				continue
			}
			if target == protocol.Gemini {
				builtins = append(builtins, entry)
			} else {
				encoded = append(encoded, entry)
			}
			continue
		}
		entry, nested, err := encodeFunctionTool(tool, target, path)
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		if target == protocol.Gemini {
			declarations = append(declarations, entry)
		} else {
			encoded = append(encoded, entry)
		}
	}
	if target == protocol.Gemini {
		if len(declarations) > 0 {
			encoded = append(encoded, map[string]any{"functionDeclarations": declarations})
		}
		encoded = append(encoded, builtins...)
	}
	return encoded, losses, nil
}

func encodeFunctionTool(tool protocol.Tool, target protocol.Format, path string) (map[string]any, []protocol.Loss, error) {
	var losses []protocol.Loss
	custom := tool.Kind == protocol.ToolCustom
	if custom && target != protocol.Chat && target != protocol.Responses {
		losses = append(losses, protocol.Loss{Path: path, Code: protocol.LossToolKindMapped, Detail: "a custom tool became a function taking one string input", To: target})
		if tool.Format != nil {
			losses = append(losses, dropped(path+".format", "target has no tool grammar", target))
		}
		tool.Parameters, tool.Strict = json.RawMessage(customToolSchema), nil
	}
	var entry map[string]any
	switch target {
	case protocol.Chat:
		if custom {
			definition := map[string]any{"name": tool.Name, "description": tool.Description}
			if tool.Format != nil {
				definition["format"] = protocol.DecodeJSONOrNil(tool.Format)
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
	case protocol.Responses:
		entry = map[string]any{"type": "function", "name": tool.Name, "description": tool.Description}
		if custom {
			entry["type"] = "custom"
			if tool.Format != nil {
				entry["format"] = protocol.DecodeJSONOrNil(tool.Format)
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
	case protocol.Messages:
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
			entry["cache_control"] = encodeCacheControl(tool.CacheControl)
		}
	case protocol.Gemini:
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
	if tool.CacheControl != nil && target != protocol.Messages {
		losses = append(losses, dropped(path+".cache_control", "target has no prompt cache markers", target))
	}
	losses = append(losses, ApplyExtras(entry, tool.Extras, target, path+".extras")...)
	return entry, losses, nil
}
