package responses

import (
	"errors"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

// decodedItem tracks the blocks one output item produced. A message item yields one block per content
// part; every other item yields exactly one.
type decodedItem struct {
	blocks map[int]int
}

type streamDecoder struct {
	blocks  *format.BlockStream
	done    bool
	items   map[string]*decodedItem
	usage   *protocol.Usage
	finish  protocol.FinishReason
	failure *protocol.Error
	losses  []protocol.Loss
}

func (Codec) NewStreamDecoder(peer format.Peer) format.StreamDecoder {
	return &streamDecoder{blocks: format.NewBlockStream(), items: map[string]*decodedItem{}, finish: protocol.FinishStop}
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
	case "response.created":
		response := protocol.Object(root["response"])
		return []protocol.Event{{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: protocol.String(response["id"]), Model: protocol.String(response["model"])}}}, nil
	case "response.output_item.added":
		return decoder.itemAdded(itemKey(root, protocol.Object(root["item"])), protocol.Object(root["item"]))
	case "response.content_part.added":
		return decoder.partAdded(root)
	case "response.output_text.delta", "response.refusal.delta", "response.function_call_arguments.delta", "response.custom_tool_call_input.delta", "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return decoder.delta(root)
	case "response.output_text.annotation.added":
		return nil, decoder.annotate(root)
	case "response.output_item.done":
		return decoder.itemDone(itemKey(root, protocol.Object(root["item"])), protocol.Object(root["item"]))
	case "response.completed", "response.incomplete", "response.failed":
		return decoder.finishResponse(protocol.Object(root["response"]))
	case "error":
		failure := protocol.Object(root["error"])
		if failure == nil {
			failure = root
		}
		return nil, &format.StreamError{Code: protocol.String(failure["code"]), Message: protocol.String(failure["message"])}
	}
	return nil, nil
}

// itemKey identifies an output item; some compatible providers send items without an id, in which
// case the output index is the only handle.
func itemKey(root map[string]any, item map[string]any) string {
	if id := protocol.String(item["id"]); id != "" {
		return id
	}
	if id := protocol.String(root["item_id"]); id != "" {
		return id
	}
	return "output:" + strconv.FormatInt(protocol.IntegerOrZero(root["output_index"]), 10)
}

func (decoder *streamDecoder) state(itemID string) *decodedItem {
	state := decoder.items[itemID]
	if state == nil {
		state = &decodedItem{blocks: map[int]int{}}
		decoder.items[itemID] = state
	}
	return state
}

// startItem opens the blocks of an item. Message items open lazily, when their content parts are
// announced, because the part type decides between text and refusal.
func (decoder *streamDecoder) itemAdded(itemID string, item map[string]any) ([]protocol.Event, error) {
	if protocol.String(item["type"]) == "message" {
		decoder.state(itemID)
		return nil, nil
	}
	block, err := format.DecodeBlock(protocol.Responses, item)
	if err != nil {
		return nil, err
	}
	index, events := decoder.blocks.Start(protocol.WithStreamedContent(block, ""))
	decoder.state(itemID).blocks[0] = index
	return events, nil
}

func (decoder *streamDecoder) partAdded(root map[string]any) ([]protocol.Event, error) {
	contentIndex := int(protocol.IntegerOrZero(root["content_index"]))
	state := decoder.state(itemKey(root, nil))
	if _, known := state.blocks[contentIndex]; known {
		return nil, nil
	}
	part := protocol.Object(root["part"])
	block := protocol.Block{Kind: protocol.KindText}
	if protocol.String(part["type"]) == "refusal" {
		block.Kind = protocol.KindRefusal
	}
	index, events := decoder.blocks.Start(block)
	state.blocks[contentIndex] = index
	return events, nil
}

func (decoder *streamDecoder) delta(root map[string]any) ([]protocol.Event, error) {
	text := protocol.String(root["delta"])
	if text == "" {
		return nil, nil
	}
	state := decoder.state(itemKey(root, nil))
	contentIndex := int(protocol.IntegerOrZero(root["content_index"]))
	index, known := state.blocks[contentIndex]
	var events []protocol.Event
	if !known {
		kind := protocol.KindText
		if strings.Contains(protocol.String(root["type"]), "refusal") {
			kind = protocol.KindRefusal
		}
		if protocol.String(root["type"]) == "response.reasoning_text.delta" || protocol.String(root["type"]) == "response.reasoning_summary_text.delta" {
			return nil, errors.New("reasoning delta for an item that was never added")
		}
		index, events = decoder.blocks.Start(protocol.Block{Kind: kind})
		state.blocks[contentIndex] = index
	}
	if protocol.String(root["type"]) == "response.reasoning_summary_text.delta" && protocol.IntegerOrZero(root["summary_index"]) > 0 && decoder.blocks.Streamed(index) != "" {
		text = "\n\n" + text
	}
	nested, err := decoder.blocks.Delta(index, text)
	return append(events, nested...), err
}

func (decoder *streamDecoder) annotate(root map[string]any) error {
	annotation := protocol.Object(root["annotation"])
	state := decoder.items[itemKey(root, nil)]
	if annotation == nil || state == nil {
		return nil
	}
	if index, known := state.blocks[int(protocol.IntegerOrZero(root["content_index"]))]; known {
		decoder.blocks.Update(index, func(block *protocol.Block) {
			block.Citations = append(block.Citations, format.DecodeCitation(protocol.Responses, annotation))
		})
	}
	return nil
}

func (decoder *streamDecoder) itemDone(itemID string, item map[string]any) ([]protocol.Event, error) {
	finals, err := decodeFinalBlocks(item)
	if err != nil {
		return nil, err
	}
	state := decoder.state(itemID)
	var events []protocol.Event
	for position, final := range finals {
		index, known := state.blocks[position]
		if !known {
			var started []protocol.Event
			index, started = decoder.blocks.Start(final)
			events = append(events, started...)
			state.blocks[position] = index
		}
		decoder.blocks.Update(index, func(block *protocol.Block) { mergeFinal(block, final) })
		events = append(events, decoder.blocks.Stop(index)...)
	}
	for _, index := range state.blocks {
		events = append(events, decoder.blocks.Stop(index)...)
	}
	return events, nil
}

func mergeFinal(block *protocol.Block, final protocol.Block) {
	arguments := ""
	if block.ToolCall != nil {
		arguments = block.ToolCall.Arguments
	}
	protocol.MergeBlock(block, final)
	if block.ToolCall != nil && final.ToolCall != nil && final.ToolCall.Kind != protocol.ToolServer && arguments != "" && final.ToolCall.Arguments == "" {
		block.ToolCall.Arguments = arguments
	}
}

// decodeFinalBlocks reads a complete output item: a message yields one block per content part.
func decodeFinalBlocks(item map[string]any) ([]protocol.Block, error) {
	if protocol.String(item["type"]) != "message" {
		block, err := format.DecodeBlock(protocol.Responses, item)
		return []protocol.Block{block}, err
	}
	blocks, err := format.DecodeContent(protocol.Responses, item["content"])
	if err != nil {
		return nil, err
	}
	return blocks, nil
}

func (decoder *streamDecoder) finishResponse(response map[string]any) ([]protocol.Event, error) {
	var events []protocol.Event
	for position, item := range protocol.Array(response["output"]) {
		entry := protocol.Object(item)
		key := itemKey(map[string]any{"output_index": float64(position)}, entry)
		if _, seen := decoder.items[key]; seen || entry == nil {
			continue
		}
		nested, err := decoder.itemDone(key, entry)
		if err != nil {
			return nil, err
		}
		events = append(events, nested...)
	}
	decoder.usage = format.ParseResponsesUsage(response)
	decoder.finish, decoder.failure = finishOf(response, nil)
	return append(events, decoder.finishEvents()...), nil
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
		protocol.Event{Kind: protocol.EventMessageDelta, Usage: decoder.usage, Finish: finish, Failure: decoder.failure, Losses: decoder.losses},
		protocol.Event{Kind: protocol.EventMessageStop})
}

// End synthesises a clean terminator when the provider closed the connection after delivering a
// complete response; an incomplete response is still reported as truncated.
func (decoder *streamDecoder) End() ([]protocol.Event, error) {
	if decoder.done {
		return nil, nil
	}
	if !format.CleanEOF(decoder.blocks) {
		return nil, format.ErrTruncated
	}
	decoder.losses = append(decoder.losses, protocol.Loss{
		Code: protocol.LossCleanEOF, Detail: "the provider closed the stream without a terminal response event", From: protocol.Responses,
	})
	return decoder.finishEvents(), nil
}
