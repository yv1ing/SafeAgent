package chat_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/chat"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

var (
	client   = format.Peer{Side: format.Client, Vendor: protocol.VendorOpenAI, Model: "m"}
	upstream = format.Peer{Side: format.Upstream, Vendor: protocol.VendorOpenAI, Model: "m"}
)

func decode(t *testing.T, payloads ...string) []protocol.Event {
	t.Helper()
	frames := make([]format.Frame, 0, len(payloads))
	for _, payload := range payloads {
		frames = append(frames, format.Frame{Data: []byte(payload)})
	}
	events := testkit.DecodeFrames(t, chat.New().NewStreamDecoder(upstream), frames)
	testkit.CheckEventContract(t, events)
	return events
}

func encode(t *testing.T, events []protocol.Event) []format.Frame {
	t.Helper()
	frames, _ := testkit.EncodeEvents(t, chat.New().NewStreamEncoder(protocol.ResponseMeta{ID: "c1", Model: "m"}, client), events)
	return frames
}

func TestRequestKeepsContentOrder(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"be brief"},{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]},{"role":"assistant","content":"ok","reasoning_content":"thinking","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"/tmp\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"done"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}]}`)
	request, err := chat.New().DecodeRequest(body, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	assistant := request.Messages[2]
	if assistant.Blocks[0].Kind != protocol.KindReasoning || assistant.Blocks[1].Kind != protocol.KindText || assistant.Blocks[2].Kind != protocol.KindToolCall {
		t.Fatalf("unexpected assistant blocks: %+v", assistant.Blocks)
	}
	if request.Messages[3].Blocks[0].ToolResult.CallID != "call_1" {
		t.Fatalf("unexpected tool result: %+v", request.Messages[3].Blocks[0])
	}
	out, _, losses, err := chat.New().EncodeRequest(request, upstream)
	if err != nil || len(losses) != 0 {
		t.Fatalf("%v %+v", err, losses)
	}
	testkit.AssertJSON(t, out, "messages.0.content", "be brief")
	testkit.AssertJSON(t, out, "messages.2.tool_calls.0.function.arguments", `{"path":"/tmp"}`)
	testkit.AssertJSON(t, out, "messages.3.role", "tool")
}

func TestPlainTextStaysAString(t *testing.T) {
	response := &protocol.Response{Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "hi"}}, Finish: protocol.FinishStop}
	out, losses, err := chat.New().EncodeResponse(response, client)
	if err != nil || len(losses) != 0 {
		t.Fatalf("%v %+v", err, losses)
	}
	testkit.AssertJSON(t, out, "choices.0.message.content", "hi")
}

func TestChunksCarryFinishReasonOnlyAtTheEnd(t *testing.T) {
	frames := encode(t, decode(t,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`))
	if !strings.Contains(string(frames[0].Data), `"role":"assistant"`) {
		t.Fatalf("the first chunk must carry the role: %s", frames[0].Data)
	}
	for _, frame := range frames[:len(frames)-2] {
		testkit.AssertJSON(t, frame.Data, "choices.0.finish_reason", nil)
	}
	testkit.AssertJSON(t, frames[len(frames)-2].Data, "choices.0.finish_reason", "stop")
	if string(frames[len(frames)-1].Data) != "[DONE]" {
		t.Fatalf("the stream must end with [DONE]")
	}
}

func TestStreamedToolArguments(t *testing.T) {
	events := decode(t,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"p\":"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"now","arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
	response := testkit.Aggregate(events)
	if len(response.Blocks) != 2 || response.Blocks[0].ToolCall.Arguments != `{"p":1}` || response.Finish != protocol.FinishToolCalls {
		t.Fatalf("unexpected response: %+v", response)
	}
	frames := encode(t, events)
	text := joinData(frames)
	if !strings.Contains(text, `"index":1`) || strings.Count(text, `"id":"call_`) != 2 {
		t.Fatalf("tool calls must keep their positions: %s", text)
	}
}

func TestToolCallWithoutArgumentsStreamsAnEmptyObject(t *testing.T) {
	events := []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "c1", Model: "m"}},
		{Kind: protocol.EventBlockStart, Index: 0, Block: &protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "now"}}},
		{Kind: protocol.EventBlockStop, Index: 0, Block: &protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "now"}}},
		{Kind: protocol.EventMessageDelta, Finish: protocol.FinishToolCalls},
		{Kind: protocol.EventMessageStop},
	}
	response := testkit.Aggregate(decode(t, splitData(encode(t, events))...))
	if len(response.Blocks) != 1 || response.Blocks[0].ToolCall.Arguments != "{}" {
		t.Fatalf("a call without parameters must carry {}: %+v", response.Blocks)
	}
}

func TestInterleavedReasoningAndTextKeepTheirOrder(t *testing.T) {
	response := testkit.Aggregate(decode(t,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"reasoning_content":"a"}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"b"}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"reasoning_content":"c"}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"d"}}]}`,
		`[DONE]`))
	var order string
	for _, block := range response.Blocks {
		order += string(block.Kind[:1]) + block.Text
	}
	if order != "ra"+"tb"+"rc"+"td" {
		t.Fatalf("unexpected order %q", order)
	}
}

func TestUnknownFinishReasonDegradesToStop(t *testing.T) {
	events := decode(t, `{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":"brand_new"}]}`)
	var delta protocol.Event
	for _, event := range events {
		if event.Kind == protocol.EventMessageDelta {
			delta = event
		}
	}
	if delta.Finish != protocol.FinishStop || len(delta.Losses) != 1 || delta.Losses[0].Code != protocol.LossFinishMapped {
		t.Fatalf("%+v", delta)
	}
}

func TestEmptyContentBlocksAreNotSent(t *testing.T) {
	request := &protocol.Request{Model: "m", Messages: []protocol.Message{{Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: ""}, {Kind: protocol.KindText, Text: "x"}}}}}
	out, _, _, err := chat.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, out, "messages.0.content", "x")
}

func joinData(frames []format.Frame) string {
	var text strings.Builder
	for _, frame := range frames {
		text.Write(frame.Data)
		text.WriteByte('\n')
	}
	return text.String()
}

func splitData(frames []format.Frame) []string {
	payloads := make([]string, len(frames))
	for index, frame := range frames {
		payloads[index] = string(frame.Data)
	}
	return payloads
}

func toolChunk(index int, id, name, arguments string) string {
	call := map[string]any{"index": index, "function": map[string]any{"arguments": arguments}}
	if id != "" {
		call["id"], call["type"] = id, "function"
		call["function"] = map[string]any{"name": name, "arguments": arguments}
	}
	body := protocol.Marshal(map[string]any{"id": "c1", "model": "m", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}})
	return string(body)
}

func TestInterleavedParallelToolCallsAreEmittedWholeInIndexOrder(t *testing.T) {
	events := decode(t,
		toolChunk(0, "call_a", "read", ""),
		toolChunk(1, "call_b", "now", ""),
		toolChunk(0, "", "", `{"p":`),
		toolChunk(1, "", "", `{"z"`),
		toolChunk(0, "", "", `1}`),
		toolChunk(1, "", "", `:2}`),
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
	response := testkit.Aggregate(events)
	if len(response.Blocks) != 2 || response.Finish != protocol.FinishToolCalls {
		t.Fatalf("%+v", response)
	}
	first, second := response.Blocks[0].ToolCall, response.Blocks[1].ToolCall
	if first.ID != "call_a" || first.Name != "read" || first.Arguments != `{"p":1}` || second.ID != "call_b" || second.Arguments != `{"z":2}` {
		t.Fatalf("%+v %+v", first, second)
	}
	deltas := 0
	for _, event := range events {
		if event.Kind == protocol.EventBlockDelta {
			deltas++
		}
	}
	if deltas != 2 {
		t.Fatalf("each call must send its arguments once, got %d deltas", deltas)
	}
}

func TestSequentialToolCallsAndLaterTextStayOrdered(t *testing.T) {
	response := testkit.Aggregate(decode(t,
		toolChunk(0, "call_a", "read", `{}`),
		toolChunk(1, "call_b", "now", `{"x":1}`),
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"after"}}]}`,
		toolChunk(2, "call_c", "later", `{}`),
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`))
	var order []string
	for _, block := range response.Blocks {
		if block.ToolCall != nil {
			order = append(order, block.ToolCall.ID)
		} else {
			order = append(order, block.Text)
		}
	}
	if strings.Join(order, ",") != "call_a,call_b,after,call_c" {
		t.Fatalf("%v", order)
	}
}

func TestToolCallWithoutArgumentsDecodesToEmptyObject(t *testing.T) {
	response := testkit.Aggregate(decode(t, toolChunk(0, "call_a", "now", ""), `{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`))
	if len(response.Blocks) != 1 || response.Blocks[0].ToolCall.Arguments != "{}" {
		t.Fatalf("%+v", response.Blocks)
	}
}

func TestCustomToolInputIsBufferedVerbatim(t *testing.T) {
	custom := func(input string, first bool) string {
		call := map[string]any{"index": 0, "custom": map[string]any{"input": input}}
		if first {
			call = map[string]any{"index": 0, "id": "call_p", "type": "custom", "custom": map[string]any{"name": "apply_patch", "input": input}}
		}
		return string(protocol.Marshal(map[string]any{"id": "c1", "model": "m", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}}))
	}
	response := testkit.Aggregate(decode(t, custom("*** Begin", true), custom(" Patch\n", false), `{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`))
	call := response.Blocks[0].ToolCall
	if call.Kind != protocol.ToolCustom || call.Name != "apply_patch" || call.Arguments != "*** Begin Patch\n" {
		t.Fatalf("%+v", call)
	}
}

func TestToolCallsFlushOnDoneWithoutFinishReason(t *testing.T) {
	response := testkit.Aggregate(decode(t, toolChunk(0, "call_a", "read", `{"a":1}`), `[DONE]`))
	if len(response.Blocks) != 1 || response.Blocks[0].ToolCall.Arguments != `{"a":1}` {
		t.Fatalf("%+v", response.Blocks)
	}
}

func TestStreamWithoutTerminalFrameSynthesisesACleanEnd(t *testing.T) {
	decoder := chat.Codec{}.NewStreamDecoder(format.Peer{Side: format.Upstream})
	if _, err := decoder.Feed(format.Frame{Data: []byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"complete"}}]}`)}); err != nil {
		t.Fatalf("feed: %v", err)
	}
	events, err := decoder.End()
	if err != nil {
		t.Fatalf("a complete response followed by EOF is a clean end, got %v", err)
	}
	if len(events) == 0 || events[len(events)-1].Kind != protocol.EventMessageStop {
		t.Fatalf("missing synthesised terminal events: %+v", events)
	}
}

func TestStreamWithIncompleteToolArgumentsIsTruncated(t *testing.T) {
	decoder := chat.Codec{}.NewStreamDecoder(format.Peer{Side: format.Upstream})
	frame := `{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read","arguments":"{\"a\":"}}]}}]}`
	if _, err := decoder.Feed(format.Frame{Data: []byte(frame)}); err != nil {
		t.Fatalf("feed: %v", err)
	}
	if _, err := decoder.End(); !errors.Is(err, format.ErrTruncated) {
		t.Fatalf("incomplete tool arguments must be truncated, got %v", err)
	}
}

func TestToolCallsWithoutIndexArePlacedByID(t *testing.T) {
	whole := func(id, arguments string) string {
		call := map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "read", "arguments": arguments}}
		return string(protocol.Marshal(map[string]any{"id": "c1", "model": "m", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}}))
	}
	response := testkit.Aggregate(decode(t, whole("call_a", `{"a":1}`), whole("call_b", `{"b":2}`), `[DONE]`))
	if len(response.Blocks) != 2 || response.Blocks[0].ToolCall.Arguments != `{"a":1}` || response.Blocks[1].ToolCall.ID != "call_b" {
		t.Fatalf("calls without index must not merge: %+v", response.Blocks)
	}
}
