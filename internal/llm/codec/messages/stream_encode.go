package messages

import (
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type encodePart struct {
	kind        model.PartKind
	block       int
	text        strings.Builder
	streamed    bool
	citations   []model.Citation
	closed      bool
	blockClosed bool
}

type encodeItem struct {
	kind          model.ItemKind
	role          model.Role
	tool          *model.ToolCall
	reasoning     *model.Reasoning
	reasoningText strings.Builder
	block         int
	parts         map[int]*encodePart
	order         []int
	closed        bool
	skipped       bool
}

type streamEncoder struct {
	meta    model.ResponseMeta
	peer    codec.Peer
	items   map[int]*encodeItem
	blocks  int
	started bool
	stopped bool
	usage   *model.Usage
	finish  model.Finish
	failure *model.Error
}

func (encoder *streamEncoder) orderedParts(state *encodeItem) []*encodePart {
	parts := make([]*encodePart, 0, len(state.order))
	for _, index := range state.order {
		if part := state.parts[index]; part != nil {
			parts = append(parts, part)
		}
	}
	return parts
}

func (Codec) NewStreamEncoder(meta model.ResponseMeta, peer codec.Peer) codec.StreamEncoder {
	return &streamEncoder{meta: meta, peer: peer, items: map[int]*encodeItem{}}
}

func (encoder *streamEncoder) Encode(event model.Event) ([]codec.Frame, []model.Loss, error) {
	switch event.Kind {
	case model.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
		}
		if event.Usage != nil {
			encoder.usage = event.Usage
		}
		return encoder.begin(), nil, nil
	case model.EventItemStart:
		return encoder.startItem(event)
	case model.EventPartStart:
		return encoder.startPart(event)
	case model.EventPartDelta:
		return encoder.partDelta(event)
	case model.EventPartStop:
		return encoder.stopPart(event)
	case model.EventItemStop:
		return encoder.stopItem(event)
	case model.EventMessageDelta:
		if event.Usage != nil {
			encoder.usage = mergeUsage(encoder.usage, event.Usage)
		}
		encoder.failure = event.Failure
		if event.Finish != nil {
			encoder.finish = *event.Finish
		}
		var losses []model.Loss
		if codec.FinishLoss(encoder.finish.Kind, model.Messages) {
			losses = append(losses, model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: string(encoder.finish.Kind), To: model.Messages})
		}
		return nil, losses, nil
	case model.EventMessageStop:
		return encoder.terminal(), codec.UsageLosses(encoder.usage, model.Messages), nil
	case model.EventError:
		encoder.failure = event.Failure
		return encoder.terminal(), nil, nil
	case model.EventPing:
		return encoder.frame("ping", map[string]any{}), nil, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) Resume(priorItems, priorParts int) {
	encoder.started = true
	encoder.blocks += priorParts
}

func (encoder *streamEncoder) Finish() ([]codec.Frame, []model.Loss, error) {
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) Fail(failure *model.Error) ([]codec.Frame, []model.Loss, error) {
	encoder.failure = failure
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) frame(kind string, payload map[string]any) []codec.Frame {
	payload["type"] = kind
	return []codec.Frame{{Event: kind, Data: model.Marshal(payload)}}
}

func (encoder *streamEncoder) begin() []codec.Frame {
	if encoder.started {
		return nil
	}
	encoder.started = true
	usage := map[string]any{"input_tokens": int64(0), "output_tokens": int64(0)}
	if encoder.usage != nil {
		usage = codec.EncodeMessagesUsage(encoder.usage)
	}
	return encoder.frame("message_start", map[string]any{"message": map[string]any{
		"id": encoder.meta.ID, "type": "message", "role": "assistant", "model": encoder.meta.Model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": usage,
	}})
}

func (encoder *streamEncoder) startItem(event model.Event) ([]codec.Frame, []model.Loss, error) {
	frames := encoder.begin()
	if event.Item == nil {
		return frames, nil, nil
	}
	state := &encodeItem{kind: event.Item.Kind, role: event.Item.Role, reasoning: event.Item.Reasoning, parts: map[int]*encodePart{}, block: -1}
	encoder.items[event.ItemIndex] = state
	switch event.Item.Kind {
	case model.ItemToolCall:
		if event.Item.ToolCall == nil || event.Item.ToolCall.Kind == model.ToolServer {
			state.skipped = true
			return frames, []model.Loss{codec.Degraded("item", "provider-executed tool call cannot be represented by Messages", model.Messages)}, nil
		}
		state.tool = event.Item.ToolCall
		state.block = encoder.blocks
		encoder.blocks++
		block := map[string]any{"type": "tool_use", "id": state.tool.ID, "name": state.tool.Name, "input": map[string]any{}}
		if event.Item.CacheControl != nil {
			block["cache_control"] = codec.EncodeCacheControl(event.Item.CacheControl)
		}
		frames = append(frames, encoder.frame("content_block_start", map[string]any{"index": state.block, "content_block": block})...)
	case model.ItemServerToolCall, model.ItemServerToolResult, model.ItemNative:
		native := codec.RawObject(event.Item.Raw, model.Messages)
		if native == nil {
			state.skipped = true
			return frames, []model.Loss{codec.Degraded("item", "the provider item cannot be represented by Messages", model.Messages)}, nil
		}
		state.block = encoder.blocks
		encoder.blocks++
		frames = append(frames, encoder.frame("content_block_start", map[string]any{"index": state.block, "content_block": native})...)
	}
	return frames, nil, nil
}

func (encoder *streamEncoder) startPart(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil || state.skipped || event.Part == nil {
		return nil, nil, nil
	}
	part := &encodePart{kind: event.Part.Kind, citations: event.Part.Citations, block: -1}
	state.parts[event.PartIndex] = part
	state.order = append(state.order, event.PartIndex)
	var losses []model.Loss
	switch part.kind {
	case model.PartText, model.PartRefusal:
		if part.kind == model.PartRefusal {
			losses = append(losses, codec.Degraded("refusal", "Messages carries a refusal as text", model.Messages))
		}
		part.block = encoder.blocks
		encoder.blocks++
		block := map[string]any{"type": "text", "text": ""}
		if event.Part.CacheControl != nil {
			block["cache_control"] = codec.EncodeCacheControl(event.Part.CacheControl)
		}
		return encoder.frame("content_block_start", map[string]any{"index": part.block, "content_block": block}), losses, nil
	case model.PartReasoningText, model.PartReasoningSummary:
		part.block = encoder.blocks
		encoder.blocks++
		return encoder.frame("content_block_start", map[string]any{"index": part.block, "content_block": map[string]any{"type": "thinking", "thinking": ""}}), nil, nil
	case model.PartToolArguments:
		part.block = state.block
	case model.PartRedactedReasoning:
		return encoder.startRedacted(state, event.Part, part)
	case model.PartMedia:
		losses = append(losses, codec.Degraded("media", "Messages cannot stream generated media", model.Messages))
	}
	return nil, losses, nil
}

func (encoder *streamEncoder) startRedacted(state *encodeItem, source *model.Part, part *encodePart) ([]codec.Frame, []model.Loss, error) {
	if native := codec.RawObject(source.Raw, model.Messages); native != nil {
		part.block = encoder.blocks
		encoder.blocks++
		return encoder.frame("content_block_start", map[string]any{"index": part.block, "content_block": native}), nil, nil
	}
	return nil, []model.Loss{codec.Degraded("redacted_reasoning", "redacted reasoning cannot be replayed by Messages", model.Messages)}, nil
}

func (encoder *streamEncoder) partDelta(event model.Event) ([]codec.Frame, []model.Loss, error) {
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
	if part.kind == model.PartReasoningText || part.kind == model.PartReasoningSummary {
		state.reasoningText.WriteString(event.Delta)
	}
	return encoder.deltaFrames(part, event.Delta), nil, nil
}

func (encoder *streamEncoder) deltaFrames(part *encodePart, text string) []codec.Frame {
	delta := map[string]any{"type": "text_delta", "text": text}
	switch part.kind {
	case model.PartReasoningText, model.PartReasoningSummary:
		delta = map[string]any{"type": "thinking_delta", "thinking": text}
	case model.PartToolArguments:
		delta = map[string]any{"type": "input_json_delta", "partial_json": text}
	}
	return encoder.frame("content_block_delta", map[string]any{"index": part.block, "delta": delta})
}

func (encoder *streamEncoder) stopPart(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil {
		return nil, nil, nil
	}
	part := state.parts[event.PartIndex]
	if part == nil || part.closed || part.block < 0 {
		return nil, nil, nil
	}
	var frames []codec.Frame
	var losses []model.Loss
	if event.Part != nil {
		if len(event.Part.Citations) > 0 {
			part.citations = event.Part.Citations
		}
		if content := model.StreamedContent(*event.Part); content != "" && !part.streamed {
			part.text.WriteString(content)
			frames = append(frames, encoder.deltaFrames(part, content)...)
			part.streamed = true
		}

	}
	if part.kind == model.PartToolArguments && !part.streamed {
		arguments := "{}"
		if state.tool != nil && state.tool.Arguments != "" {
			arguments = state.tool.Arguments
		}
		part.text.WriteString(arguments)
		frames = append(frames, encoder.deltaFrames(part, arguments)...)
		part.streamed = true
	}
	for _, citation := range part.citations {
		entry, nested := codec.EncodeCitation(citation, model.Messages)
		losses = append(losses, nested...)
		if entry == nil {
			continue
		}
		frames = append(frames, encoder.frame("content_block_delta", map[string]any{"index": part.block, "delta": map[string]any{"type": "citations_delta", "citation": entry}})...)
	}
	part.closed = true
	if part.kind == model.PartReasoningText || part.kind == model.PartReasoningSummary {
		return frames, losses, nil
	}
	part.blockClosed = true
	return append(frames, encoder.frame("content_block_stop", map[string]any{"index": part.block})...), losses, nil
}

func (encoder *streamEncoder) signature(state *encodeItem, part *encodePart) string {
	if state.reasoning == nil || !codec.SignatureCoversText(state.reasoning.Signature, part.kind, part.text.String()) {
		return ""
	}
	return encoder.peer.WireSignature(state.reasoning.Signature, part.text.String())
}

func (encoder *streamEncoder) stopItem(event model.Event) ([]codec.Frame, []model.Loss, error) {
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
	var frames []codec.Frame
	var losses []model.Loss
	if state.tool != nil && state.tool.Signature != nil {
		losses = append(losses, codec.Degraded("signature", "Messages has no tool call signature", model.Messages))
	}
	if state.kind == model.ItemReasoning && len(state.parts) == 0 && state.reasoning != nil && state.reasoning.Signature != nil {
		block := encoder.blocks
		encoder.blocks++
		frames = append(frames, encoder.frame("content_block_start", map[string]any{"index": block, "content_block": map[string]any{"type": "thinking", "thinking": ""}})...)
		if signature := encoder.signature(state, &encodePart{kind: model.PartReasoningText}); signature != "" {
			frames = append(frames, encoder.frame("content_block_delta", map[string]any{"index": block, "delta": map[string]any{"type": "signature_delta", "signature": signature}})...)
		}
		frames = append(frames, encoder.frame("content_block_stop", map[string]any{"index": block})...)
	}
	for _, part := range encoder.orderedParts(state) {
		if part.kind != model.PartReasoningText && part.kind != model.PartReasoningSummary {
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
	if state.kind == model.ItemToolCall && state.tool != nil {
		parts := 0
		for _, part := range encoder.orderedParts(state) {
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
	if state.kind == model.ItemServerToolCall || state.kind == model.ItemServerToolResult || state.kind == model.ItemNative {
		frames = append(frames, encoder.frame("content_block_stop", map[string]any{"index": state.block})...)
	}
	return frames, losses, nil
}

func (encoder *streamEncoder) terminal() []codec.Frame {
	if encoder.stopped {
		return nil
	}
	encoder.stopped = true
	if encoder.failure != nil {
		_, body := Codec{}.EncodeError(encoder.failure, true)
		return []codec.Frame{{Event: "error", Data: body}}
	}
	finishKind := encoder.finish.Kind
	if finishKind == "" || finishKind == model.FinishStop {
		for _, state := range encoder.items {
			if state.kind == model.ItemToolCall && !state.skipped {
				finishKind = model.FinishToolCalls
				break
			}
		}
	}
	delta := map[string]any{"stop_reason": codec.MessagesFinish(finishKind, encoder.finish.StopSequence)}
	if encoder.finish.StopSequence != "" {
		delta["stop_sequence"] = encoder.finish.StopSequence
	}
	usage := map[string]any{"output_tokens": int64(0)}
	if encoder.usage != nil {
		usage = codec.EncodeMessagesUsage(encoder.usage)
	}
	return append(encoder.frame("message_delta", map[string]any{"delta": delta, "usage": usage}), encoder.frame("message_stop", map[string]any{})...)
}
