package gemini

import (
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type encodePart struct {
	kind      model.PartKind
	text      strings.Builder
	citations []model.Citation
}

type encodeItemState struct {
	item   model.Item
	parts  map[int]*encodePart
	order  []int
	closed bool
}

type streamEncoder struct {
	meta      model.ResponseMeta
	peer      codec.Peer
	items     map[int]*encodeItemState
	itemOrder []int
	usage     *model.Usage
	finish    model.Finish
	failure   *model.Error
	stopped   bool
}

func (Codec) NewStreamEncoder(meta model.ResponseMeta, peer codec.Peer) codec.StreamEncoder {
	return &streamEncoder{meta: meta, peer: peer, items: map[int]*encodeItemState{}}
}

func (encoder *streamEncoder) Encode(event model.Event) ([]codec.Frame, []model.Loss, error) {
	switch event.Kind {
	case model.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
		}
		return nil, nil, nil
	case model.EventItemStart:
		if event.Item != nil {
			encoder.items[event.ItemIndex] = &encodeItemState{item: event.Item.Clone(), parts: map[int]*encodePart{}}
			encoder.itemOrder = append(encoder.itemOrder, event.ItemIndex)
		}
		return nil, nil, nil
	case model.EventPartStart:
		state := encoder.items[event.ItemIndex]
		if state == nil || event.Part == nil {
			return nil, nil, nil
		}
		state.parts[event.PartIndex] = &encodePart{kind: event.Part.Kind, citations: event.Part.Citations}
		state.order = append(state.order, event.PartIndex)
		if event.Part.Kind == model.PartMedia {
			part, losses, err := codec.EncodePart(*event.Part, &state.item, codec.PartContext{Target: model.Gemini, Peer: encoder.peer, Role: model.RoleAssistant}, "media")
			if err != nil || part == nil {
				return nil, losses, err
			}
			return encoder.chunk([]any{part}, "", nil), losses, nil
		}
		if event.Part.Kind == model.PartNative || event.Part.Kind == model.PartRedactedReasoning {
			return nil, []model.Loss{codec.Degraded("part", "the provider part cannot be represented by Gemini", model.Gemini)}, nil
		}
		return nil, nil, nil
	case model.EventPartDelta:
		return encoder.delta(event)
	case model.EventPartStop:
		return encoder.stopPart(event)
	case model.EventItemStop:
		return encoder.stopItem(event)
	case model.EventMessageDelta:
		if event.Usage != nil {
			encoder.usage = event.Usage
		}
		encoder.failure = event.Failure
		if event.Finish != nil {
			encoder.finish = *event.Finish
		}
		var losses []model.Loss
		if codec.FinishLoss(encoder.finish.Kind, model.Gemini) {
			losses = append(losses, model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: string(encoder.finish.Kind), To: model.Gemini})
		}
		return nil, losses, nil
	case model.EventMessageStop:
		frames, losses := encoder.terminal()
		return frames, append(losses, codec.UsageLosses(encoder.usage, model.Gemini)...), nil
	case model.EventError:
		encoder.failure = event.Failure
		frames, losses := encoder.terminal()
		return frames, losses, nil
	case model.EventPing:
		return nil, nil, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) Resume(priorItems, priorParts int) {}

func (encoder *streamEncoder) Finish() ([]codec.Frame, []model.Loss, error) {
	frames, losses := encoder.terminal()
	return frames, losses, nil
}

func (encoder *streamEncoder) Fail(failure *model.Error) ([]codec.Frame, []model.Loss, error) {
	encoder.failure = failure
	frames, losses := encoder.terminal()
	return frames, losses, nil
}

func (encoder *streamEncoder) delta(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil || event.Delta == "" {
		return nil, nil, nil
	}
	part := state.parts[event.PartIndex]
	if part == nil {
		return nil, nil, nil
	}
	part.text.WriteString(event.Delta)
	switch part.kind {
	case model.PartText, model.PartReasoningText, model.PartReasoningSummary:
		payload := map[string]any{"text": event.Delta}
		if part.kind != model.PartText {
			payload["thought"] = true
		}
		return encoder.chunk([]any{payload}, "", nil), nil, nil
	case model.PartRefusal:
		return encoder.chunk([]any{map[string]any{"text": event.Delta}}, "", nil), nil, nil
	default:
		return nil, nil, nil
	}
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
	if event.Part != nil && len(event.Part.Citations) > 0 {
		part.citations = event.Part.Citations
	}
	if event.Part != nil {
		if content := model.StreamedContent(*event.Part); content != "" && part.text.Len() == 0 {
			part.text.WriteString(content)
			payload := map[string]any{"text": content}
			if part.kind != model.PartText {
				payload["thought"] = true
			}
			return encoder.chunk([]any{payload}, "", nil), nil, nil
		}
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) signatureChunk(state *encodeItemState, part *encodePart) ([]codec.Frame, []model.Loss) {
	if state.item.Reasoning == nil {
		return nil, nil
	}
	var losses []model.Loss
	signature := state.item.Reasoning.Signature
	if signature == nil || signature.Blob == "" {
		if part.kind != model.PartReasoningText {
			return nil, nil
		}
		signature = &model.Signature{Vendor: model.VendorSafeAgent, Blob: "gateway"}
		losses = append(losses, model.Loss{Path: "reasoning.signature", Code: model.LossGeminiDummySignature, Class: model.LossDegraded, Detail: "the upstream returned no thought signature; the gateway envelope keeps the reasoning replayable", To: model.Gemini})
	} else if !codec.SignatureCoversText(signature, part.kind, part.text.String()) {
		return nil, nil
	}
	encoded := encoder.peer.WireSignature(signature, part.text.String())
	if encoded == "" {
		return nil, losses
	}
	return encoder.chunk([]any{map[string]any{"text": "", "thought": true, "thoughtSignature": encoded}}, "", nil), losses
}

func (encoder *streamEncoder) stopItem(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil || state.closed {
		return nil, nil, nil
	}
	state.closed = true
	var frames []codec.Frame
	var losses []model.Loss
	if event.Item != nil {
		model.MergeItem(&state.item, *event.Item)
	}
	if state.item.Kind == model.ItemReasoning {
		if len(state.parts) == 0 && state.item.Reasoning != nil && state.item.Reasoning.Signature != nil {
			nested, nestedLosses := encoder.signatureChunk(state, &encodePart{kind: model.PartReasoningText})
			frames = append(frames, nested...)
			losses = append(losses, nestedLosses...)
		}
		for _, part := range encoder.orderedParts(state) {
			if part.kind != model.PartReasoningText && part.kind != model.PartReasoningSummary {
				continue
			}
			nested, nestedLosses := encoder.signatureChunk(state, part)
			frames = append(frames, nested...)
			losses = append(losses, nestedLosses...)
		}
	}
	if state.item.ToolCall != nil {
		for _, part := range encoder.orderedParts(state) {
			if part.kind == model.PartToolArguments && part.text.Len() > 0 {
				state.item.ToolCall.Arguments = part.text.String()
			}
		}
	}
	switch state.item.Kind {
	case model.ItemToolCall:
		if state.item.ToolCall == nil || state.item.ToolCall.Kind == model.ToolServer {
			return nil, []model.Loss{codec.Degraded("tool_call", "provider-executed tool call cannot be represented by Gemini", model.Gemini)}, nil
		}
		entry, nested := encodeFunctionCall(&state.item, encoder.peer, "item")
		losses = append(losses, nested...)
		frames = append(frames, encoder.chunk([]any{entry}, "", nil)...)
	case model.ItemServerToolCall, model.ItemServerToolResult, model.ItemNative:
		if native := codec.RawObject(state.item.Raw, model.Gemini); native != nil {
			frames = append(frames, encoder.chunk([]any{native}, "", nil)...)
		} else {
			losses = append(losses, codec.Degraded("item", "the provider item cannot be represented by Gemini", model.Gemini))
		}
	}
	return frames, losses, nil
}

func (encoder *streamEncoder) orderedParts(state *encodeItemState) []*encodePart {
	parts := make([]*encodePart, 0, len(state.order))
	for _, index := range state.order {
		if part := state.parts[index]; part != nil {
			parts = append(parts, part)
		}
	}
	return parts
}

func (encoder *streamEncoder) terminal() ([]codec.Frame, []model.Loss) {
	if encoder.stopped {
		return nil, nil
	}
	encoder.stopped = true
	if encoder.failure != nil {
		_, body := Codec{}.EncodeError(encoder.failure, true)
		return []codec.Frame{{Data: body}}, nil
	}
	var frames []codec.Frame
	var citations []model.Citation
	for _, index := range encoder.itemOrder {
		if state := encoder.items[index]; state != nil {
			for _, part := range encoder.orderedParts(state) {
				citations = append(citations, part.citations...)
			}
		}
	}
	grounding, losses := encodeCitations(citations)
	if grounding != nil {
		candidate := map[string]any{"index": 0}
		for key, value := range grounding {
			candidate[key] = value
		}
		root := map[string]any{"candidates": []any{candidate}}
		if encoder.meta.ID != "" {
			root["responseId"] = encoder.meta.ID
		}
		if encoder.meta.Model != "" {
			root["modelVersion"] = encoder.meta.Model
		}
		frames = append(frames, codec.Frame{Data: model.Marshal(root)})
	}
	return append(frames, encoder.chunk(nil, codec.GeminiFinish(encoder.finish.Kind), encoder.usage)...), losses
}

func (encoder *streamEncoder) chunk(parts []any, finish string, usage *model.Usage) []codec.Frame {
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
		root["usageMetadata"] = codec.EncodeGeminiUsage(usage)
	}
	return []codec.Frame{{Data: model.Marshal(root)}}
}
