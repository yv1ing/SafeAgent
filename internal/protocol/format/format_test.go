package format_test

import (
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

var registry = format.NewRegistry(chat.New(), messages.New(), responses.New(), gemini.New())

var allFormats = []protocol.Format{protocol.Chat, protocol.Responses, protocol.Messages, protocol.Gemini}

var fixtureDirectory = map[protocol.Format]string{protocol.Chat: "chat", protocol.Messages: "messages", protocol.Responses: "responses", protocol.Gemini: "gemini"}

var testEnvelope = testkit.SealingEnvelope{Secret: []byte("test-secret")}

func peerOf(side format.Side, kind protocol.Format) format.Peer {
	return format.Peer{Side: side, Vendor: kind.DefaultVendor(), Model: "m", Envelope: testEnvelope}
}

func codecOf(t testing.TB, kind protocol.Format) format.Codec {
	t.Helper()
	codec, found := registry.Get(kind)
	if !found {
		t.Fatalf("no codec for %s", kind)
	}
	return codec
}

func decodeRequest(t testing.TB, kind protocol.Format, body []byte) *protocol.Request {
	t.Helper()
	request, err := codecOf(t, kind).DecodeRequest(body, nil, peerOf(format.Client, kind))
	if err != nil {
		t.Fatalf("decode %s request: %v\n%s", kind, err, body)
	}
	return request
}

func encodeRequest(t testing.TB, kind protocol.Format, request *protocol.Request) ([]byte, []protocol.Loss) {
	t.Helper()
	body, _, losses, err := codecOf(t, kind).EncodeRequest(request, peerOf(format.Upstream, kind))
	if err != nil {
		t.Fatalf("encode %s request: %v", kind, err)
	}
	testkit.AssertNoNull(t, body)
	return body, losses
}

func decodeStream(t testing.TB, kind protocol.Format, side format.Side, frames []format.Frame) []protocol.Event {
	t.Helper()
	events := testkit.DecodeFrames(t, codecOf(t, kind).NewStreamDecoder(peerOf(side, kind)), frames)
	testkit.CheckEventContract(t, events)
	return events
}

func encodeStream(t testing.TB, kind protocol.Format, peer format.Peer, events []protocol.Event) ([]format.Frame, []protocol.Loss) {
	t.Helper()
	encoder := codecOf(t, kind).NewStreamEncoder(protocol.ResponseMeta{ID: "resp_1", Model: "m"}, peer)
	return testkit.EncodeEvents(t, encoder, events)
}

func lossCodes(losses []protocol.Loss) map[protocol.LossCode]int {
	codes := map[protocol.LossCode]int{}
	for _, loss := range losses {
		codes[loss.Code]++
	}
	return codes
}

func joinFrames(frames []format.Frame) string {
	var text strings.Builder
	for _, frame := range frames {
		text.WriteString(frame.Event + " " + string(frame.Data) + "\n")
	}
	return text.String()
}

// Cross-format conversions legitimately restructure some content: custom tool grammars become
// function schemas.
func tolerated(name string, target protocol.Format) bool {
	if strings.Contains(name, "custom_tool") && target != protocol.Responses && target != protocol.Chat {
		return true
	}
	return false
}

func TestFixtureRequestsConvertToEveryFormat(t *testing.T) {
	for _, source := range allFormats {
		for _, name := range testkit.FixtureNames(t, fixtureDirectory[source], ".request.json") {
			for _, target := range allFormats {
				t.Run(name+"->"+string(target), func(t *testing.T) {
					request := decodeRequest(t, source, testkit.FixtureBytes(t, name))
					body, _ := encodeRequest(t, target, request)
					restored := decodeRequest(t, target, body)
					want := testkit.Essence(request, !strings.Contains(name, "custom_tool"))
					if missing, ok := testkit.Subsequence(want, testkit.Essence(restored, !strings.Contains(name, "custom_tool"))); !ok && !tolerated(name, target) {
						t.Fatalf("content lost: %q\nbody: %s", missing, body)
					}
				})
			}
		}
	}
}

// Encoding what was decoded from a format must reproduce the original document; the listed paths
// are the known, deliberate normalizations.
var sameFormatRequestDifferences = map[string][]string{
	"responses/reasoning.request.json":                        {"$.tool_choice"},
	"messages/anthropic-cache-control-inbound.request.json":   {"$.messages[2].content[0].content"},
	"messages/anthropic-claude-code.request.json":             {"$.messages"},
	"messages/anthropic-claude-code2.request.json":            {"$.messages"},
	"messages/anthropic-parallel2_multiple_tool.request.json": {"$.messages"},
	"messages/anthropic-thinking.request.json":                {"$.max_tokens", "$.messages"},
	"messages/anthropic-tool-result-mixed.request.json":       {"$.max_tokens", "$.messages"},
	"gemini/gemini-thinking.request.json":                     {"$.generationConfig.thinkingConfig.thinkingLevel"},
	"gemini/gemini-tools.request.json":                        {"$.tools[0].functionDeclarations[0].parameters", "$.tools[0].functionDeclarations[1].parameters"},
	"gemini/gemini-tool-result.request.json":                  {"$.tools"},
}

func TestFixtureRequestsRoundTripInTheirOwnFormat(t *testing.T) {
	ignored := []string{"model", "stream", "stream_options", "store", "include"}
	for _, source := range allFormats {
		for _, name := range testkit.FixtureNames(t, fixtureDirectory[source], ".request.json") {
			t.Run(name, func(t *testing.T) {
				original := testkit.FixtureBytes(t, name)
				body, _ := encodeRequest(t, source, decodeRequest(t, source, original))
				for _, missing := range testkit.JSONMissing(original, body, ignored...) {
					if !allowedDifference(name, missing, sameFormatRequestDifferences) {
						t.Errorf("not reproduced: %s", missing)
					}
				}
			})
		}
	}
}

func allowedDifference(name, difference string, table map[string][]string) bool {
	path, _, _ := strings.Cut(difference, ":")
	for _, allowed := range table[name] {
		if path == allowed || strings.HasPrefix(path, allowed+".") || strings.HasPrefix(path, allowed+"[") {
			return true
		}
	}
	return false
}

var sameFormatResponseDifferences = map[string][]string{
	"chat/deepseek-reasoning.response.json":            {"$.usage"},
	"chat/openai-parallel_multiple_tool.response.json": {"$.usage"},
	"chat/openai-tool.response.json":                   {"$.usage"},
	"chat/openai-tool_2.response.json":                 {"$.usage"},
	"responses/stop.response.json":                     {"$.output[0].id"},
	"responses/tool-2.response.json":                   {"$.output"},
	"messages/anthropic-server-tool.response.json":     {"$.usage"},
	"gemini/gemini-simple.response.json":               {"$.usageMetadata"},
}

func TestFixtureResponsesConvertToEveryFormat(t *testing.T) {
	for _, source := range allFormats {
		for _, name := range testkit.FixtureNames(t, fixtureDirectory[source], ".response.json") {
			response, _, err := codecOf(t, source).DecodeResponse(testkit.FixtureBytes(t, name), peerOf(format.Upstream, source))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for _, target := range allFormats {
				t.Run(name+"->"+string(target), func(t *testing.T) {
					body, _, err := codecOf(t, target).EncodeResponse(response, peerOf(format.Client, target))
					if err != nil {
						t.Fatalf("encode: %v", err)
					}
					testkit.AssertNoNull(t, body)
					restored, _, err := codecOf(t, target).DecodeResponse(body, peerOf(format.Upstream, target))
					if err != nil {
						t.Fatalf("decode: %v\n%s", err, body)
					}
					withArguments := !strings.Contains(name, "custom_tool")
					if missing, ok := testkit.Subsequence(testkit.ResponseEssence(response, withArguments), testkit.ResponseEssence(restored, withArguments)); !ok && !tolerated(name, target) {
						t.Fatalf("content lost: %q\n%s", missing, body)
					}
					if restored.Finish != response.Finish && !(response.Finish == protocol.FinishRefusal || response.Finish == protocol.FinishPauseTurn) {
						t.Fatalf("finish %s became %s", response.Finish, restored.Finish)
					}
				})
			}
		}
	}
}

func TestFixtureResponsesRoundTripInTheirOwnFormat(t *testing.T) {
	for _, source := range allFormats {
		for _, name := range testkit.FixtureNames(t, fixtureDirectory[source], ".response.json") {
			t.Run(name, func(t *testing.T) {
				original := testkit.FixtureBytes(t, name)
				codec := codecOf(t, source)
				response, _, err := codec.DecodeResponse(original, peerOf(format.Upstream, source))
				if err != nil {
					t.Fatal(err)
				}
				body, _, err := codec.EncodeResponse(response, peerOf(format.Upstream, source))
				if err != nil {
					t.Fatal(err)
				}
				for _, missing := range testkit.JSONMissing(original, body, "created_at") {
					if !allowedDifference(name, missing, sameFormatResponseDifferences) {
						t.Errorf("not reproduced: %s", missing)
					}
				}
			})
		}
	}
}

// Error fixtures are the in-band failures providers emit; the decoder must surface them instead of
// producing an empty response.
func TestFixtureStreamErrorsSurface(t *testing.T) {
	for name, kind := range map[string]protocol.Format{"responses/error.response.stream.jsonl": protocol.Responses, "messages/anthropic-error.stream.jsonl": protocol.Messages} {
		decoder := codecOf(t, kind).NewStreamDecoder(peerOf(format.Upstream, kind))
		var failed bool
		for _, frame := range testkit.FixtureFrames(t, name) {
			if _, err := decoder.Feed(frame); err != nil {
				failed = true
			}
		}
		if !failed {
			t.Errorf("%s: the error frame was ignored", name)
		}
	}
}

func TestFixtureStreamsConvertToEveryFormat(t *testing.T) {
	for _, source := range allFormats {
		for _, name := range testkit.FixtureNames(t, fixtureDirectory[source], ".jsonl") {
			if strings.Contains(name, "error") {
				continue
			}
			events := decodeStream(t, source, format.Upstream, testkit.FixtureFrames(t, name))
			response := testkit.Aggregate(events)
			if len(response.Blocks) == 0 {
				t.Fatalf("%s produced no blocks", name)
			}
			for _, target := range allFormats {
				t.Run(name+"->"+string(target), func(t *testing.T) {
					frames, _ := encodeStream(t, target, peerOf(format.Client, target), events)
					restored := testkit.Aggregate(decodeStream(t, target, format.Upstream, frames))
					withArguments := !strings.Contains(name, "custom_tool")
					if missing, ok := testkit.Subsequence(testkit.ResponseEssence(response, withArguments), testkit.ResponseEssence(restored, withArguments)); !ok && !tolerated(name, target) {
						t.Fatalf("content lost: %q\n%s", missing, joinFrames(frames))
					}
					if restored.Finish != response.Finish && response.Finish != protocol.FinishPauseTurn && response.Finish != protocol.FinishRefusal {
						t.Fatalf("finish %s became %s", response.Finish, restored.Finish)
					}
				})
			}
		}
	}
}

func TestNativeBlocksNeverBecomeNullParts(t *testing.T) {
	body := []byte(`{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"redacted_thinking","data":"opaque"},{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"q"}},{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[]},{"type":"text","text":"answer"}]},{"role":"user","content":"more"}]}`)
	request := decodeRequest(t, protocol.Messages, body)
	for _, target := range allFormats {
		t.Run(string(target), func(t *testing.T) {
			out, losses := encodeRequest(t, target, request)
			if target == protocol.Messages {
				if len(losses) != 0 && lossCodes(losses)[protocol.LossFieldDropped] != 0 {
					t.Fatalf("same-format blocks were dropped: %+v", losses)
				}
				if !strings.Contains(string(out), "redacted_thinking") || !strings.Contains(string(out), "web_search_tool_result") {
					t.Fatalf("native blocks were not replayed: %s", out)
				}
				return
			}
			if lossCodes(losses)[protocol.LossFieldDropped] < 3 {
				t.Fatalf("every dropped block must be reported: %+v", losses)
			}
		})
	}
}

func TestRedactedAndServerBlocksRoundTripLosslessly(t *testing.T) {
	events := decodeStream(t, protocol.Messages, format.Upstream, testkit.FixtureFrames(t, "messages/anthropic-server-tool.stream.jsonl"))
	frames, losses := encodeStream(t, protocol.Messages, peerOf(format.Upstream, protocol.Messages), events)
	if lossCodes(losses)[protocol.LossFieldDropped] != 0 {
		t.Fatalf("same-format stream dropped blocks: %+v", losses)
	}
	restored := testkit.Aggregate(decodeStream(t, protocol.Messages, format.Upstream, frames))
	original := testkit.Aggregate(events)
	if len(restored.Blocks) != len(original.Blocks) {
		t.Fatalf("block count changed: %d != %d", len(restored.Blocks), len(original.Blocks))
	}
	kinds := map[string]bool{}
	for index, block := range original.Blocks {
		if block.Kind != restored.Blocks[index].Kind {
			t.Fatalf("block %d changed kind %s -> %s", index, block.Kind, restored.Blocks[index].Kind)
		}
		if block.ToolCall != nil {
			kinds[string(block.ToolCall.Kind)] = true
			if string(protocol.Marshal(block.Extras)) != string(protocol.Marshal(restored.Blocks[index].Extras)) {
				t.Fatalf("block %d native object changed", index)
			}
		}
	}
	if !kinds["server"] {
		t.Fatal("the fixture must contain a server tool call")
	}
	body := []byte(`{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"redacted_thinking","data":"AAEC"},{"type":"text","text":"ok"}]},{"role":"user","content":"again"}]}`)
	out, _ := encodeRequest(t, protocol.Messages, decodeRequest(t, protocol.Messages, body))
	testkit.AssertJSON(t, out, "messages.1.content.0", map[string]any{"type": "redacted_thinking", "data": "AAEC"})
}

func TestServerToolsRoundTripOrDegradeSafely(t *testing.T) {
	cases := []struct {
		name   string
		source protocol.Format
		body   string
		kind   string
	}{
		{"messages web search", protocol.Messages, `{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"x"}],"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":3}]}`, "web_search_20250305"},
		{"messages computer", protocol.Messages, `{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"x"}],"tools":[{"type":"computer_20250124","name":"computer","display_width_px":10,"display_height_px":10}]}`, "computer_20250124"},
		{"responses web search", protocol.Responses, `{"model":"m","input":"x","tools":[{"type":"web_search","search_context_size":"high"}]}`, "web_search"},
		{"responses mcp", protocol.Responses, `{"model":"m","input":"x","tools":[{"type":"mcp","server_label":"l","server_url":"https://example.com"}]}`, "mcp"},
		{"gemini search", protocol.Gemini, `{"contents":[{"role":"user","parts":[{"text":"x"}]}],"tools":[{"googleSearch":{}}]}`, "googleSearch"},
		{"gemini url context", protocol.Gemini, `{"contents":[{"role":"user","parts":[{"text":"x"}]}],"tools":[{"urlContext":{}}]}`, "urlContext"},
		{"chat web search", protocol.Chat, `{"model":"m","messages":[{"role":"user","content":"x"}],"tools":[{"type":"web_search","web_search":{}}]}`, "web_search"},
	}
	for _, entry := range cases {
		t.Run(entry.name, func(t *testing.T) {
			request := decodeRequest(t, entry.source, []byte(entry.body))
			tool := request.Tools[0]
			if tool.Kind != protocol.ToolServer || tool.Server == nil || tool.Server.Type != entry.kind {
				t.Fatalf("not decoded as a server tool: %+v", tool)
			}
			again, _ := encodeRequest(t, entry.source, request)
			if missing := testkit.JSONMissing([]byte(entry.body), again, "model", "stream", "stream_options", "store", "include", "max_tokens", "input"); len(missing) != 0 {
				t.Fatalf("same-format replay changed the tool: %v\n%s", missing, again)
			}
			for _, target := range allFormats {
				if target == entry.source {
					continue
				}
				out, losses := encodeRequest(t, target, request)
				restored := decodeRequest(t, target, out)
				for _, other := range restored.Tools {
					if other.Kind != protocol.ToolServer {
						t.Fatalf("%s: a function tool appeared from nowhere: %+v", target, other)
					}
				}
				if len(losses) == 0 {
					t.Fatalf("%s: crossing formats with a server tool must be reported", target)
				}
			}
		})
	}
}

func TestWebSearchMapsAcrossFormats(t *testing.T) {
	pairs := []struct {
		source protocol.Format
		body   string
		target protocol.Format
		path   string
		want   any
	}{
		{protocol.Messages, `{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"x"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`, protocol.Gemini, "tools.0.googleSearch", map[string]any{}},
		{protocol.Gemini, `{"contents":[{"role":"user","parts":[{"text":"x"}]}],"tools":[{"googleSearch":{}}]}`, protocol.Responses, "tools.0.type", "web_search"},
		{protocol.Responses, `{"model":"m","input":"x","tools":[{"type":"web_search"}]}`, protocol.Messages, "tools.0.type", "web_search_20250305"},
	}
	for _, pair := range pairs {
		out, losses := encodeRequest(t, pair.target, decodeRequest(t, pair.source, []byte(pair.body)))
		testkit.AssertJSON(t, out, pair.path, pair.want)
		if lossCodes(losses)[protocol.LossToolKindMapped] != 1 {
			t.Fatalf("%s->%s: the mapping must be reported: %+v", pair.source, pair.target, losses)
		}
	}
}

func TestUnmappableServerToolIsDroppedWithoutBreakingTheRequest(t *testing.T) {
	request := decodeRequest(t, protocol.Messages, []byte(`{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"x"}],"tools":[{"type":"bash_20250124","name":"bash"},{"name":"read","description":"d","input_schema":{"type":"object"}}]}`))
	out, losses := encodeRequest(t, protocol.Chat, request)
	testkit.AssertJSON(t, out, "tools.0.function.name", "read")
	if len(protocol.Array(testkit.JSONValue(t, out, "tools"))) != 1 || lossCodes(losses)[protocol.LossFieldDropped] != 1 {
		t.Fatalf("the server tool must be dropped and reported: %s %+v", out, losses)
	}
}

func TestUnknownFieldsReplayInTheirOwnFormatOnly(t *testing.T) {
	cases := []struct {
		kind protocol.Format
		body string
		keys []string
	}{
		{protocol.Chat, `{"model":"m","logit_bias":{"1":2},"messages":[{"role":"user","name":"ann","content":"hi"}]}`, []string{"logit_bias", "messages.0.name"}},
		{protocol.Messages, `{"model":"m","max_tokens":8,"container":"c1","messages":[{"role":"user","x_hint":1,"content":[{"type":"text","text":"hi","x_part":true}]}]}`, []string{"container", "messages.0.x_hint", "messages.0.content.0.x_part"}},
		{protocol.Responses, `{"model":"m","input":[{"type":"message","role":"user","x_item":1,"content":[{"type":"input_text","text":"hi"}]}],"truncation":"auto","x_top":1}`, []string{"truncation", "x_top", "input.0.x_item"}},
		{protocol.Gemini, `{"safetySettings":[{"category":"A","threshold":"B"}],"generationConfig":{"candidateCount":1,"temperature":0.2},"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, []string{"safetySettings", "generationConfig.candidateCount"}},
	}
	for _, entry := range cases {
		t.Run(string(entry.kind), func(t *testing.T) {
			request := decodeRequest(t, entry.kind, []byte(entry.body))
			if request.Extras.Format != entry.kind || len(request.Extras.Fields) == 0 {
				t.Fatalf("request extras were not captured: %+v", request.Extras)
			}
			out, _ := encodeRequest(t, entry.kind, request)
			for _, key := range entry.keys {
				if testkit.JSONValue(t, out, key) == nil {
					t.Fatalf("%s was not replayed: %s", key, out)
				}
			}
			if entry.kind == protocol.Gemini {
				testkit.AssertJSON(t, out, "generationConfig.temperature", 0.2)
			}
			for _, target := range allFormats {
				if target == entry.kind {
					continue
				}
				out, losses := encodeRequest(t, target, request)
				if lossCodes(losses)[protocol.LossExtrasDropped] == 0 {
					t.Fatalf("%s: dropped extras must be reported", target)
				}
				if strings.Contains(string(out), "x_part") || strings.Contains(string(out), "logit_bias") {
					t.Fatalf("%s: foreign fields leaked: %s", target, out)
				}
			}
		})
	}
}

func TestResponseExtrasReplayInTheirOwnFormat(t *testing.T) {
	body := []byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"m","system_fingerprint":"fp_1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	response, _, err := codecOf(t, protocol.Chat).DecodeResponse(body, peerOf(format.Upstream, protocol.Chat))
	if err != nil {
		t.Fatal(err)
	}
	out, losses, err := codecOf(t, protocol.Chat).EncodeResponse(response, peerOf(format.Client, protocol.Chat))
	if err != nil || len(losses) != 0 {
		t.Fatalf("%v %+v", err, losses)
	}
	testkit.AssertJSON(t, out, "system_fingerprint", "fp_1")
	_, losses, err = codecOf(t, protocol.Messages).EncodeResponse(response, peerOf(format.Client, protocol.Messages))
	if err != nil || lossCodes(losses)[protocol.LossExtrasDropped] != 1 {
		t.Fatalf("%v %+v", err, losses)
	}
}

func TestSystemPromptIsNotDuplicated(t *testing.T) {
	request := decodeRequest(t, protocol.Messages, []byte(`{"model":"m","max_tokens":8,"system":"be brief","messages":[{"role":"user","content":"hi"}]}`))
	if request.Instructions != "" || len(request.Messages) != 2 || request.Messages[0].Role != protocol.RoleSystem {
		t.Fatalf("system must map to exactly one system message: %+v", request)
	}
	for _, target := range allFormats {
		out, _ := encodeRequest(t, target, request)
		if count := strings.Count(string(out), "be brief"); count != 1 {
			t.Fatalf("%s: system prompt appears %d times: %s", target, count, out)
		}
	}
	request.Instructions = "extra rules"
	out, _ := encodeRequest(t, protocol.Messages, request)
	text := string(out)
	if strings.Index(text, "extra rules") < 0 || strings.Index(text, "be brief") < 0 || strings.Index(text, "extra rules") > strings.Index(text, "be brief") {
		t.Fatalf("instructions and system messages must both be sent, instructions first: %s", out)
	}
}

func TestUnsignedReasoningKeepsItsNativeKind(t *testing.T) {
	request := decodeRequest(t, protocol.Chat, []byte(`{"model":"m","messages":[{"role":"user","content":"weather"},{"role":"assistant","content":null,"reasoning_content":"call the tool","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"22C"}]}`))
	messagesBody, _ := encodeRequest(t, protocol.Messages, request)
	testkit.AssertJSON(t, messagesBody, "messages.1.content.0", map[string]any{"type": "thinking", "thinking": "call the tool"})
	responsesBody, _ := encodeRequest(t, protocol.Responses, request)
	testkit.AssertJSON(t, responsesBody, "input.1", map[string]any{"type": "reasoning", "summary": []any{}, "content": []any{map[string]any{"type": "reasoning_text", "text": "call the tool"}}})
}

func TestResponsesTurnItemsShareOneAssistantMessage(t *testing.T) {
	request := decodeRequest(t, protocol.Responses, []byte(`{"model":"m","input":[{"role":"user","content":"weather"},{"type":"reasoning","id":"rs_1","summary":[],"content":[{"type":"reasoning_text","text":"call the tool"}]},{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"},{"type":"function_call_output","call_id":"call_1","output":"22C"}]}`))
	body, _ := encodeRequest(t, protocol.Chat, request)
	testkit.AssertJSON(t, body, "messages.1.reasoning_content", "call the tool")
	testkit.AssertJSON(t, body, "messages.1.tool_calls.0.function.name", "get_weather")
	testkit.AssertJSON(t, body, "messages.2.role", "tool")
}

func TestToolsWithoutParametersGetAnEmptyObjectSchema(t *testing.T) {
	request := &protocol.Request{Model: "m", Messages: []protocol.Message{{Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "x"}}}}, Tools: []protocol.Tool{{Kind: protocol.ToolFunction, Name: "now"}}}
	for target, path := range map[protocol.Format]string{protocol.Messages: "tools.0.input_schema", protocol.Responses: "tools.0.parameters"} {
		out, _ := encodeRequest(t, target, request)
		testkit.AssertJSON(t, out, path, map[string]any{"type": "object", "properties": map[string]any{}})
	}
}

func TestResponseFormatReachesMessagesAsOutputConfig(t *testing.T) {
	request := decodeRequest(t, protocol.Chat, []byte(`{"model":"m","messages":[{"role":"user","content":"x"}],"response_format":{"type":"json_schema","json_schema":{"name":"out","strict":true,"schema":{"type":"object","properties":{"a":{"type":"string"}}}}}}`))
	out, losses := encodeRequest(t, protocol.Messages, request)
	testkit.AssertJSON(t, out, "output_config.format.type", "json_schema")
	testkit.AssertJSON(t, out, "output_config.format.schema.properties.a.type", "string")
	if testkit.JSONValue(t, out, "output_format") != nil {
		t.Fatalf("the retired output_format field must not be sent: %s", out)
	}
	if lossCodes(losses)[protocol.LossFieldDropped] == 0 {
		t.Fatalf("the dropped schema name must be reported: %+v", losses)
	}
	back := decodeRequest(t, protocol.Messages, out)
	if back.ResponseFormat == nil || back.ResponseFormat.Kind != "json_schema" {
		t.Fatalf("the schema did not round trip: %+v", back.ResponseFormat)
	}
}

func TestAllowedToolsKeepTheirSemantics(t *testing.T) {
	body := `{"model":"m","messages":[{"role":"user","content":"x"}],"tools":[{"type":"function","function":{"name":"a","parameters":{"type":"object"}}},{"type":"function","function":{"name":"b","parameters":{"type":"object"}}}],"tool_choice":{"type":"allowed_tools","allowed_tools":{"mode":"required","tools":[{"type":"function","function":{"name":"a"}}]}}}`
	request := decodeRequest(t, protocol.Chat, []byte(body))
	if request.ToolChoice.Mode != "allowed" || !request.ToolChoice.Required || len(request.ToolChoice.Names) != 1 {
		t.Fatalf("unexpected choice: %+v", request.ToolChoice)
	}
	again, _ := encodeRequest(t, protocol.Chat, request)
	testkit.AssertJSON(t, again, "tool_choice.allowed_tools.mode", "required")
	out, _ := encodeRequest(t, protocol.Responses, request)
	testkit.AssertJSON(t, out, "tool_choice.mode", "required")
	testkit.AssertJSON(t, out, "tool_choice.tools.0.name", "a")
	out, _ = encodeRequest(t, protocol.Gemini, request)
	testkit.AssertJSON(t, out, "toolConfig.functionCallingConfig.mode", "ANY")
	testkit.AssertJSON(t, out, "toolConfig.functionCallingConfig.allowedFunctionNames", []any{"a"})
	out, losses := encodeRequest(t, protocol.Messages, request)
	testkit.AssertJSON(t, out, "tool_choice.type", "any")
	if len(protocol.Array(testkit.JSONValue(t, out, "tools"))) != 1 || lossCodes(losses)[protocol.LossFieldDropped] == 0 {
		t.Fatalf("Messages must receive only the allowed tools and report it: %s %+v", out, losses)
	}
	back := decodeRequest(t, protocol.Gemini, mustEncode(t, protocol.Gemini, request))
	if back.ToolChoice.Mode != "tool" && !(back.ToolChoice.Mode == "allowed" || len(back.ToolChoice.Names) == 1) {
		t.Fatalf("Gemini choice did not decode back: %+v", back.ToolChoice)
	}
}

func mustEncode(t testing.TB, kind protocol.Format, request *protocol.Request) []byte {
	body, _ := encodeRequest(t, kind, request)
	return body
}

func TestFileIdentifiersKeepTheirMediaKind(t *testing.T) {
	request := decodeRequest(t, protocol.Responses, []byte(`{"model":"m","input":[{"role":"user","content":[{"type":"input_file","file_id":"file_1"},{"type":"input_image","file_id":"file_2"}]}]}`))
	out, _ := encodeRequest(t, protocol.Messages, request)
	testkit.AssertJSON(t, out, "messages.0.content.0", map[string]any{"type": "document", "source": map[string]any{"type": "file", "file_id": "file_1"}})
	testkit.AssertJSON(t, out, "messages.0.content.1.type", "image")
	out, _ = encodeRequest(t, protocol.Responses, request)
	testkit.AssertJSON(t, out, "input.0.content.0", map[string]any{"type": "input_file", "file_id": "file_1"})
	if _, _, losses, err := codecOf(t, protocol.Gemini).EncodeRequest(request, peerOf(format.Upstream, protocol.Gemini)); err == nil {
		t.Fatalf("a provider file id cannot reach Gemini: %+v", losses)
	}
}

func TestClientSignatureIsOpenedAndVerified(t *testing.T) {
	issued := protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: "opaque-blob", TextDigest: protocol.TextDigest("consider")}
	sealed, err := testEnvelope.Seal(issued)
	if err != nil {
		t.Fatal(err)
	}
	client := peerOf(format.Client, protocol.Messages)
	reasoning := func(text, signature string) protocol.Block {
		return protocol.Block{Kind: protocol.KindReasoning, Text: text, Reasoning: &protocol.Reasoning{Visibility: "full", Signature: &protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: signature}}}
	}
	t.Run("intact", func(t *testing.T) {
		block := reasoning("consider", sealed)
		if losses := client.Unseal(&block); len(losses) != 0 {
			t.Fatalf("unseal: %+v", losses)
		}
		signature := block.Reasoning.Signature
		if signature == nil || signature.Blob != "opaque-blob" || signature.TextDigest != issued.TextDigest {
			t.Fatalf("the signature and its issued digest must survive: %+v", signature)
		}
	})
	t.Run("edited text", func(t *testing.T) {
		block := reasoning("something else", sealed)
		losses := client.Unseal(&block)
		if len(losses) != 1 || losses[0].Code != protocol.LossSignatureUnverified || block.Reasoning.Signature != nil {
			t.Fatalf("a signature issued for other text must be rejected: %+v %+v", losses, block.Reasoning)
		}
		if block.Text != "something else" {
			t.Fatal("the text must be kept")
		}
	})
	t.Run("forged", func(t *testing.T) {
		block := reasoning("consider", "not-an-envelope")
		losses := client.Unseal(&block)
		if len(losses) != 1 || losses[0].Code != protocol.LossSignatureUnverified || block.Reasoning.Signature != nil {
			t.Fatalf("%+v", losses)
		}
	})
	t.Run("foreign secret", func(t *testing.T) {
		other, _ := testkit.SealingEnvelope{Secret: []byte("another")}.Seal(issued)
		block := reasoning("consider", other)
		if losses := client.Unseal(&block); len(losses) != 1 || block.Reasoning.Signature != nil {
			t.Fatalf("%+v", losses)
		}
	})
	t.Run("sealed towards clients with the shown text digest", func(t *testing.T) {
		wire := client.WireSignature(&protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: "b", TextDigest: "stale"}, "shown")
		opened, ok := protocol.OpenSignature(testEnvelope.Secret, wire)
		if !ok || opened.TextDigest != protocol.TextDigest("shown") || opened.Blob != "b" {
			t.Fatalf("%+v %v", opened, ok)
		}
	})
	t.Run("raw blob towards upstream of the same vendor only", func(t *testing.T) {
		upstream := peerOf(format.Upstream, protocol.Messages)
		signature := &protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: "b"}
		if upstream.WireSignature(signature, "x") != "b" {
			t.Fatal("same vendor must receive the blob")
		}
		if peerOf(format.Upstream, protocol.Gemini).WireSignature(signature, "x") != "" {
			t.Fatal("another vendor must never receive the blob")
		}
	})
}

func TestUpstreamFinishAndStopSequenceMap(t *testing.T) {
	stopped := &protocol.Response{ResponseMeta: protocol.ResponseMeta{ID: "r", Model: "m"}, Finish: protocol.FinishStop, StopSequence: "###", Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "x"}}}
	out, _, err := codecOf(t, protocol.Messages).EncodeResponse(stopped, peerOf(format.Client, protocol.Messages))
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertJSON(t, out, "stop_reason", "stop_sequence")
	testkit.AssertJSON(t, out, "stop_sequence", "###")
	stopped.StopSequence = ""
	out, _, _ = codecOf(t, protocol.Messages).EncodeResponse(stopped, peerOf(format.Client, protocol.Messages))
	testkit.AssertJSON(t, out, "stop_reason", "end_turn")
	back, _, err := codecOf(t, protocol.Chat).DecodeResponse([]byte(`{"id":"c","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"x"},"finish_reason":"novel_reason"}]}`), peerOf(format.Upstream, protocol.Chat))
	if err != nil || back.Finish != protocol.FinishStop {
		t.Fatalf("unknown finish reasons degrade to stop: %+v %v", back, err)
	}
	_, losses, _ := codecOf(t, protocol.Chat).DecodeResponse([]byte(`{"id":"c","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"x"},"finish_reason":"novel_reason"}]}`), peerOf(format.Upstream, protocol.Chat))
	if lossCodes(losses)[protocol.LossFinishMapped] != 1 {
		t.Fatalf("the approximation must be reported: %+v", losses)
	}
}

func TestCallIDsAreSaltedPerCall(t *testing.T) {
	first, second := format.NewCallIDs(format.Peer{Salt: "turn-1"}), format.NewCallIDs(format.Peer{Salt: "turn-2"})
	unsalted := format.NewCallIDs(format.Peer{})
	if first.Assign("read", "{}") == second.Assign("read", "{}") {
		t.Fatal("identical calls of different turns must not collide")
	}
	if unsalted.Assign("read", "{}") != format.NewCallIDs(format.Peer{}).Assign("read", "{}") {
		t.Fatal("without a salt the identifier is deterministic")
	}
	repeated := format.NewCallIDs(format.Peer{})
	if repeated.Assign("read", "{}") == repeated.Assign("read", "{}") {
		t.Fatal("identical calls within one turn must differ")
	}
}

func TestErrorKindsMapToEachProtocolsVocabulary(t *testing.T) {
	cases := []struct {
		kind   string
		format protocol.Format
		path   string
		want   string
	}{
		{"unauthorized", protocol.Chat, "error.type", "invalid_request_error"},
		{"unauthorized", protocol.Responses, "error.type", "invalid_request_error"},
		{"unauthorized", protocol.Messages, "error.type", "authentication_error"},
		{"unauthorized", protocol.Gemini, "error.status", "UNAUTHENTICATED"},
		{"blocked", protocol.Gemini, "error.status", "PERMISSION_DENIED"},
		{"rate_limited", protocol.Chat, "error.type", "rate_limit_error"},
		{"rate_limited", protocol.Responses, "error.type", "rate_limit_error"},
		{"rate_limited", protocol.Messages, "error.type", "rate_limit_error"},
		{"rate_limited", protocol.Gemini, "error.status", "RESOURCE_EXHAUSTED"},
	}
	for _, entry := range cases {
		_, body := codecOf(t, entry.format).EncodeError(&protocol.Error{Kind: entry.kind, Code: "c", Message: "m"}, false)
		testkit.AssertJSON(t, body, entry.path, entry.want)
	}
}
