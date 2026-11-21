package responses

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type streamItemState struct {
	index    int
	key      string
	kind     model.ItemKind
	parts    map[string]int
	toolPart int
	open     bool
}

type streamDecoder struct {
	peer    codec.Peer
	ids     *codec.CallIDs
	stream  *model.Assembler
	items   map[string]*streamItemState
	byIndex map[int]*streamItemState
	meta    model.ResponseMeta
	usage   *model.Usage
	finish  model.Finish
	failure *model.Error
	losses  []model.Loss
	started bool
	done    bool
}

func (Codec) NewStreamDecoder(peer codec.Peer) codec.StreamDecoder {
	return &streamDecoder{
		peer: peer, ids: codec.NewCallIDs(peer), stream: model.NewAssembler(), items: map[string]*streamItemState{},
		byIndex: map[int]*streamItemState{}, finish: model.Finish{Kind: model.FinishStop},
	}
}

func (decoder *streamDecoder) Feed(frame codec.Frame) ([]model.Event, []model.Loss, error) {
	if decoder.done {
		return nil, nil, nil
	}
	root, err := model.DecodeObject(frame.Data)
	if err != nil {
		return nil, nil, errors.New("stream event must be a JSON object")
	}
	kind := model.String(root["type"])
	if kind == "" {
		kind = frame.Event
	}
	switch kind {
	case "response.created":
		response := model.Object(root["response"])
		created, _ := model.Integer(response["created_at"])
		decoder.meta = model.ResponseMeta{
			ID: model.String(response["id"]), Model: model.String(response["model"]), Created: created,
			ServiceTier: model.String(response["service_tier"]),
		}
		events, err := decoder.ensureStart()
		return events, nil, err
	case "response.in_progress":
		events, err := decoder.ensureStart()
		return events, nil, err
	case "response.output_item.added":
		events, err := decoder.itemAdded(root)
		return events, nil, err
	case "response.content_part.added":
		events, err := decoder.partAdded(root)
		return events, nil, err
	case "response.output_text.delta", "response.refusal.delta", "response.function_call_arguments.delta",
		"response.custom_tool_call_input.delta", "response.reasoning_summary_text.delta", "response.reasoning_text.delta",
		"response.output_text.done", "response.refusal.done", "response.function_call_arguments.done",
		"response.custom_tool_call_input.done", "response.reasoning_summary_text.done", "response.reasoning_text.done":
		events, err := decoder.delta(root, kind)
		return events, nil, err
	case "response.output_text.annotation.added":
		decoder.annotate(root)
		return nil, nil, nil
	case "response.output_item.done":
		events, err := decoder.itemDone(root)
		return events, nil, err
	case "response.completed", "response.incomplete", "response.failed":
		events, err := decoder.finishResponse(model.Object(root["response"]))
		return events, decoder.takeLosses(), err
	case "error":
		failure := model.Object(root["error"])
		if failure == nil {
			failure = root
		}
		return nil, nil, &codec.StreamError{
			Code: model.String(failure["code"]), Type: model.String(failure["type"]),
			Message: model.String(failure["message"]), Status: int(model.IntegerOrZero(failure["status"])),
		}
	}
	return nil, nil, nil
}

func (decoder *streamDecoder) ensureStart() ([]model.Event, error) {
	if decoder.started {
		return nil, nil
	}
	decoder.started = true
	return []model.Event{{Kind: model.EventMessageStart, Meta: &decoder.meta}}, nil
}

func itemKey(root map[string]any, item map[string]any) string {
	if id := model.String(item["id"]); id != "" {
		return "id:" + id
	}
	if id := model.String(root["item_id"]); id != "" {
		return "id:" + id
	}
	return "output:" + strconv.FormatInt(model.IntegerOrZero(root["output_index"]), 10)
}

func (decoder *streamDecoder) itemAdded(root map[string]any) ([]model.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	entry := model.Object(root["item"])
	if entry == nil {
		return events, nil
	}
	key := itemKey(root, entry)
	if _, seen := decoder.items[key]; seen {
		return events, nil
	}
	outputIndex := len(decoder.byIndex)
	if value, ok := model.Integer(root["output_index"]); ok {
		outputIndex = int(value)
	}
	item, err := decoder.outputItem(entry, key, outputIndex)
	if err != nil {
		return events, err
	}
	streamIndex, started := decoder.stream.StartItem(item)
	state := &streamItemState{index: streamIndex, key: key, kind: item.Kind, parts: map[string]int{}, toolPart: -1, open: true}
	switch item.Kind {
	case model.ItemToolCall, model.ItemServerToolCall:
		if item.ToolCall != nil && item.ToolCall.Kind != model.ToolServer {
			partIndex, partEvents, err := decoder.stream.StartPart(streamIndex, model.Part{Kind: model.PartToolArguments})
			if err != nil {
				return events, err
			}
			state.toolPart = partIndex
			events = append(events, started...)
			events = append(events, partEvents...)
		} else {
			events = append(events, started...)
		}
	default:
		events = append(events, started...)
	}
	decoder.items[key] = state
	decoder.byIndex[outputIndex] = state
	return events, nil
}

func (decoder *streamDecoder) outputItem(entry map[string]any, key string, outputIndex int) (model.Item, error) {
	entryType := model.String(entry["type"])
	if entryType == "message" || entryType == "" {
		role := model.Role(model.String(entry["role"]))
		if role == "" {
			role = model.RoleAssistant
		}
		item := model.Item{
			Kind: model.ItemMessage, Role: role, ID: model.String(entry["id"]), Status: model.String(entry["status"]),
			Raw: codec.CaptureRaw(model.Responses, entry, itemKeys...),
		}
		if entry["content"] != nil {
			nested, err := codec.DecodeContentItems(model.Responses, role, entry["content"], decoder.peer)
			if err != nil {
				return model.Item{}, err
			}
			for _, part := range nested {
				item.Parts = append(item.Parts, part.Parts...)
			}
		}
		return item, nil
	}
	items, err := codec.DecodeContentItems(model.Responses, model.RoleAssistant, []any{entry}, decoder.peer)
	if err != nil {
		return model.Item{}, err
	}
	if len(items) > 0 {
		item := &items[0]
		codec.AssignCallIDs(items, decoder.ids)
		item.Ordinal = outputIndex
		if item.Kind == model.ItemMessage {
			item.Role = model.Role(model.String(entry["role"]))
			if item.Role == "" {
				item.Role = model.RoleAssistant
			}
		}
		return *item, nil
	}
	return model.Item{Kind: model.ItemNative, Raw: &model.Raw{Format: model.Responses, Fields: map[string]json.RawMessage{"native": model.Marshal(entry)}}}, nil
}

func (decoder *streamDecoder) partAdded(root map[string]any) ([]model.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	state := decoder.items[itemKey(root, nil)]
	if state == nil {
		return events, nil
	}
	index := int(model.IntegerOrZero(root["content_index"]))
	key := "c:" + strconv.Itoa(index)
	if _, seen := state.parts[key]; seen {
		return events, nil
	}
	kind := model.PartText
	part := model.Object(root["part"])
	switch model.String(part["type"]) {
	case "refusal":
		kind = model.PartRefusal
	case "reasoning_text":
		kind = model.PartReasoningText
	case "summary_text":
		kind = model.PartReasoningSummary
	}
	partIndex, started, err := decoder.stream.StartPart(state.index, model.Part{Kind: kind, Citations: codec.DecodeCitations(part)})
	if err != nil {
		return events, err
	}
	state.parts[key] = partIndex
	return append(events, started...), nil
}

func (decoder *streamDecoder) delta(root map[string]any, eventType string) ([]model.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	text := model.String(root["delta"])
	if text == "" {
		text = model.String(root["text"])
	}
	if text == "" {
		text = model.String(root["arguments"])
	}
	if text == "" {
		text = model.String(root["input"])
	}
	if text == "" {
		return events, nil
	}
	state := decoder.items[itemKey(root, nil)]
	if state == nil {
		return events, errors.New("delta for an item that was never added")
	}
	key, kind := deltaPart(eventType, root)
	partIndex, started, err := decoder.ensurePart(state, key, kind)
	if err != nil {
		return events, err
	}
	events = append(events, started...)
	if strings.HasSuffix(eventType, ".done") && decoder.stream.Streamed(state.index, partIndex) != "" {
		return events, nil
	}
	nested, err := decoder.stream.Delta(state.index, partIndex, text)
	return append(events, nested...), err
}

func deltaPart(eventType string, root map[string]any) (string, model.PartKind) {
	switch eventType {
	case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		return "tool", model.PartToolArguments
	case "response.reasoning_summary_text.delta":
		return "s:" + strconv.FormatInt(model.IntegerOrZero(root["summary_index"]), 10), model.PartReasoningSummary
	case "response.reasoning_text.delta":
		return "c:" + strconv.FormatInt(model.IntegerOrZero(root["content_index"]), 10), model.PartReasoningText
	case "response.refusal.delta":
		return "c:" + strconv.FormatInt(model.IntegerOrZero(root["content_index"]), 10), model.PartRefusal
	}
	return "c:" + strconv.FormatInt(model.IntegerOrZero(root["content_index"]), 10), model.PartText
}

func (decoder *streamDecoder) ensurePart(state *streamItemState, key string, kind model.PartKind) (int, []model.Event, error) {
	if partIndex, known := state.parts[key]; known {
		return partIndex, nil, nil
	}
	if key == "tool" && state.toolPart >= 0 {
		return state.toolPart, nil, nil
	}
	partIndex, started, err := decoder.stream.StartPart(state.index, model.Part{Kind: kind})
	if err != nil {
		return 0, nil, err
	}
	state.parts[key] = partIndex
	if key == "tool" {
		state.toolPart = partIndex
	}
	return partIndex, started, nil
}

func (decoder *streamDecoder) annotate(root map[string]any) {
	state := decoder.items[itemKey(root, nil)]
	annotation := model.Object(root["annotation"])
	if state == nil || annotation == nil {
		return
	}
	partIndex, known := state.parts["c:"+strconv.FormatInt(model.IntegerOrZero(root["content_index"]), 10)]
	if !known {
		return
	}
	citation := codec.DecodeCitation(model.Responses, annotation)
	decoder.stream.UpdatePart(state.index, partIndex, func(part *model.Part) {
		part.Citations = append(part.Citations, citation)
	})
}

func (decoder *streamDecoder) itemDone(root map[string]any) ([]model.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	entry := model.Object(root["item"])
	if entry == nil {
		return events, nil
	}
	key := itemKey(root, entry)
	state := decoder.items[key]
	if state == nil {
		nested, err := decoder.itemAdded(root)
		if err != nil {
			return events, err
		}
		events = append(events, nested...)
		state = decoder.items[key]
		if state == nil {
			return events, nil
		}
	}
	final, err := decoder.outputItem(entry, key, state.index)
	if err != nil {
		return events, err
	}
	nested, err := decoder.finishItem(state, final)
	return append(events, nested...), err
}

func (decoder *streamDecoder) finishItem(state *streamItemState, final model.Item) ([]model.Event, error) {
	var events []model.Event
	if final.Kind == model.ItemMessage {
		for index := range final.Parts {
			part := final.Parts[index]
			key := "c:" + strconv.Itoa(index)
			partIndex, known := state.parts[key]
			if !known {
				partIndex, started, err := decoder.stream.StartPart(state.index, part)
				if err != nil {
					return events, err
				}
				events = append(events, started...)
				state.parts[key] = partIndex
				if content := model.StreamedContent(part); content != "" {
					nested, err := decoder.stream.Delta(state.index, partIndex, content)
					if err != nil {
						return events, err
					}
					events = append(events, nested...)
				}
			} else {
				decoder.stream.UpdatePart(state.index, partIndex, func(live *model.Part) {
					if len(part.Citations) > 0 {
						live.Citations = part.Citations
					}
					if part.CacheControl != nil {
						live.CacheControl = part.CacheControl
					}
				})
			}
		}
	}
	if final.Kind == model.ItemReasoning {
		decoder.stream.UpdateItem(state.index, func(live *model.Item) {
			if final.Reasoning != nil {
				live.Reasoning = final.Reasoning
			}
			if final.Raw != nil {
				live.Raw = final.Raw
			}
		})
		contentIndex, summaryIndex := 0, 0
		for index := range final.Parts {
			part := final.Parts[index]
			key := "c:" + strconv.Itoa(contentIndex)
			contentIndex++
			if part.Kind == model.PartReasoningSummary {
				key = "s:" + strconv.Itoa(summaryIndex)
				summaryIndex++
			}
			if _, known := state.parts[key]; known {
				continue
			}
			partIndex, started, err := decoder.stream.StartPart(state.index, part)
			if err != nil {
				return events, err
			}
			events = append(events, started...)
			state.parts[key] = partIndex
			if content := model.StreamedContent(part); content != "" {
				nested, err := decoder.stream.Delta(state.index, partIndex, content)
				if err != nil {
					return events, err
				}
				events = append(events, nested...)
			}
		}
	}
	if final.Kind.IsTool() && final.ToolCall != nil {
		decoder.stream.UpdateItem(state.index, func(live *model.Item) {
			if live.ToolCall == nil {
				live.ToolCall = final.ToolCall
				return
			}
			if final.ToolCall.ID != "" {
				live.ToolCall.ID = final.ToolCall.ID
			}
			if final.ToolCall.Name != "" {
				live.ToolCall.Name = final.ToolCall.Name
			}
			if final.ToolCall.Signature != nil {
				live.ToolCall.Signature = final.ToolCall.Signature
			}
			if live.ToolCall.Arguments == "" && final.ToolCall.Arguments != "" {
				live.ToolCall.Arguments = final.ToolCall.Arguments
			}
		})
		if final.ToolCall.Arguments != "" && state.toolPart >= 0 && decoder.stream.Streamed(state.index, state.toolPart) == "" {
			nested, err := decoder.stream.Delta(state.index, state.toolPart, final.ToolCall.Arguments)
			if err != nil {
				return events, err
			}
			events = append(events, nested...)
		}
	}
	order := make([]int, 0, len(state.parts))
	for _, partIndex := range state.parts {
		order = append(order, partIndex)
	}
	sort.Ints(order)
	for _, partIndex := range order {
		nested, err := decoder.stream.StopPart(state.index, partIndex)
		if err != nil {
			return events, err
		}
		events = append(events, nested...)
	}
	metadata := final.Clone()
	metadata.Parts = nil
	nested, err := decoder.stream.StopItem(state.index, &metadata)
	if err != nil {
		return events, err
	}
	state.open = false
	return append(events, nested...), nil
}

func (decoder *streamDecoder) finishResponse(response map[string]any) ([]model.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	if response == nil {
		response = map[string]any{}
	}
	for index, value := range model.Array(response["output"]) {
		entry := model.Object(value)
		if entry == nil {
			continue
		}
		key := itemKey(map[string]any{"output_index": index}, entry)
		state := decoder.byIndex[index]
		if started := decoder.items[key]; started != nil {
			if !started.open {
				continue
			}
			state = started
		} else if state == nil {
			nested, err := decoder.itemAdded(map[string]any{"item": entry, "output_index": index})
			if err != nil {
				return events, err
			}
			events = append(events, nested...)
			state = decoder.byIndex[index]
		}
		if state == nil {
			continue
		}
		final, err := decoder.outputItem(entry, key, index)
		if err != nil {
			return events, err
		}
		nested, err := decoder.finishItem(state, final)
		if err != nil {
			return events, err
		}
		events = append(events, nested...)
	}
	decoder.usage = codec.ParseResponsesUsage(response)
	decoder.finish, decoder.failure = finishOf(response)
	return append(events, decoder.finishEvents()...), nil
}

func (decoder *streamDecoder) finishEvents() []model.Event {
	if decoder.done {
		return nil
	}
	decoder.done = true
	stopped, _ := decoder.stream.StopAll()
	finish := decoder.finish
	if finish.Kind == model.FinishStop && decoder.stream.HasToolCall() {
		finish.Kind = model.FinishToolCalls
	}
	return append(stopped,
		model.Event{Kind: model.EventMessageDelta, Usage: decoder.usage, Finish: &finish, Failure: decoder.failure},
		model.Event{Kind: model.EventMessageStop})
}

func (decoder *streamDecoder) End() ([]model.Event, []model.Loss, error) {
	if decoder.done {
		return nil, decoder.takeLosses(), nil
	}
	if !decoder.started {
		return nil, nil, codec.ErrTruncated
	}
	if !model.CleanEOF(decoder.stream) {
		return nil, nil, codec.ErrTruncated
	}
	decoder.losses = append(decoder.losses, model.Loss{
		Code: model.LossCleanEOF, Class: model.LossDegraded, Detail: "the provider closed the stream without a terminal response event", From: model.Responses,
	})
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, nil, err
	}
	return append(events, decoder.finishEvents()...), decoder.takeLosses(), nil
}

func (decoder *streamDecoder) takeLosses() []model.Loss {
	losses := decoder.losses
	decoder.losses = nil
	return losses
}
