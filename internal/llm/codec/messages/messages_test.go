package messages_test

import (
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/messages"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

var envelope = testkit.SealingEnvelope{Secret: []byte("test-secret")}

var client, upstream = testkit.PeersWithEnvelope(model.Messages, envelope)

var frames = testkit.Frames

func decode(t *testing.T, peer codec.Peer, input []codec.Frame) []model.Event {
	t.Helper()
	events := testkit.DecodeFrames(t, messages.New().NewStreamDecoder(peer), input)
	testkit.CheckEventContract(t, events)
	return events
}

func encode(t *testing.T, peer codec.Peer, events []model.Event) []codec.Frame {
	t.Helper()
	output, _ := testkit.EncodeEvents(t, messages.New().NewStreamEncoder(model.ResponseMeta{ID: "msg_1", Model: "m"}, peer), events)
	return output
}

func wireIndexes(t *testing.T, input []codec.Frame, kind string) []int {
	t.Helper()
	var indexes []int
	for _, frame := range input {
		if testkit.JSONValue(t, frame.Data, "type") != kind {
			continue
		}
		index, ok := model.Integer(testkit.JSONValue(t, frame.Data, "index"))
		if !ok {
			t.Fatalf("frame without an index: %s", frame.Data)
		}
		indexes = append(indexes, int(index))
	}
	return indexes
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
		if event.Kind == model.EventItemStop {
			stops++
		}
	}
	if stops != 1 {
		t.Fatalf("an item must be stopped exactly once, got %d", stops)
	}
}

func TestWireIndexesAreUniqueAndSequential(t *testing.T) {
	events := []model.Event{
		{Kind: model.EventMessageStart, Meta: &model.ResponseMeta{ID: "m", Model: "m"}},
		{Kind: model.EventItemStart, ItemIndex: 0, Item: &model.Item{Kind: model.ItemReasoning, Role: model.RoleAssistant, Reasoning: &model.Reasoning{Visibility: "full"}}},
		{Kind: model.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartReasoningText}},
		{Kind: model.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "plan"},
		{Kind: model.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartReasoningText, Text: "plan"}},
		{Kind: model.EventItemStop, ItemIndex: 0},
		{Kind: model.EventItemStart, ItemIndex: 1, Item: &model.Item{Kind: model.ItemMessage, Role: model.RoleAssistant}},
		{Kind: model.EventPartStart, ItemIndex: 1, PartIndex: 0, Part: &model.Part{Kind: model.PartText}},
		{Kind: model.EventPartDelta, ItemIndex: 1, PartIndex: 0, Delta: "answer"},
		{Kind: model.EventPartStop, ItemIndex: 1, PartIndex: 0, Part: &model.Part{Kind: model.PartText, Text: "answer"}},
		{Kind: model.EventItemStop, ItemIndex: 1},
		{Kind: model.EventItemStart, ItemIndex: 2, Item: &model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "read", Arguments: `{}`}}},
		{Kind: model.EventItemStop, ItemIndex: 2},
		{Kind: model.EventMessageDelta, Finish: &model.Finish{Kind: model.FinishToolCalls}},
		{Kind: model.EventMessageStop},
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
	encoder := messages.New().NewStreamEncoder(model.ResponseMeta{ID: "m", Model: "m"}, upstream)
	var immediate, held, trailing []codec.Frame
	for _, event := range testkit.ToolCallEvents(model.ResponseMeta{ID: "m1", Model: "claude"}, `{"p":1}`) {
		produced, _, err := encoder.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case event.Kind == model.EventMessageDelta || event.Kind == model.EventMessageStop:
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
	opened, ok := model.OpenSignature(envelope.Secret, sealed)
	if !ok || opened.Blob != "opaque-blob" || opened.TextDigest != model.TextDigest("consider") {
		t.Fatalf("the client must see a sealed signature bound to its text: %+v %v", opened, ok)
	}
	if strings.Contains(testkit.JoinPayloads(output), "opaque-blob") {
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
	text := testkit.JoinPayloads(output)
	if !strings.Contains(text, `"stop_reason":"stop_sequence"`) || !strings.Contains(text, `"stop_sequence":"###"`) {
		t.Fatalf("%s", text)
	}
}

func TestRequestRequiredToolChoiceBecomesAny(t *testing.T) {
	request := &model.Request{
		Model:      "m",
		Items:      []model.Item{model.MessageItem(model.RoleUser, "hi")},
		Tools:      []model.Tool{{Kind: model.ToolFunction, Name: "read", Parameters: []byte(`{"type":"object"}`)}},
		ToolChoice: &model.ToolChoice{Mode: "required"},
	}
	body, _, _, err := messages.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, body, "tool_choice.type", "any")
}

func TestThinkingBudgetRaisesTheTokenLimit(t *testing.T) {
	budget, limit := int64(8000), int64(1000)
	request := &model.Request{Model: "m", MaxOutputTokens: &limit, Reasoning: &model.ReasoningConfig{BudgetTokens: &budget}, Items: []model.Item{model.MessageItem(model.RoleUser, "hi")}}
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
	if len(request.Items) != 3 || request.Items[1].Role != model.RoleSystem || request.Items[1].Text() != "environment" {
		t.Fatalf("the system message must keep its position: %+v", request.Items)
	}
}

func TestServerToolArgumentsSurvivePolicyAndAudit(t *testing.T) {
	body := []byte(`{"model":"m","max_tokens":8,"messages":[{"role":"assistant","content":[{"type":"server_tool_use","id":"srvt_1","name":"web_search","input":{"query":"secret"}}]}]}`)
	request, _, err := messages.New().DecodeRequest(body, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if request.Items[0].Kind != model.ItemServerToolCall || request.Items[0].ToolCall.Arguments != `{"query":"secret"}` {
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
	if item.Kind != model.ItemToolResult || len(item.Parts) != 2 || item.Parts[1].Media == nil {
		t.Fatalf("%+v", item)
	}
	out, _, _, err := messages.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, out, "messages.0.content.0.content.1.type", "image")
}

func signatureEvents(signature *model.Signature) []model.Event {
	reasoning := &model.Reasoning{Visibility: "full", Signature: signature}
	return []model.Event{
		{Kind: model.EventMessageStart, Meta: &model.ResponseMeta{ID: "msg_1", Model: "m"}},
		{Kind: model.EventItemStart, ItemIndex: 0, Item: &model.Item{Kind: model.ItemReasoning, Role: model.RoleAssistant, Reasoning: reasoning}},
		{Kind: model.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartReasoningText}},
		{Kind: model.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "kept thought"},
		{Kind: model.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartReasoningText, Text: "kept thought"}},
		{Kind: model.EventItemStop, ItemIndex: 0, Item: &model.Item{Kind: model.ItemReasoning, Role: model.RoleAssistant, Reasoning: reasoning}},
		{Kind: model.EventMessageDelta, Finish: &model.Finish{Kind: model.FinishStop}},
		{Kind: model.EventMessageStop},
	}
}

func signatureOf(t *testing.T, events []model.Event) (string, []model.Loss) {
	t.Helper()
	output, losses := testkit.EncodeEvents(t, messages.New().NewStreamEncoder(model.ResponseMeta{ID: "msg_1", Model: "m"}, upstream), events)
	sealed := ""
	for _, frame := range output {
		if signature, ok := testkit.JSONValue(t, frame.Data, "delta.signature").(string); ok {
			sealed = signature
		}
	}
	return sealed, losses
}

func TestSignatureForDifferentTextIsReportedAsDropped(t *testing.T) {
	signature := &model.Signature{Vendor: model.VendorAnthropic, Blob: "opaque", TextDigest: model.TextDigest("another thought")}
	sealed, losses := signatureOf(t, signatureEvents(signature))
	if sealed != "" {
		t.Fatalf("a signature that does not cover the part must not be sent: %q", sealed)
	}
	found := false
	for _, loss := range losses {
		found = found || loss.Code == model.LossSignatureStripped
	}
	if !found {
		t.Fatalf("a dropped reasoning signature must be a visible loss: %+v", losses)
	}

	signature.TextDigest = model.TextDigest("kept thought")
	sealed, losses = signatureOf(t, signatureEvents(signature))
	if sealed != "opaque" {
		t.Fatalf("a signature covering the part must be sent: %q %+v", sealed, losses)
	}
}
