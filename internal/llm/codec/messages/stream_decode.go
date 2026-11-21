package messages

import (
	"errors"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type openBlock struct {
	itemIndex int
	partIndex int
	kind      model.PartKind
	itemKind  model.ItemKind
	text      bool
	tool      bool
}

type streamDecoder struct {
	peer    codec.Peer
	stream  *model.Assembler
	blocks  map[int]*openBlock
	meta    model.ResponseMeta
	usage   *model.Usage
	finish  model.Finish
	started bool
	done    bool
	losses  []model.Loss
}

func (Codec) NewStreamDecoder(peer codec.Peer) codec.StreamDecoder {
	return &streamDecoder{peer: peer, stream: model.NewAssembler(), blocks: map[int]*openBlock{}, finish: model.Finish{Kind: model.FinishStop}}
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
	case "message_start":
		message := model.Object(root["message"])
		decoder.meta = model.ResponseMeta{ID: model.String(message["id"]), Model: model.String(message["model"])}
		decoder.usage = codec.ParseMessagesUsage(model.Object(message["usage"]))
		events, err := decoder.ensureStart()
		return events, nil, err
	case "content_block_start":
		events, err := decoder.startBlock(root)
		return events, nil, err
	case "content_block_delta":
		events, err := decoder.delta(root)
		return events, nil, err
	case "content_block_stop":
		events, err := decoder.stopBlock(root)
		return events, nil, err
	case "message_delta":
		if usage := codec.ParseMessagesUsage(model.Object(root["usage"])); usage != nil {
			decoder.usage = mergeUsage(decoder.usage, usage)
		}
		delta := model.Object(root["delta"])
		if reason := model.String(delta["stop_reason"]); reason != "" {
			decoder.finish.Raw = reason
			parsed, known := codec.ParseMessagesFinish(reason)
			if !known {
				decoder.losses = append(decoder.losses, codec.FinishMappedLoss(reason, model.Messages))
			}
			decoder.finish.Kind = parsed
		}
		if stop := model.String(delta["stop_sequence"]); stop != "" {
			decoder.finish.StopSequence = stop
		}
		return nil, nil, nil
	case "message_stop":
		events, err := decoder.finishEvents()
		return events, decoder.takeLosses(), err
	case "error":
		failure := model.Object(root["error"])
		return nil, nil, &codec.StreamError{Code: model.String(failure["type"]), Type: model.String(failure["type"]), Message: model.String(failure["message"]), Status: int(model.IntegerOrZero(failure["status"]))}
	case "ping":
		return []model.Event{{Kind: model.EventPing}}, nil, nil
	}
	return nil, nil, nil
}

func (decoder *streamDecoder) ensureStart() ([]model.Event, error) {
	if decoder.started {
		return nil, nil
	}
	decoder.started = true
	return []model.Event{{Kind: model.EventMessageStart, Meta: &decoder.meta, Usage: decoder.usage}}, nil
}

func (decoder *streamDecoder) startBlock(root map[string]any) ([]model.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	index := int(model.IntegerOrZero(root["index"]))
	if _, seen := decoder.blocks[index]; seen {
		return events, nil
	}
	entry := model.Object(root["content_block"])
	blockType := model.String(entry["type"])
	switch blockType {
	case "text", "thinking":
		itemKind, partKind := model.ItemMessage, model.PartText
		if blockType == "thinking" {
			itemKind, partKind = model.ItemReasoning, model.PartReasoningText
		}
		item := model.Item{Kind: itemKind, Role: model.RoleAssistant}
		if itemKind == model.ItemReasoning {
			item.Reasoning = &model.Reasoning{Visibility: "full"}
		}
		itemIndex, started := decoder.stream.StartItem(item)
		partIndex, partStarted, err := decoder.stream.StartPart(itemIndex, model.Part{Kind: partKind})
		if err != nil {
			return events, err
		}
		decoder.blocks[index] = &openBlock{itemIndex: itemIndex, partIndex: partIndex, kind: partKind, itemKind: itemKind, text: true}
		events = append(events, started...)
		events = append(events, partStarted...)
		if text := model.String(entry[blockType]); text != "" {
			nested, err := decoder.stream.Delta(itemIndex, partIndex, text)
			if err != nil {
				return events, err
			}
			events = append(events, nested...)
		}
		return events, nil
	case "tool_use":
		call := model.ToolCall{Kind: model.ToolFunction, ID: model.String(entry["id"]), Name: model.String(entry["name"]), Arguments: codec.EmptyObjectIfBlank(inputJSON(entry["input"]))}
		item := model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &call, CacheControl: codec.DecodeCacheControl(entry["cache_control"])}
		itemIndex, started := decoder.stream.StartItem(item)
		partIndex, partStarted, err := decoder.stream.StartPart(itemIndex, model.Part{Kind: model.PartToolArguments})
		if err != nil {
			return events, err
		}
		decoder.blocks[index] = &openBlock{itemIndex: itemIndex, partIndex: partIndex, kind: model.PartToolArguments, itemKind: model.ItemToolCall, tool: true}
		events = append(events, started...)
		events = append(events, partStarted...)
		if input := model.Object(entry["input"]); len(input) > 0 {
			nested, err := decoder.stream.Delta(itemIndex, partIndex, string(model.Marshal(input)))
			if err != nil {
				return events, err
			}
			events = append(events, nested...)
		}
		return events, nil
	}
	items, err := codec.DecodeContentItems(model.Messages, model.RoleAssistant, []any{entry}, decoder.peer)
	if err != nil {
		return events, err
	}
	for _, item := range items {
		nested, err := decoder.emitWhole(item)
		if err != nil {
			return events, err
		}
		events = append(events, nested...)
	}
	return events, nil
}

func (decoder *streamDecoder) delta(root map[string]any) ([]model.Event, error) {
	block := decoder.blocks[int(model.IntegerOrZero(root["index"]))]
	if block == nil {
		return nil, nil
	}
	delta := model.Object(root["delta"])
	switch model.String(delta["type"]) {
	case "text_delta":
		nested, err := decoder.stream.Delta(block.itemIndex, block.partIndex, model.String(delta["text"]))
		return nested, err
	case "thinking_delta":
		nested, err := decoder.stream.Delta(block.itemIndex, block.partIndex, model.String(delta["thinking"]))
		return nested, err
	case "input_json_delta":
		nested, err := decoder.stream.Delta(block.itemIndex, block.partIndex, model.String(delta["partial_json"]))
		return nested, err
	case "signature_delta":
		signature := &model.Signature{Vendor: model.VendorAnthropic, Blob: model.String(delta["signature"])}
		decoder.stream.UpdateItem(block.itemIndex, func(item *model.Item) {
			if item.Reasoning == nil {
				item.Reasoning = &model.Reasoning{Visibility: "full"}
			}
			item.Reasoning.Signature = signature
		})
		return nil, nil
	case "citations_delta":
		citation := codec.DecodeCitation(model.Messages, model.Object(delta["citation"]))
		decoder.stream.UpdatePart(block.itemIndex, block.partIndex, func(part *model.Part) {
			part.Citations = append(part.Citations, citation)
		})
		return nil, nil
	}
	return nil, nil
}

func (decoder *streamDecoder) stopBlock(root map[string]any) ([]model.Event, error) {
	index := int(model.IntegerOrZero(root["index"]))
	block := decoder.blocks[index]
	if block == nil {
		return nil, nil
	}
	delete(decoder.blocks, index)
	if block.tool && decoder.stream.Streamed(block.itemIndex, block.partIndex) == "" {
		decoder.stream.UpdateItem(block.itemIndex, func(item *model.Item) {
			if item.ToolCall != nil && item.ToolCall.Arguments == "" {
				item.ToolCall.Arguments = "{}"
			}
		})
	}
	if block.itemKind == model.ItemReasoning {
		text := decoder.stream.Streamed(block.itemIndex, block.partIndex)
		decoder.stream.UpdateItem(block.itemIndex, func(item *model.Item) {
			if item.Reasoning != nil && item.Reasoning.Signature != nil {
				item.Reasoning.Signature.TextDigest = model.TextDigest(text)
			}
		})
	}
	var events []model.Event
	if nested, err := decoder.stream.StopPart(block.itemIndex, block.partIndex); err != nil {
		return events, err
	} else {
		events = append(events, nested...)
	}
	if nested, err := decoder.stream.StopItem(block.itemIndex, nil); err != nil {
		return events, err
	} else {
		events = append(events, nested...)
	}
	return events, nil
}

func (decoder *streamDecoder) emitWhole(item model.Item) ([]model.Event, error) {
	itemIndex, events := decoder.stream.StartItem(item)
	for partIndex := range item.Parts {
		part := item.Parts[partIndex]
		index, started, err := decoder.stream.StartPart(itemIndex, part)
		if err != nil {
			return events, err
		}
		events = append(events, started...)
		if content := model.StreamedContent(part); content != "" {
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
	stopped, err := decoder.stream.StopAll()
	if err != nil {
		return nil, err
	}
	finish := decoder.finish
	if finish.Kind == model.FinishStop && decoder.stream.HasToolCall() {
		finish.Kind = model.FinishToolCalls
	}
	return append(stopped,
		model.Event{Kind: model.EventMessageDelta, Usage: decoder.usage, Finish: &finish},
		model.Event{Kind: model.EventMessageStop}), nil
}

func (decoder *streamDecoder) End() ([]model.Event, []model.Loss, error) {
	if decoder.done {
		return nil, decoder.takeLosses(), nil
	}
	if !decoder.started || !model.CleanEOF(decoder.stream) {
		return nil, nil, codec.ErrTruncated
	}
	decoder.losses = append(decoder.losses, model.Loss{Code: model.LossCleanEOF, Class: model.LossDegraded, Detail: "the provider closed the stream without a terminal frame", From: model.Messages})
	events, err := decoder.finishEvents()
	return events, decoder.takeLosses(), err
}

func (decoder *streamDecoder) takeLosses() []model.Loss {
	losses := decoder.losses
	decoder.losses = nil
	return losses
}

func mergeUsage(current, next *model.Usage) *model.Usage {
	if current == nil {
		return next
	}
	if next == nil {
		return current
	}
	if next.Input > 0 {
		current.Input = next.Input
	}
	if next.Output > 0 {
		current.Output = next.Output
	}
	if next.CacheRead > 0 {
		current.CacheRead = next.CacheRead
	}
	if next.CacheWrite > 0 {
		current.CacheWrite = next.CacheWrite
	}
	if next.CacheWrite1h > 0 {
		current.CacheWrite1h = next.CacheWrite1h
	}
	if next.Reasoning > 0 {
		current.Reasoning = next.Reasoning
	}
	return current
}

func inputJSON(value any) string {
	if value == nil {
		return ""
	}
	return string(model.Marshal(value))
}
