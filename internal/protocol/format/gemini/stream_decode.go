package gemini

import (
	"errors"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type streamDecoder struct {
	blocks   *format.BlockStream
	ids      *format.CallIDs
	started  bool
	done     bool
	usage    *protocol.Usage
	finish   protocol.FinishReason
	finished bool
	losses   []protocol.Loss
}

func (Codec) NewStreamDecoder(peer format.Peer) format.StreamDecoder {
	return &streamDecoder{blocks: format.NewBlockStream(), ids: format.NewCallIDs(peer), finish: protocol.FinishStop}
}

func (decoder *streamDecoder) Feed(frame format.Frame) ([]protocol.Event, error) {
	if decoder.done {
		return nil, nil
	}
	root, err := protocol.DecodeObject(frame.Data)
	if err != nil {
		return nil, errors.New("stream chunk must be a JSON object")
	}
	if failure := protocol.Object(root["error"]); failure != nil {
		return nil, &format.StreamError{Code: protocol.String(failure["status"]), Message: protocol.String(failure["message"])}
	}
	var events []protocol.Event
	if !decoder.started {
		decoder.started = true
		events = append(events, protocol.Event{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: protocol.String(root["responseId"]), Model: protocol.String(root["modelVersion"])}})
	}
	if usage := format.ParseGeminiUsage(root); usage != nil {
		decoder.usage = usage
	}
	for _, item := range protocol.Array(root["candidates"]) {
		candidate := protocol.Object(item)
		if index, _ := protocol.Integer(candidate["index"]); index != 0 {
			return nil, errors.New("multiple candidates are not supported")
		}
		for _, part := range protocol.Array(protocol.Object(candidate["content"])["parts"]) {
			nested, err := decoder.partEvents(protocol.Object(part))
			if err != nil {
				return nil, err
			}
			events = append(events, nested...)
		}
		if citations := candidateCitations(candidate); len(citations) > 0 {
			if index, open := decoder.blocks.Open(); open {
				decoder.blocks.Update(index, func(block *protocol.Block) {
					block.Citations = append(block.Citations, citations...)
				})
			}
		}
		if reason := protocol.String(candidate["finishReason"]); reason != "" {
			finish, known := format.ParseGeminiFinish(reason)
			decoder.finish, decoder.finished = finish, true
			if !known {
				decoder.losses = append(decoder.losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Detail: reason, To: protocol.Gemini})
			}
		}
	}
	if feedback := protocol.Object(root["promptFeedback"]); protocol.String(feedback["blockReason"]) != "" {
		decoder.finish, decoder.finished = protocol.FinishContentFilter, true
	}
	return events, nil
}

func (decoder *streamDecoder) partEvents(part map[string]any) ([]protocol.Event, error) {
	block, err := format.DecodeBlock(protocol.Gemini, part)
	if err != nil {
		return nil, err
	}
	if block.Kind == protocol.KindText || block.Kind == protocol.KindReasoning {
		return decoder.textEvents(block)
	}
	if block.ToolCall != nil && block.ToolCall.ID == "" {
		block.ToolCall.ID = decoder.ids.Assign(block.ToolCall.Name, block.ToolCall.Arguments)
	}
	index, events := decoder.blocks.Start(block)
	return append(events, decoder.blocks.Stop(index)...), nil
}

// textEvents appends to the open block of the same kind: Gemini streams a thought or answer as a
// run of parts, and a signature arrives on the part that ends the run.
func (decoder *streamDecoder) textEvents(block protocol.Block) ([]protocol.Event, error) {
	var events []protocol.Event
	index, open := decoder.blocks.Open()
	if !open || decoder.blocks.Block(index).Kind != block.Kind {
		started := protocol.Block{Kind: block.Kind}
		if block.Reasoning != nil {
			started.Reasoning = &protocol.Reasoning{Visibility: "full"}
		}
		index, events = decoder.blocks.Start(started)
	}
	if block.Reasoning != nil && block.Reasoning.Signature != nil {
		signature := *block.Reasoning.Signature
		decoder.blocks.Update(index, func(stored *protocol.Block) {
			if stored.Reasoning == nil {
				stored.Reasoning = &protocol.Reasoning{Visibility: "full"}
			}
			stored.Reasoning.Signature = &signature
		})
	}
	nested, err := decoder.blocks.Delta(index, block.Text)
	return append(events, nested...), err
}

func (decoder *streamDecoder) finishEvents() []protocol.Event {
	if decoder.done {
		return nil
	}
	decoder.done = true
	events := decoder.blocks.StopAll()
	finish := decoder.finish
	if finish == protocol.FinishStop && decoder.blocks.HasToolCall() {
		finish = protocol.FinishToolCalls
	}
	return append(events,
		protocol.Event{Kind: protocol.EventMessageDelta, Usage: decoder.usage, Finish: finish, Losses: decoder.losses},
		protocol.Event{Kind: protocol.EventMessageStop})
}

// End synthesises a clean terminator when the stream stopped after a complete candidate.
func (decoder *streamDecoder) End() ([]protocol.Event, error) {
	if !decoder.finished {
		if !format.CleanEOF(decoder.blocks) {
			return nil, format.ErrTruncated
		}
		decoder.finished = true
		decoder.losses = append(decoder.losses, protocol.Loss{Code: protocol.LossCleanEOF, Detail: "the provider closed the stream without a finishReason", From: protocol.Gemini})
	}
	return decoder.finishEvents(), nil
}
