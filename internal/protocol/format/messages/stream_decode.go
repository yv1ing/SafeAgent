package messages

import (
	"errors"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type openBlock struct {
	itemIndex int
	partIndex int
	kind      protocol.PartKind
	itemKind  protocol.ItemKind
	text      bool
	tool      bool
}

type streamDecoder struct {
	peer    format.Peer
	stream  *format.ItemStream
	blocks  map[int]*openBlock
	meta    protocol.ResponseMeta
	usage   *protocol.Usage
	finish  protocol.Finish
	started bool
	done    bool
	losses  []protocol.Loss
}

func (Codec) NewStreamDecoder(peer format.Peer) format.StreamDecoder {
	return &streamDecoder{peer: peer, stream: format.NewItemStream(), blocks: map[int]*openBlock{}, finish: protocol.Finish{Kind: protocol.FinishStop}}
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
	case "message_start":
		message := protocol.Object(root["message"])
		decoder.meta = protocol.ResponseMeta{ID: protocol.String(message["id"]), Model: protocol.String(message["model"])}
		decoder.usage = format.ParseMessagesUsage(protocol.Object(message["usage"]))
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
		if usage := format.ParseMessagesUsage(protocol.Object(root["usage"])); usage != nil {
			decoder.usage = mergeUsage(decoder.usage, usage)
		}
		delta := protocol.Object(root["delta"])
		if reason := protocol.String(delta["stop_reason"]); reason != "" {
			decoder.finish.Raw = reason
			parsed, known := format.ParseMessagesFinish(reason)
			if !known {
				decoder.losses = append(decoder.losses, format.FinishMappedLoss(reason, protocol.Messages))
			}
			decoder.finish.Kind = parsed
		}
		if stop := protocol.String(delta["stop_sequence"]); stop != "" {
			decoder.finish.StopSequence = stop
		}
		return nil, nil, nil
	case "message_stop":
		events, err := decoder.finishEvents()
		return events, decoder.takeLosses(), err
	case "error":
		failure := protocol.Object(root["error"])
		return nil, nil, &format.StreamError{Code: protocol.String(failure["type"]), Type: protocol.String(failure["type"]), Message: protocol.String(failure["message"]), Status: int(protocol.IntegerOrZero(failure["status"]))}
	case "ping":
		return []protocol.Event{{Kind: protocol.EventPing}}, nil, nil
	}
	return nil, nil, nil
}

func (decoder *streamDecoder) ensureStart() ([]protocol.Event, error) {
	if decoder.started {
		return nil, nil
	}
	decoder.started = true
	return []protocol.Event{{Kind: protocol.EventMessageStart, Meta: &decoder.meta, Usage: decoder.usage}}, nil
}

func (decoder *streamDecoder) startBlock(root map[string]any) ([]protocol.Event, error) {
	events, err := decoder.ensureStart()
	if err != nil {
		return nil, err
	}
	index := int(protocol.IntegerOrZero(root["index"]))
	if _, seen := decoder.blocks[index]; seen {
		return events, nil
	}
	entry := protocol.Object(root["content_block"])
	blockType := protocol.String(entry["type"])
	switch blockType {
	case "text", "thinking":
		itemKind, partKind := protocol.ItemMessage, protocol.PartText
		if blockType == "thinking" {
			itemKind, partKind = protocol.ItemReasoning, protocol.PartReasoningText
		}
		item := protocol.Item{Kind: itemKind, Role: protocol.RoleAssistant}
		if itemKind == protocol.ItemReasoning {
			item.Reasoning = &protocol.Reasoning{Visibility: "full"}
		}
		itemIndex, started := decoder.stream.StartItem(item)
		partIndex, partStarted, err := decoder.stream.StartPart(itemIndex, protocol.Part{Kind: partKind})
		if err != nil {
			return events, err
		}
		decoder.blocks[index] = &openBlock{itemIndex: itemIndex, partIndex: partIndex, kind: partKind, itemKind: itemKind, text: true}
		events = append(events, started...)
		events = append(events, partStarted...)
		if text := protocol.String(entry[blockType]); text != "" {
			nested, err := decoder.stream.Delta(itemIndex, partIndex, text)
			if err != nil {
				return events, err
			}
			events = append(events, nested...)
		}
		return events, nil
	case "tool_use":
		call := protocol.ToolCall{Kind: protocol.ToolFunction, ID: protocol.String(entry["id"]), Name: protocol.String(entry["name"]), Arguments: format.EmptyObjectIfBlank(inputJSON(entry["input"]))}
		item := protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &call, CacheControl: format.DecodeCacheControl(entry["cache_control"])}
		itemIndex, started := decoder.stream.StartItem(item)
		partIndex, partStarted, err := decoder.stream.StartPart(itemIndex, protocol.Part{Kind: protocol.PartToolArguments})
		if err != nil {
			return events, err
		}
		decoder.blocks[index] = &openBlock{itemIndex: itemIndex, partIndex: partIndex, kind: protocol.PartToolArguments, itemKind: protocol.ItemToolCall, tool: true}
		events = append(events, started...)
		events = append(events, partStarted...)
		if input := protocol.Object(entry["input"]); len(input) > 0 {
			nested, err := decoder.stream.Delta(itemIndex, partIndex, string(protocol.Marshal(input)))
			if err != nil {
				return events, err
			}
			events = append(events, nested...)
		}
		return events, nil
	}
	items, err := format.DecodeContentItems(protocol.Messages, protocol.RoleAssistant, []any{entry}, decoder.peer)
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

func (decoder *streamDecoder) delta(root map[string]any) ([]protocol.Event, error) {
	block := decoder.blocks[int(protocol.IntegerOrZero(root["index"]))]
	if block == nil {
		return nil, nil
	}
	delta := protocol.Object(root["delta"])
	switch protocol.String(delta["type"]) {
	case "text_delta":
		nested, err := decoder.stream.Delta(block.itemIndex, block.partIndex, protocol.String(delta["text"]))
		return nested, err
	case "thinking_delta":
		nested, err := decoder.stream.Delta(block.itemIndex, block.partIndex, protocol.String(delta["thinking"]))
		return nested, err
	case "input_json_delta":
		nested, err := decoder.stream.Delta(block.itemIndex, block.partIndex, protocol.String(delta["partial_json"]))
		return nested, err
	case "signature_delta":
		signature := &protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: protocol.String(delta["signature"])}
		decoder.stream.UpdateItem(block.itemIndex, func(item *protocol.Item) {
			if item.Reasoning == nil {
				item.Reasoning = &protocol.Reasoning{Visibility: "full"}
			}
			item.Reasoning.Signature = signature
		})
		return nil, nil
	case "citations_delta":
		citation := format.DecodeCitation(protocol.Messages, protocol.Object(delta["citation"]))
		decoder.stream.UpdatePart(block.itemIndex, block.partIndex, func(part *protocol.Part) {
			part.Citations = append(part.Citations, citation)
		})
		return nil, nil
	}
	return nil, nil
}

func (decoder *streamDecoder) stopBlock(root map[string]any) ([]protocol.Event, error) {
	index := int(protocol.IntegerOrZero(root["index"]))
	block := decoder.blocks[index]
	if block == nil {
		return nil, nil
	}
	delete(decoder.blocks, index)
	if block.tool && decoder.stream.Streamed(block.itemIndex, block.partIndex) == "" {
		decoder.stream.UpdateItem(block.itemIndex, func(item *protocol.Item) {
			if item.ToolCall != nil && item.ToolCall.Arguments == "" {
				item.ToolCall.Arguments = "{}"
			}
		})
	}
	if block.itemKind == protocol.ItemReasoning {
		text := decoder.stream.Streamed(block.itemIndex, block.partIndex)
		decoder.stream.UpdateItem(block.itemIndex, func(item *protocol.Item) {
			if item.Reasoning != nil && item.Reasoning.Signature != nil {
				item.Reasoning.Signature.TextDigest = protocol.TextDigest(text)
			}
		})
	}
	var events []protocol.Event
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

func (decoder *streamDecoder) emitWhole(item protocol.Item) ([]protocol.Event, error) {
	itemIndex, events := decoder.stream.StartItem(item)
	for partIndex := range item.Parts {
		part := item.Parts[partIndex]
		index, started, err := decoder.stream.StartPart(itemIndex, part)
		if err != nil {
			return events, err
		}
		events = append(events, started...)
		if content := protocol.StreamedContent(part); content != "" {
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
	stopped, err := decoder.stream.StopAll()
	if err != nil {
		return nil, err
	}
	finish := decoder.finish
	if finish.Kind == protocol.FinishStop && decoder.stream.HasToolCall() {
		finish.Kind = protocol.FinishToolCalls
	}
	return append(stopped,
		protocol.Event{Kind: protocol.EventMessageDelta, Usage: decoder.usage, Finish: &finish},
		protocol.Event{Kind: protocol.EventMessageStop}), nil
}

func (decoder *streamDecoder) End() ([]protocol.Event, []protocol.Loss, error) {
	if decoder.done {
		return nil, decoder.takeLosses(), nil
	}
	if !decoder.started || !format.CleanEOF(decoder.stream) {
		return nil, nil, format.ErrTruncated
	}
	decoder.losses = append(decoder.losses, protocol.Loss{Code: protocol.LossCleanEOF, Class: protocol.LossDegraded, Detail: "the provider closed the stream without a terminal frame", From: protocol.Messages})
	events, err := decoder.finishEvents()
	return events, decoder.takeLosses(), err
}

func (decoder *streamDecoder) takeLosses() []protocol.Loss {
	losses := decoder.losses
	decoder.losses = nil
	return losses
}

func mergeUsage(current, next *protocol.Usage) *protocol.Usage {
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
	return string(protocol.Marshal(value))
}
