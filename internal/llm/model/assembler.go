package model

import (
	"encoding/json"
	"strings"
)

type assemblerPart struct {
	part    Part
	build   strings.Builder
	stopped bool
}

type assemblerItem struct {
	item   Item
	parts  []*assemblerPart
	closed bool
}

// Assembler numbers the items and parts of one decoded stream and guarantees the event contract:
// a part is stopped exactly once, an item is stopped exactly once, and streamed content travels
// only in deltas. Items and parts may be open concurrently; callers that need a sequential target
// order the events themselves.
type Assembler struct {
	items []*assemblerItem
}

func NewAssembler() *Assembler { return &Assembler{} }

func (stream *Assembler) StartItem(item Item) (int, []Event) {
	index := len(stream.items)
	state := &assemblerItem{item: stripItemContent(item)}
	state.item.Ordinal = index
	stream.items = append(stream.items, state)
	started := state.item.Clone()
	return index, []Event{{Kind: EventItemStart, ItemIndex: index, ItemID: started.ID, Item: &started}}
}

// StartPart opens a part whose static metadata travels in the start event.
func (stream *Assembler) StartPart(itemIndex int, part Part) (int, []Event, error) {
	state := stream.item(itemIndex)
	if state == nil || state.closed {
		return 0, nil, &StreamError{Code: "item_closed", Message: "The stream contains a part for an item that is not open."}
	}
	index := len(state.parts)
	stored := stripPartContent(part)
	stored.Ordinal = index
	state.parts = append(state.parts, &assemblerPart{part: stored})
	started := stored.Clone()
	return index, []Event{{Kind: EventPartStart, ItemIndex: itemIndex, ItemID: state.item.ID, PartIndex: index, PartID: started.ID, Part: &started}}, nil
}

func (stream *Assembler) Delta(itemIndex, partIndex int, text string) ([]Event, error) {
	part := stream.part(itemIndex, partIndex)
	if part == nil || part.stopped {
		return nil, &StreamError{Code: "part_closed", Message: "The stream contains a delta for a part that is not open."}
	}
	if text == "" {
		return nil, nil
	}
	part.build.WriteString(text)
	if part.part.Kind == PartToolArguments {
		if call := stream.item(itemIndex).item.ToolCall; call != nil {
			call.Arguments += text
		}
	}
	return []Event{{Kind: EventPartDelta, ItemIndex: itemIndex, ItemID: stream.item(itemIndex).item.ID, PartIndex: partIndex, PartID: part.part.ID, Delta: text}}, nil
}

func (stream *Assembler) UpdateItem(itemIndex int, change func(*Item)) {
	if state := stream.item(itemIndex); state != nil && !state.closed {
		change(&state.item)
	}
}

func (stream *Assembler) UpdatePart(itemIndex, partIndex int, change func(*Part)) {
	if part := stream.part(itemIndex, partIndex); part != nil && !part.stopped {
		change(&part.part)
	}
}

func (stream *Assembler) StopPart(itemIndex, partIndex int) ([]Event, error) {
	part := stream.part(itemIndex, partIndex)
	if part == nil || part.stopped {
		return nil, nil
	}
	var events []Event
	final, streamed := StreamedContent(part.part), part.build.String()
	if final != "" && strings.HasPrefix(final, streamed) && final != streamed {
		events = append(events, Event{Kind: EventPartDelta, ItemIndex: itemIndex, PartIndex: partIndex, Delta: final[len(streamed):]})
		part.build.WriteString(final[len(streamed):])
		streamed = final
	}
	part.part = WithStreamedContent(part.part, streamed)
	part.stopped = true
	snapshot := part.part.Clone()
	return append(events, Event{Kind: EventPartStop, ItemIndex: itemIndex, ItemID: stream.item(itemIndex).item.ID, PartIndex: partIndex, PartID: part.part.ID, Part: &snapshot}), nil
}

func (stream *Assembler) StopItem(itemIndex int, final *Item) ([]Event, error) {
	state := stream.item(itemIndex)
	if state == nil || state.closed {
		return nil, nil
	}
	var events []Event
	for index := range state.parts {
		nested, err := stream.StopPart(itemIndex, index)
		if err != nil {
			return nil, err
		}
		events = append(events, nested...)
	}
	if final != nil {
		snapshot := final.Clone()
		if final.Kind == ItemToolCall || final.Kind == ItemServerToolCall {
			for _, part := range state.parts {
				if part.part.Kind == PartToolArguments && part.build.Len() > 0 && snapshot.ToolCall != nil && snapshot.ToolCall.Arguments == "" {
					snapshot.ToolCall.Arguments = part.build.String()
				}
			}
		}
		MergeItem(&state.item, snapshot)
	}
	parts := make([]Part, 0, len(state.parts))
	for _, part := range state.parts {
		if part.part.Kind == PartToolArguments {
			continue
		}
		parts = append(parts, part.part.Clone())
	}
	state.item.Parts = parts
	state.closed = true
	snapshot := state.item.Clone()
	return append(events, Event{Kind: EventItemStop, ItemIndex: itemIndex, ItemID: state.item.ID, Item: &snapshot}), nil
}

// StopAll stops every open item in index order.
func (stream *Assembler) StopAll() ([]Event, error) {
	var events []Event
	for index := range stream.items {
		nested, err := stream.StopItem(index, nil)
		if err != nil {
			return events, err
		}
		events = append(events, nested...)
	}
	return events, nil
}

func (stream *Assembler) Item(index int) *Item {
	if state := stream.item(index); state != nil {
		return &state.item
	}
	return nil
}

func (stream *Assembler) Part(itemIndex, partIndex int) *Part {
	if part := stream.part(itemIndex, partIndex); part != nil {
		return &part.part
	}
	return nil
}

func (stream *Assembler) Streamed(itemIndex, partIndex int) string {
	if part := stream.part(itemIndex, partIndex); part != nil {
		return part.build.String()
	}
	return ""
}

func (stream *Assembler) Items() []Item {
	items := make([]Item, 0, len(stream.items))
	for _, state := range stream.items {
		items = append(items, state.item.Clone())
	}
	return items
}

func (stream *Assembler) HasToolCall() bool {
	for _, state := range stream.items {
		if state.item.Kind == ItemToolCall {
			return true
		}
	}
	return false
}

func (stream *Assembler) item(index int) *assemblerItem {
	if index < 0 || index >= len(stream.items) {
		return nil
	}
	return stream.items[index]
}

func (stream *Assembler) part(itemIndex, partIndex int) *assemblerPart {
	state := stream.item(itemIndex)
	if state == nil || partIndex < 0 || partIndex >= len(state.parts) {
		return nil
	}
	return state.parts[partIndex]
}

func stripItemContent(item Item) Item {
	clone := item.Clone()
	for index := range clone.Parts {
		clone.Parts[index] = stripPartContent(clone.Parts[index])
	}
	if clone.ToolCall != nil {
		clone.ToolCall.Arguments = ""
	}
	return clone
}

func stripPartContent(part Part) Part {
	return WithStreamedContent(part, "")
}

// CleanEOF reports whether a stream that ended without its terminal frame still carries a complete,
// usable response: at least one content item and every function call with complete JSON arguments.
func CleanEOF(stream *Assembler) bool {
	content := false
	for index := range stream.items {
		state := stream.items[index]
		switch state.item.Kind {
		case ItemMessage, ItemReasoning:
			if state.item.HasText() || (state.item.Reasoning != nil && state.item.Reasoning.Signature != nil) || state.hasStreamedText() {
				content = true
			}
		case ItemToolCall, ItemServerToolCall:
			if state.item.ToolCall == nil {
				continue
			}
			arguments := stream.toolArguments(state)
			if state.item.ToolCall.Kind != ToolCustom && arguments != "" && !json.Valid([]byte(arguments)) {
				return false
			}
			content = true
		case ItemToolResult, ItemServerToolResult, ItemNative:
			content = true
		}
	}
	return content
}

func (state *assemblerItem) hasStreamedText() bool {
	for _, part := range state.parts {
		if part.build.Len() > 0 {
			return true
		}
	}
	return false
}

func (stream *Assembler) toolArguments(state *assemblerItem) string {
	if state.item.ToolCall == nil {
		return ""
	}
	if state.item.ToolCall.Arguments != "" {
		return state.item.ToolCall.Arguments
	}
	for _, part := range state.parts {
		if part.part.Kind == PartToolArguments {
			return part.build.String()
		}
	}
	return ""
}
