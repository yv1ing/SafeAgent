package messages

import (
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type streamEncoder struct {
	meta    protocol.ResponseMeta
	peer    format.Peer
	blocks  map[int]*protocol.Block
	wire    map[int]int
	closed  map[int]bool
	next    int
	started bool
	stopped bool
	usage   *protocol.Usage
	finish  protocol.FinishReason
	stop    string
	failure *protocol.Error
}

func (Codec) NewStreamEncoder(meta protocol.ResponseMeta, peer format.Peer) format.StreamEncoder {
	return &streamEncoder{meta: meta, peer: peer, blocks: map[int]*protocol.Block{}, wire: map[int]int{}, closed: map[int]bool{}}
}

func (encoder *streamEncoder) Encode(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	switch event.Kind {
	case protocol.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
		}
		return encoder.messageStart(), nil, nil
	case protocol.EventBlockStart:
		if event.Block == nil {
			return nil, nil, nil
		}
		return encoder.startBlock(event.Index, *event.Block)
	case protocol.EventBlockDelta:
		block := encoder.blocks[event.Index]
		if block == nil || encoder.closed[event.Index] || event.Text == "" {
			return nil, nil, nil
		}
		return encoder.deltaFrames(event.Index, block, event.Text), nil, nil
	case protocol.EventBlockStop:
		return encoder.stopBlock(event)
	case protocol.EventMessageDelta:
		encoder.usage, encoder.finish, encoder.stop, encoder.failure = event.Usage, event.Finish, event.Stop, event.Failure
		var losses []protocol.Loss
		if format.FinishLoss(event.Finish, protocol.Messages) {
			losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Detail: string(event.Finish), To: protocol.Messages})
		}
		return nil, losses, nil
	case protocol.EventMessageStop:
		return encoder.terminal(), nil, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) Finish() ([]format.Frame, []protocol.Loss, error) {
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) messageStart() []format.Frame {
	if encoder.started {
		return nil
	}
	encoder.started = true
	usage := map[string]any{"input_tokens": 0, "output_tokens": 0}
	return []format.Frame{eventFrame("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": encoder.meta.ID, "type": "message", "role": "assistant", "model": encoder.meta.Model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": usage}})}
}

// startContent is the block as content_block_start carries it: text and tool input start empty and
// arrive as deltas, every other block is delivered whole.
func (encoder *streamEncoder) startContent(block protocol.Block) (map[string]any, []protocol.Loss) {
	context := format.NewPartContext(protocol.Messages, encoder.peer)
	context.Role = protocol.RoleAssistant
	switch block.Kind {
	case protocol.KindText:
		return map[string]any{"type": "text", "text": ""}, nil
	case protocol.KindRefusal:
		return map[string]any{"type": "text", "text": ""}, nil
	case protocol.KindReasoning:
		return map[string]any{"type": "thinking", "thinking": ""}, nil
	case protocol.KindToolCall:
		if block.ToolCall != nil && block.ToolCall.Kind != protocol.ToolServer {
			if block.ToolCall.Kind == protocol.ToolCustom {
				return map[string]any{"type": "tool_use", "id": block.ToolCall.ID, "name": block.ToolCall.Name, "input": map[string]any{}}, []protocol.Loss{{Path: "tool_call.kind", Code: protocol.LossToolKindMapped, Detail: "a custom tool became a function call", To: protocol.Messages}}
			}
			return map[string]any{"type": "tool_use", "id": block.ToolCall.ID, "name": block.ToolCall.Name, "input": map[string]any{}}, nil
		}
	}
	part, losses, err := format.EncodePart(block, context)
	if err != nil {
		return nil, append(losses, protocol.Loss{Path: "block", Code: protocol.LossFieldDropped, Detail: err.Error(), To: protocol.Messages})
	}
	if block.Kind == protocol.KindToolCall && part != nil {
		part["input"] = map[string]any{}
	}
	return part, losses
}

func (encoder *streamEncoder) startBlock(index int, block protocol.Block) ([]format.Frame, []protocol.Loss, error) {
	content, losses := encoder.startContent(block)
	stored := protocol.CloneBlock(block)
	encoder.blocks[index] = &stored
	if content == nil {
		encoder.closed[index] = true
		return nil, losses, nil
	}
	encoder.wire[index] = encoder.next
	encoder.next++
	frames := encoder.messageStart()
	return append(frames, eventFrame("content_block_start", map[string]any{"type": "content_block_start", "index": encoder.wire[index], "content_block": content})), losses, nil
}

func (encoder *streamEncoder) deltaFrames(index int, block *protocol.Block, text string) []format.Frame {
	delta := map[string]any{"type": "text_delta", "text": text}
	switch block.Kind {
	case protocol.KindToolCall:
		delta = map[string]any{"type": "input_json_delta", "partial_json": text}
	case protocol.KindReasoning:
		delta = map[string]any{"type": "thinking_delta", "thinking": text}
	case protocol.KindMedia, protocol.KindNative, protocol.KindRedacted, protocol.KindToolResult:
		return nil
	}
	return []format.Frame{eventFrame("content_block_delta", map[string]any{"type": "content_block_delta", "index": encoder.wire[index], "delta": delta})}
}

func (encoder *streamEncoder) stopBlock(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	block := encoder.blocks[event.Index]
	if block == nil || encoder.closed[event.Index] {
		return nil, nil, nil
	}
	encoder.closed[event.Index] = true
	if event.Block != nil {
		final := protocol.CloneBlock(*event.Block)
		block = &final
	}
	var frames []format.Frame
	var losses []protocol.Loss
	wire := encoder.wire[event.Index]
	if block.Kind == protocol.KindReasoning && block.Reasoning != nil {
		if signature := encoder.peer.WireSignature(block.Reasoning.Signature, block.Text); signature != "" {
			frames = append(frames, eventFrame("content_block_delta", map[string]any{"type": "content_block_delta", "index": wire, "delta": map[string]any{"type": "signature_delta", "signature": signature}}))
		}
	}
	if block.Kind == protocol.KindText {
		for _, citation := range block.Citations {
			encoded, nested := format.EncodeCitation(citation, protocol.Messages)
			losses = append(losses, nested...)
			if encoded != nil {
				frames = append(frames, eventFrame("content_block_delta", map[string]any{"type": "content_block_delta", "index": wire, "delta": map[string]any{"type": "citations_delta", "citation": encoded}}))
			}
		}
	}
	return append(frames, eventFrame("content_block_stop", map[string]any{"type": "content_block_stop", "index": wire})), losses, nil
}

func (encoder *streamEncoder) terminal() []format.Frame {
	if encoder.stopped {
		return nil
	}
	encoder.stopped = true
	frames := encoder.messageStart()
	if encoder.failure != nil {
		_, body := Codec{}.EncodeError(encoder.failure, true)
		return append(frames, format.Frame{Event: "error", Data: body})
	}
	delta := map[string]any{"stop_reason": format.MessagesFinish(encoder.finish, encoder.stop), "stop_sequence": nil}
	if encoder.stop != "" {
		delta["stop_sequence"] = encoder.stop
	}
	message := map[string]any{"type": "message_delta", "delta": delta}
	if encoder.usage != nil {
		message["usage"] = format.EncodeMessagesUsage(encoder.usage)
	}
	return append(frames, eventFrame("message_delta", message), eventFrame("message_stop", map[string]any{"type": "message_stop"}))
}

func eventFrame(name string, payload map[string]any) format.Frame {
	return format.Frame{Event: name, Data: protocol.Marshal(payload)}
}
