package observe_test

import (
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observe"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
)

func request() *protocol.Request {
	return &protocol.Request{
		Model:        "gpt-5",
		Instructions: "be brief",
		Messages: []protocol.Message{{
			Role: protocol.RoleUser,
			Blocks: []protocol.Block{{
				Kind: protocol.KindText,
				Text: "mail me@example.com",
			}},
		}},
		Tools: []protocol.Tool{{Kind: protocol.ToolFunction, Name: "send", Description: "post to a URL"}},
	}
}

func TestTraceCarriesBothViewsAndNormalizedTools(t *testing.T) {
	redactor := func(text, path string) (string, []observation.Span) {
		if path == "messages[0].blocks[0].text" {
			return "mail [EMAIL_1]", []observation.Span{{Start: 5, End: 19, Label: "EMAIL"}}
		}
		return text, nil
	}
	trace := observe.Request(request(), observe.Options{
		TraceID: "t1", Principal: observation.Principal{ProjectID: "p", AgentKeyID: "k"}, Redactor: redactor,
	})
	if trace.Task != "be brief" {
		t.Fatalf("instructions must become the task: %q", trace.Task)
	}
	items := trace.Items()
	var user observation.Item
	for _, item := range items {
		if item.Kind == observation.ItemText && item.Locator.Section == "message" {
			user = item
		}
	}
	if user.Text.Raw != "mail me@example.com" || user.Text.Shown != "mail [EMAIL_1]" || len(user.Text.Redactions) != 1 {
		t.Fatalf("dual views are wrong: %+v", user.Text)
	}
	if len(trace.Tools) != 1 || trace.Tools[0].Name != "send" {
		t.Fatalf("tool catalogue missing: %+v", trace.Tools)
	}
}

func TestToolTargetAndSteps(t *testing.T) {
	input := map[string]any{"url": "https://API.Example.com/collect?token=1"}
	target := observe.TargetOf("send", input)
	if target.Kind != "url" || target.Host != "api.example.com" || target.Path != "/collect" {
		t.Fatalf("target normalization: %+v", target)
	}
	withPort := observe.TargetOf("send", map[string]any{"url": "https://evil.example:8443/collect"})
	if withPort.Host != "evil.example" || withPort.Value != "https://evil.example:8443/collect" {
		t.Fatalf("host must omit the port while the value keeps it: %+v", withPort)
	}
	barePort := observe.TargetOf("send", map[string]any{"host": "evil.example:8443"})
	if barePort.Host != "evil.example" {
		t.Fatalf("a bare host with a port must normalize without it: %+v", barePort)
	}
	if got := observe.Capabilities("http_post", target); len(got) == 0 {
		t.Fatal("an HTTP tool must expose a network capability")
	}
	file := observe.TargetOf("read_file", map[string]any{"path": "~/secrets/../.ssh/id_rsa"})
	if file.Kind != "file" || file.Path == "" {
		t.Fatalf("file target: %+v", file)
	}
}

func TestStoredTraceReplaysFromAuditPayloads(t *testing.T) {
	response := auditResponse()
	trace := observe.Stored(auditRequest(), &response, observe.Options{TraceID: "c"})
	if len(trace.Turns) < 2 {
		t.Fatalf("turns were not replayed: %+v", trace.Turns)
	}
	if trace.Turns[len(trace.Turns)-1].Role != observation.RoleAssistant {
		t.Fatalf("the response turn is missing: %+v", trace.Turns)
	}
	if trace.Outcome.Finish != string(protocol.FinishStop) {
		t.Fatalf("finish was not carried: %+v", trace.Outcome)
	}
}

func auditRequest() auditevent.Request {
	return auditevent.Request{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "hi"}}}},
		Params:   protocol.Request{Model: "gpt-5"},
		Tools:    []protocol.Tool{{Kind: protocol.ToolFunction, Name: "read"}},
	}
}

func auditResponse() auditevent.Response {
	return auditevent.Response{Response: protocol.Response{
		ResponseMeta: protocol.ResponseMeta{ID: "r", Model: "gpt-5"},
		Blocks:       []protocol.Block{{Kind: protocol.KindText, Text: "hello"}},
		Finish:       protocol.FinishStop,
	}}
}

func TestHistoryMarksMissingAncestors(t *testing.T) {
	request := auditevent.Request{Messages: []protocol.Message{{
		Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "current"}},
	}}}
	response := &auditevent.Response{Response: protocol.Response{Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "answer"}}}}
	ancestor := auditevent.Request{Messages: []protocol.Message{{
		Role: protocol.RoleUser, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "earlier"}},
	}}}
	expanded, truncated := observe.History(request, []observe.Ancestor{{Found: true, Request: ancestor, Response: response}})
	if truncated || len(expanded.Messages) != 3 || expanded.Messages[1].Role != protocol.RoleAssistant {
		t.Fatalf("history was not expanded: %+v %v", expanded.Messages, truncated)
	}
	if _, truncated := observe.History(request, []observe.Ancestor{{Found: false}}); !truncated {
		t.Fatal("a missing ancestor must be reported, not silently skipped")
	}
}

func TestSinkInferenceMatchesNameWords(t *testing.T) {
	if sink := observe.SinkOf("truncate", observation.Target{Kind: "tool", Value: "truncate"}, map[string]any{"text": "hello"}); sink != observation.SinkUnknown {
		t.Fatalf("truncate must not be inferred as a shell tool: %s", sink)
	}
	if sink := observe.SinkOf("run_shell", observation.Target{Kind: "tool", Value: "run_shell"}, map[string]any{"command": "ls"}); sink != observation.SinkShellExec {
		t.Fatalf("a shell tool was not recognized: %s", sink)
	}
	if sink := observe.SinkOf("sendEmail", observation.Target{Kind: "host", Value: "example.com"}, map[string]any{"url": "https://example.com"}); sink != observation.SinkExternalSend {
		t.Fatalf("a camel-case send tool was not recognized: %s", sink)
	}
	if sink := observe.SinkOf("sendEmail", observation.Target{Kind: "tool", Value: "sendEmail"}, map[string]any{"to": "a@b"}); sink != observation.SinkExternalSend {
		t.Fatalf("a camel-case send tool without a URL target was not recognized: %s", sink)
	}
	if sink := observe.SinkOf("writeFile", observation.Target{Kind: "file", Value: "/tmp/x", Path: "/tmp/x"}, map[string]any{"path": "/tmp/x"}); sink != observation.SinkFileWrite {
		t.Fatalf("a camel-case write tool was not recognized: %s", sink)
	}
	for _, name := range []string{"postgres_query", "payload_inspector", "runtime_info"} {
		if sink := observe.SinkOf(name, observation.Target{Kind: "tool", Value: name}, map[string]any{"value": "x"}); sink != observation.SinkUnknown {
			t.Fatalf("%s must not be inferred as a side effect: %s", name, sink)
		}
	}
}

func TestToolTargetUsesTheRedactedView(t *testing.T) {
	redactor := func(text, path string) (string, []observation.Span) {
		if path == "messages[0].blocks[0].tool_call.arguments" {
			shown := `{"url":"https://[SECRET_1]/collect"}`
			return shown, []observation.Span{{Start: 15, End: 25, Label: "SECRET"}}
		}
		return text, nil
	}
	request := &protocol.Request{Model: "gpt-5", Messages: []protocol.Message{{
		Role: protocol.RoleAssistant,
		Blocks: []protocol.Block{{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{
			ID: "call_1", Name: "http_post", Kind: protocol.ToolFunction,
			Arguments: `{"url":"https://secret.example/collect"}`,
		}}},
	}}}
	trace := observe.Request(request, observe.Options{TraceID: "t", Redactor: redactor})
	item := trace.Items()[0]
	if item.ToolCall == nil || !strings.Contains(item.ToolCall.TargetShown.Value, "[SECRET_1]") {
		t.Fatalf("the shown target must come from the redacted arguments: %+v", item.ToolCall)
	}
	if item.ToolCall.Target.Host != "secret.example" {
		t.Fatalf("the raw target must stay available: %+v", item.ToolCall.Target)
	}
}
