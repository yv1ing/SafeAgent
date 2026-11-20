package format

import (
	"encoding/json"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

type partState struct {
	part    protocol.Part
	build   strings.Builder
	stopped bool
}

type itemState struct {
	item   protocol.Item
	parts  []*partState
	closed bool
}

// ItemStream numbers the items and parts of one decoded stream and guarantees the event contract:
// a part is stopped exactly once, an item is stopped exactly once, and streamed content travels
// only in deltas. Items and parts may be open concurrently; callers that need a sequential target
// order the events themselves.
type ItemStream struct {
	items []*itemState
}

func NewItemStream() *ItemStream { return &ItemStream{} }

func (stream *ItemStream) StartItem(item protocol.Item) (int, []protocol.Event) {
	index := len(stream.items)
	state := &itemState{item: stripItemContent(item)}
	state.item.Ordinal = index
	stream.items = append(stream.items, state)
	started := state.item.Clone()
	return index, []protocol.Event{{Kind: protocol.EventItemStart, ItemIndex: index, ItemID: started.ID, Item: &started}}
}

// StartPart opens a part whose static metadata travels in the start event.
func (stream *ItemStream) StartPart(itemIndex int, part protocol.Part) (int, []protocol.Event, error) {
	state := stream.item(itemIndex)
	if state == nil || state.closed {
		return 0, nil, &StreamError{Code: "item_closed", Message: "The stream contains a part for an item that is not open."}
	}
	index := len(state.parts)
	stored := stripPartContent(part)
	stored.Ordinal = index
	state.parts = append(state.parts, &partState{part: stored})
	started := stored.Clone()
	return index, []protocol.Event{{Kind: protocol.EventPartStart, ItemIndex: itemIndex, ItemID: state.item.ID, PartIndex: index, PartID: started.ID, Part: &started}}, nil
}

func (stream *ItemStream) Delta(itemIndex, partIndex int, text string) ([]protocol.Event, error) {
	part := stream.part(itemIndex, partIndex)
	if part == nil || part.stopped {
		return nil, &StreamError{Code: "part_closed", Message: "The stream contains a delta for a part that is not open."}
	}
	if text == "" {
		return nil, nil
	}
	part.build.WriteString(text)
	if part.part.Kind == protocol.PartToolArguments {
		if call := stream.item(itemIndex).item.ToolCall; call != nil {
			call.Arguments += text
		}
	}
	return []protocol.Event{{Kind: protocol.EventPartDelta, ItemIndex: itemIndex, ItemID: stream.item(itemIndex).item.ID, PartIndex: partIndex, PartID: part.part.ID, Delta: text}}, nil
}

func (stream *ItemStream) UpdateItem(itemIndex int, change func(*protocol.Item)) {
	if state := stream.item(itemIndex); state != nil && !state.closed {
		change(&state.item)
	}
}

func (stream *ItemStream) UpdatePart(itemIndex, partIndex int, change func(*protocol.Part)) {
	if part := stream.part(itemIndex, partIndex); part != nil && !part.stopped {
		change(&part.part)
	}
}

func (stream *ItemStream) StopPart(itemIndex, partIndex int) ([]protocol.Event, error) {
	part := stream.part(itemIndex, partIndex)
	if part == nil || part.stopped {
		return nil, nil
	}
	var events []protocol.Event
	final, streamed := protocol.StreamedContent(part.part), part.build.String()
	if final != "" && strings.HasPrefix(final, streamed) && final != streamed {
		events = append(events, protocol.Event{Kind: protocol.EventPartDelta, ItemIndex: itemIndex, PartIndex: partIndex, Delta: final[len(streamed):]})
		part.build.WriteString(final[len(streamed):])
		streamed = final
	}
	part.part = protocol.WithStreamedContent(part.part, streamed)
	part.stopped = true
	snapshot := part.part.Clone()
	return append(events, protocol.Event{Kind: protocol.EventPartStop, ItemIndex: itemIndex, ItemID: stream.item(itemIndex).item.ID, PartIndex: partIndex, PartID: part.part.ID, Part: &snapshot}), nil
}

func (stream *ItemStream) StopItem(itemIndex int, final *protocol.Item) ([]protocol.Event, error) {
	state := stream.item(itemIndex)
	if state == nil || state.closed {
		return nil, nil
	}
	var events []protocol.Event
	for index := range state.parts {
		nested, err := stream.StopPart(itemIndex, index)
		if err != nil {
			return nil, err
		}
		events = append(events, nested...)
	}
	if final != nil {
		snapshot := final.Clone()
		if final.Kind == protocol.ItemToolCall || final.Kind == protocol.ItemServerToolCall {
			for _, part := range state.parts {
				if part.part.Kind == protocol.PartToolArguments && part.build.Len() > 0 && snapshot.ToolCall != nil && snapshot.ToolCall.Arguments == "" {
					snapshot.ToolCall.Arguments = part.build.String()
				}
			}
		}
		protocol.MergeItem(&state.item, snapshot)
	}
	parts := make([]protocol.Part, 0, len(state.parts))
	for _, part := range state.parts {
		if part.part.Kind == protocol.PartToolArguments {
			continue
		}
		parts = append(parts, part.part.Clone())
	}
	state.item.Parts = parts
	state.closed = true
	snapshot := state.item.Clone()
	return append(events, protocol.Event{Kind: protocol.EventItemStop, ItemIndex: itemIndex, ItemID: state.item.ID, Item: &snapshot}), nil
}

// StopAll stops every open item in index order.
func (stream *ItemStream) StopAll() ([]protocol.Event, error) {
	var events []protocol.Event
	for index := range stream.items {
		nested, err := stream.StopItem(index, nil)
		if err != nil {
			return events, err
		}
		events = append(events, nested...)
	}
	return events, nil
}

func (stream *ItemStream) Item(index int) *protocol.Item {
	if state := stream.item(index); state != nil {
		return &state.item
	}
	return nil
}

func (stream *ItemStream) Part(itemIndex, partIndex int) *protocol.Part {
	if part := stream.part(itemIndex, partIndex); part != nil {
		return &part.part
	}
	return nil
}

func (stream *ItemStream) Streamed(itemIndex, partIndex int) string {
	if part := stream.part(itemIndex, partIndex); part != nil {
		return part.build.String()
	}
	return ""
}

func (stream *ItemStream) Items() []protocol.Item {
	items := make([]protocol.Item, 0, len(stream.items))
	for _, state := range stream.items {
		items = append(items, state.item.Clone())
	}
	return items
}

func (stream *ItemStream) HasToolCall() bool {
	for _, state := range stream.items {
		if state.item.Kind == protocol.ItemToolCall {
			return true
		}
	}
	return false
}

func (stream *ItemStream) item(index int) *itemState {
	if index < 0 || index >= len(stream.items) {
		return nil
	}
	return stream.items[index]
}

func (stream *ItemStream) part(itemIndex, partIndex int) *partState {
	state := stream.item(itemIndex)
	if state == nil || partIndex < 0 || partIndex >= len(state.parts) {
		return nil
	}
	return state.parts[partIndex]
}

func stripItemContent(item protocol.Item) protocol.Item {
	clone := item.Clone()
	for index := range clone.Parts {
		clone.Parts[index] = stripPartContent(clone.Parts[index])
	}
	if clone.ToolCall != nil {
		clone.ToolCall.Arguments = ""
	}
	return clone
}

func stripPartContent(part protocol.Part) protocol.Part {
	return protocol.WithStreamedContent(part, "")
}

// CleanEOF reports whether a stream that ended without its terminal frame still carries a complete,
// usable response: at least one content item and every function call with complete JSON arguments.
func CleanEOF(stream *ItemStream) bool {
	content := false
	for index := range stream.items {
		state := stream.items[index]
		switch state.item.Kind {
		case protocol.ItemMessage, protocol.ItemReasoning:
			if state.item.HasText() || (state.item.Reasoning != nil && state.item.Reasoning.Signature != nil) || state.hasStreamedText() {
				content = true
			}
		case protocol.ItemToolCall, protocol.ItemServerToolCall:
			if state.item.ToolCall == nil {
				continue
			}
			arguments := stream.toolArguments(state)
			if state.item.ToolCall.Kind != protocol.ToolCustom && arguments != "" && !json.Valid([]byte(arguments)) {
				return false
			}
			content = true
		case protocol.ItemToolResult, protocol.ItemServerToolResult, protocol.ItemNative:
			content = true
		}
	}
	return content
}

func (state *itemState) hasStreamedText() bool {
	for _, part := range state.parts {
		if part.build.Len() > 0 {
			return true
		}
	}
	return false
}

func (stream *ItemStream) toolArguments(state *itemState) string {
	if state.item.ToolCall == nil {
		return ""
	}
	if state.item.ToolCall.Arguments != "" {
		return state.item.ToolCall.Arguments
	}
	for _, part := range state.parts {
		if part.part.Kind == protocol.PartToolArguments {
			return part.build.String()
		}
	}
	return ""
}
