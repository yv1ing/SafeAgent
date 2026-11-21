package stream_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/chat"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/stream"
)

type collector struct {
	header http.Header
	body   strings.Builder
	status int
}

func (writer *collector) Header() http.Header { return writer.header }
func (writer *collector) WriteHeader(status int) {
	writer.status = status
}
func (writer *collector) Write(data []byte) (int, error) { return writer.body.Write(data) }
func (writer *collector) Flush()                         {}

type passthrough struct{}

func (passthrough) Text() stream.Restorer { return passthroughRestorer{} }
func (passthrough) JSON() stream.Restorer { return passthroughRestorer{} }

type passthroughRestorer struct{}

func (passthroughRestorer) Push(text string) string { return text }
func (passthroughRestorer) Flush() string           { return "" }

type replaceRestorer struct{ from, to string }

func (restorer replaceRestorer) Push(text string) string {
	return strings.ReplaceAll(text, restorer.from, restorer.to)
}
func (replaceRestorer) Flush() string { return "" }

type replaceRestorers struct{ from, to string }

func (restorers replaceRestorers) Text() stream.Restorer {
	return replaceRestorer(restorers)
}
func (restorers replaceRestorers) JSON() stream.Restorer { return restorers.Text() }

func conversation() *model.Response {
	return &model.Response{
		Meta: model.ResponseMeta{ID: "resp_1", Model: "m"},
		Items: []model.Item{
			{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "thinking"}}},
			{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "read", Arguments: `{"path":"/etc/passwd"}`}},
		},
		Finish: model.Finish{Kind: model.FinishToolCalls},
	}
}

func newSession(t *testing.T, response *model.Response, restorers stream.Restorers, gate stream.Gate, holdLimit int, raw bool) (*stream.Session, *collector) {
	t.Helper()
	writer := &collector{header: http.Header{}}
	meta := response.Meta
	session := stream.New(stream.Options{
		Encoder:    chat.New().NewStreamEncoder(meta, codec.Peer{Side: codec.Client, Vendor: model.VendorOpenAI, Model: "m", IncludeUsage: true}),
		Writer:     writer,
		Journal:    model.NewJournal(),
		Restorers:  restorers,
		Gate:       gate,
		Format:     model.Chat,
		ResponseID: meta.ID, ResponseModel: meta.Model,
		Raw: raw, HoldLimit: holdLimit, PreCommitBytes: 1,
	})
	return session, writer
}

func feed(t *testing.T, session *stream.Session, response *model.Response) *model.Error {
	t.Helper()
	for _, event := range stream.Events(response) {
		if failure := session.Handle([]model.Event{event}, nil); failure != nil {
			return failure
		}
	}
	return nil
}

func TestToolCallIsReleasedAfterApproval(t *testing.T) {
	var judged bool
	session, writer := newSession(t, conversation(), passthrough{}, func(model.Item) *model.Error {
		judged = true
		return nil
	}, 1<<20, false)
	if failure := feed(t, session, conversation()); failure != nil {
		t.Fatal(failure)
	}
	if err := session.StageComplete(); err != nil {
		t.Fatal(err)
	}
	session.WriteStaged()
	if !judged {
		t.Fatal("the gate must judge the complete tool call")
	}
	if !strings.Contains(writer.body.String(), "/etc/passwd") {
		t.Fatalf("an approved call must reach the client: %s", writer.body.String())
	}
}

func TestRejectedToolCallIsWithheldAndReported(t *testing.T) {
	session, writer := newSession(t, conversation(), passthrough{}, func(model.Item) *model.Error {
		return &model.Error{Kind: "blocked", Class: model.ClassPolicy, Code: "tool_call_blocked", Message: "blocked", Status: 403}
	}, 1<<20, false)
	failure := feed(t, session, conversation())
	if failure == nil || failure.Code != "tool_call_blocked" {
		t.Fatalf("the gate verdict must end the stream: %+v", failure)
	}
	session.StageFailure(failure)
	session.WriteStaged()
	body := writer.body.String()
	if strings.Contains(body, "/etc/passwd") {
		t.Fatalf("a rejected call must not reach the client: %s", body)
	}
	if !strings.Contains(body, "tool_call_blocked") {
		t.Fatalf("the client must receive a protocol error: %s", body)
	}
}

func TestToolHoldOverflowIsAResourceError(t *testing.T) {
	session, _ := newSession(t, conversation(), passthrough{}, func(model.Item) *model.Error { return nil }, 16, false)
	failure := feed(t, session, conversation())
	if failure == nil || failure.Code != "tool_call_too_large" {
		t.Fatalf("an oversized hold must be a resource error: %+v", failure)
	}
}

func TestFailureBeforeContentIsNotCommitted(t *testing.T) {
	response := conversation()
	response.Items = response.Items[1:]
	session, writer := newSession(t, response, passthrough{}, func(model.Item) *model.Error {
		return &model.Error{Kind: "blocked", Class: model.ClassPolicy, Code: "tool_call_blocked", Message: "blocked", Status: 403}
	}, 1<<20, false)
	if failure := feed(t, session, response); failure == nil {
		t.Fatal("expected a verdict")
	}
	if session.Committed() {
		t.Fatal("a tool call before any content must not commit the response")
	}
	if writer.status != 0 {
		t.Fatalf("no HTTP status may be written before content: %d", writer.status)
	}
}

func TestRestorationAppliesToStreamedText(t *testing.T) {
	response := &model.Response{
		Meta:  model.ResponseMeta{ID: "resp_1", Model: "m"},
		Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "PH"}}}},
	}
	session, writer := newSession(t, response, replaceRestorers{from: "PH", to: "secret"}, nil, 1<<20, false)
	if failure := feed(t, session, response); failure != nil {
		t.Fatal(failure)
	}
	if err := session.StageComplete(); err != nil {
		t.Fatal(err)
	}
	session.WriteStaged()
	if !strings.Contains(writer.body.String(), "secret") || strings.Contains(writer.body.String(), "PH") {
		t.Fatalf("placeholders must be restored: %s", writer.body.String())
	}
}

func TestRawFramesAreForwardedUntilAnUnsafeEvent(t *testing.T) {
	response := &model.Response{
		Meta:  model.ResponseMeta{ID: "chatcmpl-1", Model: "m"},
		Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "hi"}}}},
	}
	session, writer := newSession(t, response, passthrough{}, nil, 1<<20, true)
	frame := codec.Frame{Data: []byte(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"}}]}`)}
	for _, event := range stream.Events(response) {
		if failure := session.Handle([]model.Event{event}, &frame); failure != nil {
			t.Fatal(failure)
		}
	}
	session.WriteStaged()
	if !strings.Contains(writer.body.String(), `"chat.completion.chunk"`) {
		t.Fatalf("a raw-safe frame must reach the client unchanged: %s", writer.body.String())
	}
}

type bindingRestorers struct{}

func (bindingRestorers) Text() stream.Restorer { return textBindingRestorer{} }
func (bindingRestorers) JSON() stream.Restorer { return jsonBindingRestorer{} }

type textBindingRestorer struct{}

func (textBindingRestorer) Push(text string) string {
	return strings.ReplaceAll(text, "[TOKEN]", `a"b`)
}
func (textBindingRestorer) Flush() string { return "" }

type jsonBindingRestorer struct{}

func (jsonBindingRestorer) Push(text string) string {
	return strings.ReplaceAll(text, "[TOKEN]", `a\"b`)
}
func (jsonBindingRestorer) Flush() string { return "" }

func TestLoweredCustomToolUsesTheJSONRestorer(t *testing.T) {
	session := stream.New(stream.Options{
		Encoder: stream.NopEncoder{}, Writer: stream.NewDiscardWriter(), Journal: model.NewJournal(),
		Restorers: bindingRestorers{}, ToolKinds: map[string]model.ToolKind{"apply": model.ToolCustom},
		Format: model.Chat, HoldLimit: 1 << 20,
	})
	started := model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "apply"}}
	stopped := model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "apply", Arguments: `{"input":"[TOKEN]"}`}}
	for _, event := range []model.Event{
		{Kind: model.EventItemStart, ItemIndex: 0, Item: &started},
		{Kind: model.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartToolArguments}},
		{Kind: model.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: `{"input":"[TOKEN]"}`},
		{Kind: model.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartToolArguments, Text: `{"input":"[TOKEN]"}`}},
		{Kind: model.EventItemStop, ItemIndex: 0, Item: &stopped},
	} {
		if failure := session.Handle([]model.Event{event}, nil); failure != nil {
			t.Fatalf("%s: %v", event.Kind, failure)
		}
	}
	session.Close()
	item := session.Journal().Response(model.Chat).Items[0]
	if item.ToolCall == nil || item.ToolCall.Kind != model.ToolCustom || item.ToolCall.Arguments != `a"b` {
		t.Fatalf("the lowered call must be restored as JSON and unwrapped: %+v", item.ToolCall)
	}
}
