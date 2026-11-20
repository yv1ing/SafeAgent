package messages_test

import (
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/messages"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

var (
	envelope = testkit.SealingEnvelope{Secret: []byte("test-secret")}
	client   = format.Peer{Side: format.Client, Vendor: protocol.VendorAnthropic, Model: "m", Envelope: envelope}
	upstream = format.Peer{Side: format.Upstream, Vendor: protocol.VendorAnthropic, Model: "m"}
)

func frames(payloads ...string) []format.Frame {
	result := make([]format.Frame, len(payloads))
	for index, payload := range payloads {
		result[index] = format.Frame{Data: []byte(payload)}
	}
	return result
}

func decode(t *testing.T, peer format.Peer, input []format.Frame) []protocol.Event {
	t.Helper()
	events := testkit.DecodeFrames(t, messages.New().NewStreamDecoder(peer), input)
	testkit.CheckEventContract(t, events)
	return events
}

func encode(t *testing.T, peer format.Peer, events []protocol.Event) []format.Frame {
	t.Helper()
	output, _ := testkit.EncodeEvents(t, messages.New().NewStreamEncoder(protocol.ResponseMeta{ID: "msg_1", Model: "m"}, peer), events)
	return output
}

func wireIndexes(t *testing.T, input []format.Frame, kind string) []int {
	t.Helper()
	var indexes []int
	for _, frame := range input {
		if testkit.JSONValue(t, frame.Data, "type") != kind {
			continue
		}
		index, ok := protocol.Integer(testkit.JSONValue(t, frame.Data, "index"))
		if !ok {
			t.Fatalf("frame without an index: %s", frame.Data)
		}
		indexes = append(indexes, int(index))
	}
	return indexes
}

func toolItem(id string, arguments string) protocol.Item {
	return protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: id, Name: "read", Arguments: arguments}}
}

func toolEvents(arguments string) []protocol.Event {
	started := toolItem("call_1", "")
	stopped := toolItem("call_1", arguments)
	return []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "m1", Model: "claude"}},
		{Kind: protocol.EventItemStart, ItemIndex: 0, Item: &started},
		{Kind: protocol.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartToolArguments}},
		{Kind: protocol.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: arguments},
		{Kind: protocol.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartToolArguments, Text: arguments}},
		{Kind: protocol.EventItemStop, ItemIndex: 0, Item: &stopped},
		{Kind: protocol.EventMessageDelta, Finish: &protocol.Finish{Kind: protocol.FinishToolCalls}},
		{Kind: protocol.EventMessageStop},
	}
}

func TestDuplicateContentBlockStopIsDroppedAtTheDecoder(t *testing.T) {
	events := decode(t, upstream, frames(
		`{"type":"message_start","message":{"id":"m1","model":"claude","usage":{"input_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"read","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"p\":1}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`,
	))
	stops := 0
	for _, event := range events {
		if event.Kind == protocol.EventItemStop {
			stops++
		}
	}
	if stops != 1 {
		t.Fatalf("an item must be stopped exactly once, got %d", stops)
	}
}

func TestWireIndexesAreUniqueAndSequential(t *testing.T) {
	events := []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "m", Model: "m"}},
		{Kind: protocol.EventItemStart, ItemIndex: 0, Item: &protocol.Item{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, Reasoning: &protocol.Reasoning{Visibility: "full"}}},
		{Kind: protocol.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartReasoningText}},
		{Kind: protocol.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "plan"},
		{Kind: protocol.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartReasoningText, Text: "plan"}},
		{Kind: protocol.EventItemStop, ItemIndex: 0},
		{Kind: protocol.EventItemStart, ItemIndex: 1, Item: &protocol.Item{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant}},
		{Kind: protocol.EventPartStart, ItemIndex: 1, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartText}},
		{Kind: protocol.EventPartDelta, ItemIndex: 1, PartIndex: 0, Delta: "answer"},
		{Kind: protocol.EventPartStop, ItemIndex: 1, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartText, Text: "answer"}},
		{Kind: protocol.EventItemStop, ItemIndex: 1},
		{Kind: protocol.EventItemStart, ItemIndex: 2, Item: &protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read", Arguments: `{}`}}},
		{Kind: protocol.EventItemStop, ItemIndex: 2},
		{Kind: protocol.EventMessageDelta, Finish: &protocol.Finish{Kind: protocol.FinishToolCalls}},
		{Kind: protocol.EventMessageStop},
	}
	output := encode(t, upstream, events)
	starts, stops := wireIndexes(t, output, "content_block_start"), wireIndexes(t, output, "content_block_stop")
	for position, want := range []int{0, 1, 2} {
		if starts[position] != want || stops[position] != want {
			t.Fatalf("indexes must be 0..2 in order: starts %v stops %v", starts, stops)
		}
	}
	if len(starts) != 3 || len(stops) != 3 {
		t.Fatalf("every block must start and stop once: %v %v", starts, stops)
	}
}

func TestHeldItemFramesKeepTheSequenceValid(t *testing.T) {
	encoder := messages.New().NewStreamEncoder(protocol.ResponseMeta{ID: "m", Model: "m"}, upstream)
	var immediate, held, trailing []format.Frame
	for _, event := range toolEvents(`{"p":1}`) {
		produced, _, err := encoder.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case event.Kind == protocol.EventMessageDelta || event.Kind == protocol.EventMessageStop:
			trailing = append(trailing, produced...)
		case event.ItemIndex == 0:
			held = append(held, produced...)
		default:
			immediate = append(immediate, produced...)
		}
	}
	released := append(append(immediate, held...), trailing...)
	decoded := testkit.Aggregate(decode(t, upstream, released))
	if len(decoded.Items) != 1 || decoded.Items[0].ToolCall == nil || decoded.Items[0].ToolCall.Arguments != `{"p":1}` {
		t.Fatalf("a held item must still form a valid sequence: %+v", decoded.Items)
	}
}

func TestSignatureTravelsSealedToClients(t *testing.T) {
	events := decode(t, upstream, frames(
		`{"type":"message_start","message":{"id":"m1","model":"claude"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"consider"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque-blob"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`{"type":"message_stop"}`,
	))
	output := encode(t, client, events)
	var sealed string
	for _, frame := range output {
		if signature, ok := testkit.JSONValue(t, frame.Data, "delta.signature").(string); ok {
			sealed = signature
		}
	}
	opened, ok := protocol.OpenSignature(envelope.Secret, sealed)
	if !ok || opened.Blob != "opaque-blob" || opened.TextDigest != protocol.TextDigest("consider") {
		t.Fatalf("the client must see a sealed signature bound to its text: %+v %v", opened, ok)
	}
	if strings.Contains(joinData(output), "opaque-blob") {
		t.Fatal("the raw provider signature leaked to the client")
	}
}

func TestStopReasonFollowsTheStopSequence(t *testing.T) {
	events := decode(t, upstream, frames(
		`{"type":"message_start","message":{"id":"m1","model":"claude"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"stop_sequence","stop_sequence":"###"}}`,
		`{"type":"message_stop"}`,
	))
	output := encode(t, client, events)
	text := joinData(output)
	if !strings.Contains(text, `"stop_reason":"stop_sequence"`) || !strings.Contains(text, `"stop_sequence":"###"`) {
		t.Fatalf("%s", text)
	}
}

func TestRequestRequiredToolChoiceBecomesAny(t *testing.T) {
	request := &protocol.Request{
		Model:      "m",
		Items:      []protocol.Item{protocol.MessageItem(protocol.RoleUser, "hi")},
		Tools:      []protocol.Tool{{Kind: protocol.ToolFunction, Name: "read", Parameters: []byte(`{"type":"object"}`)}},
		ToolChoice: &protocol.ToolChoice{Mode: "required"},
	}
	body, _, _, err := messages.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, body, "tool_choice.type", "any")
}

func TestThinkingBudgetRaisesTheTokenLimit(t *testing.T) {
	budget, limit := int64(8000), int64(1000)
	request := &protocol.Request{Model: "m", MaxOutputTokens: &limit, Reasoning: &protocol.ReasoningConfig{BudgetTokens: &budget}, Items: []protocol.Item{protocol.MessageItem(protocol.RoleUser, "hi")}}
	body, _, losses, err := messages.New().EncodeRequest(request, upstream)
	if err != nil || len(losses) == 0 {
		t.Fatalf("%v %+v", err, losses)
	}
	testkit.AssertJSON(t, body, "thinking.budget_tokens", 8000)
	testkit.AssertJSON(t, body, "max_tokens", 9024)
}

func TestSystemMessagesInsideTheArrayStayAtTheHead(t *testing.T) {
	body := []byte(`{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"hi"},{"role":"system","content":[{"type":"text","text":"environment"}]},{"role":"assistant","content":"ok"}]}`)
	request, _, err := messages.New().DecodeRequest(body, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Items) != 3 || request.Items[1].Role != protocol.RoleSystem || request.Items[1].Text() != "environment" {
		t.Fatalf("the system message must keep its position: %+v", request.Items)
	}
}

func TestServerToolArgumentsSurvivePolicyAndAudit(t *testing.T) {
	body := []byte(`{"model":"m","max_tokens":8,"messages":[{"role":"assistant","content":[{"type":"server_tool_use","id":"srvt_1","name":"web_search","input":{"query":"secret"}}]}]}`)
	request, _, err := messages.New().DecodeRequest(body, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if request.Items[0].Kind != protocol.ItemServerToolCall || request.Items[0].ToolCall.Arguments != `{"query":"secret"}` {
		t.Fatalf("%+v", request.Items[0])
	}
}

func TestToolResultMediaStaysStructured(t *testing.T) {
	body := []byte(`{"model":"m","max_tokens":8,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"c","content":[{"type":"text","text":"look"},{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}}]}]}]}`)
	request, _, err := messages.New().DecodeRequest(body, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	item := request.Items[0]
	if item.Kind != protocol.ItemToolResult || len(item.Parts) != 2 || item.Parts[1].Media == nil {
		t.Fatalf("%+v", item)
	}
	out, _, _, err := messages.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, out, "messages.0.content.0.content.1.type", "image")
}

func joinData(input []format.Frame) string {
	var text strings.Builder
	for _, frame := range input {
		text.Write(frame.Data)
		text.WriteByte('\n')
	}
	return text.String()
}
