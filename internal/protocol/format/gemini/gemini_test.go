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
	started := protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read"}}
	stopped := protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read", Arguments: arguments}}
	return []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "r1", Model: "m"}},
		{Kind: protocol.EventItemStart, ItemIndex: 0, Item: &started},
		{Kind: protocol.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartToolArguments}},
		{Kind: protocol.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: arguments},
		{Kind: protocol.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartToolArguments, Text: arguments}},
		{Kind: protocol.EventItemStop, ItemIndex: 0, Item: &stopped},
		{Kind: protocol.EventMessageDelta, Finish: &protocol.Finish{Kind: protocol.FinishToolCalls}},
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

func TestDuplicateItemStopEmitsOneFunctionCall(t *testing.T) {
	events := toolEvents(`{"p":1}`)
	events = append(events[:6], append([]protocol.Event{events[5]}, events[6:]...)...)
	output, _ := testkit.EncodeEvents(t, gemini.New().NewStreamEncoder(protocol.ResponseMeta{ID: "r1", Model: "m"}, client), events)
	if calls := callsIn(t, output); calls != 1 {
		t.Fatalf("a repeated stop must not repeat the call, got %d", calls)
	}
}

func TestFunctionCallWithoutIDGetsASaltedStableID(t *testing.T) {
	body := []byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"name":"read","args":{"p":1}}}]},{"role":"user","parts":[{"functionResponse":{"name":"read","response":{"result":"ok"}}}]}]}`)
	idOf := func(salt string) (call, result string) {
		request, _, err := gemini.New().DecodeRequest(body, nil, format.Peer{Side: format.Client, Model: "m", Salt: salt})
		if err != nil {
			t.Fatal(err)
		}
		return request.Items[0].ToolCall.ID, request.Items[1].CallID
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
		for _, item := range response.Items {
			if item.ToolCall != nil {
				found = append(found, item.ToolCall.ID)
			}
		}
		return found
	}
	first, second := ids("a"), ids("b")
	if len(first) != 2 || first[0] == first[1] || first[0] == second[0] {
		t.Fatalf("%v %v", first, second)
	}
}

func TestMissingSignatureIsNotForged(t *testing.T) {
	request := &protocol.Request{Model: "m", Items: []protocol.Item{
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "c", Name: "read", Arguments: "{}"}},
	}}
	body, _, _, err := gemini.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "thoughtSignature") {
		t.Fatalf("a missing signature must not be replaced by a placeholder: %s", body)
	}
}

func TestTextAndThoughtsStreamAsRuns(t *testing.T) {
	events := []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "r1", Model: "m"}},
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
		{Kind: protocol.EventMessageDelta, Finish: &protocol.Finish{Kind: protocol.FinishStop}},
		{Kind: protocol.EventMessageStop},
	}
	output, _ := testkit.EncodeEvents(t, gemini.New().NewStreamEncoder(protocol.ResponseMeta{ID: "r1", Model: "m"}, client), events)
	response := testkit.Aggregate(testkit.DecodeFrames(t, gemini.New().NewStreamDecoder(upstream), output))
	if len(response.Items) != 2 || response.Items[0].Kind != protocol.ItemReasoning || response.Items[0].Text() != "plan" || response.Items[1].Text() != "answer" {
		t.Fatalf("%+v", response.Items)
	}
}

func TestToolResultsShareOneUserTurn(t *testing.T) {
	request := &protocol.Request{Model: "m", Items: []protocol.Item{
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "c1", Name: "a", Arguments: "{}"}},
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "c2", Name: "b", Arguments: "{}"}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "c1", Name: "a", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "ok"}}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "c2", Name: "b", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "ok"}}},
	}}
	body, _, _, err := gemini.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	if turns := protocol.Array(testkit.JSONValue(t, body, "contents")); len(turns) != 2 {
		t.Fatalf("adjacent turns of one role must merge: %s", body)
	}
}

func TestFunctionResponseKeepsStructuredOutput(t *testing.T) {
	request := &protocol.Request{Model: "m", Items: []protocol.Item{
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "c", Name: "a", Arguments: "{}"}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "c", Name: "a", Parts: []protocol.Part{{Kind: protocol.PartText, Text: `{"temp":21}`}}},
	}}
	body, _, _, err := gemini.New().EncodeRequest(request, client)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"response":{"temp":21}`) {
		t.Fatalf("%s", body)
	}
}

func TestFunctionResponseErrorShapeIsAnError(t *testing.T) {
	body := []byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"name":"a","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"a","response":{"error":"boom"}}}]}]}`)
	request, _, err := gemini.New().DecodeRequest(body, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if !request.Items[1].IsError || request.Items[1].Text() != "boom" {
		t.Fatalf("%+v", request.Items[1])
	}
}

func TestCitationSourcesStayInCitationMetadata(t *testing.T) {
	start, end := int64(1), int64(4)
	response := &protocol.Response{Items: []protocol.Item{{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant, Parts: []protocol.Part{{
		Kind: protocol.PartText, Text: "hi",
		Citations: []protocol.Citation{{Kind: "citation_source", URL: "https://example.com/a", Title: "A", Start: &start, End: &end}},
	}}}}, Finish: protocol.Finish{Kind: protocol.FinishStop}}
	body, _, err := gemini.New().EncodeResponse(response, client)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, body, "candidates.0.citationMetadata.citationSources.0.uri", "https://example.com/a")
	testkit.AssertJSON(t, body, "candidates.0.citationMetadata.citationSources.0.startIndex", 1)
	if protocol.Object(testkit.JSONValue(t, body, "candidates.0.groundingMetadata")) != nil {
		t.Fatalf("a citation source must not become a grounding chunk: %s", body)
	}
	decoded, _, err := gemini.New().DecodeResponse(body, upstream)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Items[0].Parts[0].Citations; len(got) != 1 || got[0].Kind != "citation_source" || got[0].Start == nil {
		t.Fatalf("%+v", got)
	}
}

func TestMultipleCandidatesAreRejected(t *testing.T) {
	_, _, err := gemini.New().DecodeRequest([]byte(`{"generationConfig":{"candidateCount":2},"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`), nil, client)
	if err == nil {
		t.Fatal("candidateCount>1 must be rejected")
	}
}

func TestStreamedThoughtSignatureCoversTheWholeText(t *testing.T) {
	envelope := testkit.SealingEnvelope{Secret: []byte("secret")}
	sealing := format.Peer{Side: format.Client, Vendor: protocol.VendorGoogle, Model: "m", Envelope: envelope}
	events := []protocol.Event{
		{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: "r1", Model: "m"}},
		{Kind: protocol.EventItemStart, ItemIndex: 0, Item: &protocol.Item{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, Reasoning: &protocol.Reasoning{Visibility: "full", Signature: &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: "blob"}}}},
		{Kind: protocol.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartReasoningText}},
		{Kind: protocol.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "first "},
		{Kind: protocol.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "second"},
		{Kind: protocol.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &protocol.Part{Kind: protocol.PartReasoningText, Text: "first second"}},
		{Kind: protocol.EventItemStop, ItemIndex: 0},
		{Kind: protocol.EventMessageDelta, Finish: &protocol.Finish{Kind: protocol.FinishStop}},
		{Kind: protocol.EventMessageStop},
	}
	frames, _ := testkit.EncodeEvents(t, gemini.New().NewStreamEncoder(protocol.ResponseMeta{ID: "r1", Model: "m"}, sealing), events)
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
