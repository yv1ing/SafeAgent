package chat

import (
	"errors"
	"sort"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

// pendingCall buffers one streamed tool call. Providers such as vLLM interleave the deltas of
// parallel calls, so a call is only emitted, whole, once nothing can extend it any more.
type pendingCall struct {
	block     protocol.Block
	arguments strings.Builder
}

type streamDecoder struct {
	blocks    *format.BlockStream
	started   bool
	done      bool
	pending   map[int]*pendingCall
	emitted   map[int]bool
	positions map[string]int
	last      int
	finish    protocol.FinishReason
	finished  bool
	usage     *protocol.Usage
	losses    []protocol.Loss
}

func (Codec) NewStreamDecoder(peer format.Peer) format.StreamDecoder {
	return &streamDecoder{blocks: format.NewBlockStream(), pending: map[int]*pendingCall{}, emitted: map[int]bool{}, positions: map[string]int{}, finish: protocol.FinishStop}
}

func (decoder *streamDecoder) Feed(frame format.Frame) ([]protocol.Event, error) {
	if decoder.done {
		return nil, nil
	}
	payload := strings.TrimSpace(string(frame.Data))
	if payload == "" {
		return nil, nil
	}
	if payload == "[DONE]" {
		decoder.finished = true
		return decoder.finishEvents(), nil
	}
	root, err := protocol.DecodeObject(frame.Data)
	if err != nil {
		return nil, errors.New("stream chunk must be a JSON object")
	}
	if failure := protocol.Object(root["error"]); failure != nil {
		return nil, &format.StreamError{Code: protocol.String(failure["code"]), Message: protocol.String(failure["message"])}
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
	for _, item := range protocol.Array(root["choices"]) {
		choice := protocol.Object(item)
		if index, _ := protocol.Integer(choice["index"]); index != 0 {
			return nil, errors.New("multiple choices are not supported")
		}
		nested, err := decoder.deltaEvents(protocol.Object(choice["delta"]))
		if err != nil {
			return nil, err
		}
		events = append(events, nested...)
		if finish := protocol.String(choice["finish_reason"]); finish != "" {
			reason, known := format.ParseChatFinish(finish)
			if !known {
				decoder.losses = append(decoder.losses, format.FinishMappedLoss(finish, protocol.Chat))
			}
			decoder.finish, decoder.finished = reason, true
			events = append(events, decoder.flushTools()...)
		}
	}
	return events, nil
}

func (decoder *streamDecoder) deltaEvents(delta map[string]any) ([]protocol.Event, error) {
	var events []protocol.Event
	if annotations := protocol.Array(delta["annotations"]); len(annotations) > 0 {
		if index, open := decoder.blocks.Open(); open {
			decoder.blocks.Update(index, func(block *protocol.Block) {
				for _, item := range annotations {
					if object := protocol.Object(item); object != nil {
						block.Citations = append(block.Citations, format.DecodeCitation(protocol.Chat, object))
					}
				}
			})
		}
	}
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if text := protocol.String(delta[key]); text != "" {
			nested, err := decoder.textDelta(protocol.KindReasoning, text)
			if err != nil {
				return nil, err
			}
			events = append(events, nested...)
			break
		}
	}
	if text := protocol.String(delta["refusal"]); text != "" {
		nested, err := decoder.textDelta(protocol.KindRefusal, text)
		if err != nil {
			return nil, err
		}
		events = append(events, nested...)
	}
	switch content := delta["content"].(type) {
	case string:
		if content != "" {
			nested, err := decoder.textDelta(protocol.KindText, content)
			if err != nil {
				return nil, err
			}
			events = append(events, nested...)
		}
	case []any:
		for _, part := range content {
			block, err := format.DecodeBlock(protocol.Chat, protocol.Object(part))
			if err != nil {
				return nil, err
			}
			events = append(events, decoder.flushTools()...)
			index, started := decoder.blocks.Start(block)
			events = append(events, started...)
			events = append(events, decoder.blocks.Stop(index)...)
		}
	}
	for _, item := range protocol.Array(delta["tool_calls"]) {
		if err := decoder.toolDelta(protocol.Object(item)); err != nil {
			return nil, err
		}
	}
	return events, nil
}

// textDelta continues the open block of the same kind; a different kind opens a new block, so
// alternating reasoning and text stay in their original order.
func (decoder *streamDecoder) textDelta(kind protocol.BlockKind, text string) ([]protocol.Event, error) {
	events := decoder.flushTools()
	index, open := decoder.blocks.Open()
	if !open || decoder.blocks.Block(index).Kind != kind {
		block := protocol.Block{Kind: kind}
		if kind == protocol.KindReasoning {
			block.Reasoning = &protocol.Reasoning{Visibility: "full"}
		}
		var started []protocol.Event
		index, started = decoder.blocks.Start(block)
		events = append(events, started...)
	}
	nested, err := decoder.blocks.Delta(index, text)
	return append(events, nested...), err
}

// position places a tool call delta. Some providers omit index and send each call whole with its
// id, so an id seen before keeps its place and a new id takes the next one.
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
		pending = &pendingCall{block: protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: kind}}}
		decoder.pending[position] = pending
	}
	stored := pending.block.ToolCall
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
	pending.arguments.WriteString(protocol.String(function["arguments"]))
	pending.arguments.WriteString(protocol.String(custom["input"]))
	return nil
}

// flushTools emits the buffered calls in index order, each with its complete arguments.
func (decoder *streamDecoder) flushTools() []protocol.Event {
	if len(decoder.pending) == 0 {
		return nil
	}
	positions := make([]int, 0, len(decoder.pending))
	for position := range decoder.pending {
		positions = append(positions, position)
	}
	sort.Ints(positions)
	var events []protocol.Event
	for _, position := range positions {
		pending := decoder.pending[position]
		block := pending.block
		block.ToolCall.Arguments = ""
		index, started := decoder.blocks.Start(block)
		events = append(events, started...)
		arguments := pending.arguments.String()
		if block.ToolCall.Kind == protocol.ToolFunction {
			arguments = format.EmptyObjectIfBlank(arguments)
		}
		delta, _ := decoder.blocks.Delta(index, arguments)
		events = append(events, delta...)
		events = append(events, decoder.blocks.Stop(index)...)
		decoder.emitted[position] = true
	}
	decoder.pending = map[int]*pendingCall{}
	return events
}

func (decoder *streamDecoder) finishEvents() []protocol.Event {
	if decoder.done {
		return nil
	}
	decoder.done = true
	events := decoder.flushTools()
	events = append(events, decoder.blocks.StopAll()...)
	finish := decoder.finish
	if finish == protocol.FinishStop && decoder.blocks.HasToolCall() {
		finish = protocol.FinishToolCalls
	}
	return append(events,
		protocol.Event{Kind: protocol.EventMessageDelta, Usage: decoder.usage, Finish: finish, Losses: decoder.losses},
		protocol.Event{Kind: protocol.EventMessageStop})
}

// End synthesises a clean terminator when the provider closed the connection after delivering a
// complete response; an incomplete response is still reported as truncated.
func (decoder *streamDecoder) End() ([]protocol.Event, error) {
	if !decoder.finished {
		flushed := decoder.flushTools()
		if !format.CleanEOF(decoder.blocks) {
			return nil, format.ErrTruncated
		}
		decoder.finished = true
		decoder.losses = append(decoder.losses, protocol.Loss{Code: protocol.LossCleanEOF, Detail: "the provider closed the stream without a terminal frame", From: protocol.Chat})
		return append(flushed, decoder.finishEvents()...), nil
	}
	return decoder.finishEvents(), nil
}
