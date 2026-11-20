package gemini

import (
	"encoding/json"
	"errors"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type openText struct {
	itemIndex int
	partIndex int
	kind      protocol.PartKind
	itemKind  protocol.ItemKind
}

type streamDecoder struct {
	peer     format.Peer
	stream   *format.ItemStream
	ids      *format.CallIDs
	open     *openText
	meta     protocol.ResponseMeta
	usage    *protocol.Usage
	finish   protocol.Finish
	started  bool
	finished bool
	done     bool
	losses   []protocol.Loss
}

func (Codec) NewStreamDecoder(peer format.Peer) format.StreamDecoder {
	return &streamDecoder{peer: peer, stream: format.NewItemStream(), ids: format.NewCallIDs(peer), finish: protocol.Finish{Kind: protocol.FinishStop}}
}

func (decoder *streamDecoder) Feed(frame format.Frame) ([]protocol.Event, []protocol.Loss, error) {
	if decoder.done {
		return nil, nil, nil
	}
	root, err := protocol.DecodeObject(frame.Data)
	if err != nil {
		return nil, nil, errors.New("stream chunk must be a JSON object")
	}
	if failure := protocol.Object(root["error"]); failure != nil {
		return nil, nil, &format.StreamError{Code: protocol.String(failure["status"]), Type: protocol.String(failure["status"]), Message: protocol.String(failure["message"]), Status: int(protocol.IntegerOrZero(failure["code"]))}
	}
	var events []protocol.Event
	if !decoder.started {
		decoder.started = true
		decoder.meta = protocol.ResponseMeta{ID: protocol.String(root["responseId"]), Model: protocol.String(root["modelVersion"])}
		events = append(events, protocol.Event{Kind: protocol.EventMessageStart, Meta: &decoder.meta})
	}
	if usage := format.ParseGeminiUsage(root); usage != nil {
		decoder.usage = usage
	}
	for _, value := range protocol.Array(root["candidates"]) {
		candidate := protocol.Object(value)
		if index, _ := protocol.Integer(candidate["index"]); index != 0 {
			return nil, nil, protocol.UnsupportedFeature("unsupported_multi_choice", "Multiple candidates are not supported.")
		}
		for _, value := range protocol.Array(protocol.Object(candidate["content"])["parts"]) {
			nested, err := decoder.part(protocol.Object(value))
			if err != nil {
				return events, nil, err
			}
			events = append(events, nested...)
		}
		if citations := candidateCitations(candidate); len(citations) > 0 && decoder.open != nil {
			decoder.stream.UpdatePart(decoder.open.itemIndex, decoder.open.partIndex, func(part *protocol.Part) {
				part.Citations = append(part.Citations, citations...)
			})
		}
		if reason := protocol.String(candidate["finishReason"]); reason != "" {
			finish, known := format.ParseGeminiFinish(reason)
			decoder.finish = protocol.Finish{Kind: finish, Raw: reason}
			decoder.finished = true
			if !known {
				decoder.losses = append(decoder.losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: reason, To: protocol.Gemini})
			}
		}
	}
	if feedback := protocol.Object(root["promptFeedback"]); protocol.String(feedback["blockReason"]) != "" {
		decoder.finish, decoder.finished = protocol.Finish{Kind: protocol.FinishContentFilter}, true
	}
	return events, nil, nil
}

func (decoder *streamDecoder) part(entry map[string]any) ([]protocol.Event, error) {
	if entry == nil {
		return nil, nil
	}
	if media, err := format.DecodeGeminiMedia(entry); err != nil {
		return nil, err
	} else if media != nil {
		events, err := decoder.stopText()
		if err != nil {
			return nil, err
		}
		item := protocol.Item{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartMedia, Media: media}}}
		whole, err := decoder.emitWhole(item)
		return append(events, whole...), err
	}
	if entry["functionCall"] != nil {
		events, err := decoder.stopText()
		if err != nil {
			return nil, err
		}
		call := protocol.Object(entry["functionCall"])
		name := protocol.String(call["name"])
		arguments := "{}"
		if call["args"] != nil {
			arguments = string(protocol.Marshal(call["args"]))
		}
		id := protocol.String(call["id"])
		if id == "" {
			id = decoder.ids.Assign(name, arguments)
		}
		tool := &protocol.ToolCall{Kind: protocol.ToolFunction, ID: id, Name: name, Arguments: arguments}
		if signature := protocol.String(entry["thoughtSignature"]); signature != "" {
			tool.Signature = &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: signature}
		}
		whole, err := decoder.emitWhole(protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: tool})
		return append(events, whole...), err
	}
	text := protocol.String(entry["text"])
	thought := entry["thought"] == true
	signature := protocol.String(entry["thoughtSignature"])
	if entry["text"] != nil {
		partKind, itemKind := protocol.PartText, protocol.ItemMessage
		if thought {
			partKind, itemKind = protocol.PartReasoningText, protocol.ItemReasoning
		}
		var events []protocol.Event
		if decoder.open == nil || decoder.open.kind != partKind {
			stopped, err := decoder.stopText()
			if err != nil {
				return nil, err
			}
			events = append(events, stopped...)
			item := protocol.Item{Kind: itemKind, Role: protocol.RoleAssistant}
			if itemKind == protocol.ItemReasoning {
				item.Reasoning = &protocol.Reasoning{Visibility: "full"}
			}
			itemIndex, started := decoder.stream.StartItem(item)
			partIndex, partStarted, err := decoder.stream.StartPart(itemIndex, protocol.Part{Kind: partKind})
			if err != nil {
				return events, err
			}
			decoder.open = &openText{itemIndex: itemIndex, partIndex: partIndex, kind: partKind, itemKind: itemKind}
			events = append(events, started...)
			events = append(events, partStarted...)
		}
		if signature != "" {
			decoder.stream.UpdateItem(decoder.open.itemIndex, func(item *protocol.Item) {
				if item.Reasoning == nil {
					item.Reasoning = &protocol.Reasoning{Visibility: "full"}
				}
				item.Reasoning.Signature = &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: signature}
			})
		}
		if text != "" {
			nested, err := decoder.stream.Delta(decoder.open.itemIndex, decoder.open.partIndex, text)
			if err != nil {
				return events, err
			}
			events = append(events, nested...)
		}
		return events, nil
	}
	if signature != "" {
		events, err := decoder.stopText()
		if err != nil {
			return nil, err
		}
		item := protocol.Item{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, Reasoning: &protocol.Reasoning{Visibility: "full", Signature: &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: signature}}}
		whole, err := decoder.emitWhole(item)
		return append(events, whole...), err
	}
	events, err := decoder.stopText()
	if err != nil {
		return nil, err
	}
	whole, err := decoder.emitWhole(protocol.Item{Kind: protocol.ItemNative, Role: protocol.RoleAssistant, Raw: &protocol.Raw{Format: protocol.Gemini, Fields: map[string]json.RawMessage{"native": protocol.Marshal(entry)}}})
	return append(events, whole...), err
}

func (decoder *streamDecoder) stopText() ([]protocol.Event, error) {
	if decoder.open == nil {
		return nil, nil
	}
	open := decoder.open
	decoder.open = nil
	if open.itemKind == protocol.ItemReasoning {
		text := decoder.stream.Streamed(open.itemIndex, open.partIndex)
		decoder.stream.UpdateItem(open.itemIndex, func(item *protocol.Item) {
			if item.Reasoning != nil && item.Reasoning.Signature != nil {
				item.Reasoning.Signature.TextDigest = protocol.TextDigest(text)
			}
		})
	}
	partEvents, err := decoder.stream.StopPart(open.itemIndex, open.partIndex)
	if err != nil {
		return nil, err
	}
	itemEvents, err := decoder.stream.StopItem(open.itemIndex, nil)
	if err != nil {
		return partEvents, err
	}
	return append(partEvents, itemEvents...), nil
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
	var events []protocol.Event
	if decoder.open != nil {
		stopped, err := decoder.stopText()
		if err != nil {
			return nil, err
		}
		events = append(events, stopped...)
	}
	stopped, err := decoder.stream.StopAll()
	if err != nil {
		return nil, err
	}
	events = append(events, stopped...)
	finish := decoder.finish
	if finish.Kind == protocol.FinishStop && decoder.stream.HasToolCall() {
		finish.Kind = protocol.FinishToolCalls
	}
	return append(events,
		protocol.Event{Kind: protocol.EventMessageDelta, Usage: decoder.usage, Finish: &finish},
		protocol.Event{Kind: protocol.EventMessageStop}), nil
}

func (decoder *streamDecoder) End() ([]protocol.Event, []protocol.Loss, error) {
	if decoder.done {
		return nil, decoder.takeLosses(), nil
	}
	if !decoder.finished {
		if !decoder.started {
			return nil, nil, format.ErrTruncated
		}
		if decoder.open != nil {
			if decoder.stream.Streamed(decoder.open.itemIndex, decoder.open.partIndex) == "" {
				return nil, nil, format.ErrTruncated
			}
		} else if !format.CleanEOF(decoder.stream) {
			return nil, nil, format.ErrTruncated
		}
		decoder.finished = true
		decoder.losses = append(decoder.losses, protocol.Loss{Code: protocol.LossCleanEOF, Class: protocol.LossDegraded, Detail: "the provider closed the stream without a finishReason", From: protocol.Gemini})
	}
	events, err := decoder.finishEvents()
	return events, decoder.takeLosses(), err
}

func (decoder *streamDecoder) takeLosses() []protocol.Loss {
	losses := decoder.losses
	decoder.losses = nil
	return losses
}
