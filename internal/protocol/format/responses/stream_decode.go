package responses

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type streamItemState struct {
	index    int
	key      string
	kind     protocol.ItemKind
	parts    map[string]int
	toolPart int
	open     bool
}

type streamDecoder struct {
	peer    format.Peer
	ids     *format.CallIDs
	stream  *format.ItemStream
	items   map[string]*streamItemState
	byIndex map[int]*streamItemState
	meta    protocol.ResponseMeta
	usage   *protocol.Usage
	finish  protocol.Finish
	failure *protocol.Error
	losses  []protocol.Loss
	started bool
	done    bool
}

func (Codec) NewStreamDecoder(peer format.Peer) format.StreamDecoder {
	return &streamDecoder{
		peer: peer, ids: format.NewCallIDs(peer), stream: format.NewItemStream(), items: map[string]*streamItemState{},
		byIndex: map[int]*streamItemState{}, finish: protocol.Finish{Kind: protocol.FinishStop},
	}
}

func (decoder *streamDecoder) Feed(frame format.Frame) ([]protocol.Event, []protocol.Loss, error) {
	if decoder.done {
		return nil, nil, nil
	}
	root, err := protocol.DecodeObject(frame.Data)
	if err != nil {
		return nil, nil, errors.New("stream event must be a JSON object")
	}
	kind := protocol.String(root["type"])
	if kind == "" {
		kind = frame.Event
	}
	switch kind {
	case "response.created":
		response := protocol.Object(root["response"])
		created, _ := protocol.Integer(response["created_at"])
		decoder.meta = protocol.ResponseMeta{
			ID: protocol.String(response["id"]), Model: protocol.String(response["model"]), Created: created,
			ServiceTier: protocol.String(response["service_tier"]),
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
		events, err := decoder.finishResponse(protocol.Object(root["response"]))
		return events, decoder.takeLosses(), err
	case "error":
		failure := protocol.Object(root["error"])
		if failure == nil {
			failure = root
		}
		return nil, nil, &format.StreamError{
			Code: protocol.String(failure["code"]), Type: protocol.String(failure["type"]),
			Message: protocol.String(failure["message"]), Status: int(protocol.IntegerOrZero(failure["status"])),
		}
	}
	return nil, nil, nil
}

func (decoder *streamDecoder) ensureStart() ([]protocol.Event, error) {
	if decoder.started {
		return nil, nil
	}
	decoder.started = true
	return []protocol.Event{{Kind: protocol.EventMessageStart, Meta: &decoder.meta}}, nil
}

func itemKey(root map[string]any, item map[string]any) string {
	if id := protocol.String(item["id"]); id != "" {
		return "id:" + id
	}
	if id := protocol.String(root["item_id"]); id != "" {
		return "id:" + id
	}
	return "output:" + strconv.FormatInt(protocol.IntegerOrZero(root["output_index"]), 10)
}

func (decoder *streamDecoder) itemAdded(root map[string]any) ([]protocol.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	entry := protocol.Object(root["item"])
	if entry == nil {
		return events, nil
	}
	key := itemKey(root, entry)
	if _, seen := decoder.items[key]; seen {
		return events, nil
	}
	outputIndex := len(decoder.byIndex)
	if value, ok := protocol.Integer(root["output_index"]); ok {
		outputIndex = int(value)
	}
	item, err := decoder.outputItem(entry, key, outputIndex)
	if err != nil {
		return events, err
	}
	streamIndex, started := decoder.stream.StartItem(item)
	state := &streamItemState{index: streamIndex, key: key, kind: item.Kind, parts: map[string]int{}, toolPart: -1, open: true}
	switch item.Kind {
	case protocol.ItemToolCall, protocol.ItemServerToolCall:
		if item.ToolCall != nil && item.ToolCall.Kind != protocol.ToolServer {
			partIndex, partEvents, err := decoder.stream.StartPart(streamIndex, protocol.Part{Kind: protocol.PartToolArguments})
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

func (decoder *streamDecoder) outputItem(entry map[string]any, key string, outputIndex int) (protocol.Item, error) {
	entryType := protocol.String(entry["type"])
	if entryType == "message" || entryType == "" {
		role := protocol.Role(protocol.String(entry["role"]))
		if role == "" {
			role = protocol.RoleAssistant
		}
		item := protocol.Item{
			Kind: protocol.ItemMessage, Role: role, ID: protocol.String(entry["id"]), Status: protocol.String(entry["status"]),
			Raw: format.CaptureRaw(protocol.Responses, entry, itemKeys...),
		}
		if entry["content"] != nil {
			nested, err := format.DecodeContentItems(protocol.Responses, role, entry["content"], decoder.peer)
			if err != nil {
				return protocol.Item{}, err
			}
			for _, part := range nested {
				item.Parts = append(item.Parts, part.Parts...)
			}
		}
		return item, nil
	}
	items, err := format.DecodeContentItems(protocol.Responses, protocol.RoleAssistant, []any{entry}, decoder.peer)
	if err != nil {
		return protocol.Item{}, err
	}
	if len(items) > 0 {
		item := &items[0]
		format.AssignCallIDs(items, decoder.ids)
		item.Ordinal = outputIndex
		if item.Kind == protocol.ItemMessage {
			item.Role = protocol.Role(protocol.String(entry["role"]))
			if item.Role == "" {
				item.Role = protocol.RoleAssistant
			}
		}
		return *item, nil
	}
	return protocol.Item{Kind: protocol.ItemNative, Raw: &protocol.Raw{Format: protocol.Responses, Fields: map[string]json.RawMessage{"native": protocol.Marshal(entry)}}}, nil
}

func (decoder *streamDecoder) partAdded(root map[string]any) ([]protocol.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	state := decoder.items[itemKey(root, nil)]
	if state == nil {
		return events, nil
	}
	index := int(protocol.IntegerOrZero(root["content_index"]))
	key := "c:" + strconv.Itoa(index)
	if _, seen := state.parts[key]; seen {
		return events, nil
	}
	kind := protocol.PartText
	part := protocol.Object(root["part"])
	switch protocol.String(part["type"]) {
	case "refusal":
		kind = protocol.PartRefusal
	case "reasoning_text":
		kind = protocol.PartReasoningText
	case "summary_text":
		kind = protocol.PartReasoningSummary
	}
	partIndex, started, err := decoder.stream.StartPart(state.index, protocol.Part{Kind: kind, Citations: format.DecodeCitations(part)})
	if err != nil {
		return events, err
	}
	state.parts[key] = partIndex
	return append(events, started...), nil
}

func (decoder *streamDecoder) delta(root map[string]any, eventType string) ([]protocol.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	text := protocol.String(root["delta"])
	if text == "" {
		text = protocol.String(root["text"])
	}
	if text == "" {
		text = protocol.String(root["arguments"])
	}
	if text == "" {
		text = protocol.String(root["input"])
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

func deltaPart(eventType string, root map[string]any) (string, protocol.PartKind) {
	switch eventType {
	case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		return "tool", protocol.PartToolArguments
	case "response.reasoning_summary_text.delta":
		return "s:" + strconv.FormatInt(protocol.IntegerOrZero(root["summary_index"]), 10), protocol.PartReasoningSummary
	case "response.reasoning_text.delta":
		return "c:" + strconv.FormatInt(protocol.IntegerOrZero(root["content_index"]), 10), protocol.PartReasoningText
	case "response.refusal.delta":
		return "c:" + strconv.FormatInt(protocol.IntegerOrZero(root["content_index"]), 10), protocol.PartRefusal
	}
	return "c:" + strconv.FormatInt(protocol.IntegerOrZero(root["content_index"]), 10), protocol.PartText
}

func (decoder *streamDecoder) ensurePart(state *streamItemState, key string, kind protocol.PartKind) (int, []protocol.Event, error) {
	if partIndex, known := state.parts[key]; known {
		return partIndex, nil, nil
	}
	if key == "tool" && state.toolPart >= 0 {
		return state.toolPart, nil, nil
	}
	partIndex, started, err := decoder.stream.StartPart(state.index, protocol.Part{Kind: kind})
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
	annotation := protocol.Object(root["annotation"])
	if state == nil || annotation == nil {
		return
	}
	partIndex, known := state.parts["c:"+strconv.FormatInt(protocol.IntegerOrZero(root["content_index"]), 10)]
	if !known {
		return
	}
	citation := format.DecodeCitation(protocol.Responses, annotation)
	decoder.stream.UpdatePart(state.index, partIndex, func(part *protocol.Part) {
		part.Citations = append(part.Citations, citation)
	})
}

func (decoder *streamDecoder) itemDone(root map[string]any) ([]protocol.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	entry := protocol.Object(root["item"])
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

func (decoder *streamDecoder) finishItem(state *streamItemState, final protocol.Item) ([]protocol.Event, error) {
	var events []protocol.Event
	if final.Kind == protocol.ItemMessage {
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
				if content := protocol.StreamedContent(part); content != "" {
					nested, err := decoder.stream.Delta(state.index, partIndex, content)
					if err != nil {
						return events, err
					}
					events = append(events, nested...)
				}
			} else {
				decoder.stream.UpdatePart(state.index, partIndex, func(live *protocol.Part) {
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
	if final.Kind == protocol.ItemReasoning {
		decoder.stream.UpdateItem(state.index, func(live *protocol.Item) {
			if final.Reasoning != nil {
				live.Reasoning = final.Reasoning
			}
			if final.Raw != nil {
				live.Raw = final.Raw
			}
		})
		for index := range final.Parts {
			part := final.Parts[index]
			key := "c:" + strconv.Itoa(index)
			if part.Kind == protocol.PartReasoningSummary {
				key = "s:" + strconv.Itoa(index)
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
			if content := protocol.StreamedContent(part); content != "" {
				nested, err := decoder.stream.Delta(state.index, partIndex, content)
				if err != nil {
					return events, err
				}
				events = append(events, nested...)
			}
		}
	}
	if final.Kind.IsTool() && final.ToolCall != nil {
		decoder.stream.UpdateItem(state.index, func(live *protocol.Item) {
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
	for _, partIndex := range state.parts {
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

func (decoder *streamDecoder) finishResponse(response map[string]any) ([]protocol.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	if response == nil {
		response = map[string]any{}
	}
	for index, value := range protocol.Array(response["output"]) {
		entry := protocol.Object(value)
		key := itemKey(map[string]any{"output_index": index}, entry)
		if _, seen := decoder.items[key]; seen || entry == nil {
			continue
		}
		state := decoder.byIndex[index]
		if state == nil {
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
	decoder.usage = format.ParseResponsesUsage(response)
	decoder.finish, decoder.failure = finishOf(response)
	return append(events, decoder.finishEvents()...), nil
}

func (decoder *streamDecoder) finishEvents() []protocol.Event {
	if decoder.done {
		return nil
	}
	decoder.done = true
	stopped, _ := decoder.stream.StopAll()
	finish := decoder.finish
	if finish.Kind == protocol.FinishStop && decoder.stream.HasToolCall() {
		finish.Kind = protocol.FinishToolCalls
	}
	return append(stopped,
		protocol.Event{Kind: protocol.EventMessageDelta, Usage: decoder.usage, Finish: &finish, Failure: decoder.failure},
		protocol.Event{Kind: protocol.EventMessageStop})
}

func (decoder *streamDecoder) End() ([]protocol.Event, []protocol.Loss, error) {
	if decoder.done {
		return nil, decoder.takeLosses(), nil
	}
	if !decoder.started {
		return nil, nil, format.ErrTruncated
	}
	if !format.CleanEOF(decoder.stream) {
		return nil, nil, format.ErrTruncated
	}
	decoder.losses = append(decoder.losses, protocol.Loss{
		Code: protocol.LossCleanEOF, Class: protocol.LossDegraded, Detail: "the provider closed the stream without a terminal response event", From: protocol.Responses,
	})
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, nil, err
	}
	return append(events, decoder.finishEvents()...), decoder.takeLosses(), nil
}

func (decoder *streamDecoder) takeLosses() []protocol.Loss {
	losses := decoder.losses
	decoder.losses = nil
	return losses
}
