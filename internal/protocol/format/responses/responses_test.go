package responses_test

import (
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/responses"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

var (
	client   = format.Peer{Side: format.Client, Vendor: protocol.VendorOpenAI, Model: "m"}
	upstream = format.Peer{Side: format.Upstream, Vendor: protocol.VendorOpenAI, Model: "m"}
)

func decode(t *testing.T, payloads ...string) []protocol.Event {
	t.Helper()
	input := make([]format.Frame, len(payloads))
	for index, payload := range payloads {
		input[index] = format.Frame{Data: []byte(payload)}
	}
	events := testkit.DecodeFrames(t, responses.New().NewStreamDecoder(upstream), input)
	testkit.CheckEventContract(t, events)
	return events
}

func encode(t *testing.T, events []protocol.Event) []format.Frame {
	t.Helper()
	output, _ := testkit.EncodeEvents(t, responses.New().NewStreamEncoder(protocol.ResponseMeta{ID: "resp_1", Model: "m"}, client), events)
	return output
}

func sample() []protocol.Event {
	call := protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read"}}
	done := protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read", Arguments: `{"p":1}`}}
	return []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "resp_1", Model: "m"}},
		{Kind: protocol.EventItemStart, ItemIndex: 0, Item: &protocol.Item{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant}},
		{Kind: protocol.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartText}},
		{Kind: protocol.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "hello"},
		{Kind: protocol.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartText, Text: "hello"}},
		{Kind: protocol.EventItemStop, ItemIndex: 0, Item: &protocol.Item{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant}},
		{Kind: protocol.EventItemStart, ItemIndex: 1, Item: &call},
		{Kind: protocol.EventPartStart, ItemIndex: 1, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartToolArguments}},
		{Kind: protocol.EventPartDelta, ItemIndex: 1, PartIndex: 0, Delta: `{"p":1}`},
		{Kind: protocol.EventPartStop, ItemIndex: 1, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartToolArguments, Text: `{"p":1}`}},
		{Kind: protocol.EventItemStop, ItemIndex: 1, Item: &done},
		{Kind: protocol.EventMessageDelta, Finish: &protocol.Finish{Kind: protocol.FinishToolCalls}},
		{Kind: protocol.EventMessageStop},
	}
}

func TestDuplicateOutputItemDoneIsDroppedAtTheDecoder(t *testing.T) {
	events := decode(t,
		`{"type":"response.created","response":{"id":"r1","model":"m"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"p\":1}"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read","arguments":"{\"p\":1}"}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read","arguments":"{}"}}`,
		`{"type":"response.completed","response":{"id":"r1","status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read","arguments":"{\"p\":1}"}]}}`,
	)
	response := testkit.Aggregate(events)
	if len(response.Items) != 1 || response.Items[0].ToolCall.Arguments != `{"p":1}` || response.Finish.Kind != protocol.FinishToolCalls {
		t.Fatalf("%+v", response)
	}
}

func TestEveryItemGetsItsOwnIDAndOutputIndex(t *testing.T) {
	output := encode(t, sample())
	ids, indexes := map[string]bool{}, map[int]bool{}
	var sequence int64 = -1
	for _, frame := range output {
		if protocol.String(testkit.JSONValue(t, frame.Data, "type")) == "response.output_item.added" {
			id := protocol.String(testkit.JSONValue(t, frame.Data, "item.id"))
			index, _ := protocol.Integer(testkit.JSONValue(t, frame.Data, "output_index"))
			if id == "" || ids[id] || indexes[int(index)] {
				t.Fatalf("items need unique ids and indexes: %s", frame.Data)
			}
			ids[id], indexes[int(index)] = true, true
		}
		number, ok := protocol.Integer(testkit.JSONValue(t, frame.Data, "sequence_number"))
		if !ok || number != sequence+1 {
			t.Fatalf("sequence numbers must count up without gaps: %d after %d", number, sequence)
		}
		sequence = number
	}
	if len(ids) != 2 {
		t.Fatalf("expected two items, got %v", ids)
	}
}

func TestUnrepresentableStreamItemIsNotAnnounced(t *testing.T) {
	encoder := responses.New().NewStreamEncoder(protocol.ResponseMeta{ID: "resp_1", Model: "m"}, client)
	item := protocol.Item{Kind: protocol.ItemServerToolCall, Role: protocol.RoleAssistant}
	var output []format.Frame
	var losses []protocol.Loss
	for _, event := range []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "resp_1", Model: "m"}},
		{Kind: protocol.EventItemStart, ItemIndex: 0, Item: &item},
		{Kind: protocol.EventItemStop, ItemIndex: 0, Item: &item},
		{Kind: protocol.EventMessageStop},
	} {
		frames, nested, err := encoder.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		output = append(output, frames...)
		losses = append(losses, nested...)
	}
	for _, frame := range output {
		switch protocol.String(testkit.JSONValue(t, frame.Data, "type")) {
		case "response.output_item.added", "response.output_item.done":
			t.Fatalf("an unrepresentable item must not be announced: %s", frame.Data)
		}
	}
	if len(losses) == 0 {
		t.Fatal("dropping the item must be recorded")
	}
}

func TestReasoningSummaryAndTextUseSeparateIndexes(t *testing.T) {
	item := protocol.Item{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant}
	output := encode(t, []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "resp_1", Model: "m"}},
		{Kind: protocol.EventItemStart, ItemIndex: 0, Item: &item},
		{Kind: protocol.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartReasoningSummary}},
		{Kind: protocol.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "summary"},
		{Kind: protocol.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartReasoningSummary, Text: "summary"}},
		{Kind: protocol.EventPartStart, ItemIndex: 0, PartIndex: 1, Part: &protocol.Part{Kind: protocol.PartReasoningText}},
		{Kind: protocol.EventPartDelta, ItemIndex: 0, PartIndex: 1, Delta: "detail"},
		{Kind: protocol.EventPartStop, ItemIndex: 0, PartIndex: 1, Part: &protocol.Part{Kind: protocol.PartReasoningText, Text: "detail"}},
		{Kind: protocol.EventItemStop, ItemIndex: 0, Item: &item},
		{Kind: protocol.EventMessageDelta, Finish: &protocol.Finish{Kind: protocol.FinishStop}},
		{Kind: protocol.EventMessageStop},
	})
	for _, frame := range output {
		kind := protocol.String(testkit.JSONValue(t, frame.Data, "type"))
		switch kind {
		case "response.reasoning_summary_part.added":
			testkit.AssertJSON(t, frame.Data, "summary_index", 0)
		case "response.content_part.added":
			testkit.AssertJSON(t, frame.Data, "content_index", 0)
		case "response.output_item.done":
			testkit.AssertJSON(t, frame.Data, "item.summary.0.text", "summary")
			testkit.AssertJSON(t, frame.Data, "item.content.0.type", "reasoning_text")
			testkit.AssertJSON(t, frame.Data, "item.content.0.text", "detail")
		}
	}
}

func TestHeldItemFramesKeepTheSequenceValid(t *testing.T) {
	encoder := responses.New().NewStreamEncoder(protocol.ResponseMeta{ID: "resp_1", Model: "m"}, client)
	var immediate, held []format.Frame
	for _, event := range sample() {
		produced, _, err := encoder.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		if event.ItemIndex == 1 && event.Kind != protocol.EventMessageDelta && event.Kind != protocol.EventMessageStop {
			held = append(held, produced...)
		} else {
			immediate = append(immediate, produced...)
		}
	}
	released := append(append([]format.Frame{}, immediate[:len(immediate)-1]...), held...)
	released = append(released, immediate[len(immediate)-1])
	response := testkit.Aggregate(decode(t, splitData(released)...))
	if len(response.Items) != 2 || response.Items[1].ToolCall.Arguments != `{"p":1}` || response.Finish.Kind != protocol.FinishToolCalls {
		t.Fatalf("%+v", response)
	}
}

func TestToolCallWithoutArgumentsEndsAsEmptyObject(t *testing.T) {
	call := protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "now"}}
	output := encode(t, []protocol.Event{
		{Kind: protocol.EventMessageStart},
		{Kind: protocol.EventItemStart, ItemIndex: 0, Item: &call},
		{Kind: protocol.EventItemStop, ItemIndex: 0, Item: &call},
		{Kind: protocol.EventMessageDelta, Finish: &protocol.Finish{Kind: protocol.FinishToolCalls}},
		{Kind: protocol.EventMessageStop},
	})
	text := string(output[len(output)-1].Data)
	if !strings.Contains(text, `"arguments":"{}"`) {
		t.Fatalf("%s", text)
	}
}

func TestFailureEndsWithAFailedResponse(t *testing.T) {
	events := decode(t,
		`{"type":"response.created","response":{"id":"r1"}}`,
		`{"type":"response.failed","response":{"id":"r1","status":"failed","error":{"code":"server_error","message":"boom"}}}`)
	response := testkit.Aggregate(events)
	if response.Finish.Kind != protocol.FinishError || response.Failure == nil || response.Failure.Code != "server_error" {
		t.Fatalf("%+v", response)
	}
	output := encode(t, events)
	if protocol.String(testkit.JSONValue(t, output[len(output)-1].Data, "type")) != "response.failed" {
		t.Fatalf("%s", output[len(output)-1].Data)
	}
}

func TestIncompleteReasonFollowsTheFinish(t *testing.T) {
	for finish, reason := range map[protocol.FinishReason]string{protocol.FinishLength: "max_output_tokens", protocol.FinishContentFilter: "content_filter"} {
		output := encode(t, []protocol.Event{{Kind: protocol.EventMessageStart}, {Kind: protocol.EventMessageDelta, Finish: &protocol.Finish{Kind: finish}}, {Kind: protocol.EventMessageStop}})
		last := output[len(output)-1].Data
		testkit.AssertJSON(t, last, "type", "response.incomplete")
		testkit.AssertJSON(t, last, "response.incomplete_details.reason", reason)
	}
}

func TestReasoningSummaryIsDecodedWithItsSignature(t *testing.T) {
	response := testkit.Aggregate(decode(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_1","type":"reasoning","summary":[]}}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"thinking"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_1","type":"reasoning","summary":[{"type":"summary_text","text":"thinking"}],"encrypted_content":"enc"}}`,
		`{"type":"response.completed","response":{"id":"r1","status":"completed"}}`))
	item := response.Items[0]
	if item.Kind != protocol.ItemReasoning || item.Text() != "thinking" || item.ID != "rs_1" || item.Reasoning.Signature == nil || item.Reasoning.Signature.Blob != "enc" {
		t.Fatalf("%+v", item)
	}
}

func TestCleanEOFSynthesisesTheTerminalEvent(t *testing.T) {
	events := decode(t,
		`{"type":"response.created","response":{"id":"r1","model":"m"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant"}}`,
		`{"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"hello"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","content":[{"type":"output_text","text":"hello"}]}}`,
	)
	response := testkit.Aggregate(events)
	if response.Finish.Kind != protocol.FinishStop || len(response.Items) != 1 || response.Items[0].Text() != "hello" {
		t.Fatalf("%+v", response)
	}
}

func TestIncompleteFunctionCallIsTruncatedAtEOF(t *testing.T) {
	decoder := responses.New().NewStreamDecoder(upstream)
	for _, payload := range []string{
		`{"type":"response.created","response":{"id":"r1","model":"m"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"p\":"}`,
	} {
		if _, _, err := decoder.Feed(format.Frame{Data: []byte(payload)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := decoder.End(); err != format.ErrTruncated {
		t.Fatalf("expected truncation, got %v", err)
	}
}

func TestInstructionsAndInputItemsAreSent(t *testing.T) {
	request := &protocol.Request{Model: "m", Instructions: "first", Items: []protocol.Item{
		protocol.MessageItem(protocol.RoleSystem, "second"),
		protocol.MessageItem(protocol.RoleUser, "hi"),
	}}
	body, _, _, err := responses.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, body, "instructions", "first")
	testkit.AssertJSON(t, body, "input.0.role", "system")
	testkit.AssertJSON(t, body, "input.1.role", "user")
}

func splitData(frames []format.Frame) []string {
	payloads := make([]string, len(frames))
	for index, frame := range frames {
		payloads[index] = string(frame.Data)
	}
	return payloads
}

func TestStreamKeepsReasoningSeparateAndDoesNotDuplicateDoneText(t *testing.T) {
	events := decode(t,
		`{"type":"response.created","response":{"id":"resp_1","model":"m","created_at":1}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_1","type":"reasoning","content":[],"summary":[],"encrypted_content":"enc"}}`,
		`{"type":"response.content_part.added","item_id":"rs_1","output_index":0,"content_index":0,"part":{"type":"reasoning_text","text":""}}`,
		`{"type":"response.reasoning_text.delta","item_id":"rs_1","output_index":0,"content_index":0,"delta":"think"}`,
		`{"type":"response.reasoning_text.done","item_id":"rs_1","output_index":0,"content_index":0,"text":"think"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_1","type":"reasoning","content":[{"type":"reasoning_text","text":"think"}],"summary":[],"encrypted_content":"enc"}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.content_part.added","item_id":"msg_1","output_index":1,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":1,"content_index":0,"delta":"PONG"}`,
		`{"type":"response.output_text.done","item_id":"msg_1","output_index":1,"content_index":0,"text":"PONG"}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"PONG","annotations":[]}]}}`,
		`{"type":"response.completed","response":{"id":"resp_1","model":"m","status":"completed","output":[]}}`,
	)
	response := testkit.Aggregate(events)
	if len(response.Items) != 2 {
		t.Fatalf("unexpected items: %+v", response.Items)
	}
	if response.Items[0].Kind != protocol.ItemReasoning || response.Items[0].Text() != "think" {
		t.Fatalf("reasoning was not preserved: %+v", response.Items[0])
	}
	if response.Items[1].Kind != protocol.ItemMessage || response.Items[1].Text() != "PONG" {
		t.Fatalf("visible text was duplicated or reasoning leaked: %+v", response.Items[1])
	}
}

func TestRequestAcceptsMessageItemsWithoutType(t *testing.T) {
	request, _, err := responses.New().DecodeRequest([]byte(`{"model":"m","input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`), nil, client)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(request.Items) != 1 || request.Items[0].Kind != protocol.ItemMessage || request.Items[0].Text() != "hi" {
		t.Fatalf("unexpected items: %+v", request.Items)
	}
}
