package chat

import (
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type encodePartState struct {
	kind      protocol.PartKind
	streamed  bool
	text      string
	citations []protocol.Citation
}

type encodeItemState struct {
	kind     protocol.ItemKind
	role     protocol.Role
	tool     *protocol.ToolCall
	position int
	isTool   bool
	parts    map[int]*encodePartState
	sentArgs bool
}

type streamEncoder struct {
	meta    protocol.ResponseMeta
	items   map[int]*encodeItemState
	tools   int
	usage   *protocol.Usage
	finish  protocol.FinishReason
	failure *protocol.Error
	stopped bool
}

func (Codec) NewStreamEncoder(meta protocol.ResponseMeta, peer format.Peer) format.StreamEncoder {
	return &streamEncoder{meta: meta, items: map[int]*encodeItemState{}}
}

func (encoder *streamEncoder) Encode(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	switch event.Kind {
	case protocol.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
		}
		return encoder.delta(map[string]any{"role": "assistant", "content": ""}), nil, nil
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
		encoder.usage, encoder.failure = event.Usage, event.Failure
		if event.Finish != nil {
			encoder.finish = event.Finish.Kind
		}
		var losses []protocol.Loss
		if format.FinishLoss(encoder.finish, protocol.Chat) {
			losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: string(encoder.finish), To: protocol.Chat})
		}
		return nil, losses, nil
	case protocol.EventMessageStop:
		return encoder.terminal(), format.UsageLosses(encoder.usage, protocol.Chat), nil
	case protocol.EventError:
		encoder.failure = event.Failure
		return encoder.terminal(), nil, nil
	case protocol.EventPing:
		return []format.Frame{{Comment: "keepalive"}}, nil, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) Resume(priorItems, priorParts int, priorSequence int64) {}

func (encoder *streamEncoder) Finish() ([]format.Frame, []protocol.Loss, error) {
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) Fail(failure *protocol.Error) ([]format.Frame, []protocol.Loss, error) {
	encoder.failure = failure
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) startItem(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	if event.Item == nil {
		return nil, nil, nil
	}
	state := &encodeItemState{kind: event.Item.Kind, role: event.Item.Role, parts: map[int]*encodePartState{}, position: -1}
	encoder.items[event.ItemIndex] = state
	if event.Item.Kind == protocol.ItemReasoning && event.Item.Reasoning != nil && event.Item.Reasoning.Signature != nil {
		return nil, []protocol.Loss{protocol.Loss{Path: "signature", Code: protocol.LossSignatureStripped, Class: protocol.LossDegraded, Detail: "Chat has no field for reasoning signatures", To: protocol.Chat}}, nil
	}
	if event.Item.Kind == protocol.ItemToolCall || event.Item.Kind == protocol.ItemServerToolCall {
		if event.Item.ToolCall == nil || event.Item.ToolCall.Kind == protocol.ToolServer {
			return nil, []protocol.Loss{format.Degraded("tool_call", "provider-executed tool call cannot be represented by Chat", protocol.Chat)}, nil
		}
		state.isTool = true
		state.tool = event.Item.ToolCall
		state.position = encoder.tools
		encoder.tools++
		var losses []protocol.Loss
		if state.tool.Signature != nil {
			losses = append(losses, protocol.Loss{Path: "signature", Code: protocol.LossSignatureStripped, Class: protocol.LossDegraded, Detail: "Chat has no field for tool call signatures", To: protocol.Chat})
		}
		entry := map[string]any{"index": state.position, "id": state.tool.ID, "type": "function", "function": map[string]any{"name": state.tool.Name, "arguments": ""}}
		if state.tool.Kind == protocol.ToolCustom {
			entry = map[string]any{"index": state.position, "id": state.tool.ID, "type": "custom", "custom": map[string]any{"name": state.tool.Name, "input": ""}}
		}
		return encoder.delta(map[string]any{"tool_calls": []any{entry}}), losses, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) startPart(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil || event.Part == nil {
		return nil, nil, nil
	}
	part := &encodePartState{kind: event.Part.Kind, citations: event.Part.Citations, text: protocol.StreamedContent(*event.Part)}
	state.parts[event.PartIndex] = part
	switch part.kind {
	case protocol.PartMedia:
		return nil, []protocol.Loss{format.Degraded("media", "Chat cannot stream generated media", protocol.Chat)}, nil
	case protocol.PartNative, protocol.PartRedactedReasoning:
		return nil, []protocol.Loss{format.Degraded("part", "the provider part cannot be represented by Chat", protocol.Chat)}, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) partDelta(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
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

func (encoder *streamEncoder) stopPart(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
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
		if content := protocol.StreamedContent(*event.Part); content != "" && !part.streamed {
			part.text = content
		}
	}
	var frames []format.Frame
	var losses []protocol.Loss
	if part.kind == protocol.PartToolArguments && !part.streamed && part.text != "" {
		frames = append(frames, encoder.deltaFor(state, part, part.text)...)
		part.streamed = true
		state.sentArgs = true
	}
	if part.kind == protocol.PartToolArguments && part.streamed {
		state.sentArgs = true
	}
	if len(part.citations) > 0 && state.isTool == false {
		if annotations, nested := encodeChatCitations(part.citations); len(annotations) > 0 {
			frames = append(frames, encoder.delta(map[string]any{"annotations": annotations})...)
			losses = append(losses, nested...)
		} else {
			losses = append(losses, nested...)
		}
	}
	return frames, losses, nil
}

func (encoder *streamEncoder) stopItem(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil {
		return nil, nil, nil
	}
	var losses []protocol.Loss
	if event.Item != nil && event.Item.Reasoning != nil && event.Item.Reasoning.Signature != nil {
		losses = append(losses, protocol.Loss{Path: "signature", Code: protocol.LossSignatureStripped, Class: protocol.LossDegraded, Detail: "Chat has no field for reasoning signatures", To: protocol.Chat})
	}
	if state.isTool && !state.sentArgs {
		arguments := ""
		if event.Item != nil && event.Item.ToolCall != nil {
			arguments = event.Item.ToolCall.Arguments
		}
		if state.tool != nil && state.tool.Kind != protocol.ToolCustom {
			arguments = format.EmptyObjectIfBlank(arguments)
		}
		if arguments != "" {
			part := &encodePartState{kind: protocol.PartToolArguments}
			return encoder.deltaFor(state, part, arguments), losses, nil
		}
	}
	return nil, losses, nil
}

func (encoder *streamEncoder) deltaFor(state *encodeItemState, part *encodePartState, text string) []format.Frame {
	if !state.isTool {
		switch part.kind {
		case protocol.PartReasoningText, protocol.PartReasoningSummary:
			return encoder.delta(map[string]any{"reasoning_content": text})
		case protocol.PartRefusal:
			return encoder.delta(map[string]any{"refusal": text})
		case protocol.PartText:
			return encoder.delta(map[string]any{"content": text})
		}
		return nil
	}
	if state.position < 0 {
		return nil
	}
	field, key := map[string]any{"arguments": text}, "function"
	if state.tool != nil && state.tool.Kind == protocol.ToolCustom {
		field, key = map[string]any{"input": text}, "custom"
	}
	return encoder.delta(map[string]any{"tool_calls": []any{map[string]any{"index": state.position, key: field}}})
}

func (encoder *streamEncoder) terminal() []format.Frame {
	if encoder.stopped {
		return nil
	}
	encoder.stopped = true
	if encoder.failure != nil {
		_, body := Codec{}.EncodeError(encoder.failure, true)
		return []format.Frame{{Data: body}, {Data: []byte("[DONE]")}}
	}
	finishKind := encoder.finish
	if (finishKind == "" || finishKind == protocol.FinishStop) && encoder.tools > 0 {
		finishKind = protocol.FinishToolCalls
	}
	finish := format.ChatFinish(finishKind)
	if finish == "" {
		finish = "stop"
	}
	root := encoder.chunk(map[string]any{}, finish)
	if encoder.usage != nil {
		root["usage"] = format.EncodeChatUsage(encoder.usage)
	}
	return []format.Frame{{Data: protocol.Marshal(root)}, {Data: []byte("[DONE]")}}
}

func (encoder *streamEncoder) delta(delta map[string]any) []format.Frame {
	return []format.Frame{{Data: protocol.Marshal(encoder.chunk(delta, ""))}}
}

func (encoder *streamEncoder) chunk(delta map[string]any, finish string) map[string]any {
	var reason any
	if finish != "" {
		reason = finish
	}
	return map[string]any{
		"id":      encoder.meta.ID,
		"object":  "chat.completion.chunk",
		"model":   encoder.meta.Model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": reason}},
	}
}
