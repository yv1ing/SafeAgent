package chat

import (
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

type streamEncoder struct {
	meta     protocol.ResponseMeta
	blocks   map[int]*protocol.Block
	position map[int]int
	streamed map[int]bool
	tools    int
	usage    *protocol.Usage
	finish   protocol.FinishReason
	stop     string
	failure  *protocol.Error
	stopped  bool
}

func (Codec) NewStreamEncoder(meta protocol.ResponseMeta, peer format.Peer) format.StreamEncoder {
	return &streamEncoder{meta: meta, blocks: map[int]*protocol.Block{}, position: map[int]int{}, streamed: map[int]bool{}}
}

func (encoder *streamEncoder) Encode(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	switch event.Kind {
	case protocol.EventMessageStart:
		if event.Meta != nil {
			encoder.meta = *event.Meta
		}
		return encoder.delta(map[string]any{"role": "assistant", "content": ""}), nil, nil
	case protocol.EventBlockStart:
		if event.Block == nil {
			return nil, nil, nil
		}
		block := protocol.CloneBlock(*event.Block)
		encoder.blocks[event.Index] = &block
		return encoder.startBlock(event.Index, &block)
	case protocol.EventBlockDelta:
		block := encoder.blocks[event.Index]
		if block == nil || event.Text == "" {
			return nil, nil, nil
		}
		encoder.streamed[event.Index] = true
		return encoder.blockDelta(event.Index, block, event.Text), nil, nil
	case protocol.EventBlockStop:
		return encoder.stopBlock(event)
	case protocol.EventMessageDelta:
		encoder.usage, encoder.finish, encoder.stop, encoder.failure = event.Usage, event.Finish, event.Stop, event.Failure
		var losses []protocol.Loss
		if format.FinishLoss(event.Finish, protocol.Chat) {
			losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Detail: string(event.Finish), To: protocol.Chat})
		}
		return nil, losses, nil
	case protocol.EventMessageStop:
		return encoder.terminal(), nil, nil
	}
	return nil, nil, nil
}

func (encoder *streamEncoder) Finish() ([]format.Frame, []protocol.Loss, error) {
	return encoder.terminal(), nil, nil
}

func (encoder *streamEncoder) startBlock(index int, block *protocol.Block) ([]format.Frame, []protocol.Loss, error) {
	switch block.Kind {
	case protocol.KindToolCall:
		call := block.ToolCall
		if call == nil || call.Kind == protocol.ToolServer {
			return nil, []protocol.Loss{{Path: "tool_call", Code: protocol.LossFieldDropped, Detail: "provider-executed tool call cannot be represented by Chat", To: protocol.Chat}}, nil
		}
		encoder.position[index] = encoder.tools
		encoder.tools++
		entry := map[string]any{"index": encoder.position[index], "id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": ""}}
		if call.Kind == protocol.ToolCustom {
			entry["type"], entry["custom"] = "custom", map[string]any{"name": call.Name, "input": ""}
			delete(entry, "function")
		}
		return encoder.delta(map[string]any{"tool_calls": []any{entry}}), nil, nil
	case protocol.KindText, protocol.KindReasoning, protocol.KindRefusal:
		return nil, nil, nil
	}
	return nil, []protocol.Loss{{Path: "block", Code: protocol.LossFieldDropped, Detail: "Chat streams only carry text, reasoning, refusals and tool calls", To: protocol.Chat}}, nil
}

func (encoder *streamEncoder) blockDelta(index int, block *protocol.Block, text string) []format.Frame {
	switch block.Kind {
	case protocol.KindToolCall:
		position, known := encoder.position[index]
		if !known {
			return nil
		}
		field := map[string]any{"arguments": text}
		key := "function"
		if block.ToolCall.Kind == protocol.ToolCustom {
			field, key = map[string]any{"input": text}, "custom"
		}
		return encoder.delta(map[string]any{"tool_calls": []any{map[string]any{"index": position, key: field}}})
	case protocol.KindReasoning:
		return encoder.delta(map[string]any{"reasoning_content": text})
	case protocol.KindRefusal:
		return encoder.delta(map[string]any{"refusal": text})
	case protocol.KindText:
		return encoder.delta(map[string]any{"content": text})
	}
	return nil
}

func (encoder *streamEncoder) stopBlock(event protocol.Event) ([]format.Frame, []protocol.Loss, error) {
	block := encoder.blocks[event.Index]
	if block == nil {
		return nil, nil, nil
	}
	if event.Block != nil {
		final := protocol.CloneBlock(*event.Block)
		block = &final
	}
	var losses []protocol.Loss
	if block.Kind == protocol.KindReasoning && block.Reasoning != nil && block.Reasoning.Signature != nil {
		losses = append(losses, protocol.Loss{Path: "signature", Code: protocol.LossSignatureStripped, Detail: "Chat has no field for reasoning signatures", To: protocol.Chat})
	}
	if _, isTool := encoder.position[event.Index]; !isTool || encoder.streamed[event.Index] {
		return nil, losses, nil
	}
	encoder.streamed[event.Index] = true
	arguments := format.EmptyObjectIfBlank(block.ToolCall.Arguments)
	if block.ToolCall.Kind == protocol.ToolCustom {
		arguments = block.ToolCall.Arguments
	}
	if arguments == "" {
		return nil, losses, nil
	}
	return encoder.blockDelta(event.Index, block, arguments), losses, nil
}

func (encoder *streamEncoder) terminal() []format.Frame {
	if encoder.stopped {
		return nil
	}
	encoder.stopped = true
	if encoder.failure != nil {
		_, body := Codec{}.EncodeError(encoder.failure, true)
		return []format.Frame{{Data: body}, {Data: []byte("[DONE]")}}
	}
	finish := format.ChatFinish(encoder.finish)
	if finish == "" {
		finish = "stop"
	}
	root := encoder.chunk(map[string]any{}, finish)
	if encoder.usage != nil {
		root["usage"] = format.EncodeChatUsage(encoder.usage)
	}
	return []format.Frame{{Data: protocol.Marshal(root)}, {Data: []byte("[DONE]")}}
}

func (encoder *streamEncoder) delta(delta map[string]any) []format.Frame {
	return []format.Frame{{Data: protocol.Marshal(encoder.chunk(delta, ""))}}
}

func (encoder *streamEncoder) chunk(delta map[string]any, finish string) map[string]any {
	var reason any
	if finish != "" {
		reason = finish
	}
	return map[string]any{
		"id":      encoder.meta.ID,
		"object":  "chat.completion.chunk",
		"model":   encoder.meta.Model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": reason}},
	}
}
