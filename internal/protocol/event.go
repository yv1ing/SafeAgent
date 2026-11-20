package protocol

type EventKind string

const (
	EventMessageStart EventKind = "message_start"
	EventItemStart    EventKind = "item_start"
	EventPartStart    EventKind = "part_start"
	EventPartDelta    EventKind = "part_delta"
	EventPartStop     EventKind = "part_stop"
	EventItemStop     EventKind = "item_stop"
	EventMessageDelta EventKind = "message_delta"
	EventMessageStop  EventKind = "message_stop"
	EventError        EventKind = "error"
	EventPing         EventKind = "ping"
)

// Event is one step of a canonical response stream. Items and parts open and close independently so
// a decoder can report concurrent tool calls or interleaved reasoning. A part's streamed content
// travels only in EventPartDelta: EventPartStart carries the part without it and the snapshot on
// EventPartStop carries exactly the concatenated deltas. An item's event sequence is self-contained
// so an encoder may hold one item back and release it as a group.
type Event struct {
	Kind      EventKind     `json:"kind"`
	Sequence  int64         `json:"sequence,omitempty"`
	ItemID    string        `json:"item_id,omitempty"`
	ItemIndex int           `json:"item_index"`
	PartID    string        `json:"part_id,omitempty"`
	PartIndex int           `json:"part_index"`
	Item      *Item         `json:"item,omitempty"`
	Part      *Part         `json:"part,omitempty"`
	Delta     string        `json:"delta,omitempty"`
	Meta      *ResponseMeta `json:"meta,omitempty"`
	Usage     *Usage        `json:"usage,omitempty"`
	Finish    *Finish       `json:"finish,omitempty"`
	Failure   *Error        `json:"failure,omitempty"`
	Losses    []Loss        `json:"losses,omitempty"`
}

func (event Event) IsContent() bool {
	switch event.Kind {
	case EventPartDelta:
		return event.Delta != ""
	case EventItemStart:
		return event.Item != nil && event.Item.Kind.IsTool()
	}
	return false
}

// StreamedContent is the part of a part that a stream delivers incrementally.
func StreamedContent(part Part) string {
	switch part.Kind {
	case PartText, PartRefusal, PartReasoningText, PartReasoningSummary, PartToolArguments:
		return part.Text
	}
	return ""
}

// WithStreamedContent returns a copy of part whose streamed content is content.
func WithStreamedContent(part Part, content string) Part {
	clone := part.Clone()
	switch clone.Kind {
	case PartText, PartRefusal, PartReasoningText, PartReasoningSummary, PartToolArguments:
		clone.Text = content
	}
	return clone
}

// MergePart overlays the final snapshot of a part onto its streamed state. Unmodelled fields of
// the snapshot win so a provider's closing frame can add citations or cache metadata.
func MergePart(target *Part, final Part) {
	if final.Kind != "" {
		target.Kind = final.Kind
	}
	if final.Text != "" {
		target.Text = final.Text
	}
	if final.Media != nil {
		target.Media = final.Media
	}
	if len(final.Citations) > 0 {
		target.Citations = final.Citations
	}
	if final.CacheControl != nil {
		target.CacheControl = final.CacheControl
	}
	if len(final.Native) > 0 {
		target.Native = final.Native
	}
	if final.Raw != nil {
		target.Raw = final.Raw
	}
}

// MergeItem overlays the final snapshot of an item onto its streamed state.
func MergeItem(target *Item, final Item) {
	if final.Kind != "" {
		target.Kind = final.Kind
	}
	if final.Role != "" {
		target.Role = final.Role
	}
	if final.Status != "" {
		target.Status = final.Status
	}
	if final.ToolCall != nil {
		if target.ToolCall == nil {
			target.ToolCall = final.ToolCall
		} else {
			if final.ToolCall.ID != "" {
				target.ToolCall.ID = final.ToolCall.ID
			}
			if final.ToolCall.Name != "" {
				target.ToolCall.Name = final.ToolCall.Name
			}
			if final.ToolCall.Arguments != "" {
				target.ToolCall.Arguments = final.ToolCall.Arguments
			}
			if final.ToolCall.Kind != "" {
				target.ToolCall.Kind = final.ToolCall.Kind
			}
			if final.ToolCall.Signature != nil {
				target.ToolCall.Signature = final.ToolCall.Signature
			}
		}
	}
	if final.CallID != "" {
		target.CallID = final.CallID
	}
	if final.Name != "" {
		target.Name = final.Name
	}
	target.IsError = target.IsError || final.IsError
	if final.Reasoning != nil {
		target.Reasoning = final.Reasoning
	}
	if final.CacheControl != nil {
		target.CacheControl = final.CacheControl
	}
	if len(final.Native) > 0 {
		target.Native = final.Native
	}
	if len(final.Parts) > 0 {
		target.Parts = final.Parts
	}
	if final.Raw != nil {
		target.Raw = final.Raw
	}
}
