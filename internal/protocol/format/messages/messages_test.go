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

// wireIndexes lists the content block indexes of the frames of one event type, in order.
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

func toolBlocks(id string) (protocol.Block, protocol.Block) {
	started := protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: id, Name: "read"}}
	stopped := protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: id, Name: "read", Arguments: `{"p":1}`}}
	return started, stopped
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
		if event.Kind == protocol.EventBlockStop {
			stops++
		}
	}
	if stops != 1 {
		t.Fatalf("a block must be stopped exactly once, got %d", stops)
	}
}

func TestWireIndexesAreUniqueAndSequential(t *testing.T) {
	started, stopped := toolBlocks("call_1")
	events := []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "m", Model: "m"}},
		{Kind: protocol.EventBlockStart, Index: 0, Block: &protocol.Block{Kind: protocol.KindReasoning, Reasoning: &protocol.Reasoning{Visibility: "full"}}},
		{Kind: protocol.EventBlockDelta, Index: 0, Text: "plan"},
		{Kind: protocol.EventBlockStop, Index: 0, Block: &protocol.Block{Kind: protocol.KindReasoning, Text: "plan"}},
		{Kind: protocol.EventBlockStart, Index: 1, Block: &protocol.Block{Kind: protocol.KindText}},
		{Kind: protocol.EventBlockDelta, Index: 1, Text: "answer"},
		{Kind: protocol.EventBlockStop, Index: 1, Block: &protocol.Block{Kind: protocol.KindText, Text: "answer"}},
		{Kind: protocol.EventBlockStart, Index: 2, Block: &started},
		{Kind: protocol.EventBlockStop, Index: 2, Block: &stopped},
		{Kind: protocol.EventBlockStart, Index: 3, Block: &protocol.Block{Kind: protocol.KindText}},
		{Kind: protocol.EventBlockDelta, Index: 3, Text: "tail"},
		{Kind: protocol.EventBlockStop, Index: 3, Block: &protocol.Block{Kind: protocol.KindText, Text: "tail"}},
		{Kind: protocol.EventMessageDelta, Finish: protocol.FinishToolCalls},
		{Kind: protocol.EventMessageStop},
	}
	output := encode(t, upstream, events)
	starts, stops := wireIndexes(t, output, "content_block_start"), wireIndexes(t, output, "content_block_stop")
	for position, want := range []int{0, 1, 2, 3} {
		if starts[position] != want || stops[position] != want {
			t.Fatalf("indexes must be 0..3 in order: starts %v stops %v", starts, stops)
		}
	}
	if len(starts) != 4 || len(stops) != 4 {
		t.Fatalf("every block must start and stop once: %v %v", starts, stops)
	}
}

func TestBlockFramesCanBeHeldBackAndReleasedTogether(t *testing.T) {
	started, stopped := toolBlocks("call_1")
	encoder := messages.New().NewStreamEncoder(protocol.ResponseMeta{ID: "m", Model: "m"}, upstream)
	var immediate, held []format.Frame
	for _, event := range []protocol.Event{
		{Kind: protocol.EventMessageStart},
		{Kind: protocol.EventBlockStart, Index: 0, Block: &protocol.Block{Kind: protocol.KindText}},
		{Kind: protocol.EventBlockDelta, Index: 0, Text: "hi"},
		{Kind: protocol.EventBlockStop, Index: 0, Block: &protocol.Block{Kind: protocol.KindText, Text: "hi"}},
		{Kind: protocol.EventBlockStart, Index: 1, Block: &started},
		{Kind: protocol.EventBlockDelta, Index: 1, Text: `{"p":1}`},
		{Kind: protocol.EventBlockStop, Index: 1, Block: &stopped},
		{Kind: protocol.EventMessageDelta, Finish: protocol.FinishToolCalls},
	} {
		produced, _, err := encoder.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		if event.Index == 1 && event.Kind != protocol.EventMessageDelta {
			held = append(held, produced...)
		} else {
			immediate = append(immediate, produced...)
		}
	}
	trailing, _, _ := encoder.Finish()
	released := append(append(immediate, held...), trailing...)
	decoded := testkit.Aggregate(decode(t, upstream, released))
	if len(decoded.Blocks) != 2 || decoded.Blocks[1].ToolCall == nil || decoded.Blocks[1].ToolCall.Arguments != `{"p":1}` {
		t.Fatalf("a held block must still form a valid sequence: %+v", decoded.Blocks)
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
		Messages:   []protocol.Message{{Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "hi"}}}},
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
	request := &protocol.Request{Model: "m", MaxOutputTokens: &limit, Reasoning: &protocol.ReasoningConfig{BudgetTokens: &budget}, Messages: []protocol.Message{{Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "hi"}}}}}
	body, _, losses, err := messages.New().EncodeRequest(request, upstream)
	if err != nil || len(losses) == 0 {
		t.Fatalf("%v %+v", err, losses)
	}
	testkit.AssertJSON(t, body, "thinking.budget_tokens", 8000)
	testkit.AssertJSON(t, body, "max_tokens", 9024)
}

func joinData(input []format.Frame) string {
	var text strings.Builder
	for _, frame := range input {
		text.Write(frame.Data)
		text.WriteByte('\n')
	}
	return text.String()
}

func TestSystemMessagesInsideTheArrayAreAccepted(t *testing.T) {
	body := []byte(`{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"hi"},{"role":"system","content":[{"type":"text","text":"environment"}]},{"role":"assistant","content":"ok"}]}`)
	request, err := messages.New().DecodeRequest(body, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 3 || request.Messages[1].Role != protocol.RoleSystem || request.Messages[1].Blocks[0].Text != "environment" {
		t.Fatalf("the system message must keep its position: %+v", request.Messages)
	}
}
