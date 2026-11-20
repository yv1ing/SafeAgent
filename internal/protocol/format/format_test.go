package format_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/chat"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/gemini"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/messages"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/responses"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

var codecs = format.NewRegistry(chat.New(), responses.New(), messages.New(), gemini.New())

var envelopes = testkit.SealingEnvelope{Secret: []byte("matrix-secret")}

func peers(kind protocol.Format) (format.Peer, format.Peer) {
	vendor := kind.DefaultVendor()
	model := "m"
	if kind == protocol.Gemini {
		model = "gemini-3-pro"
	}
	client := format.Peer{Side: format.Client, Vendor: vendor, Model: model}
	if kind == protocol.Messages {
		client.Envelope = envelopes
	}
	return client, format.Peer{Side: format.Upstream, Vendor: vendor, Model: model}
}

func codecOf(t *testing.T, kind protocol.Format) format.Codec {
	t.Helper()
	codec, found := codecs.Get(kind)
	if !found {
		t.Fatalf("no codec for %s", kind)
	}
	return codec
}

func canonicalRequest() *protocol.Request {
	return &protocol.Request{
		Model:           "m",
		MaxOutputTokens: intPointer(256),
		Temperature:     floatPointer(0.2),
		TopP:            floatPointer(0.9),
		Items: []protocol.Item{
			protocol.MessageItem(protocol.RoleSystem, "be brief"),
			{Kind: protocol.ItemMessage, Role: protocol.RoleUser, Parts: []protocol.Part{
				{Kind: protocol.PartText, Text: "look"},
				{Kind: protocol.PartMedia, Media: &protocol.Media{Kind: "image", MIMEType: "image/png", URL: "https://example.com/a.png"}},
			}},
			{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartReasoningText, Text: "plan"}}, Reasoning: &protocol.Reasoning{Visibility: "full"}},
			protocol.MessageItem(protocol.RoleAssistant, "answer"),
			{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read", Arguments: `{"p":1}`}},
			{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_2", Name: "now", Arguments: `{}`}},
			{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "call_1", Name: "read", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "ok"}}},
			{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "call_2", Name: "now", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "12:00"}}},
			protocol.MessageItem(protocol.RoleUser, "more"),
		},
		Tools: []protocol.Tool{
			{Kind: protocol.ToolFunction, Name: "read", Description: "read a file", Parameters: []byte(`{"type":"object","properties":{"p":{"type":"string"}}}`)},
			{Kind: protocol.ToolFunction, Name: "now", Parameters: []byte(`{"type":"object"}`)},
		},
	}
}

func canonicalResponse() *protocol.Response {
	return &protocol.Response{
		Meta: protocol.ResponseMeta{ID: "resp_1", Model: "m", Created: 1700000000},
		Items: []protocol.Item{
			{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartReasoningText, Text: "plan"}}, Reasoning: &protocol.Reasoning{Visibility: "full"}},
			protocol.MessageItem(protocol.RoleAssistant, "answer"),
			{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "call_1", Name: "read", Arguments: `{"p":1}`}},
		},
		Finish: protocol.Finish{Kind: protocol.FinishToolCalls},
		Usage:  &protocol.Usage{Input: 10, Output: 5, CacheRead: 2, Reasoning: 1},
	}
}

func TestRequestSurvivesEverySourceTargetRoundTrip(t *testing.T) {
	original := canonicalRequest()
	want := testkit.Essence(original, true)
	for _, source := range []protocol.Format{protocol.Chat, protocol.Responses, protocol.Messages, protocol.Gemini} {
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
		if got := testkit.Essence(decoded, true); !equal(got, want) {
			t.Fatalf("%s request round trip changed essence:\nwant %v\ngot  %v\nbody %s", source, want, got, body)
		}
	}
}

func TestRequestCrossFormatConversionKeepsEssence(t *testing.T) {
	original := canonicalRequest()
	want := testkit.Essence(original, true)
	for _, source := range []protocol.Format{protocol.Chat, protocol.Responses, protocol.Messages, protocol.Gemini} {
		for _, target := range []protocol.Format{protocol.Chat, protocol.Responses, protocol.Messages, protocol.Gemini} {
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
			if essence := testkit.Essence(got, true); !equal(essence, want) {
				t.Fatalf("%s->%s essence:\nwant %v\ngot  %v\nbody %s", source, target, want, essence, converted)
			}
		}
	}
}

func TestResponseSurvivesEveryTargetStreamRoundTrip(t *testing.T) {
	original := canonicalResponse()
	want := testkit.ResponseEssence(original, true)
	for _, target := range []protocol.Format{protocol.Chat, protocol.Responses, protocol.Messages, protocol.Gemini} {
		targetCodec := codecOf(t, target)
		client, upstream := peers(target)
		encoder := targetCodec.NewStreamEncoder(protocol.ResponseMeta{ID: "resp_1", Model: "m"}, client)
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
		if got := testkit.ResponseEssence(response, true); !equal(got, want) {
			t.Fatalf("%s stream essence:\nwant %v\ngot  %v\nlosses %+v", target, want, got, losses)
		}
		if response.Finish.Kind != protocol.FinishToolCalls {
			t.Fatalf("%s finish: %+v", target, response.Finish)
		}
	}
}

func TestResponseSurvivesEveryTargetJSONRoundTrip(t *testing.T) {
	original := canonicalResponse()
	want := testkit.ResponseEssence(original, true)
	for _, target := range []protocol.Format{protocol.Chat, protocol.Responses, protocol.Messages, protocol.Gemini} {
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
		if got := testkit.ResponseEssence(response, true); !equal(got, want) {
			t.Fatalf("%s JSON essence:\nwant %v\ngot  %v\nbody %s", target, want, got, body)
		}
	}
}

func TestDefaultMaxTokensIsNeverSilentlySmall(t *testing.T) {
	request := &protocol.Request{Model: "m", Items: []protocol.Item{protocol.MessageItem(protocol.RoleUser, "hi")}}
	body, _, losses, err := messages.New().EncodeRequest(request, format.Peer{Side: format.Upstream, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, body, "max_tokens", 8192)
	found := false
	for _, loss := range losses {
		if loss.Code == protocol.LossDefaultTokenLimit {
			found = true
		}
	}
	if !found {
		t.Fatalf("a defaulted limit must be recorded: %+v", losses)
	}
}

func TestMediaCapabilityPreflightRejectsUnrepresentableContent(t *testing.T) {
	request := &protocol.Request{Source: protocol.Gemini, Items: []protocol.Item{
		{Kind: protocol.ItemMessage, Role: protocol.RoleUser, Parts: []protocol.Part{{Kind: protocol.PartMedia, Media: &protocol.Media{Kind: "video", MIMEType: "video/mp4", URL: "https://example.com/a.mp4"}}}},
	}}
	losses := protocol.ValidateRequestCapability(request, messages.New().Capability())
	fatal := false
	for _, loss := range losses {
		fatal = fatal || loss.Fatal()
	}
	if !fatal {
		t.Fatalf("video must be rejected for Messages: %+v", losses)
	}
}

func TestRawFieldsReplayOnlyTowardsTheirFormat(t *testing.T) {
	request := &protocol.Request{Source: protocol.Chat, Model: "m", Items: []protocol.Item{protocol.MessageItem(protocol.RoleUser, "hi")}, Raw: &protocol.Raw{Format: protocol.Chat, Fields: map[string]json.RawMessage{"vendor_flag": json.RawMessage(`true`)}}}
	body, _, losses, err := chat.New().EncodeRequest(request, format.Peer{Side: format.Upstream, Model: "m"})
	if err != nil || len(losses) != 0 {
		t.Fatalf("%v %+v", err, losses)
	}
	testkit.AssertJSON(t, body, "vendor_flag", true)
	other, _, losses, err := responses.New().EncodeRequest(request, format.Peer{Side: format.Upstream, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(other), "vendor_flag") || len(losses) == 0 {
		t.Fatalf("a foreign raw field must be dropped and reported: %s %+v", other, losses)
	}
}

func TestItemRawFieldsReplayOnlyTowardsTheirFormat(t *testing.T) {
	request := &protocol.Request{Source: protocol.Chat, Model: "m", Items: []protocol.Item{{
		Kind: protocol.ItemMessage, Role: protocol.RoleUser, Parts: []protocol.Part{{Kind: protocol.PartText, Text: "hi"}},
		Raw: &protocol.Raw{Format: protocol.Chat, Fields: map[string]json.RawMessage{"vendor_flag": json.RawMessage(`true`)}},
	}}}
	body, _, losses, err := chat.New().EncodeRequest(request, format.Peer{Side: format.Upstream, Model: "m"})
	if err != nil || len(losses) != 0 {
		t.Fatalf("%v %+v", err, losses)
	}
	testkit.AssertJSON(t, body, "messages.0.vendor_flag", true)
	other, _, losses, err := responses.New().EncodeRequest(request, format.Peer{Side: format.Upstream, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(other), "vendor_flag") || len(losses) == 0 {
		t.Fatalf("foreign item raw fields must be dropped and reported: %s %+v", other, losses)
	}
}

func TestToolResultMediaReachesGemini(t *testing.T) {
	request := &protocol.Request{Model: "m", Source: protocol.Messages, Items: []protocol.Item{
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolFunction, ID: "c", Name: "read", Arguments: "{}"}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "c", Name: "read", Parts: []protocol.Part{
			{Kind: protocol.PartText, Text: "look"},
			{Kind: protocol.PartMedia, Media: &protocol.Media{Kind: "image", MIMEType: "image/png", Data: "AAAA"}},
		}},
	}}
	body, _, losses, err := gemini.New().EncodeRequest(request, format.Peer{Side: format.Upstream, Model: "gemini-3-pro"})
	if err != nil {
		t.Fatalf("%v %+v", err, losses)
	}
	parts := protocol.Array(testkit.JSONValue(t, body, "contents.1.parts.0.functionResponse.parts"))
	if len(parts) != 1 || protocol.Object(parts[0])["inlineData"] == nil {
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
	}
	forwarded, losses := format.ForwardHeaders(source, protocol.Messages, []string{"x-custom"})
	if forwarded.Get("Anthropic-Beta") != "features-2025" || forwarded.Get("Anthropic-Version") != "2023-06-01" || forwarded.Get("X-Custom") != "x" {
		t.Fatalf("%+v", forwarded)
	}
	if forwarded.Get("Authorization") != "" || forwarded.Get("Cookie") != "" {
		t.Fatal("credentials must never be forwarded")
	}
	if len(losses) == 0 {
		t.Fatal("a protocol header the target cannot consume must be reported")
	}
}

func TestUsagePresenceAndBuckets(t *testing.T) {
	usage := &protocol.Usage{Input: 10, Output: 5, CacheRead: 2, CacheWrite: 3, CacheWrite1h: 1, Reasoning: 4, Details: map[string]int64{"prompt.audio": 1}}
	messagesBody := format.EncodeMessagesUsage(usage)
	if messagesBody["input_tokens"] != int64(5) || messagesBody["cache_read_input_tokens"] != int64(2) || messagesBody["cache_creation_input_tokens"] != int64(3) {
		t.Fatalf("%+v", messagesBody)
	}
	chatBody := format.EncodeChatUsage(usage)
	if chatBody["prompt_tokens_details"].(map[string]any)["audio_tokens"] != int64(1) {
		t.Fatalf("%+v", chatBody)
	}
}

func TestFinishMappingCarriesLosses(t *testing.T) {
	for _, target := range []protocol.Format{protocol.Chat, protocol.Responses, protocol.Messages, protocol.Gemini} {
		if target == protocol.Responses || target == protocol.Gemini {
			continue
		}
		body := format.ChatFinish(protocol.FinishPauseTurn)
		if body == "" {
			t.Fatal("a pause must map to a concrete terminal value")
		}
	}
	if !format.FinishLoss(protocol.FinishPauseTurn, protocol.Chat) {
		t.Fatal("Chat cannot represent a paused turn")
	}
	if format.FinishLoss(protocol.FinishToolCalls, protocol.Messages) {
		t.Fatal("Messages represents tool calls exactly")
	}
}

func equal(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func intPointer(value int64) *int64       { return &value }
func floatPointer(value float64) *float64 { return &value }

func TestRealClientShapesSurviveConversion(t *testing.T) {
	cases := []struct {
		name   string
		kind   protocol.Format
		body   string
		checks []string
	}{
		{
			name: "codex_responses", kind: protocol.Responses,
			body:   `{"model":"gpt-5","instructions":"You are Codex.","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"open the file"}]},{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"enc"},{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"ls\"}"},{"type":"function_call_output","call_id":"call_1","output":"a.txt"},{"type":"message","role":"user","content":[{"type":"input_text","text":"now read it"}]}],"tools":[{"type":"function","name":"shell","parameters":{"type":"object"},"strict":false}],"tool_choice":"auto","parallel_tool_calls":false,"store":false,"stream":true,"include":["reasoning.encrypted_content"]}`,
			checks: []string{"open the file", "shell", "a.txt", "now read it"},
		},
		{
			name: "claude_code_messages", kind: protocol.Messages,
			body:   `{"model":"claude","max_tokens":32000,"stream":true,"system":[{"type":"text","text":"You are Claude Code.","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"run the tests"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"need bash","signature":"sig"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./..."}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"ok"}]}]}],"tools":[{"name":"Bash","description":"run a command","input_schema":{"type":"object"}}],"thinking":{"type":"enabled","budget_tokens":8000}}`,
			checks: []string{"run the tests", "Bash", "go test", "ok"},
		},
		{
			name: "gemini_cli", kind: protocol.Gemini,
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
	request := &protocol.Request{Model: "m", Items: []protocol.Item{
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{Kind: protocol.ToolCustom, ID: "call_p", Name: "apply_patch", Arguments: "*** Begin Patch"}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "call_p", Name: "apply_patch", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "done"}}},
	}, Tools: []protocol.Tool{{Kind: protocol.ToolCustom, Name: "apply_patch", Description: "apply a patch", Format: []byte(`{"type":"grammar","syntax":"lark","definition":"start: /.+/"}`)}}}
	for _, target := range []protocol.Format{protocol.Messages, protocol.Gemini} {
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
			found = found || loss.Code == protocol.LossToolKindMapped
		}
		if !found {
			t.Fatalf("%s must record the custom-to-function mapping: %+v", target, losses)
		}
	}
}

func TestCitationsSurviveMessagesToChat(t *testing.T) {
	request := &protocol.Request{Model: "m", Source: protocol.Messages, Items: []protocol.Item{
		{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartText, Text: "quoted", Citations: []protocol.Citation{{Kind: "url_citation", CitedText: "quoted", Title: "doc", URL: "https://example.com/doc", Start: intPointer(0), End: intPointer(6)}}}}},
	}}
	body, _, _, err := chat.New().EncodeRequest(request, format.Peer{Side: format.Upstream, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	annotations := protocol.Array(testkit.JSONValue(t, body, "messages.0.annotations"))
	if len(annotations) != 1 || protocol.String(protocol.Object(annotations[0])["type"]) != "url_citation" {
		t.Fatalf("%s", body)
	}
}

func TestServerToolsMapByCapabilityOrFail(t *testing.T) {
	request := &protocol.Request{Model: "m", Source: protocol.Responses, Tools: []protocol.Tool{
		{Kind: protocol.ToolServer, Name: "web_search", Server: &protocol.ServerTool{Format: protocol.Responses, Type: "web_search", Config: []byte(`{"type":"web_search"}`)}},
	}, Items: []protocol.Item{protocol.MessageItem(protocol.RoleUser, "search")}}
	body, _, losses, err := messages.New().EncodeRequest(request, format.Peer{Side: format.Upstream, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "web_search_20250305") {
		t.Fatalf("the capability must map: %s", body)
	}
	found := false
	for _, loss := range losses {
		found = found || loss.Code == protocol.LossToolKindMapped
	}
	if !found {
		t.Fatalf("the mapping must be recorded: %+v", losses)
	}
	required := protocol.CloneRequest(request)
	required.ToolChoice = &protocol.ToolChoice{Mode: "tool", Names: []string{"web_search"}}
	capability := chat.New().Capability()
	fatal := false
	for _, loss := range protocol.ValidateRequestCapability(required, capability) {
		fatal = fatal || loss.Fatal()
	}
	if !fatal {
		t.Fatal("a required provider tool with no equivalent must be fatal")
	}
}

func TestPDFMediaCapabilityDependsOnTheTarget(t *testing.T) {
	request := &protocol.Request{Source: protocol.Messages, Items: []protocol.Item{
		{Kind: protocol.ItemMessage, Role: protocol.RoleUser, Parts: []protocol.Part{{Kind: protocol.PartMedia, Media: &protocol.Media{Kind: "document", MIMEType: "application/pdf", URL: "https://example.com/a.pdf"}}}},
	}}
	fatal := false
	for _, loss := range protocol.ValidateRequestCapability(request, chat.New().Capability()) {
		fatal = fatal || loss.Fatal()
	}
	if !fatal {
		t.Fatal("Chat cannot carry a PDF URL and must reject it")
	}
	for _, target := range []protocol.Format{protocol.Messages, protocol.Gemini, protocol.Responses} {
		for _, loss := range protocol.ValidateRequestCapability(request, codecOf(t, target).Capability()) {
			if loss.Fatal() {
				t.Fatalf("%s carries a PDF URL: %+v", target, loss)
			}
		}
	}
}

func TestForeignSignaturesAreSealedForClientsAndDroppedForOtherUpstreams(t *testing.T) {
	signature := &protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: "provider-blob"}
	client := format.Peer{Side: format.Client, Vendor: protocol.VendorGoogle, Envelope: testkit.SealingEnvelope{Secret: []byte("secret")}}
	sealed := client.WireSignature(signature, "thinking")
	if sealed == "" {
		t.Fatal("a client must receive a verifiable envelope for a foreign signature")
	}
	opened, ok := (testkit.SealingEnvelope{Secret: []byte("secret")}).Open(sealed)
	if !ok || opened.Vendor != protocol.VendorAnthropic || opened.Blob != "provider-blob" {
		t.Fatalf("the envelope did not preserve the provider signature: %+v %v", opened, ok)
	}
	other := format.Peer{Side: format.Upstream, Vendor: protocol.VendorOpenAI}
	if other.WireSignature(signature, "thinking") != "" {
		t.Fatal("a foreign signature must not be sent to another provider")
	}
}
