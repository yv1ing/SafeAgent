package protocol

import (
	"strconv"
)

// RepairRequest restores the invariants every target protocol expects from a client history and
// records each change. A conversation may arrive with unanswered calls, orphan results, duplicate
// results, empty turns or reasoning placed after visible content; forwarding any of those would make
// the upstream reject otherwise valid conversations.
func RepairRequest(request *Request, target Format) []Repair {
	var repairs []Repair
	items := make([]Item, 0, len(request.Items))
	for index, item := range request.Items {
		if item.Empty() && !replayableContainer(item, target) {
			repairs = append(repairs, Repair{Kind: "dropped_empty_item", Path: itemKey(index), ItemKey: item.ID})
			continue
		}
		items = append(items, item)
	}

	calls := map[string]int{}
	results := map[string]int{}
	for index, item := range items {
		switch {
		case item.ToolCall != nil && item.ToolCall.ID != "" && item.Kind == ItemToolCall:
			calls[item.ToolCall.ID] = index
		case item.CallID != "" && item.Kind == ItemToolResult:
			if _, duplicate := results[item.CallID]; duplicate {
				continue
			}
			results[item.CallID] = index
		}
	}

	kept := make([]Item, 0, len(items))
	for index, item := range items {
		switch {
		case item.ToolCall != nil && item.Kind == ItemToolCall:
			id := item.ToolCall.ID
			if id == "" {
				kept = append(kept, item)
				continue
			}
			if _, found := results[id]; !found {
				repairs = append(repairs, Repair{Kind: "dropped_unanswered_call", Path: itemKey(index), Detail: item.ToolCall.Name, ItemKey: item.ID})
				continue
			}
			kept = append(kept, item)
		case item.Kind == ItemToolResult:
			if _, found := calls[item.CallID]; !found {
				repairs = append(repairs, Repair{Kind: "dropped_orphan_result", Path: itemKey(index), Detail: item.CallID, ItemKey: item.ID})
				continue
			}
			if results[item.CallID] != index {
				repairs = append(repairs, Repair{Kind: "dropped_duplicate_result", Path: itemKey(index), Detail: item.CallID, ItemKey: item.ID})
				continue
			}
			kept = append(kept, item)
		default:
			kept = append(kept, item)
		}
	}

	kept = relocateResults(kept, &repairs)
	if target == Responses || target == Messages {
		kept = liftReasoning(kept, &repairs)
	}
	if request.ToolChoice != nil && request.ToolChoice.Mode != "auto" && request.ToolChoice.Mode != "none" {
		if !choiceAvailable(request.ToolChoice, request.Tools) {
			repairs = append(repairs, Repair{Kind: "relaxed_tool_choice", Path: "tool_choice", Detail: request.ToolChoice.Mode})
			request.ToolChoice = nil
		}
	}
	for index := range kept {
		kept[index].Ordinal = index
	}
	request.Items = kept
	return repairs
}

// replayableContainer keeps an otherwise empty message that carries fields only its own wire
// format can replay. Cross-format targets cannot use those fields, so the item is dropped.
func replayableContainer(item Item, target Format) bool {
	return item.Kind == ItemMessage && item.Raw != nil && !item.Raw.IsEmpty() && item.Raw.Format == target
}

// relocateResults places every tool result directly after the assistant turn that issued its call.
// All four protocols require that adjacency, and clients occasionally interleave user text before
// the results of a tool loop.
func relocateResults(items []Item, repairs *[]Repair) []Item {
	results := map[string]Item{}
	for _, item := range items {
		if item.Kind == ItemToolResult && item.CallID != "" {
			results[item.CallID] = item
		}
	}
	emitted := map[string]bool{}
	out := make([]Item, 0, len(items))
	for index := 0; index < len(items); {
		item := items[index]
		if item.Role != RoleAssistant || item.Kind == ItemServerToolResult {
			if item.Kind == ItemToolResult {
				if _, planned := results[item.CallID]; planned {
					index++
					continue
				}
			}
			out = append(out, item)
			index++
			continue
		}
		end := index
		for end < len(items) && items[end].Role == RoleAssistant && items[end].Kind != ItemServerToolResult && items[end].Kind != ItemToolResult {
			end++
		}
		out = append(out, items[index:end]...)
		var order []string
		for position := index; position < end; position++ {
			if items[position].Kind == ItemToolCall && items[position].ToolCall != nil {
				order = append(order, items[position].ToolCall.ID)
			}
		}
		before := len(out)
		for _, callID := range order {
			result, found := results[callID]
			if !found || emitted[callID] {
				continue
			}
			out = append(out, result)
			emitted[callID] = true
		}
		if extras := len(out) - before; extras > 0 && !resultsFollow(items, end, order) {
			for range extras {
				*repairs = append(*repairs, Repair{Kind: "moved_tool_result", Path: "items", Detail: "tool results follow their assistant turn"})
			}
		}
		index = end
	}
	return out
}

func resultsFollow(items []Item, start int, order []string) bool {
	position := start
	for _, callID := range order {
		if position >= len(items) || items[position].Kind != ItemToolResult || items[position].CallID != callID {
			return false
		}
		position++
	}
	return true
}

func choiceAvailable(choice *ToolChoice, tools []Tool) bool {
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, name := range choice.Names {
		if !names[name] {
			return false
		}
	}
	return true
}

// liftReasoning moves thinking blocks ahead of the visible assistant content of the same turn, the
// order Messages requires for an echoed assistant message that contains both.
func liftReasoning(items []Item, repairs *[]Repair) []Item {
	result := make([]Item, 0, len(items))
	for index := 0; index < len(items); {
		item := items[index]
		if item.Role != RoleAssistant {
			result = append(result, item)
			index++
			continue
		}
		end := index
		for end < len(items) && items[end].Role == RoleAssistant {
			end++
		}
		turn := items[index:end]
		var reasoning, rest []Item
		for position, entry := range turn {
			if entry.Kind == ItemReasoning {
				reasoning = append(reasoning, entry)
				if position > 0 {
					*repairs = append(*repairs, Repair{Kind: "moved_reasoning_first", Path: "items[" + strconv.Itoa(index+position) + "]", ItemKey: entry.ID})
				}
				continue
			}
			rest = append(rest, entry)
		}
		result = append(result, reasoning...)
		result = append(result, rest...)
		index = end
	}
	return result
}

// MissingToolReasoning reports whether the request contains a tool call in an assistant turn
// without any reasoning item. Providers that require reasoning replay reject such histories, and
// some clients (notably Gemini CLI) drop thought parts before sending the next turn.
func MissingToolReasoning(request *Request) bool {
	reasoning := false
	for _, item := range request.Items {
		switch item.Kind {
		case ItemReasoning:
			reasoning = true
		case ItemToolCall, ItemServerToolCall:
			if !reasoning {
				return true
			}
		case ItemToolResult, ItemMessage:
			if item.Kind == ItemToolResult || item.Role == RoleUser || item.Role == RoleTool {
				reasoning = false
			}
		}
	}
	return false
}
