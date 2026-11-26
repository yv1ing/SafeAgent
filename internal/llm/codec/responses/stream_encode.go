package responses

import (
	"strings"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type outputPart struct {
	kind         model.PartKind
	contentIndex int
	summaryIndex int
	text         strings.Builder
	citations    []model.Citation
	streamed     bool
}

type outputItemState struct {
	index        int
	item         model.Item
	itemID       string
	peer         codec.Peer
	toolPart     int
	contentParts int
	summaryParts int
	parts        map[int]*outputPart
	order        []int
	closed       bool
}

type streamEncoder struct {
	meta     model.ResponseMeta
	peer     codec.Peer
	created  int64
	items    map[int]*outputItemState
	order    []*outputItemState
	outputs  int
	sequence int
	started  bool
	stopped  bool
	usage    *model.Usage
	finish   model.Finish
	failure  *model.Error
}

func (Codec) NewStreamEncoder(meta model.ResponseMeta, peer codec.Peer) codec.StreamEncoder {
	return &streamEncoder{meta: meta, peer: peer, items: map[int]*outputItemState{}, created: time.Now().Unix()}
}

func (encoder *streamEncoder) Encode(event model.Event) ([]codec.Frame, []model.Loss, error) {
	switch event.Kind {
	case model.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
			if event.Meta.Created != 0 {
				encoder.created = event.Meta.Created
			}
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
		encoder.usage, encoder.failure = event.Usage, event.Failure
		if event.Finish != nil {
			encoder.finish = *event.Finish
		}
		var losses []model.Loss
		if codec.FinishLoss(encoder.finish.Kind, model.Responses) {
			losses = append(losses, model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: string(encoder.finish.Kind), To: model.Responses})
		}
		return nil, losses, nil
	case model.EventMessageStop:
		return encoder.terminal(), codec.UsageLosses(encoder.usage, model.Responses), nil
	case model.EventError:
		encoder.failure = event.Failure
		return encoder.terminal(), nil, nil
	case model.EventPing:
		return nil, nil, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) Finish() ([]codec.Frame, []model.Loss, error) {
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) Fail(failure *model.Error) ([]codec.Frame, []model.Loss, error) {
	encoder.failure = failure
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) begin() []codec.Frame {
	if encoder.started {
		return nil
	}
	encoder.started = true
	frames := encoder.emit("response.created", map[string]any{"response": encoder.snapshot("in_progress")})
	return append(frames, encoder.emit("response.in_progress", map[string]any{"response": encoder.snapshot("in_progress")})...)
}

func (encoder *streamEncoder) emit(kind string, payload map[string]any) []codec.Frame {
	payload["type"] = kind
	payload["sequence_number"] = encoder.sequence
	encoder.sequence++
	return []codec.Frame{{Event: kind, Data: model.Marshal(payload)}}
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
		snapshot["usage"] = codec.EncodeResponsesUsage(encoder.usage)
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

func (encoder *streamEncoder) startItem(event model.Event) ([]codec.Frame, []model.Loss, error) {
	frames := encoder.begin()
	if event.Item == nil {
		return frames, nil, nil
	}
	item := event.Item.Clone()
	itemKind := item.Kind
	if itemKind == "" {
		itemKind = model.ItemMessage
	}
	var losses []model.Loss
	switch itemKind {
	case model.ItemServerToolCall, model.ItemServerToolResult, model.ItemNative:
		if codec.RawObject(item.Raw, model.Responses) == nil {
			return frames, []model.Loss{codec.Degraded("item", "the provider item cannot be represented by Responses", model.Responses)}, nil
		}
	case model.ItemToolCall:
		if item.ToolCall == nil || item.ToolCall.Kind == model.ToolServer {
			return frames, []model.Loss{codec.Degraded("item", "provider-executed tool calls cannot be represented by this gateway", model.Responses)}, nil
		}
	}
	outputIndex := encoder.outputs
	encoder.outputs++
	id, synthesized := model.ItemID(model.Responses, itemKind, item.ID, toolCallID(&item), outputIndex)
	if synthesized {
		losses = append(losses, model.Loss{Path: "id", Code: model.LossItemIDMapped, Class: model.LossDegraded, Detail: "the target response id was synthesized", To: model.Responses})
	}
	state := &outputItemState{index: outputIndex, item: item, itemID: id, peer: encoder.peer, parts: map[int]*outputPart{}, toolPart: -1}
	encoder.items[event.ItemIndex] = state
	encoder.order = append(encoder.order, state)
	announced := map[string]any{"id": id}
	switch itemKind {
	case model.ItemMessage:
		role := string(item.Role)
		if role == "" {
			role = "assistant"
		}
		announced["type"], announced["role"], announced["status"], announced["content"] = "message", role, "in_progress", []any{}
	case model.ItemReasoning:
		announced["type"], announced["summary"] = "reasoning", []any{}
		if itemHasFullReasoning(&item) {
			announced["content"] = []any{}
		}
	case model.ItemServerToolCall, model.ItemServerToolResult:
		native := codec.RawObject(item.Raw, model.Responses)
		native["id"] = id
		announced = native
	case model.ItemToolCall:
		if item.ToolCall.Kind == model.ToolCustom {
			announced["type"], announced["call_id"], announced["name"], announced["input"], announced["status"] = "custom_tool_call", item.ToolCall.ID, item.ToolCall.Name, "", "in_progress"
		} else {
			announced["type"], announced["call_id"], announced["name"], announced["arguments"], announced["status"] = "function_call", item.ToolCall.ID, item.ToolCall.Name, "", "in_progress"
		}
	case model.ItemNative:
		announced = codec.RawObject(item.Raw, model.Responses)
		announced["id"] = id
	}
	if itemKind == model.ItemToolCall && item.ToolCall != nil && item.ToolCall.Signature != nil {
		losses = append(losses, codec.Degraded("signature", "Responses has no tool call signature", model.Responses))
	}
	frames = append(frames, encoder.emit("response.output_item.added", map[string]any{"output_index": outputIndex, "item": announced})...)
	return frames, losses, nil
}

func (encoder *streamEncoder) itemOf(irIndex int) *outputItemState { return encoder.items[irIndex] }

func toolCallID(item *model.Item) string {
	if item != nil && item.ToolCall != nil {
		return item.ToolCall.ID
	}
	return ""
}

func itemHasFullReasoning(item *model.Item) bool {
	for _, part := range item.Parts {
		if part.Kind == model.PartReasoningText {
			return true
		}
	}
	return false
}

func (encoder *streamEncoder) startPart(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.itemOf(event.ItemIndex)
	if state == nil || event.Part == nil {
		return nil, nil, nil
	}
	part := &outputPart{kind: event.Part.Kind, citations: event.Part.Citations}
	state.parts[event.PartIndex] = part
	state.order = append(state.order, event.PartIndex)
	switch part.kind {
	case model.PartText, model.PartRefusal, model.PartReasoningText:
		part.contentIndex = state.contentParts
		state.contentParts++
		empty := map[string]any{"type": "output_text", "text": "", "annotations": []any{}}
		if part.kind == model.PartRefusal {
			empty = map[string]any{"type": "refusal", "refusal": ""}
		}
		if part.kind == model.PartReasoningText {
			empty = map[string]any{"type": "reasoning_text", "text": ""}
		}
		return encoder.emit("response.content_part.added", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": part.contentIndex, "part": empty}), nil, nil
	case model.PartReasoningSummary:
		part.summaryIndex = state.summaryParts
		state.summaryParts++
		return encoder.emit("response.reasoning_summary_part.added", map[string]any{"item_id": state.itemID, "output_index": state.index, "summary_index": part.summaryIndex, "part": map[string]any{"type": "summary_text", "text": ""}}), nil, nil
	case model.PartToolArguments:
		state.toolPart = event.PartIndex
	case model.PartMedia:
		return nil, []model.Loss{codec.Degraded("media", "Responses cannot stream generated media", model.Responses)}, nil
	case model.PartNative, model.PartRedactedReasoning:
		return nil, []model.Loss{codec.Degraded("part", "the provider part cannot be represented by Responses", model.Responses)}, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) partDelta(event model.Event) ([]codec.Frame, []model.Loss, error) {
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

func (encoder *streamEncoder) deltaFrames(state *outputItemState, part *outputPart, text string) []codec.Frame {
	body := map[string]any{"item_id": state.itemID, "output_index": state.index, "delta": text}
	switch part.kind {
	case model.PartText:
		body["content_index"] = part.contentIndex
		return encoder.emit("response.output_text.delta", body)
	case model.PartRefusal:
		body["content_index"] = part.contentIndex
		return encoder.emit("response.refusal.delta", body)
	case model.PartReasoningSummary:
		body["summary_index"] = part.summaryIndex
		return encoder.emit("response.reasoning_summary_text.delta", body)
	case model.PartReasoningText:
		body["content_index"] = part.contentIndex
		return encoder.emit("response.reasoning_text.delta", body)
	case model.PartToolArguments:
		if state.item.ToolCall != nil && state.item.ToolCall.Kind == model.ToolCustom {
			return encoder.emit("response.custom_tool_call_input.delta", body)
		}
		return encoder.emit("response.function_call_arguments.delta", body)
	}
	return nil
}

func (encoder *streamEncoder) stopPart(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.itemOf(event.ItemIndex)
	if state == nil {
		return nil, nil, nil
	}
	part := state.parts[event.PartIndex]
	if part == nil {
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
			frames = append(frames, encoder.deltaFrames(state, part, content)...)
			part.streamed = true
		}
	}
	if part.kind == model.PartToolArguments && !part.streamed {
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
	if len(part.citations) > 0 && part.kind == model.PartText {
		for index, citation := range part.citations {
			annotation, nested := codec.EncodeCitation(citation, model.Responses)
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
	frames = append(frames, encoder.doneFrames(state, part)...)
	return frames, losses, nil
}

// doneFrames closes one content part with the done event family the Responses SDKs expect before
// the enclosing item closes.
func (encoder *streamEncoder) doneFrames(state *outputItemState, part *outputPart) []codec.Frame {
	text := part.text.String()
	var frames []codec.Frame
	switch part.kind {
	case model.PartText:
		frames = append(frames, encoder.emit("response.output_text.done", map[string]any{
			"item_id": state.itemID, "output_index": state.index, "content_index": part.contentIndex, "text": text,
		})...)
		content := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
		if annotations, _ := encodeAnnotations(part.citations); len(annotations) > 0 {
			content["annotations"] = annotations
		}
		frames = append(frames, encoder.emit("response.content_part.done", map[string]any{
			"item_id": state.itemID, "output_index": state.index, "content_index": part.contentIndex, "part": content,
		})...)
	case model.PartRefusal:
		frames = append(frames, encoder.emit("response.refusal.done", map[string]any{
			"item_id": state.itemID, "output_index": state.index, "content_index": part.contentIndex, "refusal": text,
		})...)
		frames = append(frames, encoder.emit("response.content_part.done", map[string]any{
			"item_id": state.itemID, "output_index": state.index, "content_index": part.contentIndex,
			"part": map[string]any{"type": "refusal", "refusal": text},
		})...)
	case model.PartReasoningText:
		frames = append(frames, encoder.emit("response.reasoning_text.done", map[string]any{
			"item_id": state.itemID, "output_index": state.index, "content_index": part.contentIndex, "text": text,
		})...)
		frames = append(frames, encoder.emit("response.content_part.done", map[string]any{
			"item_id": state.itemID, "output_index": state.index, "content_index": part.contentIndex,
			"part": map[string]any{"type": "reasoning_text", "text": text},
		})...)
	case model.PartReasoningSummary:
		frames = append(frames, encoder.emit("response.reasoning_summary_text.done", map[string]any{
			"item_id": state.itemID, "output_index": state.index, "summary_index": part.summaryIndex, "text": text,
		})...)
		frames = append(frames, encoder.emit("response.reasoning_summary_part.done", map[string]any{
			"item_id": state.itemID, "output_index": state.index, "summary_index": part.summaryIndex,
			"part": map[string]any{"type": "summary_text", "text": text},
		})...)
	case model.PartToolArguments:
		if state.item.ToolCall != nil && state.item.ToolCall.Kind == model.ToolCustom {
			frames = append(frames, encoder.emit("response.custom_tool_call_input.done", map[string]any{
				"item_id": state.itemID, "output_index": state.index, "input": text,
			})...)
			break
		}
		frames = append(frames, encoder.emit("response.function_call_arguments.done", map[string]any{
			"item_id": state.itemID, "output_index": state.index, "arguments": text,
		})...)
	}
	return frames
}

func (encoder *streamEncoder) stopItem(event model.Event) ([]codec.Frame, []model.Loss, error) {
	state := encoder.itemOf(event.ItemIndex)
	if state == nil || state.closed {
		return nil, nil, nil
	}
	if event.Item != nil {
		model.MergeItem(&state.item, *event.Item)
	}
	for _, part := range state.parts {
		if part.kind == model.PartToolArguments && !part.streamed && state.item.ToolCall != nil {
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
	case model.ItemMessage, "":
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
			case model.PartText:
				entry := map[string]any{"type": "output_text", "text": part.text.String(), "annotations": []any{}}
				if annotations, _ := encodeAnnotations(part.citations); len(annotations) > 0 {
					entry["annotations"] = annotations
				}
				content = append(content, entry)
			case model.PartRefusal:
				content = append(content, map[string]any{"type": "refusal", "refusal": part.text.String()})
			}
		}
		item["content"] = content
	case model.ItemReasoning:
		item["type"] = "reasoning"
		summary, content := []any{}, []any{}
		for _, partIndex := range state.order {
			part := state.parts[partIndex]
			if part == nil {
				continue
			}
			switch part.kind {
			case model.PartReasoningSummary:
				summary = append(summary, map[string]any{"type": "summary_text", "text": part.text.String()})
			case model.PartReasoningText:
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
	case model.ItemToolCall:
		if state.item.ToolCall == nil {
			item["type"] = "function_call"
			break
		}
		if state.item.ToolCall.Kind == model.ToolCustom {
			item["type"], item["call_id"], item["name"], item["input"], item["status"] = "custom_tool_call", state.item.ToolCall.ID, state.item.ToolCall.Name, state.toolText(), "completed"
		} else {
			arguments := state.toolText()
			if arguments == "" {
				arguments = "{}"
			}
			item["type"], item["call_id"], item["name"], item["arguments"], item["status"] = "function_call", state.item.ToolCall.ID, state.item.ToolCall.Name, arguments, "completed"
		}
	case model.ItemNative, model.ItemServerToolCall, model.ItemServerToolResult:
		if native := codec.RawObject(state.item.Raw, model.Responses); native != nil {
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
	text := state.reasoningText()
	if !codec.SignatureCoversText(state.item.Reasoning.Signature, model.PartReasoningText, text) {
		return ""
	}
	return state.peer.WireSignature(state.item.Reasoning.Signature, text)
}

func (state *outputItemState) reasoningText() string {
	var text strings.Builder
	for _, partIndex := range state.order {
		part := state.parts[partIndex]
		if part != nil && (part.kind == model.PartReasoningText || part.kind == model.PartReasoningSummary) {
			text.WriteString(part.text.String())
		}
	}
	if text.Len() == 0 {
		return state.item.Text()
	}
	return text.String()
}

func encodeAnnotations(citations []model.Citation) ([]any, []model.Loss) {
	var losses []model.Loss
	annotations := make([]any, 0, len(citations))
	for _, citation := range citations {
		entry, nested := codec.EncodeCitation(citation, model.Responses)
		losses = append(losses, nested...)
		if entry != nil {
			annotations = append(annotations, entry)
		}
	}
	return annotations, losses
}

func (encoder *streamEncoder) terminal() []codec.Frame {
	if encoder.stopped {
		return nil
	}
	encoder.stopped = true
	for _, state := range encoder.order {
		state.closed = true
	}
	status := codec.ResponsesStatus(encoder.finish.Kind)
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
