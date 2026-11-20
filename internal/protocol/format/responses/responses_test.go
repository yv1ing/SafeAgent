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
	call := protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read"}}
	done := protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read", Arguments: `{"p":1}`}}
	return []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "resp_1", Model: "m"}},
		{Kind: protocol.EventBlockStart, Index: 0, Block: &protocol.Block{Kind: protocol.KindText}},
		{Kind: protocol.EventBlockDelta, Index: 0, Text: "hello"},
		{Kind: protocol.EventBlockStop, Index: 0, Block: &protocol.Block{Kind: protocol.KindText, Text: "hello"}},
		{Kind: protocol.EventBlockStart, Index: 1, Block: &call},
		{Kind: protocol.EventBlockDelta, Index: 1, Text: `{"p":1}`},
		{Kind: protocol.EventBlockStop, Index: 1, Block: &done},
		{Kind: protocol.EventMessageDelta, Finish: protocol.FinishToolCalls},
		{Kind: protocol.EventMessageStop},
	}
}

func TestDuplicateOutputItemDoneIsDroppedAtTheDecoder(t *testing.T) {
	events := decode(t,
		`{"type":"response.created","response":{"id":"r1","model":"m"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"p\":1}"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read","arguments":"{\"p\":1}"}}`,
		`{"type":"response.completed","response":{"id":"r1","status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read","arguments":"{\"p\":1}"}]}}`,
	)
	stops := 0
	for _, event := range events {
		if event.Kind == protocol.EventBlockStop {
			stops++
		}
	}
	response := testkit.Aggregate(events)
	if stops != 1 || len(response.Blocks) != 1 || response.Blocks[0].ToolCall.Arguments != `{"p":1}` || response.Finish != protocol.FinishToolCalls {
		t.Fatalf("stops=%d %+v", stops, response)
	}
}

func TestEveryItemGetsItsOwnIDAndOutputIndex(t *testing.T) {
	output := encode(t, sample())
	ids, indexes := map[string]bool{}, map[int]bool{}
	var sequence int = -1
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
		if !ok || int(number) != sequence+1 {
			t.Fatalf("sequence numbers must count up from 0 without gaps: %d after %d", number, sequence)
		}
		sequence = int(number)
	}
	if len(ids) != 2 {
		t.Fatalf("expected two items, got %v", ids)
	}
}

func TestHeldBlockFramesKeepTheSequenceValid(t *testing.T) {
	encoder := responses.New().NewStreamEncoder(protocol.ResponseMeta{ID: "resp_1", Model: "m"}, client)
	var immediate, held []format.Frame
	for _, event := range sample() {
		produced, _, err := encoder.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		if event.Index == 1 && event.Kind != protocol.EventMessageDelta && event.Kind != protocol.EventMessageStop {
			held = append(held, produced...)
		} else {
			immediate = append(immediate, produced...)
		}
	}
	released := append(append([]format.Frame{}, immediate[:len(immediate)-1]...), held...)
	released = append(released, immediate[len(immediate)-1])
	response := testkit.Aggregate(decode(t, splitData(released)...))
	if len(response.Blocks) != 2 || response.Blocks[1].ToolCall.Arguments != `{"p":1}` || response.Finish != protocol.FinishToolCalls {
		t.Fatalf("%+v", response)
	}
}

func TestToolCallWithoutArgumentsEndsAsEmptyObject(t *testing.T) {
	call := protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "now"}}
	output := encode(t, []protocol.Event{
		{Kind: protocol.EventMessageStart},
		{Kind: protocol.EventBlockStart, Index: 0, Block: &call},
		{Kind: protocol.EventBlockStop, Index: 0, Block: &call},
		{Kind: protocol.EventMessageDelta, Finish: protocol.FinishToolCalls},
		{Kind: protocol.EventMessageStop},
	})
	if !strings.Contains(string(output[len(output)-1].Data), `\"arguments\":\"{}\"`) && !strings.Contains(string(output[len(output)-1].Data), `"arguments":"{}"`) {
		t.Fatalf("%s", output[len(output)-1].Data)
	}
}

func TestFailureEndsWithAFailedResponse(t *testing.T) {
	events := decode(t,
		`{"type":"response.created","response":{"id":"r1"}}`,
		`{"type":"response.failed","response":{"id":"r1","status":"failed","error":{"code":"server_error","message":"boom"}}}`)
	response := testkit.Aggregate(events)
	if response.Finish != protocol.FinishError || response.Failure == nil || response.Failure.Code != "server_error" {
		t.Fatalf("%+v", response)
	}
	output := encode(t, events)
	if protocol.String(testkit.JSONValue(t, output[len(output)-1].Data, "type")) != "response.failed" {
		t.Fatalf("%s", output[len(output)-1].Data)
	}
}

func TestIncompleteReasonFollowsTheFinish(t *testing.T) {
	for finish, reason := range map[protocol.FinishReason]string{protocol.FinishLength: "max_output_tokens", protocol.FinishContentFilter: "content_filter"} {
		output := encode(t, []protocol.Event{{Kind: protocol.EventMessageStart}, {Kind: protocol.EventMessageDelta, Finish: finish}, {Kind: protocol.EventMessageStop}})
		last := output[len(output)-1].Data
		testkit.AssertJSON(t, last, "type", "response.incomplete")
		testkit.AssertJSON(t, last, "response.incomplete_details.reason", reason)
	}
}

func TestReasoningSummaryIsDecoded(t *testing.T) {
	response := testkit.Aggregate(decode(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_1","type":"reasoning","summary":[]}}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"thinking"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_1","type":"reasoning","summary":[{"type":"summary_text","text":"thinking"}],"encrypted_content":"enc"}}`,
		`{"type":"response.completed","response":{"id":"r1","status":"completed"}}`))
	block := response.Blocks[0]
	if block.Kind != protocol.KindReasoning || block.Text != "thinking" || block.Reasoning.ItemID != "rs_1" || block.Reasoning.Signature == nil || block.Reasoning.Signature.Blob != "enc" {
		t.Fatalf("%+v", block)
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
	if response.Finish != protocol.FinishStop || len(response.Blocks) != 1 || response.Blocks[0].Text != "hello" {
		t.Fatalf("%+v", response)
	}
	losses := false
	for _, event := range events {
		for _, loss := range event.Losses {
			losses = losses || loss.Code == protocol.LossCleanEOF
		}
	}
	if !losses {
		t.Fatal("a synthesized end must report a clean EOF loss")
	}
}

func TestIncompleteFunctionCallIsTruncatedAtEOF(t *testing.T) {
	decoder := responses.New().NewStreamDecoder(upstream)
	for _, payload := range []string{
		`{"type":"response.created","response":{"id":"r1","model":"m"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"p\":"}`,
	} {
		if _, err := decoder.Feed(format.Frame{Data: []byte(payload)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := decoder.End(); err != format.ErrTruncated {
		t.Fatalf("expected truncation, got %v", err)
	}
}

func TestInstructionsStayAtTheHeadOfTheRequest(t *testing.T) {
	request := &protocol.Request{Model: "m", Instructions: "first", Messages: []protocol.Message{
		{Role: protocol.RoleSystem, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "second"}}},
		{Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "hi"}}},
	}}
	body, _, _, err := responses.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, body, "instructions", "first\n\nsecond")
	testkit.AssertJSON(t, body, "input.0.role", "user")
}

func splitData(frames []format.Frame) []string {
	payloads := make([]string, len(frames))
	for index, frame := range frames {
		payloads[index] = string(frame.Data)
	}
	return payloads
}
