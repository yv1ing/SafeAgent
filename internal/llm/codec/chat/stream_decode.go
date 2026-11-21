package chat

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type pendingCall struct {
	call model.ToolCall
	args strings.Builder
}

type openText struct {
	itemIndex int
	partIndex int
	kind      model.PartKind
}

type streamDecoder struct {
	ids         *codec.CallIDs
	stream      *model.Assembler
	started     bool
	done        bool
	finished    bool
	pending     map[int]*pendingCall
	emitted     map[int]bool
	positions   map[string]int
	last        int
	open        *openText
	finish      model.Finish
	usage       *model.Usage
	losses      []model.Loss
	multiChoice bool
}

func (Codec) NewStreamDecoder(peer codec.Peer) codec.StreamDecoder {
	return &streamDecoder{
		ids:    codec.NewCallIDs(peer),
		stream: model.NewAssembler(), pending: map[int]*pendingCall{}, emitted: map[int]bool{},
		positions: map[string]int{}, finish: model.Finish{Kind: model.FinishStop},
	}
}

func (decoder *streamDecoder) Feed(frame codec.Frame) ([]model.Event, []model.Loss, error) {
	if decoder.done {
		return nil, nil, nil
	}
	payload := strings.TrimSpace(string(frame.Data))
	if payload == "" {
		return nil, nil, nil
	}
	if payload == "[DONE]" {
		if !decoder.pendingClean() {
			return nil, nil, codec.ErrTruncated
		}
		decoder.finished = true
		events, err := decoder.finishEvents()
		return events, decoder.takeLosses(), err
	}
	root, err := model.DecodeObject(frame.Data)
	if err != nil {
		return nil, nil, errors.New("stream chunk must be a JSON object")
	}
	if failure := model.Object(root["error"]); failure != nil {
		return nil, nil, &codec.StreamError{
			Code: model.String(failure["code"]), Type: model.String(failure["type"]),
			Message: model.String(failure["message"]), Status: int(model.IntegerOrZero(failure["status"])),
		}
	}
	var events []model.Event
	if !decoder.started {
		decoder.started = true
		created, _ := model.Integer(root["created"])
		events = append(events, model.Event{Kind: model.EventMessageStart, Meta: &model.ResponseMeta{
			ID: model.String(root["id"]), Model: model.String(root["model"]), Created: created,
			SystemFingerprint: model.String(root["system_fingerprint"]), ServiceTier: model.String(root["service_tier"]),
		}})
	}
	if usage := codec.ParseChatUsage(root); usage != nil {
		decoder.usage = usage
	}
	choices := model.Array(root["choices"])
	if len(choices) > 1 && !decoder.multiChoice {
		decoder.multiChoice = true
		decoder.losses = append(decoder.losses, model.Loss{Path: "choices", Code: model.LossMultiChoicePassthrough, Class: model.LossDegraded, Detail: "only the first choice is convertible; multiple choices require same-format pass-through", From: model.Chat})
	}
	for _, value := range choices {
		choice := model.Object(value)
		if index, _ := model.Integer(choice["index"]); index != 0 {
			continue
		}
		nested, err := decoder.deltaEvents(model.Object(choice["delta"]))
		if err != nil {
			return events, nil, err
		}
		events = append(events, nested...)
		if logprobs := choice["logprobs"]; logprobs != nil && decoder.open != nil {
			raw := model.Marshal(logprobs)
			decoder.stream.UpdatePart(decoder.open.itemIndex, decoder.open.partIndex, func(part *model.Part) {
				part.Logprobs = raw
			})
		}
		if finish := model.String(choice["finish_reason"]); finish != "" {
			reason, known := codec.ParseChatFinish(finish)
			if !known {
				decoder.losses = append(decoder.losses, codec.FinishMappedLoss(finish, model.Chat))
			}
			decoder.finish = model.Finish{Kind: reason, Raw: finish}
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

func (decoder *streamDecoder) deltaEvents(delta map[string]any) ([]model.Event, error) {
	if delta == nil {
		return nil, nil
	}
	var events []model.Event
	if len(model.Array(delta["annotations"])) > 0 && decoder.open != nil {
		for _, value := range model.Array(delta["annotations"]) {
			if object := model.Object(value); object != nil {
				citation := codec.DecodeCitation(model.Chat, object)
				decoder.stream.UpdatePart(decoder.open.itemIndex, decoder.open.partIndex, func(part *model.Part) {
					part.Citations = append(part.Citations, citation)
				})
			}
		}
	}
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if text := model.String(delta[key]); text != "" {
			nested, err := decoder.textDelta(model.PartReasoningText, text, model.ItemReasoning)
			if err != nil {
				return nil, err
			}
			events = append(events, nested...)
			break
		}
	}
	if text := model.String(delta["refusal"]); text != "" {
		nested, err := decoder.textDelta(model.PartRefusal, text, model.ItemMessage)
		if err != nil {
			return nil, err
		}
		events = append(events, nested...)
	}
	switch content := delta["content"].(type) {
	case string:
		if content != "" {
			nested, err := decoder.textDelta(model.PartText, content, model.ItemMessage)
			if err != nil {
				return nil, err
			}
			events = append(events, nested...)
		}
	case []any:
		items, err := codec.DecodeContentItems(model.Chat, model.RoleAssistant, content, codec.Peer{})
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
	for _, value := range model.Array(delta["tool_calls"]) {
		if err := decoder.toolDelta(model.Object(value)); err != nil {
			return nil, err
		}
	}
	if legacy := model.Object(delta["function_call"]); legacy != nil {
		if err := decoder.toolDelta(map[string]any{"index": 0, "type": "function", "function": legacy}); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func (decoder *streamDecoder) textDelta(partKind model.PartKind, text string, itemKind model.ItemKind) ([]model.Event, error) {
	var events []model.Event
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

func (decoder *streamDecoder) startTextItem(itemKind model.ItemKind, partKind model.PartKind) (model.Item, []model.Event, error) {
	item := model.Item{Kind: itemKind, Role: model.RoleAssistant}
	if itemKind == model.ItemReasoning {
		item.Reasoning = &model.Reasoning{Visibility: "full"}
	}
	itemIndex, events := decoder.stream.StartItem(item)
	part := model.Part{Kind: partKind}
	partIndex, started, err := decoder.stream.StartPart(itemIndex, part)
	if err != nil {
		return item, events, err
	}
	decoder.open = &openText{itemIndex: itemIndex, partIndex: partIndex, kind: partKind}
	return item, append(events, started...), nil
}

func (decoder *streamDecoder) stopOpenText() ([]model.Event, error) {
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
	if index, ok := model.Integer(call["index"]); ok {
		decoder.last = int(index)
		return decoder.last
	}
	id := model.String(call["id"])
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
	function, custom := model.Object(call["function"]), model.Object(call["custom"])
	pending := decoder.pending[position]
	if pending == nil {
		kind := model.ToolFunction
		if model.String(call["type"]) == "custom" || custom != nil {
			kind = model.ToolCustom
		}
		pending = &pendingCall{call: model.ToolCall{Kind: kind}}
		decoder.pending[position] = pending
	}
	stored := &pending.call
	named := function
	if custom != nil {
		named = custom
	}
	if id := model.String(call["id"]); id != "" {
		stored.ID = id
	}
	if name := model.String(named["name"]); name != "" {
		stored.Name = name
	}
	pending.args.WriteString(model.String(function["arguments"]))
	pending.args.WriteString(model.String(custom["input"]))
	return nil
}

func (decoder *streamDecoder) flushTools() ([]model.Event, error) {
	if len(decoder.pending) == 0 {
		return nil, nil
	}
	positions := make([]int, 0, len(decoder.pending))
	for position := range decoder.pending {
		positions = append(positions, position)
	}
	sort.Ints(positions)
	var events []model.Event
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
		if pending.call.Kind == model.ToolFunction {
			arguments = codec.EmptyObjectIfBlank(arguments)
		}
		if pending.call.ID == "" {
			pending.call.ID = decoder.ids.Assign(pending.call.Name, arguments)
		}
		item := model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &pending.call}
		itemIndex, started := decoder.stream.StartItem(item)
		events = append(events, started...)
		partIndex, partStarted, err := decoder.stream.StartPart(itemIndex, model.Part{Kind: model.PartToolArguments})
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

func (decoder *streamDecoder) emitWhole(item model.Item) ([]model.Event, error) {
	var events []model.Event
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
		content := model.StreamedContent(part)
		if part.Kind != model.PartToolArguments && content != "" {
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

func (decoder *streamDecoder) finishEvents() ([]model.Event, error) {
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
	if finish.Kind == model.FinishStop && decoder.stream.HasToolCall() {
		finish.Kind = model.FinishToolCalls
	}
	events = append(events,
		model.Event{Kind: model.EventMessageDelta, Usage: decoder.usage, Finish: &finish},
		model.Event{Kind: model.EventMessageStop})
	return events, nil
}

func (decoder *streamDecoder) End() ([]model.Event, []model.Loss, error) {
	if !decoder.finished {
		if !decoder.pendingClean() || !model.CleanEOF(decoder.stream) {
			return nil, nil, codec.ErrTruncated
		}
		decoder.finished = true
		decoder.losses = append(decoder.losses, model.Loss{Code: model.LossCleanEOF, Class: model.LossDegraded, Detail: "the provider closed the stream without a terminal frame", From: model.Chat})
	}
	events, err := decoder.finishEvents()
	return events, decoder.takeLosses(), err
}

// pendingClean reports whether every buffered tool call has a complete argument object.
// CleanEOF inspects the assembler only, so pending calls must be checked before they are flushed.
func (decoder *streamDecoder) pendingClean() bool {
	for _, pending := range decoder.pending {
		if pending.call.Kind == model.ToolCustom {
			continue
		}
		arguments := pending.args.String()
		if arguments != "" && !json.Valid([]byte(arguments)) {
			return false
		}
	}
	return true
}

func (decoder *streamDecoder) takeLosses() []model.Loss {
	losses := decoder.losses
	decoder.losses = nil
	return losses
}
