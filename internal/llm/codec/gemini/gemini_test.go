package gemini_test

import (
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/gemini"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

var client, upstream = testkit.Peers(model.Gemini)

func callsIn(t *testing.T, frames []codec.Frame) int {
	t.Helper()
	count := 0
	for _, frame := range frames {
		for _, part := range model.Array(testkit.JSONValue(t, frame.Data, "candidates.0.content.parts")) {
			if model.Object(part)["functionCall"] != nil {
				count++
			}
		}
	}
	return count
}

func TestDuplicateItemStopEmitsOneFunctionCall(t *testing.T) {
	events := testkit.ToolCallEvents(model.ResponseMeta{ID: "r1", Model: "m"}, `{"p":1}`)
	events = append(events[:6], append([]model.Event{events[5]}, events[6:]...)...)
	output, _ := testkit.EncodeEvents(t, gemini.New().NewStreamEncoder(model.ResponseMeta{ID: "r1", Model: "m"}, client), events)
	if calls := callsIn(t, output); calls != 1 {
		t.Fatalf("a repeated stop must not repeat the call, got %d", calls)
	}
}

func TestFunctionCallWithoutIDGetsASaltedStableID(t *testing.T) {
	body := []byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"name":"read","args":{"p":1}}}]},{"role":"user","parts":[{"functionResponse":{"name":"read","response":{"result":"ok"}}}]}]}`)
	idOf := func(salt string) (call, result string) {
		request, _, err := gemini.New().DecodeRequest(body, nil, codec.Peer{Side: codec.Client, Model: "m", Salt: salt})
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
	frames := []codec.Frame{{Data: []byte(`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"functionCall":{"name":"read","args":{}}},{"functionCall":{"name":"read","args":{}}}]},"finishReason":"STOP"}]}`)}}
	ids := func(salt string) []string {
		decoder := gemini.New().NewStreamDecoder(codec.Peer{Side: codec.Upstream, Salt: salt})
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
	request := &model.Request{Model: "m", Items: []model.Item{
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "c", Name: "read", Arguments: "{}"}},
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
	events := []model.Event{
		{Kind: model.EventMessageStart, Meta: &model.ResponseMeta{ID: "r1", Model: "m"}},
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
		{Kind: model.EventMessageDelta, Finish: &model.Finish{Kind: model.FinishStop}},
		{Kind: model.EventMessageStop},
	}
	output, _ := testkit.EncodeEvents(t, gemini.New().NewStreamEncoder(model.ResponseMeta{ID: "r1", Model: "m"}, client), events)
	response := testkit.Aggregate(testkit.DecodeFrames(t, gemini.New().NewStreamDecoder(upstream), output))
	if len(response.Items) != 2 || response.Items[0].Kind != model.ItemReasoning || response.Items[0].Text() != "plan" || response.Items[1].Text() != "answer" {
		t.Fatalf("%+v", response.Items)
	}
}

func TestToolResultsShareOneUserTurn(t *testing.T) {
	request := &model.Request{Model: "m", Items: []model.Item{
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "c1", Name: "a", Arguments: "{}"}},
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "c2", Name: "b", Arguments: "{}"}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "c1", Name: "a", Parts: []model.Part{{Kind: model.PartText, Text: "ok"}}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "c2", Name: "b", Parts: []model.Part{{Kind: model.PartText, Text: "ok"}}},
	}}
	body, _, _, err := gemini.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	if turns := model.Array(testkit.JSONValue(t, body, "contents")); len(turns) != 2 {
		t.Fatalf("adjacent turns of one role must merge: %s", body)
	}
}

func TestFunctionResponseKeepsStructuredOutput(t *testing.T) {
	request := &model.Request{Model: "m", Items: []model.Item{
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "c", Name: "a", Arguments: "{}"}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "c", Name: "a", Parts: []model.Part{{Kind: model.PartText, Text: `{"temp":21}`}}},
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
	response := &model.Response{Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{
		Kind: model.PartText, Text: "hi",
		Citations: []model.Citation{{Kind: "citation_source", URL: "https://example.com/a", Title: "A", Start: &start, End: &end}},
	}}}}, Finish: model.Finish{Kind: model.FinishStop}}
	body, _, err := gemini.New().EncodeResponse(response, client)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, body, "candidates.0.citationMetadata.citationSources.0.uri", "https://example.com/a")
	testkit.AssertJSON(t, body, "candidates.0.citationMetadata.citationSources.0.startIndex", 1)
	if model.Object(testkit.JSONValue(t, body, "candidates.0.groundingMetadata")) != nil {
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

func TestMultipleCandidatesAreRecorded(t *testing.T) {
	request, _, err := gemini.New().DecodeRequest([]byte(`{"generationConfig":{"candidateCount":2},"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`), nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if request.ChoiceCount != 2 {
		t.Fatalf("candidateCount must be recorded for the pass-through decision: %+v", request.ChoiceCount)
	}
}

func TestStreamedThoughtSignatureCoversTheWholeText(t *testing.T) {
	envelope := testkit.SealingEnvelope{Secret: []byte("secret")}
	sealing := codec.Peer{Side: codec.Client, Vendor: model.VendorGoogle, Model: "m", Envelope: envelope}
	events := []model.Event{
		{Kind: model.EventMessageStart, Meta: &model.ResponseMeta{ID: "r1", Model: "m"}},
		{Kind: model.EventItemStart, ItemIndex: 0, Item: &model.Item{Kind: model.ItemReasoning, Role: model.RoleAssistant, Reasoning: &model.Reasoning{Visibility: "full", Signature: &model.Signature{Vendor: model.VendorGoogle, Blob: "blob"}}}},
		{Kind: model.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartReasoningText}},
		{Kind: model.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "first "},
		{Kind: model.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "second"},
		{Kind: model.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartReasoningText, Text: "first second"}},
		{Kind: model.EventItemStop, ItemIndex: 0},
		{Kind: model.EventMessageDelta, Finish: &model.Finish{Kind: model.FinishStop}},
		{Kind: model.EventMessageStop},
	}
	frames, _ := testkit.EncodeEvents(t, gemini.New().NewStreamEncoder(model.ResponseMeta{ID: "r1", Model: "m"}, sealing), events)
	for _, frame := range frames {
		for _, part := range model.Array(testkit.JSONValue(t, frame.Data, "candidates.0.content.parts")) {
			sealed := model.String(model.Object(part)["thoughtSignature"])
			if sealed == "" {
				continue
			}
			opened, ok := envelope.Open(sealed)
			if !ok || opened.TextDigest != model.TextDigest("first second") {
				t.Fatalf("the signature must be sealed for the whole thought: %+v", opened)
			}
			return
		}
	}
	t.Fatal("no signature part was sent")
}

func signatureEvents(signature *model.Signature) []model.Event {
	reasoning := &model.Reasoning{Visibility: "full", Signature: signature}
	return []model.Event{
		{Kind: model.EventItemStart, ItemIndex: 0, Item: &model.Item{Kind: model.ItemReasoning, Role: model.RoleAssistant, Reasoning: reasoning}},
		{Kind: model.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartReasoningText}},
		{Kind: model.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "kept thought"},
		{Kind: model.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartReasoningText, Text: "kept thought"}},
		{Kind: model.EventItemStop, ItemIndex: 0, Item: &model.Item{Kind: model.ItemReasoning, Role: model.RoleAssistant, Reasoning: reasoning}},
	}
}

func thoughtSignatureOf(t *testing.T, events []model.Event) (string, []model.Loss) {
	t.Helper()
	output, losses := testkit.EncodeEvents(t, gemini.New().NewStreamEncoder(model.ResponseMeta{ID: "resp_1", Model: "m"}, upstream), events)
	sealed := ""
	for _, frame := range output {
		for _, part := range model.Array(testkit.JSONValue(t, frame.Data, "candidates.0.content.parts")) {
			if signature := model.String(model.Object(part)["thoughtSignature"]); signature != "" {
				sealed = signature
			}
		}
	}
	return sealed, losses
}

func TestThoughtSignatureForDifferentTextIsReportedAsDropped(t *testing.T) {
	signature := &model.Signature{Vendor: model.VendorGoogle, Blob: "opaque", TextDigest: model.TextDigest("another thought")}
	sealed, losses := thoughtSignatureOf(t, signatureEvents(signature))
	if sealed != "" {
		t.Fatalf("a signature that does not cover the part must not be sent: %q", sealed)
	}
	found := false
	for _, loss := range losses {
		found = found || loss.Code == model.LossSignatureStripped
	}
	if !found {
		t.Fatalf("a dropped thought signature must be a visible loss: %+v", losses)
	}

	signature.TextDigest = model.TextDigest("kept thought")
	sealed, _ = thoughtSignatureOf(t, signatureEvents(signature))
	if sealed != "opaque" {
		t.Fatalf("a signature covering the part must be sent: %q", sealed)
	}
}
