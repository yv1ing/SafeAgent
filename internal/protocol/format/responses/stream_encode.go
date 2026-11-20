package responses

import (
	"time"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

// outputItem is one output item. Output indexes are assigned in the order items start, and sequence
// numbers in the order frames are produced, so the frames of a block form a contiguous run that a
// caller may hold back and release as a whole.
type outputItem struct {
	block  *protocol.Block
	index  int
	itemID string
	open   bool
}

type streamEncoder struct {
	meta     protocol.ResponseMeta
	created  int64
	peer     format.Peer
	items    map[int]*outputItem
	order    []*outputItem
	outputs  int
	sequence int
	started  bool
	stopped  bool
	usage    *protocol.Usage
	finish   protocol.FinishReason
	failure  *protocol.Error
}

func (Codec) NewStreamEncoder(meta protocol.ResponseMeta, peer format.Peer) format.StreamEncoder {
	return &streamEncoder{meta: meta, peer: peer, items: map[int]*outputItem{}, created: time.Now().Unix()}
}

func (encoder *streamEncoder) Encode(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	switch event.Kind {
	case protocol.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
		}
		return encoder.begin(), nil, nil
	case protocol.EventBlockStart:
		if event.Block == nil {
			return nil, nil, nil
		}
		return encoder.startItem(event.Index, *event.Block)
	case protocol.EventBlockDelta:
		item := encoder.items[event.Index]
		if item == nil || !item.open || event.Text == "" {
			return nil, nil, nil
		}
		return encoder.delta(item, event.Text), nil, nil
	case protocol.EventBlockStop:
		return encoder.stopItem(event)
	case protocol.EventMessageDelta:
		encoder.usage, encoder.finish, encoder.failure = event.Usage, event.Finish, event.Failure
		var losses []protocol.Loss
		if format.FinishLoss(event.Finish, protocol.Responses) {
			losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Detail: string(event.Finish), To: protocol.Responses})
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

func (encoder *streamEncoder) begin() []format.Frame {
	if encoder.started {
		return nil
	}
	encoder.started = true
	frames := encoder.emit("response.created", map[string]any{"response": encoder.snapshot("in_progress")})
	return append(frames, encoder.emit("response.in_progress", map[string]any{"response": encoder.snapshot("in_progress")})...)
}

// itemOf encodes a block as the output item it becomes; nil means the block has no Responses form.
func (encoder *streamEncoder) itemOf(block protocol.Block, outputIndex int) (map[string]any, []protocol.Loss, error) {
	context := format.NewPartContext(protocol.Responses, encoder.peer)
	context.Output = true
	items, losses, err := encodeItems(protocol.Message{Role: protocol.RoleAssistant, Blocks: []protocol.Block{block}}, context, "block")
	if err != nil || len(items) == 0 {
		return nil, losses, err
	}
	item := items[0].(map[string]any)
	assignItemID(item, outputIndex)
	if item["type"] == "message" {
		item["status"] = "completed"
	}
	return item, losses, nil
}

func (encoder *streamEncoder) startItem(index int, block protocol.Block) ([]format.Frame, []protocol.Loss, error) {
	frames := encoder.begin()
	output := encoder.outputs
	item, losses, err := encoder.itemOf(block, output)
	if err != nil {
		return nil, losses, err
	}
	if item == nil {
		return frames, losses, nil
	}
	encoder.outputs++
	cloned := protocol.CloneBlock(block)
	state := &outputItem{block: &cloned, index: output, itemID: protocol.String(item["id"]), open: true}
	encoder.items[index] = state
	encoder.order = append(encoder.order, state)
	frames = append(frames, encoder.emit("response.output_item.added", map[string]any{"output_index": output, "item": announced(item)})...)
	return append(frames, encoder.openContent(state)...), losses, nil
}

// announced strips what the deltas deliver, as output_item.added describes an item before its
// content exists.
func announced(item map[string]any) map[string]any {
	shallow := make(map[string]any, len(item))
	for key, value := range item {
		shallow[key] = value
	}
	switch shallow["type"] {
	case "message":
		shallow["status"], shallow["content"] = "in_progress", []any{}
	case "function_call":
		shallow["status"], shallow["arguments"] = "in_progress", ""
	case "custom_tool_call":
		shallow["status"], shallow["input"] = "in_progress", ""
	case "reasoning":
		shallow["summary"] = []any{}
		if _, present := shallow["content"]; present {
			shallow["content"] = []any{}
		}
	}
	return shallow
}

func (encoder *streamEncoder) openContent(state *outputItem) []format.Frame {
	switch state.block.Kind {
	case protocol.KindText, protocol.KindRefusal:
		return encoder.emit("response.content_part.added", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": 0, "part": emptyPart(state.block)})
	case protocol.KindReasoning:
		if fullReasoning(state.block) {
			return encoder.emit("response.content_part.added", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": 0, "part": map[string]any{"type": "reasoning_text", "text": ""}})
		}
		return encoder.emit("response.reasoning_summary_part.added", map[string]any{"item_id": state.itemID, "output_index": state.index, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": ""}})
	}
	return nil
}

// fullReasoning reports a block that carries the model's raw reasoning, which Responses streams
// as reasoning_text content rather than as a summary.
func fullReasoning(block *protocol.Block) bool {
	return block.Reasoning != nil && block.Reasoning.Visibility == "full"
}

func emptyPart(block *protocol.Block) map[string]any {
	if block.Kind == protocol.KindRefusal {
		return map[string]any{"type": "refusal", "refusal": ""}
	}
	return map[string]any{"type": "output_text", "text": "", "annotations": []any{}}
}

func (encoder *streamEncoder) delta(state *outputItem, text string) []format.Frame {
	block := state.block
	body := map[string]any{"item_id": state.itemID, "output_index": state.index, "delta": text}
	switch block.Kind {
	case protocol.KindToolCall:
		if block.ToolCall.Kind == protocol.ToolServer {
			return nil
		}
		block.ToolCall.Arguments += text
		if block.ToolCall.Kind == protocol.ToolCustom {
			return encoder.emit("response.custom_tool_call_input.delta", body)
		}
		return encoder.emit("response.function_call_arguments.delta", body)
	case protocol.KindReasoning:
		block.Text += text
		if fullReasoning(block) {
			body["content_index"] = 0
			return encoder.emit("response.reasoning_text.delta", body)
		}
		body["summary_index"] = 0
		return encoder.emit("response.reasoning_summary_text.delta", body)
	case protocol.KindRefusal:
		block.Text += text
		body["content_index"] = 0
		return encoder.emit("response.refusal.delta", body)
	case protocol.KindText:
		block.Text += text
		body["content_index"] = 0
		return encoder.emit("response.output_text.delta", body)
	}
	return nil
}

func (encoder *streamEncoder) stopItem(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	state := encoder.items[event.Index]
	if state == nil || !state.open {
		return nil, nil, nil
	}
	state.open = false
	var frames []format.Frame
	if event.Block != nil {
		final := protocol.CloneBlock(*event.Block)
		state.block = &final
	}
	block := state.block
	annotations := 0
	for _, annotation := range block.Citations {
		if entry, _ := format.EncodeCitation(annotation, protocol.Responses); entry != nil && block.Kind == protocol.KindText {
			frames = append(frames, encoder.emit("response.output_text.annotation.added", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": 0, "annotation_index": annotations, "annotation": entry})...)
			annotations++
		}
	}
	frames = append(frames, encoder.closeContent(state)...)
	item, losses, err := encoder.itemOf(*block, state.index)
	if err != nil {
		return nil, losses, err
	}
	item["id"] = state.itemID
	return append(frames, encoder.emit("response.output_item.done", map[string]any{"output_index": state.index, "item": item})...), losses, nil
}

func (encoder *streamEncoder) closeContent(state *outputItem) []format.Frame {
	block := state.block
	switch block.Kind {
	case protocol.KindText:
		frames := encoder.emit("response.output_text.done", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": 0, "text": block.Text})
		part := emptyPart(block)
		part["text"] = block.Text
		return append(frames, encoder.emit("response.content_part.done", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": 0, "part": part})...)
	case protocol.KindRefusal:
		frames := encoder.emit("response.refusal.done", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": 0, "refusal": block.Text})
		return append(frames, encoder.emit("response.content_part.done", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": 0, "part": map[string]any{"type": "refusal", "refusal": block.Text}})...)
	case protocol.KindReasoning:
		if fullReasoning(block) {
			frames := encoder.emit("response.reasoning_text.done", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": 0, "text": block.Text})
			return append(frames, encoder.emit("response.content_part.done", map[string]any{"item_id": state.itemID, "output_index": state.index, "content_index": 0, "part": map[string]any{"type": "reasoning_text", "text": block.Text}})...)
		}
		frames := encoder.emit("response.reasoning_summary_text.done", map[string]any{"item_id": state.itemID, "output_index": state.index, "summary_index": 0, "text": block.Text})
		return append(frames, encoder.emit("response.reasoning_summary_part.done", map[string]any{"item_id": state.itemID, "output_index": state.index, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": block.Text}})...)
	case protocol.KindToolCall:
		if block.ToolCall.Kind == protocol.ToolCustom {
			return encoder.emit("response.custom_tool_call_input.done", map[string]any{"item_id": state.itemID, "output_index": state.index, "input": block.ToolCall.Arguments})
		}
		if block.ToolCall.Kind == protocol.ToolFunction {
			return encoder.emit("response.function_call_arguments.done", map[string]any{"item_id": state.itemID, "output_index": state.index, "arguments": format.EmptyObjectIfBlank(block.ToolCall.Arguments)})
		}
	}
	return nil
}

func (encoder *streamEncoder) terminal() []format.Frame {
	if encoder.stopped {
		return nil
	}
	encoder.stopped = true
	frames := encoder.begin()
	for _, state := range encoder.order {
		if state.open {
			state.open = false
			frames = append(frames, encoder.closeContent(state)...)
			if item, _, err := encoder.itemOf(*state.block, state.index); err == nil {
				item["id"] = state.itemID
				frames = append(frames, encoder.emit("response.output_item.done", map[string]any{"output_index": state.index, "item": item})...)
			}
		}
	}
	status := format.ResponsesStatus(encoder.finish)
	name := "response.completed"
	snapshot := encoder.snapshot(status)
	switch {
	case encoder.failure != nil || status == "failed":
		name, snapshot["status"] = "response.failed", "failed"
		failure := encoder.failure
		if failure == nil {
			failure = &protocol.Error{Code: "upstream_failure", Message: "The upstream reported a failure."}
		}
		snapshot["error"] = map[string]any{"code": failure.Code, "message": failure.Message}
	case status == "incomplete":
		name = "response.incomplete"
		snapshot["incomplete_details"] = map[string]any{"reason": format.ResponsesIncompleteReason(encoder.finish)}
	}
	return append(frames, encoder.emit(name, map[string]any{"response": snapshot})...)
}

func (encoder *streamEncoder) snapshot(status string) map[string]any {
	output := []any{}
	if status != "in_progress" {
		for _, state := range encoder.order {
			if item, _, err := encoder.itemOf(*state.block, state.index); err == nil && item != nil {
				item["id"] = state.itemID
				output = append(output, item)
			}
		}
	}
	snapshot := map[string]any{"id": encoder.meta.ID, "object": "response", "created_at": encoder.created, "model": encoder.meta.Model, "status": status, "output": output}
	if usage := format.EncodeResponsesUsage(encoder.usage); usage != nil {
		snapshot["usage"] = usage
	}
	return snapshot
}

func (encoder *streamEncoder) emit(name string, payload map[string]any) []format.Frame {
	payload["type"] = name
	payload["sequence_number"] = encoder.sequence
	encoder.sequence++
	return []format.Frame{{Event: name, Data: protocol.Marshal(payload)}}
}
