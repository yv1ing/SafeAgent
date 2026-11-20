package format

import (
	"errors"
	"fmt"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

// RestrictTools implements an allowed-tools choice for targets that cannot express it: Messages
// receives only the permitted function tools, so the model cannot call anything else.
func RestrictTools(tools []protocol.Tool, choice *protocol.ToolChoice) []protocol.Tool {
	if choice == nil || choice.Mode != "allowed" {
		return tools
	}
	allowed := make(map[string]bool, len(choice.Names))
	for _, name := range choice.Names {
		allowed[name] = true
	}
	restricted := make([]protocol.Tool, 0, len(tools))
	for _, tool := range tools {
		if allowed[tool.Name] {
			restricted = append(restricted, tool)
		}
	}
	return restricted
}

func ParseToolChoice(kind protocol.Format, value any) (*protocol.ToolChoice, error) {
	if value == nil {
		return nil, nil
	}
	if text, isText := value.(string); isText {
		return plainChoice(text)
	}
	object := protocol.Object(value)
	if object == nil {
		return nil, errors.New("tool choice must be a string or an object")
	}
	if kind == protocol.Gemini {
		return parseGeminiChoice(object)
	}
	mode := protocol.String(object["type"])
	switch mode {
	case "function", "tool", "custom":
		name := protocol.String(object["name"])
		if name == "" {
			name = protocol.String(protocol.Object(object["function"])["name"])
		}
		if name == "" {
			name = protocol.String(protocol.Object(object["custom"])["name"])
		}
		if name == "" {
			return nil, errors.New("named tool choice requires a tool name")
		}
		return &protocol.ToolChoice{Mode: "tool", Names: []string{name}}, nil
	case "allowed_tools":
		return parseAllowedTools(object)
	case "auto", "none", "any", "required":
		return plainChoice(mode)
	}
	return nil, fmt.Errorf("unsupported tool selection %q", mode)
}

func plainChoice(mode string) (*protocol.ToolChoice, error) {
	switch mode {
	case "auto", "none", "required":
		return &protocol.ToolChoice{Mode: mode}, nil
	case "any":
		return &protocol.ToolChoice{Mode: "required"}, nil
	}
	return nil, fmt.Errorf("unsupported tool selection %q", mode)
}

func parseAllowedTools(object map[string]any) (*protocol.ToolChoice, error) {
	nested := protocol.Object(object["allowed_tools"])
	if nested == nil {
		nested = object
	}
	choice := &protocol.ToolChoice{Mode: "allowed"}
	switch mode := protocol.String(nested["mode"]); mode {
	case "", "auto":
	case "required":
		choice.Required = true
	default:
		return nil, errors.New("invalid allowed tool mode")
	}
	for _, tool := range protocol.Array(nested["tools"]) {
		entry := protocol.Object(tool)
		name := protocol.String(entry["name"])
		if name == "" {
			name = protocol.String(protocol.Object(entry["function"])["name"])
		}
		if name == "" {
			name = protocol.String(protocol.Object(entry["custom"])["name"])
		}
		if name == "" {
			return nil, errors.New("allowed tool requires a name")
		}
		choice.Names = append(choice.Names, name)
	}
	if len(choice.Names) == 0 {
		return nil, errors.New("allowed tools require at least one name")
	}
	return choice, nil
}

func parseGeminiChoice(object map[string]any) (*protocol.ToolChoice, error) {
	var names []string
	for _, name := range protocol.Array(object["allowedFunctionNames"]) {
		text, ok := name.(string)
		if !ok {
			return nil, errors.New("allowed tool names must be strings")
		}
		names = append(names, text)
	}
	mode := protocol.String(object["mode"])
	if mode == "" {
		mode = "AUTO"
	}
	choice := &protocol.ToolChoice{}
	switch mode {
	case "AUTO", "VALIDATED":
		choice.Mode = "auto"
	case "NONE":
		choice.Mode = "none"
	case "ANY":
		choice.Mode, choice.Required = "required", true
	default:
		return nil, fmt.Errorf("unsupported Gemini tool mode %q", mode)
	}
	if len(names) == 0 {
		return choice, nil
	}
	if choice.Mode == "none" {
		return nil, errors.New("Gemini allowedFunctionNames requires ANY, AUTO or VALIDATED mode")
	}
	if len(names) == 1 && choice.Required {
		return &protocol.ToolChoice{Mode: "tool", Names: names}, nil
	}
	choice.Mode, choice.Names = "allowed", names
	return choice, nil
}

func toolKindOf(tools []protocol.Tool, name string) protocol.ToolKind {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Kind
		}
	}
	return protocol.ToolFunction
}

func encodeToolChoice(choice *protocol.ToolChoice, tools []protocol.Tool, target protocol.Format) (any, []protocol.Loss, error) {
	var losses []protocol.Loss
	switch choice.Mode {
	case "auto", "none":
		return plainEncodedChoice(choice.Mode, target), nil, nil
	case "required":
		return plainEncodedChoice("required", target), nil, nil
	case "tool":
		if len(choice.Names) != 1 {
			return nil, nil, errors.New("a named tool choice requires exactly one name")
		}
		name := choice.Names[0]
		custom := toolKindOf(tools, name) == protocol.ToolCustom
		switch target {
		case protocol.Chat:
			if custom {
				return map[string]any{"type": "custom", "custom": map[string]any{"name": name}}, nil, nil
			}
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}, nil, nil
		case protocol.Responses:
			if custom {
				return map[string]any{"type": "custom", "name": name}, nil, nil
			}
			return map[string]any{"type": "function", "name": name}, nil, nil
		case protocol.Messages:
			return map[string]any{"type": "tool", "name": name}, nil, nil
		case protocol.Gemini:
			return map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY", "allowedFunctionNames": []string{name}}}, nil, nil
		}
	case "allowed":
		if len(choice.Names) == 0 {
			return nil, nil, errors.New("allowed tools require at least one name")
		}
		mode := "auto"
		if choice.Required {
			mode = "required"
		}
		switch target {
		case protocol.Chat, protocol.Responses:
			entries := make([]any, 0, len(choice.Names))
			for _, name := range choice.Names {
				entry := map[string]any{"type": "function", "name": name}
				if target == protocol.Chat {
					entry = map[string]any{"type": "function", "function": map[string]any{"name": name}}
				}
				if toolKindOf(tools, name) == protocol.ToolCustom {
					entry = map[string]any{"type": "custom", "name": name}
					if target == protocol.Chat {
						entry = map[string]any{"type": "custom", "custom": map[string]any{"name": name}}
					}
				}
				entries = append(entries, entry)
			}
			if target == protocol.Chat {
				return map[string]any{"type": "allowed_tools", "allowed_tools": map[string]any{"mode": mode, "tools": entries}}, nil, nil
			}
			return map[string]any{"type": "allowed_tools", "mode": mode, "tools": entries}, nil, nil
		case protocol.Messages:
			losses = append(losses, dropped("tool_choice", "target cannot name a tool subset; only the allowed tools are sent", target))
			return plainEncodedChoice(mode, target), losses, nil
		case protocol.Gemini:
			geminiMode := "VALIDATED"
			if choice.Required {
				geminiMode = "ANY"
			}
			return map[string]any{"functionCallingConfig": map[string]any{"mode": geminiMode, "allowedFunctionNames": choice.Names}}, nil, nil
		}
	}
	return nil, nil, errors.New("target cannot represent tool selection")
}

func plainEncodedChoice(mode string, target protocol.Format) any {
	switch target {
	case protocol.Messages:
		if mode == "required" {
			mode = "any"
		}
		return map[string]any{"type": mode}
	case protocol.Gemini:
		return map[string]any{"functionCallingConfig": map[string]any{"mode": map[string]string{"auto": "AUTO", "none": "NONE", "required": "ANY"}[mode]}}
	}
	return mode
}
