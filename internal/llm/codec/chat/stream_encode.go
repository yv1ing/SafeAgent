package chat

import (
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type encodePartState struct {
	kind      model.PartKind
	streamed  bool
	text      string
	citations []model.Citation
}

type encodeItemState struct {
	kind     model.ItemKind
	role     model.Role
	tool     *model.ToolCall
	position int
	isTool   bool
	parts    map[int]*encodePartState
	sentArgs bool
}

type streamEncoder struct {
	meta         model.ResponseMeta
	items        map[int]*encodeItemState
	tools        int
	usage        *model.Usage
	finish       model.FinishReason
	failure      *model.Error
	includeUsage bool
	created      int64
	stopped      bool
}

func (Codec) NewStreamEncoder(meta model.ResponseMeta, peer codec.Peer) codec.StreamEncoder {
	created := meta.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	return &streamEncoder{meta: meta, items: map[int]*encodeItemState{}, includeUsage: peer.IncludeUsage, created: created}
}

func (encoder *streamEncoder) Encode(event model.Event) ([]codec.Frame, []model.Loss, error) {
	switch event.Kind {
	case model.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
		}
		return encoder.delta(map[string]any{"role": "assistant", "content": ""}), nil, nil
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
		encoder.usage, encoder.failure = event.Usage, event.Failure
		if event.Finish != nil {
			encoder.finish = event.Finish.Kind
		}
		var losses []model.Loss
		if codec.FinishLoss(encoder.finish, model.Chat) {
			losses = append(losses, model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: string(encoder.finish), To: model.Chat})
		}
		return nil, losses, nil
	case model.EventMessageStop:
		return encoder.terminal(), codec.UsageLosses(encoder.usage, model.Chat), nil
	case model.EventError:
		encoder.failure = event.Failure
		return encoder.terminal(), nil, nil
	case model.EventPing:
		return []codec.Frame{{Comment: "keepalive"}}, nil, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) Resume(priorItems, priorParts int) {}

func (encoder *streamEncoder) Finish() ([]codec.Frame, []model.Loss, error) {
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) Fail(failure *model.Error) ([]codec.Frame, []model.Loss, error) {
	encoder.failure = failure
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) startItem(event model.Event) ([]codec.Frame, []model.Loss, error) {
	if event.Item == nil {
		return nil, nil, nil
	}
	state := &encodeItemState{kind: event.Item.Kind, role: event.Item.Role, parts: map[int]*encodePartState{}, position: -1}
	encoder.items[event.ItemIndex] = state
	if event.Item.Kind == model.ItemReasoning && event.Item.Reasoning != nil && event.Item.Reasoning.Signature != nil {
		return nil, []model.Loss{model.Loss{Path: "signature", Code: model.LossSignatureStripped, Class: model.LossDegraded, Detail: "Chat has no field for reasoning signatures", To: model.Chat}}, nil
	}
	if event.Item.Kind == model.ItemToolCall || event.Item.Kind == model.ItemServerToolCall {
		if event.Item.ToolCall == nil || event.Item.ToolCall.Kind == model.ToolServer {
			return nil, []model.Loss{codec.Degraded("tool_call", "provider-executed tool call cannot be represented by Chat", model.Chat)}, nil
		}
		state.isTool = true
		state.tool = event.Item.ToolCall
		state.position = encoder.tools
		encoder.tools++
		var losses []model.Loss
		if state.tool.Signature != nil {
			losses = append(losses, model.Loss{Path: "signature", Code: model.LossSignatureStripped, Class: model.LossDegraded, Detail: "Chat has no field for tool call signatures", To: model.Chat})
		}
		entry := map[string]any{"index": state.position, "id": state.tool.ID, "type": "function", "function": map[string]any{"name": state.tool.Name, "arguments": ""}}
		if state.tool.Kind == model.ToolCustom {
			entry = map[string]any{"index": state.position, "id": state.tool.ID, "type": "custom", "custom": map[string]any{"name": state.tool.Name, "input": ""}}
		}
		return encoder.delta(map[string]any{"tool_calls": []any{entry}}), losses, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) startPart(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil || event.Part == nil {
		return nil, nil, nil
	}
	part := &encodePartState{kind: event.Part.Kind, citations: event.Part.Citations, text: model.StreamedContent(*event.Part)}
	state.parts[event.PartIndex] = part
	switch part.kind {
	case model.PartMedia:
		return nil, []model.Loss{codec.Degraded("media", "Chat cannot stream generated media", model.Chat)}, nil
	case model.PartNative, model.PartRedactedReasoning:
		return nil, []model.Loss{codec.Degraded("part", "the provider part cannot be represented by Chat", model.Chat)}, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) partDelta(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil || event.Delta == "" {
		return nil, nil, nil
	}
	part := state.parts[event.PartIndex]
	if part == nil {
		return nil, nil, nil
	}
	part.streamed = true
	part.text += event.Delta
	return encoder.deltaFor(state, part, event.Delta), nil, nil
}

func (encoder *streamEncoder) stopPart(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil {
		return nil, nil, nil
	}
	part := state.parts[event.PartIndex]
	if part == nil {
		return nil, nil, nil
	}
	if event.Part != nil {
		if len(event.Part.Citations) > 0 {
			part.citations = event.Part.Citations
		}
		if content := model.StreamedContent(*event.Part); content != "" && !part.streamed {
			part.text = content
		}
	}
	var frames []codec.Frame
	var losses []model.Loss
	if event.Part != nil && len(event.Part.Logprobs) > 0 {
		losses = append(losses, model.Loss{Path: "logprobs", Code: model.LossFieldDropped, Class: model.LossDegraded, Detail: "token logprobs are not reproduced after conversion", To: model.Chat})
	}
	if !part.streamed && part.text != "" {
		frames = append(frames, encoder.deltaFor(state, part, part.text)...)
		part.streamed = true
		if part.kind == model.PartToolArguments {
			state.sentArgs = true
		}
	}
	if part.kind == model.PartToolArguments && part.streamed {
		state.sentArgs = true
	}
	if len(part.citations) > 0 && !state.isTool {
		if annotations, nested := encodeChatCitations(part.citations); len(annotations) > 0 {
			frames = append(frames, encoder.delta(map[string]any{"annotations": annotations})...)
			losses = append(losses, nested...)
		} else {
			losses = append(losses, nested...)
		}
	}
	return frames, losses, nil
}

func (encoder *streamEncoder) stopItem(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil {
		return nil, nil, nil
	}
	var losses []model.Loss
	if event.Item != nil && event.Item.Reasoning != nil && event.Item.Reasoning.Signature != nil {
		losses = append(losses, model.Loss{Path: "signature", Code: model.LossSignatureStripped, Class: model.LossDegraded, Detail: "Chat has no field for reasoning signatures", To: model.Chat})
	}
	if state.isTool && !state.sentArgs {
		arguments := ""
		if event.Item != nil && event.Item.ToolCall != nil {
			arguments = event.Item.ToolCall.Arguments
		}
		if state.tool != nil && state.tool.Kind != model.ToolCustom {
			arguments = codec.EmptyObjectIfBlank(arguments)
		}
		if arguments != "" {
			part := &encodePartState{kind: model.PartToolArguments}
			return encoder.deltaFor(state, part, arguments), losses, nil
		}
	}
	return nil, losses, nil
}

func (encoder *streamEncoder) deltaFor(state *encodeItemState, part *encodePartState, text string) []codec.Frame {
	if !state.isTool {
		switch part.kind {
		case model.PartReasoningText, model.PartReasoningSummary:
			return encoder.delta(map[string]any{"reasoning_content": text})
		case model.PartRefusal:
			return encoder.delta(map[string]any{"refusal": text})
		case model.PartText:
			return encoder.delta(map[string]any{"content": text})
		}
		return nil
	}
	if state.position < 0 {
		return nil
	}
	field, key := map[string]any{"arguments": text}, "function"
	if state.tool != nil && state.tool.Kind == model.ToolCustom {
		field, key = map[string]any{"input": text}, "custom"
	}
	return encoder.delta(map[string]any{"tool_calls": []any{map[string]any{"index": state.position, key: field}}})
}

func (encoder *streamEncoder) terminal() []codec.Frame {
	if encoder.stopped {
		return nil
	}
	encoder.stopped = true
	if encoder.failure != nil {
		_, body := Codec{}.EncodeError(encoder.failure, true)
		return []codec.Frame{{Data: body}, {Data: []byte("[DONE]")}}
	}
	finishKind := encoder.finish
	if (finishKind == "" || finishKind == model.FinishStop) && encoder.tools > 0 {
		finishKind = model.FinishToolCalls
	}
	finish := codec.ChatFinish(finishKind)
	if finish == "" {
		finish = "stop"
	}
	frames := []codec.Frame{{Data: model.Marshal(encoder.chunk(map[string]any{}, finish))}}
	if encoder.includeUsage && encoder.usage != nil {
		frames = append(frames, codec.Frame{Data: model.Marshal(encoder.usageChunk())})
	}
	return append(frames, codec.Frame{Data: []byte("[DONE]")})
}

func (encoder *streamEncoder) delta(delta map[string]any) []codec.Frame {
	return []codec.Frame{{Data: model.Marshal(encoder.chunk(delta, ""))}}
}

func (encoder *streamEncoder) chunk(delta map[string]any, finish string) map[string]any {
	var reason any
	if finish != "" {
		reason = finish
	}
	chunk := map[string]any{
		"id":      encoder.meta.ID,
		"object":  "chat.completion.chunk",
		"created": encoder.created,
		"model":   encoder.meta.Model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": reason}},
	}
	if encoder.meta.SystemFingerprint != "" {
		chunk["system_fingerprint"] = encoder.meta.SystemFingerprint
	}
	if encoder.meta.ServiceTier != "" {
		chunk["service_tier"] = encoder.meta.ServiceTier
	}
	return chunk
}

func (encoder *streamEncoder) usageChunk() map[string]any {
	chunk := encoder.chunk(map[string]any{}, "")
	chunk["choices"] = []any{}
	chunk["usage"] = codec.EncodeChatUsage(encoder.usage)
	return chunk
}
