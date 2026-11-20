package observation

import "strings"

// ItemRef is an item with its semantic position. Detectors use the position for evidence and
// stable fingerprints.
type ItemRef struct {
	Turn     int
	Position int
	Item     Item
}

// ItemRefs returns every item of the trace in turn order, including tool result content.
func (trace *Trace) ItemRefs() []ItemRef {
	refs := make([]ItemRef, 0, len(trace.Turns)*2)
	for turnIndex := range trace.Turns {
		turn := &trace.Turns[turnIndex]
		for itemIndex := range turn.Items {
			appendRefs(&refs, turnIndex, itemIndex, turn.Items[itemIndex])
		}
	}
	return refs
}

func appendRefs(refs *[]ItemRef, turn, item int, value Item) {
	*refs = append(*refs, ItemRef{Turn: turn, Position: item, Item: value})
	if value.ToolResult != nil {
		for _, child := range value.ToolResult.Content {
			appendRefs(refs, turn, item, child)
		}
	}
}

// Items returns every item of the trace in turn order, including tool result content.
func (trace *Trace) Items() []Item {
	items := make([]Item, 0, len(trace.Turns)*2)
	for index := range trace.Turns {
		items = append(items, flatten(trace.Turns[index].Items)...)
	}
	return items
}

func flatten(items []Item) []Item {
	result := make([]Item, 0, len(items))
	for _, item := range items {
		result = append(result, item)
		if item.ToolResult != nil {
			result = append(result, flatten(item.ToolResult.Content)...)
		}
	}
	return result
}

func (trace *Trace) ToolCallByID(id string) (ToolCall, bool) {
	for _, item := range trace.Items() {
		if item.Kind == ItemToolCall && item.ToolCall != nil && item.ToolCall.ID == id {
			return *item.ToolCall, true
		}
	}
	return ToolCall{}, false
}

func (trace *Trace) ToolSpecByName(name string) (ToolSpec, bool) {
	for _, tool := range trace.Tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return ToolSpec{}, false
}

// RebuildSteps recomputes the ordered tool projection. Adapters may fill Steps directly; this keeps
// them in step with the turns after a mutation such as injecting the declared tool catalogue.
func (trace *Trace) RebuildSteps() {
	steps := []ToolStep{}
	callIndex := map[string]int{}
	for turnIndex := range trace.Turns {
		turn := &trace.Turns[turnIndex]
		for itemIndex := range turn.Items {
			item := &turn.Items[itemIndex]
			switch item.Kind {
			case ItemToolCall:
				if item.ToolCall == nil {
					continue
				}
				steps = append(steps, ToolStep{
					Turn: turnIndex, Item: itemIndex, CallID: item.ToolCall.ID,
					Name: item.ToolCall.Name, Kind: item.ToolCall.Kind,
					Target: item.ToolCall.Target, TargetShown: item.ToolCall.TargetShown, Sink: item.ToolCall.Sink,
				})
				callIndex[item.ToolCall.ID] = len(steps) - 1
			case ItemToolResult:
				if item.ToolResult == nil {
					continue
				}
				step := ToolStep{
					Turn: turnIndex, Item: itemIndex, CallID: item.ToolResult.CallID,
					Name: item.ToolResult.Name, IsError: item.ToolResult.IsError,
					Result: NewValue(ToolResultText(item.ToolResult), "", nil), Sink: SinkNone,
				}
				if position, found := callIndex[item.ToolResult.CallID]; found {
					step.Name = steps[position].Name
					step.Kind = steps[position].Kind
					step.Target = steps[position].Target
					step.TargetShown = steps[position].TargetShown
					step.Sink = steps[position].Sink
					steps[position].Result = step.Result
					steps[position].IsError = step.IsError
					continue
				}
				steps = append(steps, step)
			}
		}
	}
	trace.Steps = steps
}

func ToolResultText(result *ToolResult) string {
	if result == nil {
		return ""
	}
	return strings.Join(itemTexts(result.Content), "\n")
}

func itemTexts(items []Item) []string {
	texts := []string{}
	for _, item := range items {
		if item.Text.Raw != "" {
			texts = append(texts, item.Text.Raw)
		}
		if item.ToolResult != nil {
			texts = append(texts, itemTexts(item.ToolResult.Content)...)
		}
	}
	return texts
}
