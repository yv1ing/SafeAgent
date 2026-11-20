package messages

import (
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type encodePart struct {
	kind        protocol.PartKind
	block       int
	text        strings.Builder
	streamed    bool
	citations   []protocol.Citation
	closed      bool
	blockClosed bool
}

type encodeItem struct {
	kind          protocol.ItemKind
	role          protocol.Role
	tool          *protocol.ToolCall
	reasoning     *protocol.Reasoning
	reasoningText strings.Builder
	block         int
	parts         map[int]*encodePart
	order         []int
	closed        bool
	skipped       bool
}

type streamEncoder struct {
	meta    protocol.ResponseMeta
	peer    format.Peer
	items   map[int]*encodeItem
	blocks  int
	started bool
	stopped bool
	usage   *protocol.Usage
	finish  protocol.Finish
	failure *protocol.Error
}

func (Codec) NewStreamEncoder(meta protocol.ResponseMeta, peer format.Peer) format.StreamEncoder {
	return &streamEncoder{meta: meta, peer: peer, items: map[int]*encodeItem{}}
}

func (encoder *streamEncoder) Encode(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	switch event.Kind {
	case protocol.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
		}
		if event.Usage != nil {
			encoder.usage = event.Usage
		}
		return encoder.begin(), nil, nil
	case protocol.EventItemStart:
		return encoder.startItem(event)
	case protocol.EventPartStart:
		return encoder.startPart(event)
	case protocol.EventPartDelta:
		return encoder.partDelta(event)
	case protocol.EventPartStop:
		return encoder.stopPart(event)
	case protocol.EventItemStop:
		return encoder.stopItem(event)
	case protocol.EventMessageDelta:
		if event.Usage != nil {
			encoder.usage = mergeUsage(encoder.usage, event.Usage)
		}
		encoder.failure = event.Failure
		if event.Finish != nil {
			encoder.finish = *event.Finish
		}
		var losses []protocol.Loss
		if format.FinishLoss(encoder.finish.Kind, protocol.Messages) {
			losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: string(encoder.finish.Kind), To: protocol.Messages})
		}
		return nil, losses, nil
	case protocol.EventMessageStop:
		return encoder.terminal(), format.UsageLosses(encoder.usage, protocol.Messages), nil
	case protocol.EventError:
		encoder.failure = event.Failure
		return encoder.terminal(), nil, nil
	case protocol.EventPing:
		return encoder.frame("ping", map[string]any{}), nil, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) Resume(priorItems, priorParts int, priorSequence int64) {
	encoder.started = true
	encoder.blocks += priorParts
}

func (encoder *streamEncoder) Finish() ([]format.Frame, []protocol.Loss, error) {
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) Fail(failure *protocol.Error) ([]format.Frame, []protocol.Loss, error) {
	encoder.failure = failure
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) frame(kind string, payload map[string]any) []format.Frame {
	payload["type"] = kind
	return []format.Frame{{Event: kind, Data: protocol.Marshal(payload)}}
}

func (encoder *streamEncoder) begin() []format.Frame {
	if encoder.started {
		return nil
	}
	encoder.started = true
	usage := map[string]any{"input_tokens": int64(0), "output_tokens": int64(0)}
	if encoder.usage != nil {
		usage = format.EncodeMessagesUsage(encoder.usage)
	}
	return encoder.frame("message_start", map[string]any{"message": map[string]any{
		"id": encoder.meta.ID, "type": "message", "role": "assistant", "model": encoder.meta.Model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": usage,
	}})
}

func (encoder *streamEncoder) startItem(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	frames := encoder.begin()
	if event.Item == nil {
		return frames, nil, nil
	}
	state := &encodeItem{kind: event.Item.Kind, role: event.Item.Role, reasoning: event.Item.Reasoning, parts: map[int]*encodePart{}, block: -1}
	encoder.items[event.ItemIndex] = state
	switch event.Item.Kind {
	case protocol.ItemToolCall:
		if event.Item.ToolCall == nil || event.Item.ToolCall.Kind == protocol.ToolServer {
			state.skipped = true
			return frames, []protocol.Loss{format.Degraded("item", "provider-executed tool call cannot be represented by Messages", protocol.Messages)}, nil
		}
		state.tool = event.Item.ToolCall
		state.block = encoder.blocks
		encoder.blocks++
		block := map[string]any{"type": "tool_use", "id": state.tool.ID, "name": state.tool.Name, "input": map[string]any{}}
		if event.Item.CacheControl != nil {
			block["cache_control"] = format.EncodeCacheControl(event.Item.CacheControl)
		}
		frames = append(frames, encoder.frame("content_block_start", map[string]any{"index": state.block, "content_block": block})...)
	case protocol.ItemServerToolCall, protocol.ItemServerToolResult, protocol.ItemNative:
		native := format.RawObject(event.Item.Raw, protocol.Messages)
		if native == nil {
			state.skipped = true
			return frames, []protocol.Loss{format.Degraded("item", "the provider item cannot be represented by Messages", protocol.Messages)}, nil
		}
		state.block = encoder.blocks
		encoder.blocks++
		frames = append(frames, encoder.frame("content_block_start", map[string]any{"index": state.block, "content_block": native})...)
	}
	return frames, nil, nil
}

func (encoder *streamEncoder) startPart(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil || state.skipped || event.Part == nil {
		return nil, nil, nil
	}
	part := &encodePart{kind: event.Part.Kind, citations: event.Part.Citations, block: -1}
	state.parts[event.PartIndex] = part
	state.order = append(state.order, event.PartIndex)
	var losses []protocol.Loss
	switch part.kind {
	case protocol.PartText, protocol.PartRefusal:
		if part.kind == protocol.PartRefusal {
			losses = append(losses, format.Degraded("refusal", "Messages carries a refusal as text", protocol.Messages))
		}
		part.block = encoder.blocks
		encoder.blocks++
		block := map[string]any{"type": "text", "text": ""}
		if event.Part.CacheControl != nil {
			block["cache_control"] = format.EncodeCacheControl(event.Part.CacheControl)
		}
		return encoder.frame("content_block_start", map[string]any{"index": part.block, "content_block": block}), losses, nil
	case protocol.PartReasoningText, protocol.PartReasoningSummary:
		part.block = encoder.blocks
		encoder.blocks++
		return encoder.frame("content_block_start", map[string]any{"index": part.block, "content_block": map[string]any{"type": "thinking", "thinking": ""}}), nil, nil
	case protocol.PartToolArguments:
		part.block = state.block
	case protocol.PartRedactedReasoning:
		return encoder.startRedacted(state, event.Part, part)
	case protocol.PartMedia:
		losses = append(losses, format.Degraded("media", "Messages cannot stream generated media", protocol.Messages))
	}
	return nil, losses, nil
}

func (encoder *streamEncoder) startRedacted(state *encodeItem, source *protocol.Part, part *encodePart) ([]format.Frame, []protocol.Loss, error) {
	if native := format.RawObject(source.Raw, protocol.Messages); native != nil {
		part.block = encoder.blocks
		encoder.blocks++
		return encoder.frame("content_block_start", map[string]any{"index": part.block, "content_block": native}), nil, nil
	}
	return nil, []protocol.Loss{format.Degraded("redacted_reasoning", "redacted reasoning cannot be replayed by Messages", protocol.Messages)}, nil
}

func (encoder *streamEncoder) partDelta(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil || event.Delta == "" {
		return nil, nil, nil
	}
	part := state.parts[event.PartIndex]
	if part == nil || part.block < 0 {
		return nil, nil, nil
	}
	part.streamed = true
	part.text.WriteString(event.Delta)
	if part.kind == protocol.PartReasoningText || part.kind == protocol.PartReasoningSummary {
		state.reasoningText.WriteString(event.Delta)
	}
	return encoder.deltaFrames(part, event.Delta), nil, nil
}

func (encoder *streamEncoder) deltaFrames(part *encodePart, text string) []format.Frame {
	delta := map[string]any{"type": "text_delta", "text": text}
	switch part.kind {
	case protocol.PartReasoningText, protocol.PartReasoningSummary:
		delta = map[string]any{"type": "thinking_delta", "thinking": text}
	case protocol.PartToolArguments:
		delta = map[string]any{"type": "input_json_delta", "partial_json": text}
	}
	return encoder.frame("content_block_delta", map[string]any{"index": part.block, "delta": delta})
}

func (encoder *streamEncoder) stopPart(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil {
		return nil, nil, nil
	}
	part := state.parts[event.PartIndex]
	if part == nil || part.closed || part.block < 0 {
		return nil, nil, nil
	}
	var frames []format.Frame
	var losses []protocol.Loss
	if event.Part != nil {
		if len(event.Part.Citations) > 0 {
			part.citations = event.Part.Citations
		}
		if content := protocol.StreamedContent(*event.Part); content != "" && !part.streamed {
			part.text.WriteString(content)
			frames = append(frames, encoder.deltaFrames(part, content)...)
			part.streamed = true
		}

	}
	if part.kind == protocol.PartToolArguments && !part.streamed {
		arguments := "{}"
		if state.tool != nil && state.tool.Arguments != "" {
			arguments = state.tool.Arguments
		}
		part.text.WriteString(arguments)
		frames = append(frames, encoder.deltaFrames(part, arguments)...)
		part.streamed = true
	}
	for _, citation := range part.citations {
		entry, nested := format.EncodeCitation(citation, protocol.Messages)
		losses = append(losses, nested...)
		if entry == nil {
			continue
		}
		frames = append(frames, encoder.frame("content_block_delta", map[string]any{"index": part.block, "delta": map[string]any{"type": "citations_delta", "citation": entry}})...)
	}
	part.closed = true
	if part.kind == protocol.PartReasoningText || part.kind == protocol.PartReasoningSummary {
		return frames, losses, nil
	}
	part.blockClosed = true
	return append(frames, encoder.frame("content_block_stop", map[string]any{"index": part.block})...), losses, nil
}

func (encoder *streamEncoder) signature(state *encodeItem, part *encodePart) string {
	if state.reasoning == nil || state.reasoning.Signature == nil {
		return ""
	}
	return encoder.peer.WireSignature(state.reasoning.Signature, state.reasoningText.String())
}

func (encoder *streamEncoder) stopItem(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil || state.closed || state.skipped {
		return nil, nil, nil
	}
	state.closed = true
	if event.Item != nil {
		if event.Item.Reasoning != nil {
			state.reasoning = event.Item.Reasoning
		}
		if event.Item.ToolCall != nil {
			state.tool = event.Item.ToolCall
		}
	}
	var frames []format.Frame
	var losses []protocol.Loss
	if state.tool != nil && state.tool.Signature != nil {
		losses = append(losses, format.Degraded("signature", "Messages has no tool call signature", protocol.Messages))
	}
	if state.kind == protocol.ItemReasoning && len(state.parts) == 0 && state.reasoning != nil && state.reasoning.Signature != nil {
		block := encoder.blocks
		encoder.blocks++
		frames = append(frames, encoder.frame("content_block_start", map[string]any{"index": block, "content_block": map[string]any{"type": "thinking", "thinking": ""}})...)
		if signature := encoder.signature(state, &encodePart{kind: protocol.PartReasoningText}); signature != "" {
			frames = append(frames, encoder.frame("content_block_delta", map[string]any{"index": block, "delta": map[string]any{"type": "signature_delta", "signature": signature}})...)
		}
		frames = append(frames, encoder.frame("content_block_stop", map[string]any{"index": block})...)
	}
	for _, part := range state.parts {
		if part.kind != protocol.PartReasoningText && part.kind != protocol.PartReasoningSummary {
			continue
		}
		if signature := encoder.signature(state, part); signature != "" {
			frames = append(frames, encoder.frame("content_block_delta", map[string]any{"index": part.block, "delta": map[string]any{"type": "signature_delta", "signature": signature}})...)
		}
		if !part.blockClosed {
			part.blockClosed = true
			frames = append(frames, encoder.frame("content_block_stop", map[string]any{"index": part.block})...)
		}
	}
	if state.kind == protocol.ItemToolCall && state.tool != nil {
		parts := 0
		for _, part := range state.parts {
			if part.block >= 0 {
				parts++
			}
		}
		if parts == 0 {
			arguments := state.tool.Arguments
			if arguments == "" {
				arguments = "{}"
			}
			frames = append(frames, encoder.frame("content_block_delta", map[string]any{"index": state.block, "delta": map[string]any{"type": "input_json_delta", "partial_json": arguments}})...)
			frames = append(frames, encoder.frame("content_block_stop", map[string]any{"index": state.block})...)
		}
	}
	if state.kind == protocol.ItemServerToolCall || state.kind == protocol.ItemServerToolResult || state.kind == protocol.ItemNative {
		frames = append(frames, encoder.frame("content_block_stop", map[string]any{"index": state.block})...)
	}
	return frames, losses, nil
}

func (encoder *streamEncoder) terminal() []format.Frame {
	if encoder.stopped {
		return nil
	}
	encoder.stopped = true
	if encoder.failure != nil {
		_, body := Codec{}.EncodeError(encoder.failure, true)
		return []format.Frame{{Event: "error", Data: body}}
	}
	finishKind := encoder.finish.Kind
	if finishKind == "" || finishKind == protocol.FinishStop {
		for _, state := range encoder.items {
			if state.kind == protocol.ItemToolCall && !state.skipped {
				finishKind = protocol.FinishToolCalls
				break
			}
		}
	}
	delta := map[string]any{"stop_reason": format.MessagesFinish(finishKind, encoder.finish.StopSequence)}
	if encoder.finish.StopSequence != "" {
		delta["stop_sequence"] = encoder.finish.StopSequence
	}
	usage := map[string]any{"output_tokens": int64(0)}
	if encoder.usage != nil {
		usage = format.EncodeMessagesUsage(encoder.usage)
	}
	return append(encoder.frame("message_delta", map[string]any{"delta": delta, "usage": usage}), encoder.frame("message_stop", map[string]any{})...)
}
