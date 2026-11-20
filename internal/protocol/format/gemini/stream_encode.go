package gemini

import (
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type streamEncoder struct {
	meta    protocol.ResponseMeta
	peer    format.Peer
	blocks  map[int]*protocol.Block
	closed  map[int]bool
	usage   *protocol.Usage
	finish  protocol.FinishReason
	failure *protocol.Error
	stopped bool
}

func (Codec) NewStreamEncoder(meta protocol.ResponseMeta, peer format.Peer) format.StreamEncoder {
	return &streamEncoder{meta: meta, peer: peer, blocks: map[int]*protocol.Block{}, closed: map[int]bool{}}
}

func (encoder *streamEncoder) Encode(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	switch event.Kind {
	case protocol.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
		}
	case protocol.EventBlockStart:
		if event.Block != nil {
			block := protocol.CloneBlock(*event.Block)
			encoder.blocks[event.Index] = &block
		}
	case protocol.EventBlockDelta:
		return encoder.delta(event)
	case protocol.EventBlockStop:
		return encoder.stopBlock(event)
	case protocol.EventMessageDelta:
		encoder.usage, encoder.finish, encoder.failure = event.Usage, event.Finish, event.Failure
		var losses []protocol.Loss
		if format.FinishLoss(event.Finish, protocol.Gemini) {
			losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Detail: string(event.Finish), To: protocol.Gemini})
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

func isTextual(block *protocol.Block) bool {
	return block.Kind == protocol.KindText || block.Kind == protocol.KindReasoning || block.Kind == protocol.KindRefusal
}

// delta streams text as it arrives. Everything else is delivered whole when its block stops,
// because Gemini has no partial function call.
func (encoder *streamEncoder) delta(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	block := encoder.blocks[event.Index]
	if block == nil || encoder.closed[event.Index] || event.Text == "" {
		return nil, nil, nil
	}
	if !isTextual(block) {
		if block.Kind == protocol.KindToolCall && block.ToolCall != nil {
			block.ToolCall.Arguments += event.Text
		}
		return nil, nil, nil
	}
	block.Text += event.Text
	return encoder.textChunk(block, event.Text)
}

func (encoder *streamEncoder) textChunk(block *protocol.Block, text string) ([]format.Frame, []protocol.Loss, error) {
	shown := *block
	shown.Text = text
	shown.Reasoning = nil
	if block.Kind == protocol.KindReasoning {
		shown.Kind, shown.Reasoning = protocol.KindReasoning, &protocol.Reasoning{Visibility: "full"}
	}
	part, losses, err := encodeStreamPart(shown, encoder.peer)
	if err != nil || part == nil {
		return nil, losses, err
	}
	return encoder.chunk([]any{part}, "", nil), losses, nil
}

func encodeStreamPart(block protocol.Block, peer format.Peer) (map[string]any, []protocol.Loss, error) {
	context := format.NewPartContext(protocol.Gemini, peer)
	context.Role = protocol.RoleAssistant
	return format.EncodePart(block, context)
}

// stopBlock closes a block. Text already went out as deltas, so a textual block only adds its
// signature, computed over the whole text; anything else is sent whole now.
func (encoder *streamEncoder) stopBlock(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	block := encoder.blocks[event.Index]
	if block == nil || encoder.closed[event.Index] {
		return nil, nil, nil
	}
	encoder.closed[event.Index] = true
	if event.Block != nil {
		final := protocol.CloneBlock(*event.Block)
		block = &final
		encoder.blocks[event.Index] = block
	}
	if isTextual(block) {
		return encoder.signatureChunk(block), nil, nil
	}
	part, losses, err := encodeStreamPart(*block, encoder.peer)
	if err != nil || part == nil {
		return nil, losses, err
	}
	return encoder.chunk([]any{part}, "", nil), losses, nil
}

// signatureChunk closes a thought or answer: the signature belongs to its last part, so it travels
// in a part of its own with empty text.
func (encoder *streamEncoder) signatureChunk(block *protocol.Block) []format.Frame {
	if block.Reasoning == nil {
		return nil
	}
	signature := encoder.peer.WireSignature(block.Reasoning.Signature, block.Text)
	if signature == "" {
		return nil
	}
	part := map[string]any{"text": "", "thoughtSignature": signature}
	if block.Kind == protocol.KindReasoning {
		part["thought"] = true
	}
	return encoder.chunk([]any{part}, "", nil)
}

func (encoder *streamEncoder) terminal() []format.Frame {
	if encoder.stopped {
		return nil
	}
	encoder.stopped = true
	if encoder.failure != nil {
		_, body := Codec{}.EncodeError(encoder.failure, true)
		return []format.Frame{{Data: body}}
	}
	return encoder.chunk(nil, format.GeminiFinish(encoder.finish), encoder.usage)
}

func (encoder *streamEncoder) chunk(parts []any, finish string, usage *protocol.Usage) []format.Frame {
	candidate := map[string]any{"index": 0}
	if parts != nil {
		candidate["content"] = map[string]any{"role": "model", "parts": parts}
	}
	if finish != "" {
		candidate["finishReason"] = finish
	}
	root := map[string]any{"candidates": []any{candidate}}
	if encoder.meta.ID != "" {
		root["responseId"] = encoder.meta.ID
	}
	if encoder.meta.Model != "" {
		root["modelVersion"] = encoder.meta.Model
	}
	if usage != nil {
		root["usageMetadata"] = format.EncodeGeminiUsage(usage)
	}
	return []format.Frame{{Data: protocol.Marshal(root)}}
}
