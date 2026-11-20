package messages

import (
	"errors"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type streamDecoder struct {
	blocks *format.BlockStream
	done   bool
	byWire map[int]int
	usage  *protocol.Usage
	finish protocol.FinishReason
	stop   string
	losses []protocol.Loss
}

func (Codec) NewStreamDecoder(peer format.Peer) format.StreamDecoder {
	return &streamDecoder{blocks: format.NewBlockStream(), byWire: map[int]int{}, finish: protocol.FinishStop}
}

func (decoder *streamDecoder) Feed(frame format.Frame) ([]protocol.Event, error) {
	if decoder.done {
		return nil, nil
	}
	root, err := protocol.DecodeObject(frame.Data)
	if err != nil {
		return nil, errors.New("stream event must be a JSON object")
	}
	kind := protocol.String(root["type"])
	if kind == "" {
		kind = frame.Event
	}
	switch kind {
	case "message_start":
		message := protocol.Object(root["message"])
		decoder.usage = format.ParseMessagesUsage(protocol.Object(message["usage"]))
		return []protocol.Event{{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: protocol.String(message["id"]), Model: protocol.String(message["model"])}}}, nil
	case "content_block_start":
		return decoder.startBlock(root)
	case "content_block_delta":
		return decoder.delta(root)
	case "content_block_stop":
		index, known := decoder.byWire[wireIndex(root)]
		if !known {
			return nil, nil
		}
		return decoder.blocks.Stop(index), nil
	case "message_delta":
		if usage := format.ParseMessagesUsage(protocol.Object(root["usage"])); usage != nil {
			decoder.usage = mergeUsage(decoder.usage, usage)
		}
		delta := protocol.Object(root["delta"])
		if reason := protocol.String(delta["stop_reason"]); reason != "" {
			parsed, known := format.ParseMessagesFinish(reason)
			if !known {
				decoder.losses = append(decoder.losses, format.FinishMappedLoss(reason, protocol.Messages))
			}
			decoder.finish = parsed
		}
		if stop := protocol.String(delta["stop_sequence"]); stop != "" {
			decoder.stop = stop
		}
		return nil, nil
	case "message_stop":
		return decoder.finishEvents(), nil
	case "error":
		failure := protocol.Object(root["error"])
		return nil, &format.StreamError{Code: protocol.String(failure["type"]), Message: protocol.String(failure["message"])}
	}
	return nil, nil
}

func wireIndex(root map[string]any) int {
	index, _ := protocol.Integer(root["index"])
	return int(index)
}

func (decoder *streamDecoder) startBlock(root map[string]any) ([]protocol.Event, error) {
	entry := protocol.Object(root["content_block"])
	if entry == nil {
		return nil, errors.New("content block start without a block")
	}
	block, err := format.DecodeBlock(protocol.Messages, entry)
	if err != nil {
		return nil, err
	}
	if block.Kind == protocol.KindToolCall {
		// content_block_start carries "input": {} as a placeholder; the real input follows as deltas.
		block = protocol.WithStreamedContent(block, "")
	}
	index, events := decoder.blocks.Start(block)
	decoder.byWire[wireIndex(root)] = index
	return events, nil
}

func (decoder *streamDecoder) delta(root map[string]any) ([]protocol.Event, error) {
	index, known := decoder.byWire[wireIndex(root)]
	if !known {
		return nil, errors.New("content block delta for an unopened block")
	}
	delta := protocol.Object(root["delta"])
	switch protocol.String(delta["type"]) {
	case "text_delta":
		return decoder.blocks.Delta(index, protocol.String(delta["text"]))
	case "thinking_delta":
		return decoder.blocks.Delta(index, protocol.String(delta["thinking"]))
	case "input_json_delta":
		return decoder.blocks.Delta(index, protocol.String(delta["partial_json"]))
	case "signature_delta":
		decoder.blocks.Update(index, func(block *protocol.Block) {
			if block.Reasoning == nil {
				block.Reasoning = &protocol.Reasoning{Visibility: "full"}
			}
			block.Reasoning.Signature = &protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: protocol.String(delta["signature"])}
		})
	case "citations_delta":
		if citation := protocol.Object(delta["citation"]); citation != nil {
			decoder.blocks.Update(index, func(block *protocol.Block) {
				block.Citations = append(block.Citations, format.DecodeCitation(protocol.Messages, citation))
			})
		}
	}
	return nil, nil
}

func (decoder *streamDecoder) finishEvents() []protocol.Event {
	if decoder.done {
		return nil
	}
	decoder.done = true
	events := decoder.blocks.StopAll()
	return append(events,
		protocol.Event{Kind: protocol.EventMessageDelta, Usage: decoder.usage, Finish: decoder.finish, Stop: decoder.stop, Losses: decoder.losses},
		protocol.Event{Kind: protocol.EventMessageStop})
}

// End synthesises a clean terminator when the stream stopped after a complete response.
func (decoder *streamDecoder) End() ([]protocol.Event, error) {
	if decoder.done {
		return nil, nil
	}
	if !format.CleanEOF(decoder.blocks) {
		return nil, format.ErrTruncated
	}
	decoder.losses = append(decoder.losses, protocol.Loss{Code: protocol.LossCleanEOF, Detail: "the provider closed the stream without message_stop", From: protocol.Messages})
	return decoder.finishEvents(), nil
}

// mergeUsage applies a message_delta, which restates output tokens and may restate input counts.
func mergeUsage(current, update *protocol.Usage) *protocol.Usage {
	if current == nil {
		return update
	}
	merged := *current
	if update.Input > 0 {
		merged.Input, merged.CacheRead, merged.CacheWrite, merged.CacheWrite1h = update.Input, update.CacheRead, update.CacheWrite, update.CacheWrite1h
	}
	if update.Output > 0 {
		merged.Output = update.Output
	}
	if update.Reasoning > 0 {
		merged.Reasoning = update.Reasoning
	}
	return &merged
}
