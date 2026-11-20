package chat

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type pendingCall struct {
	call protocol.ToolCall
	args strings.Builder
}

type openText struct {
	itemIndex int
	partIndex int
	kind      protocol.PartKind
}

type streamDecoder struct {
	ids       *format.CallIDs
	stream    *format.ItemStream
	started   bool
	done      bool
	finished  bool
	pending   map[int]*pendingCall
	emitted   map[int]bool
	positions map[string]int
	last      int
	open      *openText
	finish    protocol.Finish
	usage     *protocol.Usage
	losses    []protocol.Loss
}

func (Codec) NewStreamDecoder(peer format.Peer) format.StreamDecoder {
	return &streamDecoder{
		ids:    format.NewCallIDs(peer),
		stream: format.NewItemStream(), pending: map[int]*pendingCall{}, emitted: map[int]bool{},
		positions: map[string]int{}, finish: protocol.Finish{Kind: protocol.FinishStop},
	}
}

func (decoder *streamDecoder) Feed(frame format.Frame) ([]protocol.Event, []protocol.Loss, error) {
	if decoder.done {
		return nil, nil, nil
	}
	payload := strings.TrimSpace(string(frame.Data))
	if payload == "" {
		return nil, nil, nil
	}
	if payload == "[DONE]" {
		if !decoder.pendingClean() {
			return nil, nil, format.ErrTruncated
		}
		events, err := decoder.finishEvents()
		return events, decoder.takeLosses(), err
	}
	root, err := protocol.DecodeObject(frame.Data)
	if err != nil {
		return nil, nil, errors.New("stream chunk must be a JSON object")
	}
	if failure := protocol.Object(root["error"]); failure != nil {
		return nil, nil, &format.StreamError{
			Code: protocol.String(failure["code"]), Type: protocol.String(failure["type"]),
			Message: protocol.String(failure["message"]), Status: int(protocol.IntegerOrZero(failure["status"])),
		}
	}
	var events []protocol.Event
	if !decoder.started {
		decoder.started = true
		created, _ := protocol.Integer(root["created"])
		events = append(events, protocol.Event{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{
			ID: protocol.String(root["id"]), Model: protocol.String(root["model"]), Created: created,
			SystemFingerprint: protocol.String(root["system_fingerprint"]), ServiceTier: protocol.String(root["service_tier"]),
		}})
	}
	if usage := format.ParseChatUsage(root); usage != nil {
		decoder.usage = usage
	}
	for _, value := range protocol.Array(root["choices"]) {
		choice := protocol.Object(value)
		if index, _ := protocol.Integer(choice["index"]); index != 0 {
			return nil, nil, protocol.UnsupportedFeature("unsupported_multi_choice", "Multiple choices are not supported.")
		}
		nested, err := decoder.deltaEvents(protocol.Object(choice["delta"]))
		if err != nil {
			return events, nil, err
		}
		events = append(events, nested...)
		if finish := protocol.String(choice["finish_reason"]); finish != "" {
			reason, known := format.ParseChatFinish(finish)
			if !known {
				decoder.losses = append(decoder.losses, format.FinishMappedLoss(finish, protocol.Chat))
			}
			decoder.finish = protocol.Finish{Kind: reason, Raw: finish}
			decoder.finished = true
			flushed, err := decoder.flushTools()
			if err != nil {
				return events, nil, err
			}
			events = append(events, flushed...)
		}
	}
	return events, nil, nil
}

func (decoder *streamDecoder) deltaEvents(delta map[string]any) ([]protocol.Event, error) {
	if delta == nil {
		return nil, nil
	}
	var events []protocol.Event
	if len(protocol.Array(delta["annotations"])) > 0 && decoder.open != nil {
		for _, value := range protocol.Array(delta["annotations"]) {
			if object := protocol.Object(value); object != nil {
				citation := format.DecodeCitation(protocol.Chat, object)
				decoder.stream.UpdatePart(decoder.open.itemIndex, decoder.open.partIndex, func(part *protocol.Part) {
					part.Citations = append(part.Citations, citation)
				})
			}
		}
	}
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if text := protocol.String(delta[key]); text != "" {
			nested, err := decoder.textDelta(protocol.PartReasoningText, text, protocol.ItemReasoning)
			if err != nil {
				return nil, err
			}
			events = append(events, nested...)
			break
		}
	}
	if text := protocol.String(delta["refusal"]); text != "" {
		nested, err := decoder.textDelta(protocol.PartRefusal, text, protocol.ItemMessage)
		if err != nil {
			return nil, err
		}
		events = append(events, nested...)
	}
	switch content := delta["content"].(type) {
	case string:
		if content != "" {
			nested, err := decoder.textDelta(protocol.PartText, content, protocol.ItemMessage)
			if err != nil {
				return nil, err
			}
			events = append(events, nested...)
		}
	case []any:
		items, err := format.DecodeContentItems(protocol.Chat, protocol.RoleAssistant, content, format.Peer{})
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			nested, err := decoder.emitWhole(item)
			if err != nil {
				return nil, err
			}
			events = append(events, nested...)
		}
	}
	for _, value := range protocol.Array(delta["tool_calls"]) {
		if err := decoder.toolDelta(protocol.Object(value)); err != nil {
			return nil, err
		}
	}
	if legacy := protocol.Object(delta["function_call"]); legacy != nil {
		if err := decoder.toolDelta(map[string]any{"index": 0, "type": "function", "function": legacy}); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func (decoder *streamDecoder) textDelta(partKind protocol.PartKind, text string, itemKind protocol.ItemKind) ([]protocol.Event, error) {
	var events []protocol.Event
	if decoder.open != nil && (decoder.open.kind != partKind || decoder.open.itemIndex < 0) {
		stopped, err := decoder.stopOpenText()
		if err != nil {
			return nil, err
		}
		events = append(events, stopped...)
	}
	if decoder.open == nil {
		flushed, err := decoder.flushTools()
		if err != nil {
			return nil, err
		}
		events = append(events, flushed...)
		item, stopped, err := decoder.startTextItem(itemKind, partKind)
		if err != nil {
			return nil, err
		}
		events = append(events, stopped...)
		_ = item
	}
	nested, err := decoder.stream.Delta(decoder.open.itemIndex, decoder.open.partIndex, text)
	return append(events, nested...), err
}

func (decoder *streamDecoder) startTextItem(itemKind protocol.ItemKind, partKind protocol.PartKind) (protocol.Item, []protocol.Event, error) {
	item := protocol.Item{Kind: itemKind, Role: protocol.RoleAssistant}
	if itemKind == protocol.ItemReasoning {
		item.Reasoning = &protocol.Reasoning{Visibility: "full"}
	}
	itemIndex, events := decoder.stream.StartItem(item)
	part := protocol.Part{Kind: partKind}
	partIndex, started, err := decoder.stream.StartPart(itemIndex, part)
	if err != nil {
		return item, events, err
	}
	decoder.open = &openText{itemIndex: itemIndex, partIndex: partIndex, kind: partKind}
	return item, append(events, started...), nil
}

func (decoder *streamDecoder) stopOpenText() ([]protocol.Event, error) {
	if decoder.open == nil {
		return nil, nil
	}
	partEvents, err := decoder.stream.StopPart(decoder.open.itemIndex, decoder.open.partIndex)
	if err != nil {
		return nil, err
	}
	itemEvents, err := decoder.stream.StopItem(decoder.open.itemIndex, nil)
	decoder.open = nil
	if err != nil {
		return partEvents, err
	}
	return append(partEvents, itemEvents...), nil
}

func (decoder *streamDecoder) position(call map[string]any) int {
	if index, ok := protocol.Integer(call["index"]); ok {
		decoder.last = int(index)
		return decoder.last
	}
	id := protocol.String(call["id"])
	if id == "" {
		return decoder.last
	}
	if position, seen := decoder.positions[id]; seen {
		decoder.last = position
		return position
	}
	position := len(decoder.positions)
	for decoder.pending[position] != nil || decoder.emitted[position] {
		position++
	}
	decoder.positions[id], decoder.last = position, position
	return position
}

func (decoder *streamDecoder) toolDelta(call map[string]any) error {
	position := decoder.position(call)
	if decoder.emitted[position] {
		return errors.New("tool call delta after the call was already emitted")
	}
	function, custom := protocol.Object(call["function"]), protocol.Object(call["custom"])
	pending := decoder.pending[position]
	if pending == nil {
		kind := protocol.ToolFunction
		if protocol.String(call["type"]) == "custom" || custom != nil {
			kind = protocol.ToolCustom
		}
		pending = &pendingCall{call: protocol.ToolCall{Kind: kind}}
		decoder.pending[position] = pending
	}
	stored := &pending.call
	named := function
	if custom != nil {
		named = custom
	}
	if id := protocol.String(call["id"]); id != "" {
		stored.ID = id
	}
	if name := protocol.String(named["name"]); name != "" {
		stored.Name = name
	}
	pending.args.WriteString(protocol.String(function["arguments"]))
	pending.args.WriteString(protocol.String(custom["input"]))
	return nil
}

func (decoder *streamDecoder) flushTools() ([]protocol.Event, error) {
	if len(decoder.pending) == 0 {
		return nil, nil
	}
	positions := make([]int, 0, len(decoder.pending))
	for position := range decoder.pending {
		positions = append(positions, position)
	}
	sort.Ints(positions)
	var events []protocol.Event
	for _, position := range positions {
		pending := decoder.pending[position]
		if decoder.open != nil {
			stopped, err := decoder.stopOpenText()
			if err != nil {
				return events, err
			}
			events = append(events, stopped...)
		}
		arguments := pending.args.String()
		if pending.call.Kind == protocol.ToolFunction {
			arguments = format.EmptyObjectIfBlank(arguments)
		}
		if pending.call.ID == "" {
			pending.call.ID = decoder.ids.Assign(pending.call.Name, arguments)
		}
		item := protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &pending.call}
		itemIndex, started := decoder.stream.StartItem(item)
		events = append(events, started...)
		partIndex, partStarted, err := decoder.stream.StartPart(itemIndex, protocol.Part{Kind: protocol.PartToolArguments})
		if err != nil {
			return events, err
		}
		events = append(events, partStarted...)
		if arguments != "" {
			nested, err := decoder.stream.Delta(itemIndex, partIndex, arguments)
			if err != nil {
				return events, err
			}
			events = append(events, nested...)
		}
		if nested, err := decoder.stream.StopPart(itemIndex, partIndex); err != nil {
			return events, err
		} else {
			events = append(events, nested...)
		}
		if nested, err := decoder.stream.StopItem(itemIndex, nil); err != nil {
			return events, err
		} else {
			events = append(events, nested...)
		}
		decoder.emitted[position] = true
	}
	decoder.pending = map[int]*pendingCall{}
	return events, nil
}

func (decoder *streamDecoder) emitWhole(item protocol.Item) ([]protocol.Event, error) {
	var events []protocol.Event
	if decoder.open != nil {
		stopped, err := decoder.stopOpenText()
		if err != nil {
			return nil, err
		}
		events = append(events, stopped...)
	}
	flushed, err := decoder.flushTools()
	if err != nil {
		return nil, err
	}
	events = append(events, flushed...)
	itemIndex, started := decoder.stream.StartItem(item)
	events = append(events, started...)
	for partIndex := range item.Parts {
		part := item.Parts[partIndex]
		index, started, err := decoder.stream.StartPart(itemIndex, part)
		if err != nil {
			return events, err
		}
		events = append(events, started...)
		content := protocol.StreamedContent(part)
		if part.Kind != protocol.PartToolArguments && content != "" {
			nested, err := decoder.stream.Delta(itemIndex, index, content)
			if err != nil {
				return events, err
			}
			events = append(events, nested...)
		}
		if nested, err := decoder.stream.StopPart(itemIndex, index); err != nil {
			return events, err
		} else {
			events = append(events, nested...)
		}
	}
	if nested, err := decoder.stream.StopItem(itemIndex, &item); err != nil {
		return events, err
	} else {
		events = append(events, nested...)
	}
	return events, nil
}

func (decoder *streamDecoder) finishEvents() ([]protocol.Event, error) {
	if decoder.done {
		return nil, nil
	}
	decoder.done = true
	flushed, err := decoder.flushTools()
	if err != nil {
		return nil, err
	}
	stopped, err := decoder.stream.StopAll()
	if err != nil {
		return nil, err
	}
	events := append(flushed, stopped...)
	finish := decoder.finish
	if finish.Kind == protocol.FinishStop && decoder.stream.HasToolCall() {
		finish.Kind = protocol.FinishToolCalls
	}
	events = append(events,
		protocol.Event{Kind: protocol.EventMessageDelta, Usage: decoder.usage, Finish: &finish},
		protocol.Event{Kind: protocol.EventMessageStop})
	return events, nil
}

func (decoder *streamDecoder) End() ([]protocol.Event, []protocol.Loss, error) {
	if !decoder.finished {
		if !decoder.pendingClean() || !format.CleanEOF(decoder.stream) {
			return nil, nil, format.ErrTruncated
		}
		decoder.finished = true
		decoder.losses = append(decoder.losses, protocol.Loss{Code: protocol.LossCleanEOF, Class: protocol.LossDegraded, Detail: "the provider closed the stream without a terminal frame", From: protocol.Chat})
	}
	events, err := decoder.finishEvents()
	return events, decoder.takeLosses(), err
}

// pendingClean reports whether every buffered tool call has a complete argument object.
// CleanEOF inspects ItemStream only, so pending calls must be checked before they are flushed.
func (decoder *streamDecoder) pendingClean() bool {
	for _, pending := range decoder.pending {
		if pending.call.Kind == protocol.ToolCustom {
			continue
		}
		arguments := pending.args.String()
		if arguments != "" && !json.Valid([]byte(arguments)) {
			return false
		}
	}
	return true
}

func (decoder *streamDecoder) takeLosses() []protocol.Loss {
	losses := decoder.losses
	decoder.losses = nil
	return losses
}
