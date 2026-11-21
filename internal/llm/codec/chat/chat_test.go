package chat_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/chat"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

var client, upstream = testkit.Peers(model.Chat)

func decode(t *testing.T, payloads ...string) []model.Event {
	t.Helper()
	events := testkit.DecodeFrames(t, chat.New().NewStreamDecoder(upstream), testkit.Frames(payloads...))
	testkit.CheckEventContract(t, events)
	return events
}

func encode(t *testing.T, events []model.Event) []codec.Frame {
	t.Helper()
	frames, _ := testkit.EncodeEvents(t, chat.New().NewStreamEncoder(model.ResponseMeta{ID: "c1", Model: "m"}, client), events)
	return frames
}

func TestRequestKeepsContentOrder(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"be brief"},{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]},{"role":"assistant","content":"ok","reasoning_content":"thinking","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"/tmp\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"done"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}]}`)
	request, losses, err := chat.New().DecodeRequest(body, nil, client)
	if err != nil || len(losses) != 0 {
		t.Fatalf("%v %+v", err, losses)
	}
	if len(request.Items) != 6 {
		t.Fatalf("unexpected items: %+v", request.Items)
	}
	if request.Items[2].Kind != model.ItemReasoning || request.Items[3].Kind != model.ItemMessage || request.Items[4].Kind != model.ItemToolCall {
		t.Fatalf("unexpected assistant items: %+v", request.Items[2:5])
	}
	if request.Items[5].CallID != "call_1" {
		t.Fatalf("unexpected tool result: %+v", request.Items[5])
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
	response := &model.Response{Items: []model.Item{model.MessageItem(model.RoleAssistant, "hi")}, Finish: model.Finish{Kind: model.FinishStop}}
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

func TestInterleavedParallelToolCallsKeepTheirPositions(t *testing.T) {
	events := decode(t,
		toolChunk(0, "call_a", "read", ""),
		toolChunk(1, "call_b", "now", ""),
		toolChunk(0, "", "", `{"p":`),
		toolChunk(1, "", "", `{"z"`),
		toolChunk(0, "", "", `1}`),
		toolChunk(1, "", "", `:2}`),
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
	response := testkit.Aggregate(events)
	if len(response.Items) != 2 || response.Finish.Kind != model.FinishToolCalls {
		t.Fatalf("%+v", response)
	}
	first, second := response.Items[0].ToolCall, response.Items[1].ToolCall
	if first.ID != "call_a" || first.Name != "read" || first.Arguments != `{"p":1}` || second.ID != "call_b" || second.Arguments != `{"z":2}` {
		t.Fatalf("%+v %+v", first, second)
	}
	frames := encode(t, events)
	text := testkit.JoinPayloads(frames)
	if !strings.Contains(text, `"index":1`) || strings.Count(text, `"id":"call_`) != 2 {
		t.Fatalf("tool calls must keep their positions: %s", text)
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
	for _, item := range response.Items {
		if item.Kind == model.ItemReasoning {
			order += "r" + item.Text()
		} else {
			order += "t" + item.Text()
		}
	}
	if order != "ra"+"tb"+"rc"+"td" {
		t.Fatalf("unexpected order %q", order)
	}
}

func TestToolCallWithoutArgumentsStreamsAnEmptyObject(t *testing.T) {
	events := []model.Event{
		{Kind: model.EventMessageStart, Meta: &model.ResponseMeta{ID: "c1", Model: "m"}},
		{Kind: model.EventItemStart, ItemIndex: 0, Item: &model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "now"}}},
		{Kind: model.EventItemStop, ItemIndex: 0, Item: &model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "now"}}},
		{Kind: model.EventMessageDelta, Finish: &model.Finish{Kind: model.FinishToolCalls}},
		{Kind: model.EventMessageStop},
	}
	response := testkit.Aggregate(decode(t, testkit.Payloads(encode(t, events))...))
	if len(response.Items) != 1 || response.Items[0].ToolCall.Arguments != "{}" {
		t.Fatalf("a call without parameters must carry {}: %+v", response.Items)
	}
}

func TestCustomToolInputIsBufferedVerbatim(t *testing.T) {
	custom := func(input string, first bool) string {
		call := map[string]any{"index": 0, "custom": map[string]any{"input": input}}
		if first {
			call = map[string]any{"index": 0, "id": "call_p", "type": "custom", "custom": map[string]any{"name": "apply_patch", "input": input}}
		}
		return string(model.Marshal(map[string]any{"id": "c1", "model": "m", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}}))
	}
	response := testkit.Aggregate(decode(t, custom("*** Begin", true), custom(" Patch\n", false), `{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`))
	call := response.Items[0].ToolCall
	if call.Kind != model.ToolCustom || call.Name != "apply_patch" || call.Arguments != "*** Begin Patch\n" {
		t.Fatalf("%+v", call)
	}
}

func TestLegacyFunctionRoleBecomesAToolResult(t *testing.T) {
	request, _, err := chat.New().DecodeRequest([]byte(`{"model":"m","messages":[{"role":"assistant","function_call":{"name":"read","arguments":"{}"}},{"role":"function","name":"read","content":"done"}]}`), nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Items) != 2 || request.Items[1].Kind != model.ItemToolResult || request.Items[1].CallID != request.Items[0].ToolCall.ID {
		t.Fatalf("%+v", request.Items)
	}
}

func TestAssistantMessageRawFieldsStayOnTheMessage(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"assistant","vendor_flag":true,"tool_calls":[{"id":"call_1","type":"function","vendor_call_flag":1,"function":{"name":"read","arguments":"{}"}}]}]}`)
	request, _, err := chat.New().DecodeRequest(body, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, _, err := chat.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, encoded, "messages.0.vendor_flag", true)
	testkit.AssertJSON(t, encoded, "messages.0.tool_calls.0.vendor_call_flag", 1)
}

func TestNestedRawFieldsSurviveTheSameFormat(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}","vendor_flag":true}}]}]}`)
	request, _, err := chat.New().DecodeRequest(body, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, _, err := chat.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, encoded, "messages.0.tool_calls.0.function.vendor_flag", true)
}

func TestLegacyFunctionCallBecomesAToolCall(t *testing.T) {
	request, _, err := chat.New().DecodeRequest([]byte(`{"model":"m","messages":[{"role":"assistant","function_call":{"name":"read","arguments":"{\"p\":1}"}}]}`), nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Items) != 1 || request.Items[0].ToolCall == nil || request.Items[0].ToolCall.Name != "read" {
		t.Fatalf("%+v", request.Items)
	}
}

func TestStreamWithoutTerminalFrameSynthesisesACleanEnd(t *testing.T) {
	decoder := chat.Codec{}.NewStreamDecoder(codec.Peer{Side: codec.Upstream})
	if _, _, err := decoder.Feed(codec.Frame{Data: []byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"complete"}}]}`)}); err != nil {
		t.Fatalf("feed: %v", err)
	}
	events, _, err := decoder.End()
	if err != nil {
		t.Fatalf("a complete response followed by EOF is a clean end, got %v", err)
	}
	if len(events) == 0 || events[len(events)-1].Kind != model.EventMessageStop {
		t.Fatalf("missing synthesised terminal events: %+v", events)
	}
}

func TestStreamWithIncompleteToolArgumentsIsTruncated(t *testing.T) {
	decoder := chat.Codec{}.NewStreamDecoder(codec.Peer{Side: codec.Upstream})
	frame := `{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read","arguments":"{\"a\":"}}]}}]}`
	if _, _, err := decoder.Feed(codec.Frame{Data: []byte(frame)}); err != nil {
		t.Fatalf("feed: %v", err)
	}
	if _, _, err := decoder.End(); !errors.Is(err, codec.ErrTruncated) {
		t.Fatalf("incomplete tool arguments must be truncated, got %v", err)
	}
}

func TestTerminalFrameDoesNotMaskIncompleteToolArguments(t *testing.T) {
	decoder := chat.Codec{}.NewStreamDecoder(codec.Peer{Side: codec.Upstream})
	frames := []string{
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read","arguments":"{\"a\":"}}]}}]}`,
		`[DONE]`,
	}
	for _, frame := range frames[:2] {
		if _, _, err := decoder.Feed(codec.Frame{Data: []byte(frame)}); err != nil {
			t.Fatalf("feed: %v", err)
		}
	}
	events, _, err := decoder.Feed(codec.Frame{Data: []byte(frames[2])})
	if !errors.Is(err, codec.ErrTruncated) {
		t.Fatalf("a terminal frame must not hide incomplete arguments, got %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("no terminal events may precede a truncation: %+v", events)
	}
}

func TestStreamWithTextAndIncompleteToolArgumentsIsTruncated(t *testing.T) {
	decoder := chat.Codec{}.NewStreamDecoder(codec.Peer{Side: codec.Upstream})
	frames := []string{
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read","arguments":"{\"a\":"}}]}}]}`,
	}
	for _, frame := range frames {
		if _, _, err := decoder.Feed(codec.Frame{Data: []byte(frame)}); err != nil {
			t.Fatalf("feed: %v", err)
		}
	}
	if _, _, err := decoder.End(); !errors.Is(err, codec.ErrTruncated) {
		t.Fatalf("text followed by incomplete tool arguments must be truncated, got %v", err)
	}
}

func TestUnknownFinishReasonDegradesToStop(t *testing.T) {
	decoder := chat.Codec{}.NewStreamDecoder(codec.Peer{Side: codec.Upstream})
	if _, _, err := decoder.Feed(codec.Frame{Data: []byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":"brand_new"}]}`)}); err != nil {
		t.Fatal(err)
	}
	_, losses, err := decoder.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 1 || losses[0].Code != model.LossFinishMapped {
		t.Fatalf("%+v", losses)
	}
}

func TestToolCallsWithoutIndexArePlacedByID(t *testing.T) {
	whole := func(id, arguments string) string {
		call := map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "read", "arguments": arguments}}
		return string(model.Marshal(map[string]any{"id": "c1", "model": "m", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}}))
	}
	response := testkit.Aggregate(decode(t, whole("call_a", `{"a":1}`), whole("call_b", `{"b":2}`), `[DONE]`))
	if len(response.Items) != 2 || response.Items[0].ToolCall.Arguments != `{"a":1}` || response.Items[1].ToolCall.ID != "call_b" {
		t.Fatalf("calls without index must not merge: %+v", response.Items)
	}
}

func TestRequestRecordsMultipleChoices(t *testing.T) {
	request, _, err := chat.New().DecodeRequest([]byte(`{"model":"m","n":2,"messages":[{"role":"user","content":"hi"}]}`), nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if request.ChoiceCount != 2 {
		t.Fatalf("n must be recorded for the pass-through decision: %+v", request.ChoiceCount)
	}
}

func toolChunk(index int, id, name, arguments string) string {
	call := map[string]any{"index": index, "function": map[string]any{"arguments": arguments}}
	if id != "" {
		call["id"], call["type"] = id, "function"
		call["function"] = map[string]any{"name": name, "arguments": arguments}
	}
	body := model.Marshal(map[string]any{"id": "c1", "model": "m", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}})
	return string(body)
}

func TestDoneFrameIsATerminalFrameEvenWithoutFinishReason(t *testing.T) {
	decoder := chat.New().NewStreamDecoder(upstream)
	for _, payload := range []string{
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		`[DONE]`,
	} {
		if _, _, err := decoder.Feed(codec.Frame{Data: []byte(payload)}); err != nil {
			t.Fatal(err)
		}
	}
	_, losses, err := decoder.End()
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range losses {
		if loss.Code == model.LossCleanEOF {
			t.Fatalf("a DONE frame is terminal: %+v", losses)
		}
	}
}
