package gemini

import (
	"encoding/json"
	"errors"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type openText struct {
	itemIndex int
	partIndex int
	kind      model.PartKind
	itemKind  model.ItemKind
}

type streamDecoder struct {
	peer        codec.Peer
	stream      *model.Assembler
	ids         *codec.CallIDs
	open        *openText
	meta        model.ResponseMeta
	usage       *model.Usage
	finish      model.Finish
	started     bool
	finished    bool
	done        bool
	multiChoice bool
	losses      []model.Loss
}

func (Codec) NewStreamDecoder(peer codec.Peer) codec.StreamDecoder {
	return &streamDecoder{peer: peer, stream: model.NewAssembler(), ids: codec.NewCallIDs(peer), finish: model.Finish{Kind: model.FinishStop}}
}

func (decoder *streamDecoder) Feed(frame codec.Frame) ([]model.Event, []model.Loss, error) {
	if decoder.done {
		return nil, nil, nil
	}
	root, err := model.DecodeObject(frame.Data)
	if err != nil {
		return nil, nil, errors.New("stream chunk must be a JSON object")
	}
	if failure := model.Object(root["error"]); failure != nil {
		return nil, nil, &codec.StreamError{Code: model.String(failure["status"]), Type: model.String(failure["status"]), Message: model.String(failure["message"]), Status: int(model.IntegerOrZero(failure["code"]))}
	}
	var events []model.Event
	if !decoder.started {
		decoder.started = true
		decoder.meta = model.ResponseMeta{ID: model.String(root["responseId"]), Model: model.String(root["modelVersion"])}
		events = append(events, model.Event{Kind: model.EventMessageStart, Meta: &decoder.meta})
	}
	if usage := codec.ParseGeminiUsage(root); usage != nil {
		decoder.usage = usage
	}
	candidates := model.Array(root["candidates"])
	if len(candidates) > 1 && !decoder.multiChoice {
		decoder.multiChoice = true
		decoder.losses = append(decoder.losses, model.Loss{Path: "candidates", Code: model.LossMultiChoicePassthrough, Class: model.LossDegraded, Detail: "only the first candidate is convertible; multiple candidates require same-format pass-through", From: model.Gemini})
	}
	for _, value := range candidates {
		candidate := model.Object(value)
		if index, _ := model.Integer(candidate["index"]); index != 0 {
			continue
		}
		for _, value := range model.Array(model.Object(candidate["content"])["parts"]) {
			nested, err := decoder.part(model.Object(value))
			if err != nil {
				return events, nil, err
			}
			events = append(events, nested...)
		}
		if citations := candidateCitations(candidate); len(citations) > 0 && decoder.open != nil {
			decoder.stream.UpdatePart(decoder.open.itemIndex, decoder.open.partIndex, func(part *model.Part) {
				part.Citations = append(part.Citations, citations...)
			})
		}
		if reason := model.String(candidate["finishReason"]); reason != "" {
			finish, known := codec.ParseGeminiFinish(reason)
			decoder.finish = model.Finish{Kind: finish, Raw: reason}
			decoder.finished = true
			if !known {
				decoder.losses = append(decoder.losses, model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: reason, To: model.Gemini})
			}
		}
	}
	if feedback := model.Object(root["promptFeedback"]); model.String(feedback["blockReason"]) != "" {
		decoder.finish, decoder.finished = model.Finish{Kind: model.FinishContentFilter}, true
	}
	return events, nil, nil
}

func (decoder *streamDecoder) part(entry map[string]any) ([]model.Event, error) {
	if entry == nil {
		return nil, nil
	}
	if item, ok := serverPart(model.RoleAssistant, entry); ok {
		events, err := decoder.stopText()
		if err != nil {
			return nil, err
		}
		whole, err := decoder.emitWhole(item)
		return append(events, whole...), err
	}
	if media, err := codec.DecodeGeminiMedia(entry); err != nil {
		return nil, err
	} else if media != nil {
		events, err := decoder.stopText()
		if err != nil {
			return nil, err
		}
		item := model.Item{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartMedia, Media: media}}}
		whole, err := decoder.emitWhole(item)
		return append(events, whole...), err
	}
	if entry["functionCall"] != nil {
		events, err := decoder.stopText()
		if err != nil {
			return nil, err
		}
		call := model.Object(entry["functionCall"])
		name := model.String(call["name"])
		arguments := "{}"
		if call["args"] != nil {
			arguments = string(model.Marshal(call["args"]))
		}
		id := model.String(call["id"])
		if id == "" {
			id = decoder.ids.Assign(name, arguments)
		}
		tool := &model.ToolCall{Kind: model.ToolFunction, ID: id, Name: name, Arguments: arguments}
		if signature := model.String(entry["thoughtSignature"]); signature != "" {
			tool.Signature = &model.Signature{Vendor: model.VendorGoogle, Blob: signature}
		}
		whole, err := decoder.emitWhole(model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: tool})
		return append(events, whole...), err
	}
	text := model.String(entry["text"])
	thought := entry["thought"] == true
	signature := model.String(entry["thoughtSignature"])
	if entry["text"] != nil {
		partKind, itemKind := model.PartText, model.ItemMessage
		reasoning := thought || (signature != "" && text != "")
		if reasoning {
			partKind, itemKind = model.PartReasoningText, model.ItemReasoning
		}
		var events []model.Event
		if decoder.open == nil || decoder.open.kind != partKind {
			stopped, err := decoder.stopText()
			if err != nil {
				return nil, err
			}
			events = append(events, stopped...)
			item := model.Item{Kind: itemKind, Role: model.RoleAssistant}
			if itemKind == model.ItemReasoning {
				item.Reasoning = &model.Reasoning{Visibility: "full"}
			}
			itemIndex, started := decoder.stream.StartItem(item)
			partIndex, partStarted, err := decoder.stream.StartPart(itemIndex, model.Part{Kind: partKind})
			if err != nil {
				return events, err
			}
			decoder.open = &openText{itemIndex: itemIndex, partIndex: partIndex, kind: partKind, itemKind: itemKind}
			events = append(events, started...)
			events = append(events, partStarted...)
		}
		if signature != "" && reasoning {
			decoder.stream.UpdateItem(decoder.open.itemIndex, func(item *model.Item) {
				if item.Reasoning == nil {
					item.Reasoning = &model.Reasoning{Visibility: "full"}
				}
				item.Reasoning.Signature = &model.Signature{Vendor: model.VendorGoogle, Blob: signature, TextDigest: model.TextDigest(text)}
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
		item := model.Item{Kind: model.ItemReasoning, Role: model.RoleAssistant, Reasoning: &model.Reasoning{Visibility: "full", Signature: &model.Signature{Vendor: model.VendorGoogle, Blob: signature}}}
		whole, err := decoder.emitWhole(item)
		return append(events, whole...), err
	}
	events, err := decoder.stopText()
	if err != nil {
		return nil, err
	}
	whole, err := decoder.emitWhole(model.Item{Kind: model.ItemNative, Role: model.RoleAssistant, Raw: &model.Raw{Format: model.Gemini, Fields: map[string]json.RawMessage{"native": model.Marshal(entry)}}})
	return append(events, whole...), err
}

func (decoder *streamDecoder) stopText() ([]model.Event, error) {
	if decoder.open == nil {
		return nil, nil
	}
	open := decoder.open
	decoder.open = nil
	if open.itemKind == model.ItemReasoning {
		text := decoder.stream.Streamed(open.itemIndex, open.partIndex)
		decoder.stream.UpdateItem(open.itemIndex, func(item *model.Item) {
			if item.Reasoning != nil && item.Reasoning.Signature != nil {
				item.Reasoning.Signature.TextDigest = model.TextDigest(text)
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
	var events []model.Event
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
	if finish.Kind == model.FinishStop && decoder.stream.HasToolCall() {
		finish.Kind = model.FinishToolCalls
	}
	return append(events,
		model.Event{Kind: model.EventMessageDelta, Usage: decoder.usage, Finish: &finish},
		model.Event{Kind: model.EventMessageStop}), nil
}

func (decoder *streamDecoder) End() ([]model.Event, []model.Loss, error) {
	if decoder.done {
		return nil, decoder.takeLosses(), nil
	}
	if !decoder.finished {
		if !decoder.started {
			return nil, nil, codec.ErrTruncated
		}
		if decoder.open != nil {
			if decoder.stream.Streamed(decoder.open.itemIndex, decoder.open.partIndex) == "" {
				return nil, nil, codec.ErrTruncated
			}
		} else if !model.CleanEOF(decoder.stream) {
			return nil, nil, codec.ErrTruncated
		}
		decoder.finished = true
		decoder.losses = append(decoder.losses, model.Loss{Code: model.LossCleanEOF, Class: model.LossDegraded, Detail: "the provider closed the stream without a finishReason", From: model.Gemini})
	}
	events, err := decoder.finishEvents()
	return events, decoder.takeLosses(), err
}

func (decoder *streamDecoder) takeLosses() []model.Loss {
	losses := decoder.losses
	decoder.losses = nil
	return losses
}
