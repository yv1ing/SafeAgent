package codec_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/chat"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/gemini"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/messages"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/responses"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

var codecs = codec.NewRegistry(chat.New(), responses.New(), messages.New(), gemini.New())

var envelopes = testkit.SealingEnvelope{Secret: []byte("matrix-secret")}

func peers(kind model.Format) (codec.Peer, codec.Peer) {
	client, upstream := testkit.PeersWithEnvelope(kind, envelopes)
	if kind == model.Gemini {
		client.Model, upstream.Model = "gemini-3-pro", "gemini-3-pro"
	}
	return client, upstream
}

func codecOf(t *testing.T, kind model.Format) codec.Codec {
	t.Helper()
	coder, found := codecs.Get(kind)
	if !found {
		t.Fatalf("no codec for %s", kind)
	}
	return coder
}

func canonicalRequest() *model.Request {
	return &model.Request{
		Model:           "m",
		MaxOutputTokens: intPointer(256),
		Temperature:     floatPointer(0.2),
		TopP:            floatPointer(0.9),
		Items: []model.Item{
			model.MessageItem(model.RoleSystem, "be brief"),
			{Kind: model.ItemMessage, Role: model.RoleUser, Parts: []model.Part{
				{Kind: model.PartText, Text: "look"},
				{Kind: model.PartMedia, Media: &model.Media{Kind: "image", MIMEType: "image/png", URL: "https://example.com/a.png"}},
			}},
			{Kind: model.ItemReasoning, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartReasoningText, Text: "plan"}}, Reasoning: &model.Reasoning{Visibility: "full"}},
			model.MessageItem(model.RoleAssistant, "answer"),
			{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "read", Arguments: `{"p":1}`}},
			{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_2", Name: "now", Arguments: `{}`}},
			{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "call_1", Name: "read", Parts: []model.Part{{Kind: model.PartText, Text: "ok"}}},
			{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "call_2", Name: "now", Parts: []model.Part{{Kind: model.PartText, Text: "12:00"}}},
			model.MessageItem(model.RoleUser, "more"),
		},
		Tools: []model.Tool{
			{Kind: model.ToolFunction, Name: "read", Description: "read a file", Parameters: []byte(`{"type":"object","properties":{"p":{"type":"string"}}}`)},
			{Kind: model.ToolFunction, Name: "now", Parameters: []byte(`{"type":"object"}`)},
		},
	}
}

func canonicalResponse() *model.Response {
	return &model.Response{
		Meta: model.ResponseMeta{ID: "resp_1", Model: "m", Created: 1700000000},
		Items: []model.Item{
			{Kind: model.ItemReasoning, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartReasoningText, Text: "plan"}}, Reasoning: &model.Reasoning{Visibility: "full"}},
			model.MessageItem(model.RoleAssistant, "answer"),
			{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "read", Arguments: `{"p":1}`}},
		},
		Finish: model.Finish{Kind: model.FinishToolCalls},
		Usage:  &model.Usage{Input: 10, Output: 5, CacheRead: 2, Reasoning: 1},
	}
}

func TestRequestSurvivesEverySourceTargetRoundTrip(t *testing.T) {
	original := canonicalRequest()
	want := testkit.Essence(original, true)
	for _, source := range []model.Format{model.Chat, model.Responses, model.Messages, model.Gemini} {
		sourceCodec := codecOf(t, source)
		client, _ := peers(source)
		body, _, _, err := sourceCodec.EncodeRequest(original, client)
		if err != nil {
			t.Fatalf("%s encode: %v", source, err)
		}
		decoded, _, err := sourceCodec.DecodeRequest(body, nil, client)
		if err != nil {
			t.Fatalf("%s decode: %v", source, err)
		}
		if got := testkit.Essence(decoded, true); !slices.Equal(got, want) {
			t.Fatalf("%s request round trip changed essence:\nwant %v\ngot  %v\nbody %s", source, want, got, body)
		}
	}
}

func TestRequestCrossFormatConversionKeepsEssence(t *testing.T) {
	original := canonicalRequest()
	want := testkit.Essence(original, true)
	for _, source := range []model.Format{model.Chat, model.Responses, model.Messages, model.Gemini} {
		for _, target := range []model.Format{model.Chat, model.Responses, model.Messages, model.Gemini} {
			sourceCodec, targetCodec := codecOf(t, source), codecOf(t, target)
			client, _ := peers(source)
			body, _, _, err := sourceCodec.EncodeRequest(original, client)
			if err != nil {
				t.Fatalf("%s->%s source encode: %v", source, target, err)
			}
			decoded, _, err := sourceCodec.DecodeRequest(body, nil, client)
			if err != nil {
				t.Fatalf("%s->%s source decode: %v", source, target, err)
			}
			decoded.Source = source
			targetClient, targetUpstream := peers(target)
			converted, _, losses, err := targetCodec.EncodeRequest(decoded, targetUpstream)
			if err != nil {
				t.Fatalf("%s->%s target encode: %v", source, target, err)
			}
			got, _, err := targetCodec.DecodeRequest(converted, nil, targetClient)
			if err != nil {
				t.Fatalf("%s->%s target decode: %v\n%s\n%+v", source, target, err, converted, losses)
			}
			if essence := testkit.Essence(got, true); !slices.Equal(essence, want) {
				t.Fatalf("%s->%s essence:\nwant %v\ngot  %v\nbody %s", source, target, want, essence, converted)
			}
		}
	}
}

func TestResponseSurvivesEveryTargetStreamRoundTrip(t *testing.T) {
	original := canonicalResponse()
	want := testkit.ResponseEssence(original, true)
	for _, target := range []model.Format{model.Chat, model.Responses, model.Messages, model.Gemini} {
		targetCodec := codecOf(t, target)
		client, upstream := peers(target)
		encoder := targetCodec.NewStreamEncoder(model.ResponseMeta{ID: "resp_1", Model: "m"}, client)
		frames, losses := testkit.EncodeEvents(t, encoder, testkit.EventsOf(original))
		decoder := targetCodec.NewStreamDecoder(upstream)
		events := testkit.DecodeFrames(t, decoder, frames)
		testkit.CheckEventContract(t, events)
		response := testkit.Aggregate(events)
		if testing.Verbose() {
			for _, frame := range frames {
				t.Logf("%s", frame.Data)
			}
			t.Logf("aggregate: %+v", response.Items)
		}
		if got := testkit.ResponseEssence(response, true); !slices.Equal(got, want) {
			t.Fatalf("%s stream essence:\nwant %v\ngot  %v\nlosses %+v", target, want, got, losses)
		}
		if response.Finish.Kind != model.FinishToolCalls {
			t.Fatalf("%s finish: %+v", target, response.Finish)
		}
	}
}

func TestResponseSurvivesEveryTargetJSONRoundTrip(t *testing.T) {
	original := canonicalResponse()
	want := testkit.ResponseEssence(original, true)
	for _, target := range []model.Format{model.Chat, model.Responses, model.Messages, model.Gemini} {
		targetCodec := codecOf(t, target)
		client, upstream := peers(target)
		body, losses, err := targetCodec.EncodeResponse(original, client)
		if err != nil {
			t.Fatalf("%s encode: %v %+v", target, err, losses)
		}
		response, _, err := targetCodec.DecodeResponse(body, upstream)
		if err != nil {
			t.Fatalf("%s decode: %v\n%s", target, err, body)
		}
		if got := testkit.ResponseEssence(response, true); !slices.Equal(got, want) {
			t.Fatalf("%s JSON essence:\nwant %v\ngot  %v\nbody %s", target, want, got, body)
		}
	}
}

func TestDefaultMaxTokensIsNeverSilentlySmall(t *testing.T) {
	request := &model.Request{Model: "m", Items: []model.Item{model.MessageItem(model.RoleUser, "hi")}}
	body, _, losses, err := messages.New().EncodeRequest(request, codec.Peer{Side: codec.Upstream, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, body, "max_tokens", 8192)
	found := false
	for _, loss := range losses {
		if loss.Code == model.LossDefaultTokenLimit {
			found = true
		}
	}
	if !found {
		t.Fatalf("a defaulted limit must be recorded: %+v", losses)
	}
}

func TestMediaCapabilityPreflightRejectsUnrepresentableContent(t *testing.T) {
	request := &model.Request{Source: model.Gemini, Items: []model.Item{
		{Kind: model.ItemMessage, Role: model.RoleUser, Parts: []model.Part{{Kind: model.PartMedia, Media: &model.Media{Kind: "video", MIMEType: "video/mp4", URL: "https://example.com/a.mp4"}}}},
	}}
	losses := model.ValidateRequestCapability(request, messages.New().BaseCapability())
	fatal := false
	for _, loss := range losses {
		fatal = fatal || loss.Fatal()
	}
	if !fatal {
		t.Fatalf("video must be rejected for Messages: %+v", losses)
	}
}

func TestRawFieldsReplayOnlyTowardsTheirFormat(t *testing.T) {
	request := &model.Request{Source: model.Chat, Model: "m", Items: []model.Item{model.MessageItem(model.RoleUser, "hi")}, Raw: &model.Raw{Format: model.Chat, Fields: map[string]json.RawMessage{"vendor_flag": json.RawMessage(`true`)}}}
	body, _, losses, err := chat.New().EncodeRequest(request, codec.Peer{Side: codec.Upstream, Model: "m"})
	if err != nil || len(losses) != 0 {
		t.Fatalf("%v %+v", err, losses)
	}
	testkit.AssertJSON(t, body, "vendor_flag", true)
	other, _, losses, err := responses.New().EncodeRequest(request, codec.Peer{Side: codec.Upstream, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(other), "vendor_flag") || len(losses) == 0 {
		t.Fatalf("a foreign raw field must be dropped and reported: %s %+v", other, losses)
	}
}

func TestItemRawFieldsReplayOnlyTowardsTheirFormat(t *testing.T) {
	request := &model.Request{Source: model.Chat, Model: "m", Items: []model.Item{{
		Kind: model.ItemMessage, Role: model.RoleUser, Parts: []model.Part{{Kind: model.PartText, Text: "hi"}},
		Raw: &model.Raw{Format: model.Chat, Fields: map[string]json.RawMessage{"vendor_flag": json.RawMessage(`true`)}},
	}}}
	body, _, losses, err := chat.New().EncodeRequest(request, codec.Peer{Side: codec.Upstream, Model: "m"})
	if err != nil || len(losses) != 0 {
		t.Fatalf("%v %+v", err, losses)
	}
	testkit.AssertJSON(t, body, "messages.0.vendor_flag", true)
	other, _, losses, err := responses.New().EncodeRequest(request, codec.Peer{Side: codec.Upstream, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(other), "vendor_flag") || len(losses) == 0 {
		t.Fatalf("foreign item raw fields must be dropped and reported: %s %+v", other, losses)
	}
}

func TestToolResultMediaReachesGemini(t *testing.T) {
	request := &model.Request{Model: "m", Source: model.Messages, Items: []model.Item{
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "c", Name: "read", Arguments: "{}"}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "c", Name: "read", Parts: []model.Part{
			{Kind: model.PartText, Text: "look"},
			{Kind: model.PartMedia, Media: &model.Media{Kind: "image", MIMEType: "image/png", Data: "AAAA"}},
		}},
	}}
	body, _, losses, err := gemini.New().EncodeRequest(request, codec.Peer{Side: codec.Upstream, Model: "gemini-3-pro"})
	if err != nil {
		t.Fatalf("%v %+v", err, losses)
	}
	parts := model.Array(testkit.JSONValue(t, body, "contents.1.parts.0.functionResponse.parts"))
	if len(parts) != 1 || model.Object(parts[0])["inlineData"] == nil {
		t.Fatalf("tool result media must travel in functionResponse.parts: %s", body)
	}
}

func TestHeaderAllowlistNeverForwardsCredentials(t *testing.T) {
	source := http.Header{
		"Anthropic-Beta":      {"features-2025"},
		"Anthropic-Version":   {"2023-06-01"},
		"Authorization":       {"Bearer secret"},
		"Cookie":              {"session=1"},
		"Openai-Organization": {"org"},
		"X-Custom":            {"x"},
		"X-SafeAgent-Session": {"session"},
	}
	forwarded, losses := codec.ForwardHeaders(source, model.Messages, []string{"x-custom"})
	if forwarded.Get("Anthropic-Beta") != "features-2025" || forwarded.Get("Anthropic-Version") != "2023-06-01" || forwarded.Get("X-Custom") != "x" {
		t.Fatalf("%+v", forwarded)
	}
	if forwarded.Get("Authorization") != "" || forwarded.Get("Cookie") != "" || forwarded.Get("X-SafeAgent-Session") != "" {
		t.Fatal("credentials, cookies and internal headers must never be forwarded")
	}
	if len(losses) == 0 {
		t.Fatal("a protocol header the target cannot consume must be reported")
	}
}

func TestUsagePresenceAndBuckets(t *testing.T) {
	usage := &model.Usage{Input: 10, Output: 5, CacheRead: 2, CacheWrite: 3, CacheWrite1h: 1, Reasoning: 4, Details: map[string]int64{"prompt.audio": 1}}
	messagesBody := codec.EncodeMessagesUsage(usage)
	if messagesBody["input_tokens"] != int64(5) || messagesBody["cache_read_input_tokens"] != int64(2) || messagesBody["cache_creation_input_tokens"] != int64(3) {
		t.Fatalf("%+v", messagesBody)
	}
	chatBody := codec.EncodeChatUsage(usage)
	if chatBody["prompt_tokens_details"].(map[string]any)["audio_tokens"] != int64(1) {
		t.Fatalf("%+v", chatBody)
	}
}

func TestToolSignatureIsBoundToNameAndArguments(t *testing.T) {
	envelope := testkit.SealingEnvelope{Secret: []byte("signature-test")}
	client := codec.Peer{Side: codec.Client, Vendor: model.VendorGoogle, Envelope: envelope}
	sealed := client.WireToolSignature(&model.Signature{Vendor: model.VendorGoogle, Blob: "provider-blob"}, "run", `{"a":1}`)
	if sealed == "" {
		t.Fatal("a tool signature must be sealed")
	}
	accepted := model.Item{Kind: model.ItemToolCall, ToolCall: &model.ToolCall{Kind: model.ToolFunction, Name: "run", Arguments: `{"a":1}`, Signature: &model.Signature{Blob: sealed}}}
	if losses := client.Unseal(&accepted); len(losses) != 0 || accepted.ToolCall.Signature.Blob != "provider-blob" {
		t.Fatalf("matching arguments must be accepted: %+v %+v", losses, accepted.ToolCall.Signature)
	}
	rejected := model.Item{Kind: model.ItemToolCall, ToolCall: &model.ToolCall{Kind: model.ToolFunction, Name: "run", Arguments: `{"a":2}`, Signature: &model.Signature{Blob: sealed}}}
	losses := client.Unseal(&rejected)
	if len(losses) == 0 || rejected.ToolCall.Signature != nil {
		t.Fatalf("changed arguments must invalidate the signature: %+v %+v", losses, rejected.ToolCall.Signature)
	}
}

func TestFinishMappingCarriesLosses(t *testing.T) {
	if body := codec.ChatFinish(model.FinishPauseTurn); body != "stop" {
		t.Fatalf("a paused turn must map to a concrete Chat value, got %q", body)
	}
	for target, wantLoss := range map[model.Format]bool{
		model.Chat: true, model.Responses: true, model.Messages: false, model.Gemini: true,
	} {
		if got := codec.FinishLoss(model.FinishPauseTurn, target); got != wantLoss {
			t.Fatalf("%s pause loss = %t, want %t", target, got, wantLoss)
		}
	}
	if codec.FinishLoss(model.FinishToolCalls, model.Messages) {
		t.Fatal("Messages represents tool calls exactly")
	}
}

func intPointer(value int64) *int64       { return &value }
func floatPointer(value float64) *float64 { return &value }

func TestRealClientShapesSurviveConversion(t *testing.T) {
	cases := []struct {
		name   string
		kind   model.Format
		body   string
		checks []string
	}{
		{
			name: "codex_responses", kind: model.Responses,
			body:   `{"model":"gpt-5","instructions":"You are Codex.","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"open the file"}]},{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"enc"},{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"ls\"}"},{"type":"function_call_output","call_id":"call_1","output":"a.txt"},{"type":"message","role":"user","content":[{"type":"input_text","text":"now read it"}]}],"tools":[{"type":"function","name":"shell","parameters":{"type":"object"},"strict":false}],"tool_choice":"auto","parallel_tool_calls":false,"store":false,"stream":true,"include":["reasoning.encrypted_content"]}`,
			checks: []string{"open the file", "shell", "a.txt", "now read it"},
		},
		{
			name: "claude_code_messages", kind: model.Messages,
			body:   `{"model":"claude","max_tokens":32000,"stream":true,"system":[{"type":"text","text":"You are Claude Code.","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"run the tests"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"need bash","signature":"sig"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./..."}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"ok"}]}]}],"tools":[{"name":"Bash","description":"run a command","input_schema":{"type":"object"}}],"thinking":{"type":"enabled","budget_tokens":8000}}`,
			checks: []string{"run the tests", "Bash", "go test", "ok"},
		},
		{
			name: "gemini_cli", kind: model.Gemini,
			body:   `{"model":"gemini","systemInstruction":{"parts":[{"text":"You are Gemini CLI."}]},"contents":[{"role":"user","parts":[{"text":"list files"}]},{"role":"model","parts":[{"functionCall":{"name":"ls","args":{"path":"."}}}]},{"role":"user","parts":[{"functionResponse":{"name":"ls","response":{"result":"a.txt"}}}]}],"tools":[{"functionDeclarations":[{"name":"ls","parametersJsonSchema":{"type":"object"}}]}]}`,
			checks: []string{"list files", "ls", "a.txt"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			codec := codecOf(t, testCase.kind)
			client, upstream := peers(testCase.kind)
			request, _, err := codec.DecodeRequest([]byte(testCase.body), nil, client)
			if err != nil {
				t.Fatal(err)
			}
			converted, _, losses, err := chat.New().EncodeRequest(request, upstream)
			if err != nil {
				t.Fatal(err)
			}
			decoded, _, err := chat.New().DecodeRequest(converted, nil, upstream)
			if err != nil {
				t.Fatal(err)
			}
			essence := strings.Join(testkit.Essence(decoded, true), "\n")
			for _, check := range testCase.checks {
				if !strings.Contains(essence, check) {
					t.Fatalf("%s lost %q: %s\nlosses %+v\nbody %s", testCase.name, check, essence, losses, converted)
				}
			}
		})
	}
}

func TestCustomToolInputIsLoweredReversibly(t *testing.T) {
	request := &model.Request{Model: "m", Items: []model.Item{
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolCustom, ID: "call_p", Name: "apply_patch", Arguments: "*** Begin Patch"}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "call_p", Name: "apply_patch", Parts: []model.Part{{Kind: model.PartText, Text: "done"}}},
	}, Tools: []model.Tool{{Kind: model.ToolCustom, Name: "apply_patch", Description: "apply a patch", Format: []byte(`{"type":"grammar","syntax":"lark","definition":"start: /.+/"}`)}}}
	for _, target := range []model.Format{model.Messages, model.Gemini} {
		codec := codecOf(t, target)
		_, upstream := peers(target)
		body, _, losses, err := codec.EncodeRequest(request, upstream)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if !strings.Contains(string(body), "Begin Patch") || !strings.Contains(string(body), "apply_patch") {
			t.Fatalf("%s lost the custom input: %s", target, body)
		}
		found := false
		for _, loss := range losses {
			found = found || loss.Code == model.LossToolKindMapped
		}
		if !found {
			t.Fatalf("%s must record the custom-to-function mapping: %+v", target, losses)
		}
	}
}

func TestCitationsSurviveMessagesToChat(t *testing.T) {
	request := &model.Request{Model: "m", Source: model.Messages, Items: []model.Item{
		{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "quoted", Citations: []model.Citation{{Kind: "url_citation", CitedText: "quoted", Title: "doc", URL: "https://example.com/doc", Start: intPointer(0), End: intPointer(6)}}}}},
	}}
	body, _, _, err := chat.New().EncodeRequest(request, codec.Peer{Side: codec.Upstream, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	annotations := model.Array(testkit.JSONValue(t, body, "messages.0.annotations"))
	if len(annotations) != 1 || model.String(model.Object(annotations[0])["type"]) != "url_citation" {
		t.Fatalf("%s", body)
	}
}

func TestServerToolsMapByCapabilityOrFail(t *testing.T) {
	request := &model.Request{Model: "m", Source: model.Responses, Tools: []model.Tool{
		{Kind: model.ToolServer, Name: "web_search", Server: &model.ServerTool{Format: model.Responses, Type: "web_search", Config: []byte(`{"type":"web_search"}`)}},
	}, Items: []model.Item{model.MessageItem(model.RoleUser, "search")}}
	body, _, losses, err := messages.New().EncodeRequest(request, codec.Peer{Side: codec.Upstream, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "web_search_20250305") {
		t.Fatalf("the capability must map: %s", body)
	}
	found := false
	for _, loss := range losses {
		found = found || loss.Code == model.LossToolKindMapped
	}
	if !found {
		t.Fatalf("the mapping must be recorded: %+v", losses)
	}
	required := model.CloneRequest(request)
	required.ToolChoice = &model.ToolChoice{Mode: "tool", Names: []string{"web_search"}}
	capability := chat.New().BaseCapability()
	fatal := false
	for _, loss := range model.ValidateRequestCapability(required, capability) {
		fatal = fatal || loss.Fatal()
	}
	if !fatal {
		t.Fatal("a required provider tool with no equivalent must be fatal")
	}
}

func TestPDFMediaCapabilityDependsOnTheTarget(t *testing.T) {
	request := &model.Request{Source: model.Messages, Items: []model.Item{
		{Kind: model.ItemMessage, Role: model.RoleUser, Parts: []model.Part{{Kind: model.PartMedia, Media: &model.Media{Kind: "document", MIMEType: "application/pdf", URL: "https://example.com/a.pdf"}}}},
	}}
	fatal := false
	for _, loss := range model.ValidateRequestCapability(request, chat.New().BaseCapability()) {
		fatal = fatal || loss.Fatal()
	}
	if !fatal {
		t.Fatal("Chat cannot carry a PDF URL and must reject it")
	}
	for _, target := range []model.Format{model.Messages, model.Gemini, model.Responses} {
		for _, loss := range model.ValidateRequestCapability(request, codecOf(t, target).BaseCapability()) {
			if loss.Fatal() {
				t.Fatalf("%s carries a PDF URL: %+v", target, loss)
			}
		}
	}
}

func TestForeignSignaturesAreSealedForClientsAndDroppedForOtherUpstreams(t *testing.T) {
	signature := &model.Signature{Vendor: model.VendorAnthropic, Blob: "provider-blob"}
	client := codec.Peer{Side: codec.Client, Vendor: model.VendorGoogle, Envelope: testkit.SealingEnvelope{Secret: []byte("secret")}}
	sealed := client.WireSignature(signature, "thinking")
	if sealed == "" {
		t.Fatal("a client must receive a verifiable envelope for a foreign signature")
	}
	opened, ok := (testkit.SealingEnvelope{Secret: []byte("secret")}).Open(sealed)
	if !ok || opened.Vendor != model.VendorAnthropic || opened.Blob != "provider-blob" {
		t.Fatalf("the envelope did not preserve the provider signature: %+v %v", opened, ok)
	}
	other := codec.Peer{Side: codec.Upstream, Vendor: model.VendorOpenAI}
	if other.WireSignature(signature, "thinking") != "" {
		t.Fatal("a foreign signature must not be sent to another provider")
	}
}

func TestCustomToolIsLoweredWhenTheEndpointHasNoCustomTools(t *testing.T) {
	request := &model.Request{Model: "m", Tools: []model.Tool{{Kind: model.ToolCustom, Name: "apply_patch", Description: "apply a patch"}}}
	upstream, _ := peers(model.Chat)
	upstream.Profile.Capability.Tools.Custom = false
	body, _, losses, err := chat.New().EncodeRequest(request, upstream)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"custom"`) || !strings.Contains(string(body), `"function"`) {
		t.Fatalf("a custom tool must be lowered for an endpoint without custom tools: %s", body)
	}
	found := false
	for _, loss := range losses {
		found = found || loss.Code == model.LossToolKindMapped
	}
	if !found {
		t.Fatalf("lowering must be recorded: %+v", losses)
	}
}

func TestChatDecodesTopKAndMessageCacheControl(t *testing.T) {
	client, _ := peers(model.Chat)
	request, _, err := chat.New().DecodeRequest([]byte(`{"model":"m","top_k":7,"messages":[{"role":"user","content":"hi","cache_control":{"type":"ephemeral"}}]}`), nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if request.TopK == nil || *request.TopK != 7 {
		t.Fatalf("top_k was not decoded: %+v", request.TopK)
	}
	if len(request.Items) != 1 || request.Items[0].CacheControl == nil || request.Items[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("message cache control was not decoded: %+v", request.Items)
	}
}

func TestMediaPreflightMatchesEncoderConstraints(t *testing.T) {
	audio := &model.Request{Model: "m", Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleUser, Parts: []model.Part{{Kind: model.PartMedia, Media: &model.Media{Kind: "audio", URL: "https://example.com/a.wav", MIMEType: "audio/wav"}}}}}}
	if losses := model.ValidateRequestCapability(audio, chat.New().BaseCapability()); !hasFatal(losses) {
		t.Fatalf("Chat audio input requires inline data: %+v", losses)
	}
	geminiImage := &model.Request{Model: "m", Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleUser, Parts: []model.Part{{Kind: model.PartMedia, Media: &model.Media{Kind: "image", URL: "https://example.com/a"}}}}}}
	if losses := model.ValidateRequestCapability(geminiImage, gemini.New().BaseCapability()); !hasFatal(losses) {
		t.Fatalf("Gemini media requires a MIME type: %+v", losses)
	}
}

func hasFatal(losses []model.Loss) bool {
	for _, loss := range losses {
		if loss.Fatal() {
			return true
		}
	}
	return false
}
