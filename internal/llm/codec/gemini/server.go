package gemini

import (
	"encoding/json"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// serverPart recognizes a provider-executed tool step in a Gemini content part. The whole part is
// kept so the same format replays it exactly while other formats map or drop it by capability.
func serverPart(role model.Role, entry map[string]any) (model.Item, bool) {
	kind, serverType, capability := model.ItemKind(""), "", ""
	switch {
	case entry["executableCode"] != nil:
		kind, serverType, capability = model.ItemServerToolCall, "codeExecution", "code_execution"
	case entry["codeExecutionResult"] != nil:
		kind, serverType, capability = model.ItemServerToolResult, "codeExecutionResult", "code_execution"
	default:
		return model.Item{}, false
	}
	item := model.Item{
		Kind: kind, Role: role,
		Server: &model.ServerInfo{Format: model.Gemini, Type: serverType, Capability: capability, Config: model.Marshal(entry)},
		Raw:    &model.Raw{Format: model.Gemini, Fields: map[string]json.RawMessage{"native": model.Marshal(entry)}},
	}
	if code := model.Object(entry["executableCode"]); code != nil {
		item.ToolCall = &model.ToolCall{Kind: model.ToolServer, Name: "code_execution", Arguments: string(model.Marshal(map[string]any{"code": code["code"], "language": code["language"]}))}
	}
	return item, true
}
