package protocol

type EventKind string

const (
	EventMessageStart EventKind = "message_start"
	EventBlockStart   EventKind = "block_start"
	EventBlockDelta   EventKind = "block_delta"
	EventBlockStop    EventKind = "block_stop"
	EventMessageDelta EventKind = "message_delta"
	EventMessageStop  EventKind = "message_stop"
)

// Event is one step of a canonical response stream. A block's streamed content (StreamedContent)
// travels only in EventBlockDelta: EventBlockStart carries the block without it, and the snapshot
// on EventBlockStop carries exactly the concatenated deltas. A block's events are self-contained:
// encoders emit frames for a block only while handling that block's events, so a caller may hold a
// block's frames back and release them as one group.
type Event struct {
	Kind    EventKind     `json:"kind"`
	Index   int           `json:"index"`
	Block   *Block        `json:"block,omitempty"`
	Text    string        `json:"text,omitempty"`
	Meta    *ResponseMeta `json:"meta,omitempty"`
	Usage   *Usage        `json:"usage,omitempty"`
	Finish  FinishReason  `json:"finish,omitempty"`
	Failure *Error        `json:"failure,omitempty"`
	// Stop carries the matched stop sequence when the provider reports one.
	Stop string `json:"stop,omitempty"`
	// Losses reports what the decoder had to approximate; it is set on EventMessageDelta.
	Losses []Loss `json:"losses,omitempty"`
}

func streams(block Block) bool {
	switch block.Kind {
	case KindText, KindReasoning, KindRefusal:
		return true
	case KindToolCall:
		return block.ToolCall != nil
	}
	return false
}

// StreamedContent is the part of a block that a stream delivers incrementally: the text of text,
// reasoning and refusal blocks, or the arguments of a tool call.
func StreamedContent(block Block) string {
	if !streams(block) {
		return ""
	}
	if block.Kind == KindToolCall {
		return block.ToolCall.Arguments
	}
	return block.Text
}

// WithStreamedContent returns a copy of block whose streamed content is content.
func WithStreamedContent(block Block, content string) Block {
	clone := CloneBlock(block)
	if !streams(clone) {
		return clone
	}
	if clone.Kind == KindToolCall {
		clone.ToolCall.Arguments = content
	} else {
		clone.Text = content
	}
	return clone
}

// MergeBlock overlays the final snapshot of a block onto its streamed state.
func MergeBlock(target *Block, final Block) {
	if final.Text != "" {
		target.Text = final.Text
	}
	if final.Kind != "" {
		target.Kind = final.Kind
	}
	if final.Reasoning != nil {
		target.Reasoning = final.Reasoning
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
	if final.ToolResult != nil {
		target.ToolResult = final.ToolResult
	}
	if final.Media != nil {
		target.Media = final.Media
	}
	if final.CacheControl != nil {
		target.CacheControl = final.CacheControl
	}
	if len(final.Citations) > 0 {
		target.Citations = final.Citations
	}
	if len(final.Extras.Fields) > 0 {
		target.Extras = final.Extras
	}
}
