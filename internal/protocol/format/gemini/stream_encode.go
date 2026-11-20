package gemini

import (
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type encodePart struct {
	kind      protocol.PartKind
	text      strings.Builder
	media     *protocol.Media
	citations []protocol.Citation
}

type encodeItemState struct {
	item   protocol.Item
	parts  map[int]*encodePart
	order  []int
	sent   bool
	closed bool
}

type streamEncoder struct {
	meta    protocol.ResponseMeta
	peer    format.Peer
	items   map[int]*encodeItemState
	usage   *protocol.Usage
	finish  protocol.Finish
	failure *protocol.Error
	stopped bool
}

func (Codec) NewStreamEncoder(meta protocol.ResponseMeta, peer format.Peer) format.StreamEncoder {
	return &streamEncoder{meta: meta, peer: peer, items: map[int]*encodeItemState{}}
}

func (encoder *streamEncoder) Encode(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	switch event.Kind {
	case protocol.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
		}
		return nil, nil, nil
	case protocol.EventItemStart:
		if event.Item != nil {
			encoder.items[event.ItemIndex] = &encodeItemState{item: event.Item.Clone(), parts: map[int]*encodePart{}}
		}
		return nil, nil, nil
	case protocol.EventPartStart:
		state := encoder.items[event.ItemIndex]
		if state == nil || event.Part == nil {
			return nil, nil, nil
		}
		state.parts[event.PartIndex] = &encodePart{kind: event.Part.Kind, media: event.Part.Media, citations: event.Part.Citations}
		state.order = append(state.order, event.PartIndex)
		if event.Part.Kind == protocol.PartMedia {
			part, losses, err := format.EncodePart(*event.Part, &state.item, format.PartContext{Target: protocol.Gemini, Peer: encoder.peer, Role: protocol.RoleAssistant}, "media")
			if err != nil || part == nil {
				return nil, losses, err
			}
			return encoder.chunk([]any{part}, "", nil), losses, nil
		}
		if event.Part.Kind == protocol.PartNative || event.Part.Kind == protocol.PartRedactedReasoning {
			return nil, []protocol.Loss{format.Degraded("part", "the provider part cannot be represented by Gemini", protocol.Gemini)}, nil
		}
		return nil, nil, nil
	case protocol.EventPartDelta:
		return encoder.delta(event)
	case protocol.EventPartStop:
		return encoder.stopPart(event)
	case protocol.EventItemStop:
		return encoder.stopItem(event)
	case protocol.EventMessageDelta:
		if event.Usage != nil {
			encoder.usage = event.Usage
		}
		encoder.failure = event.Failure
		if event.Finish != nil {
			encoder.finish = *event.Finish
		}
		var losses []protocol.Loss
		if format.FinishLoss(encoder.finish.Kind, protocol.Gemini) {
			losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: string(encoder.finish.Kind), To: protocol.Gemini})
		}
		return nil, losses, nil
	case protocol.EventMessageStop:
		frames, losses := encoder.terminal()
		return frames, append(losses, format.UsageLosses(encoder.usage, protocol.Gemini)...), nil
	case protocol.EventError:
		encoder.failure = event.Failure
		frames, losses := encoder.terminal()
		return frames, losses, nil
	case protocol.EventPing:
		return nil, nil, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) Resume(priorItems, priorParts int, priorSequence int64) {}

func (encoder *streamEncoder) Finish() ([]format.Frame, []protocol.Loss, error) {
	frames, losses := encoder.terminal()
	return frames, losses, nil
}

func (encoder *streamEncoder) Fail(failure *protocol.Error) ([]format.Frame, []protocol.Loss, error) {
	encoder.failure = failure
	frames, losses := encoder.terminal()
	return frames, losses, nil
}

func (encoder *streamEncoder) delta(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
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
	case protocol.PartText, protocol.PartReasoningText, protocol.PartReasoningSummary:
		payload := map[string]any{"text": event.Delta}
		if part.kind != protocol.PartText {
			payload["thought"] = true
		}
		return encoder.chunk([]any{payload}, "", nil), nil, nil
	case protocol.PartRefusal:
		return encoder.chunk([]any{map[string]any{"text": event.Delta}}, "", nil), nil, nil
	default:
		return nil, nil, nil
	}
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
	if event.Part != nil && len(event.Part.Citations) > 0 {
		part.citations = event.Part.Citations
	}
	if event.Part != nil {
		if content := protocol.StreamedContent(*event.Part); content != "" && part.text.Len() == 0 {
			part.text.WriteString(content)
			payload := map[string]any{"text": content}
			if part.kind != protocol.PartText {
				payload["thought"] = true
			}
			return encoder.chunk([]any{payload}, "", nil), nil, nil
		}
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) signatureChunk(state *encodeItemState, part *encodePart) ([]format.Frame, []protocol.Loss) {
	if state.item.Reasoning == nil {
		return nil, nil
	}
	var losses []protocol.Loss
	signature := state.item.Reasoning.Signature
	if signature == nil || signature.Blob == "" {
		signature = &protocol.Signature{Vendor: protocol.VendorSafeAgent, Blob: "gateway"}
		losses = append(losses, protocol.Loss{Path: "reasoning.signature", Code: protocol.LossGeminiDummySignature, Class: protocol.LossDegraded, Detail: "the upstream returned no thought signature; the gateway envelope keeps the reasoning replayable", To: protocol.Gemini})
	}
	encoded := encoder.peer.WireSignature(signature, part.text.String())
	if encoded == "" {
		return nil, losses
	}
	return encoder.chunk([]any{map[string]any{"text": "", "thought": true, "thoughtSignature": encoded}}, "", nil), losses
}

func (encoder *streamEncoder) stopItem(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.items[event.ItemIndex]
	if state == nil || state.closed {
		return nil, nil, nil
	}
	state.closed = true
	var frames []format.Frame
	var losses []protocol.Loss
	if event.Item != nil {
		protocol.MergeItem(&state.item, *event.Item)
	}
	if state.item.Kind == protocol.ItemReasoning {
		if len(state.parts) == 0 && state.item.Reasoning != nil && state.item.Reasoning.Signature != nil {
			nested, nestedLosses := encoder.signatureChunk(state, &encodePart{kind: protocol.PartReasoningText})
			frames = append(frames, nested...)
			losses = append(losses, nestedLosses...)
		}
		for _, part := range state.parts {
			if part.kind != protocol.PartReasoningText && part.kind != protocol.PartReasoningSummary {
				continue
			}
			nested, nestedLosses := encoder.signatureChunk(state, part)
			frames = append(frames, nested...)
			losses = append(losses, nestedLosses...)
		}
	}
	if state.item.ToolCall != nil {
		for _, part := range state.parts {
			if part.kind == protocol.PartToolArguments && part.text.Len() > 0 {
				state.item.ToolCall.Arguments = part.text.String()
			}
		}
	}
	switch state.item.Kind {
	case protocol.ItemToolCall:
		if state.item.ToolCall == nil || state.item.ToolCall.Kind == protocol.ToolServer {
			return nil, []protocol.Loss{format.Degraded("tool_call", "provider-executed tool call cannot be represented by Gemini", protocol.Gemini)}, nil
		}
		entry, nested := encodeFunctionCall(&state.item, encoder.peer, "item")
		losses = append(losses, nested...)
		frames = append(frames, encoder.chunk([]any{entry}, "", nil)...)
	case protocol.ItemServerToolCall, protocol.ItemServerToolResult, protocol.ItemNative:
		if native := format.RawObject(state.item.Raw, protocol.Gemini); native != nil {
			frames = append(frames, encoder.chunk([]any{native}, "", nil)...)
		} else {
			losses = append(losses, format.Degraded("item", "the provider item cannot be represented by Gemini", protocol.Gemini))
		}
	}
	return frames, losses, nil
}

func (encoder *streamEncoder) terminal() ([]format.Frame, []protocol.Loss) {
	if encoder.stopped {
		return nil, nil
	}
	encoder.stopped = true
	if encoder.failure != nil {
		_, body := Codec{}.EncodeError(encoder.failure, true)
		return []format.Frame{{Data: body}}, nil
	}
	var frames []format.Frame
	var citations []protocol.Citation
	for _, state := range encoder.items {
		for _, part := range state.parts {
			citations = append(citations, part.citations...)
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
		frames = append(frames, format.Frame{Data: protocol.Marshal(root)})
	}
	return append(frames, encoder.chunk(nil, format.GeminiFinish(encoder.finish.Kind), encoder.usage)...), losses
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
