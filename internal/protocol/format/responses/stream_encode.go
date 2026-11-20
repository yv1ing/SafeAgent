package responses

import (
	"strings"
	"time"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type outputPart struct {
	kind         protocol.PartKind
	contentIndex int
	summaryIndex int
	text         strings.Builder
	citations    []protocol.Citation
	streamed     bool
}

type outputItemState struct {
	index        int
	item         protocol.Item
	itemID       string
	peer         format.Peer
	toolPart     int
	contentParts int
	summaryParts int
	parts        map[int]*outputPart
	order        []int
	closed       bool
}

type streamEncoder struct {
	meta     protocol.ResponseMeta
	peer     format.Peer
	created  int64
	items    map[int]*outputItemState
	order    []*outputItemState
	outputs  int
	sequence int
	started  bool
	stopped  bool
	usage    *protocol.Usage
	finish   protocol.Finish
	failure  *protocol.Error
}

func (Codec) NewStreamEncoder(meta protocol.ResponseMeta, peer format.Peer) format.StreamEncoder {
	return &streamEncoder{meta: meta, peer: peer, items: map[int]*outputItemState{}, created: time.Now().Unix()}
}

func (encoder *streamEncoder) Encode(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	switch event.Kind {
	case protocol.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
			if event.Meta.Created != 0 {
				encoder.created = event.Meta.Created
			}
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
		encoder.usage, encoder.failure = event.Usage, event.Failure
		if event.Finish != nil {
			encoder.finish = *event.Finish
		}
		var losses []protocol.Loss
		if format.FinishLoss(encoder.finish.Kind, protocol.Responses) {
			losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: string(encoder.finish.Kind), To: protocol.Responses})
		}
		return nil, losses, nil
	case protocol.EventMessageStop:
		return encoder.terminal(), format.UsageLosses(encoder.usage, protocol.Responses), nil
	case protocol.EventError:
		encoder.failure = event.Failure
		return encoder.terminal(), nil, nil
	case protocol.EventPing:
		return nil, nil, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) Resume(priorItems, priorParts int, priorSequence int64) {
	encoder.started = true
	encoder.outputs += priorItems
	if priorSequence > int64(encoder.sequence) {
		encoder.sequence = int(priorSequence)
	}
}

func (encoder *streamEncoder) Finish() ([]format.Frame, []protocol.Loss, error) {
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) Fail(failure *protocol.Error) ([]format.Frame, []protocol.Loss, error) {
	encoder.failure = failure
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) begin() []format.Frame {
	if encoder.started {
		return nil
	}
	encoder.started = true
	frames := encoder.emit("response.created", map[string]any{"response": encoder.snapshot("in_progress")})
	return append(frames, encoder.emit("response.in_progress", map[string]any{"response": encoder.snapshot("in_progress")})...)
}

func (encoder *streamEncoder) emit(kind string, payload map[string]any) []format.Frame {
	payload["type"] = kind
	payload["sequence_number"] = encoder.sequence
	encoder.sequence++
	return []format.Frame{{Event: kind, Data: protocol.Marshal(payload)}}
}

func (encoder *streamEncoder) snapshot(status string) map[string]any {
	snapshot := map[string]any{
		"id": encoder.meta.ID, "object": "response", "created_at": encoder.created,
		"model": encoder.meta.Model, "status": status, "output": encoder.outputItems(),
	}
	if encoder.meta.ServiceTier != "" {
		snapshot["service_tier"] = encoder.meta.ServiceTier
	}
	if encoder.usage != nil {
		snapshot["usage"] = format.EncodeResponsesUsage(encoder.usage)
	}
	if encoder.failure != nil {
		snapshot["error"] = map[string]any{"code": encoder.failure.Code, "message": encoder.failure.Message}
	}
	if status == "incomplete" {
		snapshot["incomplete_details"] = map[string]any{"reason": incompleteReason(encoder.finish)}
	}
	return snapshot
}

func (encoder *streamEncoder) outputItems() []any {
	items := make([]any, 0, len(encoder.order))
	for _, state := range encoder.order {
		if state.closed {
			items = append(items, finalOutputMap(state))
		}
	}
	return items
}

func (encoder *streamEncoder) startItem(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	frames := encoder.begin()
	if event.Item == nil {
		return frames, nil, nil
	}
	item := event.Item.Clone()
	itemKind := item.Kind
	if itemKind == "" {
		itemKind = protocol.ItemMessage
	}
	var losses []protocol.Loss
	switch itemKind {
	case protocol.ItemServerToolCall, protocol.ItemServerToolResult, protocol.ItemNative:
		if format.RawObject(item.Raw, protocol.Responses) == nil {
			return frames, []protocol.Loss{format.Degraded("item", "the provider item cannot be represented by Responses", protocol.Responses)}, nil
		}
	case protocol.ItemToolCall:
		if item.ToolCall == nil || item.ToolCall.Kind == protocol.ToolServer {
			return frames, []protocol.Loss{format.Degraded("item", "provider-executed tool calls cannot be represented by this gateway", protocol.Responses)}, nil
		}
	}
	outputIndex := encoder.outputs
	encoder.outputs++
	id, synthesized := protocol.ItemID(protocol.Responses, itemKind, item.ID, toolCallID(&item), outputIndex)
	if synthesized {
		losses = append(losses, protocol.Loss{Path: "id", Code: protocol.LossItemIDMapped, Class: protocol.LossDegraded, Detail: "the target response id was synthesized", To: protocol.Responses})
	}
	state := &outputItemState{index: outputIndex, item: item, itemID: id, peer: encoder.peer, parts: map[int]*outputPart{}, toolPart: -1}
	encoder.items[event.ItemIndex] = state
	encoder.order = append(encoder.order, state)
	announced := map[string]any{"id": id}
	switch itemKind {
	case protocol.ItemMessage:
		role := string(item.Role)
		if role == "" {
			role = "assistant"
		}
		announced["type"], announced["role"], announced["status"], announced["content"] = "message", role, "in_progress", []any{}
	case protocol.ItemReasoning:
		announced["type"], announced["summary"] = "reasoning", []any{}
		if itemHasFullReasoning(&item) {
			announced["content"] = []any{}
		}
	case protocol.ItemServerToolCall, protocol.ItemServerToolResult:
		native := format.RawObject(item.Raw, protocol.Responses)
		native["id"] = id
		announced = native
	case protocol.ItemToolCall:
		if item.ToolCall.Kind == protocol.ToolCustom {
			announced["type"], announced["call_id"], announced["name"], announced["input"], announced["status"] = "custom_tool_call", item.ToolCall.ID, item.ToolCall.Name, "", "in_progress"
		} else {
			announced["type"], announced["call_id"], announced["name"], announced["arguments"], announced["status"] = "function_call", item.ToolCall.ID, item.ToolCall.Name, "", "in_progress"
		}
	case protocol.ItemNative:
		announced = format.RawObject(item.Raw, protocol.Responses)
		announced["id"] = id
	}
	if itemKind == protocol.ItemToolCall && item.ToolCall != nil && item.ToolCall.Signature != nil {
		losses = append(losses, format.Degraded("signature", "Responses has no tool call signature", protocol.Responses))
	}
	frames = append(frames, encoder.emit("response.output_item.added", map[string]any{"output_index": outputIndex, "item": announced})...)
	return frames, losses, nil
}

func (encoder *streamEncoder) itemOf(irIndex int) *outputItemState { return encoder.items[irIndex] }

func toolCallID(item *protocol.Item) string {
	if item != nil && item.ToolCall != nil {
		return item.ToolCall.ID
	}
	return ""
}

func itemHasFullReasoning(item *protocol.Item) bool {
	for _, part := range item.Parts {
		if part.Kind == protocol.PartReasoningText {
			return true
		}
	}
	return false
}

func (encoder *streamEncoder) startPart(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.itemOf(event.ItemIndex)
	if state == nil || event.Part == nil {
		return nil, nil, nil
	}
	part := &outputPart{kind: event.Part.Kind, citations: event.Part.Citations}
	state.parts[event.PartIndex] = part
	state.order = append(state.order, event.PartIndex)
	switch part.kind {
	case protocol.PartText, protocol.PartRefusal, protocol.PartReasoningText:
		part.contentIndex = state.contentParts
		state.contentParts++
		empty := map[string]any{"type": "output_text", "text": "", "annotations": []any{}}
		if part.kind == protocol.PartRefusal {
			empty = map[string]any{"type": "refusal", "refusal": ""}
		}
		if part.kind == protocol.PartReasoningText {
			empty = map[string]any{"type": "reasoning_text", "text": ""}
		}
		return encoder.emit("response.content_part.added", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": part.contentIndex, "part": empty}), nil, nil
	case protocol.PartReasoningSummary:
		part.summaryIndex = state.summaryParts
		state.summaryParts++
		return encoder.emit("response.reasoning_summary_part.added", map[string]any{"item_id": state.itemID, "output_index": state.index, "summary_index": part.summaryIndex, "part": map[string]any{"type": "summary_text", "text": ""}}), nil, nil
	case protocol.PartToolArguments:
		state.toolPart = event.PartIndex
	case protocol.PartMedia:
		return nil, []protocol.Loss{format.Degraded("media", "Responses cannot stream generated media", protocol.Responses)}, nil
	case protocol.PartNative, protocol.PartRedactedReasoning:
		return nil, []protocol.Loss{format.Degraded("part", "the provider part cannot be represented by Responses", protocol.Responses)}, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) partDelta(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.itemOf(event.ItemIndex)
	if state == nil || event.Delta == "" {
		return nil, nil, nil
	}
	part := state.parts[event.PartIndex]
	if part == nil {
		return nil, nil, nil
	}
	part.streamed = true
	part.text.WriteString(event.Delta)
	return encoder.deltaFrames(state, part, event.Delta), nil, nil
}

func (encoder *streamEncoder) deltaFrames(state *outputItemState, part *outputPart, text string) []format.Frame {
	body := map[string]any{"item_id": state.itemID, "output_index": state.index, "delta": text}
	switch part.kind {
	case protocol.PartText:
		body["content_index"] = part.contentIndex
		return encoder.emit("response.output_text.delta", body)
	case protocol.PartRefusal:
		body["content_index"] = part.contentIndex
		return encoder.emit("response.refusal.delta", body)
	case protocol.PartReasoningSummary:
		body["summary_index"] = part.summaryIndex
		return encoder.emit("response.reasoning_summary_text.delta", body)
	case protocol.PartReasoningText:
		body["content_index"] = part.contentIndex
		return encoder.emit("response.reasoning_text.delta", body)
	case protocol.PartToolArguments:
		if state.item.ToolCall != nil && state.item.ToolCall.Kind == protocol.ToolCustom {
			return encoder.emit("response.custom_tool_call_input.delta", body)
		}
		return encoder.emit("response.function_call_arguments.delta", body)
	}
	return nil
}

func (encoder *streamEncoder) stopPart(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.itemOf(event.ItemIndex)
	if state == nil {
		return nil, nil, nil
	}
	part := state.parts[event.PartIndex]
	if part == nil {
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
			frames = append(frames, encoder.deltaFrames(state, part, content)...)
			part.streamed = true
		}
	}
	if part.kind == protocol.PartToolArguments && !part.streamed {
		arguments := ""
		if state.item.ToolCall != nil {
			arguments = state.item.ToolCall.Arguments
		}
		if arguments != "" {
			part.text.WriteString(arguments)
			frames = append(frames, encoder.deltaFrames(state, part, arguments)...)
			part.streamed = true
		}
	}
	if len(part.citations) > 0 && part.kind == protocol.PartText {
		for index, citation := range part.citations {
			annotation, nested := format.EncodeCitation(citation, protocol.Responses)
			losses = append(losses, nested...)
			if annotation == nil {
				continue
			}
			frames = append(frames, encoder.emit("response.output_text.annotation.added", map[string]any{
				"item_id": state.itemID, "output_index": state.index, "content_index": part.contentIndex,
				"annotation_index": index, "annotation": annotation,
			})...)
		}
	}
	return frames, losses, nil
}

func (encoder *streamEncoder) stopItem(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.itemOf(event.ItemIndex)
	if state == nil || state.closed {
		return nil, nil, nil
	}
	if event.Item != nil {
		protocol.MergeItem(&state.item, *event.Item)
	}
	for _, part := range state.parts {
		if part.kind == protocol.PartToolArguments && !part.streamed && state.item.ToolCall != nil {
			arguments := state.item.ToolCall.Arguments
			if arguments != "" {
				part.text.WriteString(arguments)
				part.streamed = true
			}
		}
	}
	state.closed = true
	final := finalOutputMap(state)
	return encoder.emit("response.output_item.done", map[string]any{"output_index": state.index, "item": final}), nil, nil
}

func finalOutputMap(state *outputItemState) map[string]any {
	item := map[string]any{"id": state.itemID}
	switch state.item.Kind {
	case protocol.ItemMessage, "":
		role := string(state.item.Role)
		if role == "" {
			role = "assistant"
		}
		item["type"], item["role"], item["status"] = "message", role, "completed"
		content := []any{}
		for _, partIndex := range state.order {
			part := state.parts[partIndex]
			if part == nil {
				continue
			}
			switch part.kind {
			case protocol.PartText:
				entry := map[string]any{"type": "output_text", "text": part.text.String(), "annotations": []any{}}
				if annotations, _ := encodeAnnotations(part.citations); len(annotations) > 0 {
					entry["annotations"] = annotations
				}
				content = append(content, entry)
			case protocol.PartRefusal:
				content = append(content, map[string]any{"type": "refusal", "refusal": part.text.String()})
			}
		}
		item["content"] = content
	case protocol.ItemReasoning:
		item["type"] = "reasoning"
		summary, content := []any{}, []any{}
		for _, partIndex := range state.order {
			part := state.parts[partIndex]
			if part == nil {
				continue
			}
			switch part.kind {
			case protocol.PartReasoningSummary:
				summary = append(summary, map[string]any{"type": "summary_text", "text": part.text.String()})
			case protocol.PartReasoningText:
				content = append(content, map[string]any{"type": "reasoning_text", "text": part.text.String()})
			}
		}
		item["summary"] = summary
		if len(content) > 0 {
			item["content"] = content
		}
		if state.item.Reasoning != nil && state.item.Reasoning.Signature != nil {
			if blob := signatureBlob(state); blob != "" {
				item["encrypted_content"] = blob
			}
		}
	case protocol.ItemToolCall:
		if state.item.ToolCall == nil {
			item["type"] = "function_call"
			break
		}
		if state.item.ToolCall.Kind == protocol.ToolCustom {
			item["type"], item["call_id"], item["name"], item["input"], item["status"] = "custom_tool_call", state.item.ToolCall.ID, state.item.ToolCall.Name, state.toolText(), "completed"
		} else {
			arguments := state.toolText()
			if arguments == "" {
				arguments = "{}"
			}
			item["type"], item["call_id"], item["name"], item["arguments"], item["status"] = "function_call", state.item.ToolCall.ID, state.item.ToolCall.Name, arguments, "completed"
		}
	case protocol.ItemNative, protocol.ItemServerToolCall, protocol.ItemServerToolResult:
		if native := format.RawObject(state.item.Raw, protocol.Responses); native != nil {
			native["id"] = state.itemID
			return native
		}
		item["type"] = "message"
	}
	return item
}

func (state *outputItemState) toolText() string {
	if state.toolPart >= 0 {
		if part := state.parts[state.toolPart]; part != nil {
			if part.text.Len() > 0 {
				return part.text.String()
			}
		}
	}
	if state.item.ToolCall != nil {
		return state.item.ToolCall.Arguments
	}
	return ""
}

func signatureBlob(state *outputItemState) string {
	if state.item.Reasoning == nil || state.item.Reasoning.Signature == nil {
		return ""
	}
	return state.peer.WireSignature(state.item.Reasoning.Signature, state.reasoningText())
}

func (state *outputItemState) reasoningText() string {
	var text strings.Builder
	for _, partIndex := range state.order {
		part := state.parts[partIndex]
		if part != nil && (part.kind == protocol.PartReasoningText || part.kind == protocol.PartReasoningSummary) {
			text.WriteString(part.text.String())
		}
	}
	if text.Len() == 0 {
		return state.item.Text()
	}
	return text.String()
}

func encodeAnnotations(citations []protocol.Citation) ([]any, []protocol.Loss) {
	var losses []protocol.Loss
	annotations := make([]any, 0, len(citations))
	for _, citation := range citations {
		entry, nested := format.EncodeCitation(citation, protocol.Responses)
		losses = append(losses, nested...)
		if entry != nil {
			annotations = append(annotations, entry)
		}
	}
	return annotations, losses
}

func (encoder *streamEncoder) terminal() []format.Frame {
	if encoder.stopped {
		return nil
	}
	encoder.stopped = true
	for _, state := range encoder.order {
		state.closed = true
	}
	status := format.ResponsesStatus(encoder.finish.Kind)
	if encoder.failure != nil {
		status = "failed"
	}
	kind := "response.completed"
	switch status {
	case "incomplete":
		kind = "response.incomplete"
	case "failed":
		kind = "response.failed"
	}
	return encoder.emit(kind, map[string]any{"response": encoder.snapshot(status)})
}
