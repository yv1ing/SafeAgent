package gemini_test

import (
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/gemini"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

var (
	client   = format.Peer{Side: format.Client, Vendor: protocol.VendorGoogle, Model: "m"}
	upstream = format.Peer{Side: format.Upstream, Vendor: protocol.VendorGoogle, Model: "m"}
)

func toolEvents(arguments string) []protocol.Event {
	started := protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read"}}
	stopped := protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read", Arguments: arguments}}
	return []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "r1", Model: "m"}},
		{Kind: protocol.EventBlockStart, Index: 0, Block: &started},
		{Kind: protocol.EventBlockDelta, Index: 0, Text: arguments},
		{Kind: protocol.EventBlockStop, Index: 0, Block: &stopped},
		{Kind: protocol.EventMessageDelta, Finish: protocol.FinishToolCalls},
		{Kind: protocol.EventMessageStop},
	}
}

func callsIn(t *testing.T, frames []format.Frame) int {
	t.Helper()
	count := 0
	for _, frame := range frames {
		for _, part := range protocol.Array(testkit.JSONValue(t, frame.Data, "candidates.0.content.parts")) {
			if protocol.Object(part)["functionCall"] != nil {
				count++
			}
		}
	}
	return count
}

func TestDuplicateBlockStopEmitsOneFunctionCall(t *testing.T) {
	events := toolEvents(`{"p":1}`)
	events = append(events[:4], append([]protocol.Event{events[3]}, events[4:]...)...)
	output, _ := testkit.EncodeEvents(t, gemini.New().NewStreamEncoder(protocol.ResponseMeta{ID: "r1", Model: "m"}, client), events)
	if calls := callsIn(t, output); calls != 1 {
		t.Fatalf("a repeated stop must not repeat the call, got %d", calls)
	}
}

func TestEncodingFailureIsReturnedNotSwallowed(t *testing.T) {
	events := toolEvents(`not json`)
	encoder := gemini.New().NewStreamEncoder(protocol.ResponseMeta{ID: "r1", Model: "m"}, client)
	var failed bool
	for _, event := range events {
		if _, _, err := encoder.Encode(event); err != nil {
			failed = true
		}
	}
	if !failed {
		t.Fatal("arguments that cannot form a function call must fail loudly")
	}
}

func TestFunctionCallWithoutIDGetsASaltedStableID(t *testing.T) {
	body := []byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"name":"read","args":{"p":1}}}]},{"role":"user","parts":[{"functionResponse":{"name":"read","response":{"result":"ok"}}}]}]}`)
	idOf := func(salt string) (call, result string) {
		request, err := gemini.New().DecodeRequest(body, nil, format.Peer{Side: format.Client, Model: "m", Salt: salt})
		if err != nil {
			t.Fatal(err)
		}
		return request.Messages[0].Blocks[0].ToolCall.ID, request.Messages[1].Blocks[0].ToolResult.CallID
	}
	plain, plainResult := idOf("")
	first, firstResult := idOf("turn-1")
	second, _ := idOf("turn-2")
	if plain != plainResult || first != firstResult {
		t.Fatal("a result must be paired with its call")
	}
	if first == second || plain == first {
		t.Fatalf("salts must separate identical calls: %s %s %s", plain, first, second)
	}
	again, _ := idOf("turn-1")
	if again != first {
		t.Fatal("the same salt must give the same identifier")
	}
}

func TestStreamedCallsGetDistinctSaltedIDs(t *testing.T) {
	frames := []format.Frame{{Data: []byte(`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"functionCall":{"name":"read","args":{}}},{"functionCall":{"name":"read","args":{}}}]},"finishReason":"STOP"}]}`)}}
	ids := func(salt string) []string {
		decoder := gemini.New().NewStreamDecoder(format.Peer{Side: format.Upstream, Salt: salt})
		response := testkit.Aggregate(testkit.DecodeFrames(t, decoder, frames))
		var found []string
		for _, block := range response.Blocks {
			found = append(found, block.ToolCall.ID)
		}
		return found
	}
	first, second := ids("a"), ids("b")
	if len(first) != 2 || first[0] == first[1] || first[0] == second[0] {
		t.Fatalf("%v %v", first, second)
	}
}

func TestMissingSignatureUsesThePlaceholderTowardsUpstream(t *testing.T) {
	request := &protocol.Request{Model: "m", Messages: []protocol.Message{{Role: protocol.RoleAssistant, Blocks: []protocol.Block{{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "c", Name: "read", Arguments: "{}"}}}}}}
	body, _, losses, err := gemini.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, body, "contents.0.parts.0.thoughtSignature", "skip_thought_signature_validator")
	if len(losses) != 1 || losses[0].Code != protocol.LossGeminiDummySignature {
		t.Fatalf("%+v", losses)
	}
}

func TestTextAndThoughtsStreamAsRuns(t *testing.T) {
	events := []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "r1", Model: "m"}},
		{Kind: protocol.EventBlockStart, Index: 0, Block: &protocol.Block{Kind: protocol.KindReasoning, Reasoning: &protocol.Reasoning{Visibility: "full"}}},
		{Kind: protocol.EventBlockDelta, Index: 0, Text: "plan"},
		{Kind: protocol.EventBlockStop, Index: 0, Block: &protocol.Block{Kind: protocol.KindReasoning, Text: "plan", Reasoning: &protocol.Reasoning{Visibility: "full"}}},
		{Kind: protocol.EventBlockStart, Index: 1, Block: &protocol.Block{Kind: protocol.KindText}},
		{Kind: protocol.EventBlockDelta, Index: 1, Text: "answer"},
		{Kind: protocol.EventBlockStop, Index: 1, Block: &protocol.Block{Kind: protocol.KindText, Text: "answer"}},
		{Kind: protocol.EventMessageDelta, Finish: protocol.FinishStop},
		{Kind: protocol.EventMessageStop},
	}
	output, _ := testkit.EncodeEvents(t, gemini.New().NewStreamEncoder(protocol.ResponseMeta{ID: "r1", Model: "m"}, client), events)
	response := testkit.Aggregate(testkit.DecodeFrames(t, gemini.New().NewStreamDecoder(upstream), output))
	if len(response.Blocks) != 2 || response.Blocks[0].Kind != protocol.KindReasoning || response.Blocks[0].Text != "plan" || response.Blocks[1].Text != "answer" {
		t.Fatalf("%+v", response.Blocks)
	}
}

func TestToolResultsShareOneUserTurn(t *testing.T) {
	call := func(id, name string) protocol.Message {
		return protocol.Message{Role: protocol.RoleAssistant, Blocks: []protocol.Block{{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: id, Name: name, Arguments: "{}"}}}}
	}
	result := func(id, name string) protocol.Message {
		return protocol.Message{Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindToolResult, ToolResult: &protocol.ToolResult{CallID: id, Name: name, Content: []protocol.Block{{Kind: protocol.KindText, Text: "ok"}}}}}}
	}
	request := &protocol.Request{Model: "m", Messages: []protocol.Message{call("c1", "a"), call("c2", "b"), result("c1", "a"), result("c2", "b")}}
	body, _, _, err := gemini.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	if turns := protocol.Array(testkit.JSONValue(t, body, "contents")); len(turns) != 2 {
		t.Fatalf("adjacent turns of one role must merge: %s", body)
	}
}

func TestFunctionResponseKeepsStructuredOutput(t *testing.T) {
	request := &protocol.Request{Model: "m", Messages: []protocol.Message{
		{Role: protocol.RoleAssistant, Blocks: []protocol.Block{{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "c", Name: "a", Arguments: "{}"}}}},
		{Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindToolResult, ToolResult: &protocol.ToolResult{CallID: "c", Name: "a", Content: []protocol.Block{{Kind: protocol.KindText, Text: `{"temp":21}`}}}}}},
	}}
	body, _, _, err := gemini.New().EncodeRequest(request, client)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"response":{"temp":21}`) {
		t.Fatalf("%s", body)
	}
}

func TestStreamedThoughtSignatureCoversTheWholeText(t *testing.T) {
	envelope := testkit.SealingEnvelope{Secret: []byte("secret")}
	sealing := format.Peer{Side: format.Client, Vendor: protocol.VendorGoogle, Model: "m", Envelope: envelope}
	started := protocol.Block{Kind: protocol.KindReasoning, Reasoning: &protocol.Reasoning{Visibility: "full"}}
	stopped := protocol.Block{Kind: protocol.KindReasoning, Text: "first second", Reasoning: &protocol.Reasoning{Visibility: "full", Signature: &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: "blob"}}}
	frames, _ := testkit.EncodeEvents(t, gemini.Codec{}.NewStreamEncoder(protocol.ResponseMeta{ID: "r1", Model: "m"}, sealing), []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "r1", Model: "m"}},
		{Kind: protocol.EventBlockStart, Index: 0, Block: &started},
		{Kind: protocol.EventBlockDelta, Index: 0, Text: "first "},
		{Kind: protocol.EventBlockDelta, Index: 0, Text: "second"},
		{Kind: protocol.EventBlockStop, Index: 0, Block: &stopped},
		{Kind: protocol.EventMessageDelta, Finish: protocol.FinishStop},
		{Kind: protocol.EventMessageStop},
	})
	for _, frame := range frames {
		for _, part := range protocol.Array(testkit.JSONValue(t, frame.Data, "candidates.0.content.parts")) {
			sealed := protocol.String(protocol.Object(part)["thoughtSignature"])
			if sealed == "" {
				continue
			}
			opened, ok := envelope.Open(sealed)
			if !ok || opened.TextDigest != protocol.TextDigest("first second") {
				t.Fatalf("the signature must be sealed for the whole thought: %+v", opened)
			}
			return
		}
	}
	t.Fatal("no signature part was sent")
}
