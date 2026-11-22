package stream_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
		Raw: raw, HoldLimit: holdLimit,
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

func TestReviewBlocksBeforeTheFirstByte(t *testing.T) {
	response := &model.Response{
		Meta:  model.ResponseMeta{ID: "chatcmpl-1", Model: "m"},
		Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "bad content"}}}},
	}
	writer := &collector{header: http.Header{}}
	reviews := 0
	session := stream.New(stream.Options{
		Encoder:   chat.New().NewStreamEncoder(response.Meta, codec.Peer{Side: codec.Client, Vendor: model.VendorOpenAI, Model: "m"}),
		Writer:    writer,
		Journal:   model.NewJournal(),
		Restorers: passthrough{},
		Review: func(input stream.ReviewInput) *model.Error {
			reviews++
			if input.Items != 1 || input.ToolCalls != 0 {
				t.Fatalf("unexpected review input: %+v", input)
			}
			if strings.Contains(input.Text, "bad") {
				return &model.Error{Kind: "blocked", Class: model.ClassPolicy, Code: "response_blocked", Message: "blocked", Status: 403}
			}
			return nil
		},
		ReviewBytes: 4, ReviewTimeout: time.Hour,
		ResponseID: "chatcmpl-1", ResponseModel: "m", HoldLimit: 1 << 20,
	})
	failure := feed(t, session, response)
	if failure == nil || failure.Code != "response_blocked" {
		t.Fatalf("expected a review block, got %+v", failure)
	}
	if reviews != 1 {
		t.Fatalf("review ran %d times", reviews)
	}
	if session.Committed() || writer.status != 0 || writer.body.Len() != 0 {
		t.Fatalf("a blocked review must not commit: status=%d body=%q", writer.status, writer.body.String())
	}
}

func TestReviewReleasesTheWindowAndContinues(t *testing.T) {
	response := &model.Response{
		Meta:  model.ResponseMeta{ID: "chatcmpl-1", Model: "m"},
		Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "safe content"}}}},
	}
	writer := &collector{header: http.Header{}}
	session := stream.New(stream.Options{
		Encoder:     chat.New().NewStreamEncoder(response.Meta, codec.Peer{Side: codec.Client, Vendor: model.VendorOpenAI, Model: "m"}),
		Writer:      writer,
		Journal:     model.NewJournal(),
		Restorers:   passthrough{},
		Review:      func(stream.ReviewInput) *model.Error { return nil },
		ReviewBytes: 2, ReviewTimeout: time.Hour,
		ResponseID: "chatcmpl-1", ResponseModel: "m", HoldLimit: 1 << 20,
	})
	if failure := feed(t, session, response); failure != nil {
		t.Fatal(failure)
	}
	if !session.Committed() || writer.status != http.StatusOK {
		t.Fatalf("an allowed review must commit: committed=%v status=%d", session.Committed(), writer.status)
	}
	if !strings.Contains(writer.body.String(), "safe content") {
		t.Fatalf("the reviewed window must reach the client: %s", writer.body.String())
	}
}

func TestReviewTimeoutIsCheckedOnTheNextFrame(t *testing.T) {
	response := &model.Response{
		Meta:  model.ResponseMeta{ID: "chatcmpl-1", Model: "m"},
		Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "ok"}}}},
	}
	writer := &collector{header: http.Header{}}
	reviews := 0
	session := stream.New(stream.Options{
		Encoder:   chat.New().NewStreamEncoder(response.Meta, codec.Peer{Side: codec.Client, Vendor: model.VendorOpenAI, Model: "m"}),
		Writer:    writer,
		Journal:   model.NewJournal(),
		Restorers: passthrough{},
		Review: func(stream.ReviewInput) *model.Error {
			reviews++
			return nil
		},
		ReviewBytes: 1024, ReviewTimeout: time.Nanosecond,
		ResponseID: "chatcmpl-1", ResponseModel: "m", HoldLimit: 1 << 20,
	})
	if failure := feed(t, session, response); failure != nil {
		t.Fatal(failure)
	}
	if reviews != 1 || !session.Committed() || writer.status != http.StatusOK {
		t.Fatalf("reviews=%d committed=%v status=%d", reviews, session.Committed(), writer.status)
	}
}

func TestReviewWindowKeepsUTF8Intact(t *testing.T) {
	response := &model.Response{
		Meta:  model.ResponseMeta{ID: "chatcmpl-1", Model: "m"},
		Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "你好"}}}},
	}
	writer := &collector{header: http.Header{}}
	reviewed := ""
	session := stream.New(stream.Options{
		Encoder:   chat.New().NewStreamEncoder(response.Meta, codec.Peer{Side: codec.Client, Vendor: model.VendorOpenAI, Model: "m"}),
		Writer:    writer,
		Journal:   model.NewJournal(),
		Restorers: passthrough{},
		Review: func(input stream.ReviewInput) *model.Error {
			reviewed = input.Text
			if !utf8.ValidString(input.Text) {
				t.Fatalf("the review window split a rune: %q", input.Text)
			}
			return nil
		},
		ReviewBytes: 2, ReviewTimeout: time.Hour,
		ResponseID: "chatcmpl-1", ResponseModel: "m", HoldLimit: 1 << 20,
	})
	if failure := feed(t, session, response); failure != nil {
		t.Fatal(failure)
	}
	if reviewed != "你" {
		t.Fatalf("reviewed text = %q", reviewed)
	}
	if !strings.Contains(writer.body.String(), "你好") {
		t.Fatalf("the full text must reach the client: %s", writer.body.String())
	}
}

func TestReviewWindowKeepsAContiguousUTF8Prefix(t *testing.T) {
	response := &model.Response{
		Meta:  model.ResponseMeta{ID: "chatcmpl-1", Model: "m"},
		Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "ab你好"}}}},
	}
	writer := &collector{header: http.Header{}}
	reviewed := ""
	session := stream.New(stream.Options{
		Encoder:   chat.New().NewStreamEncoder(response.Meta, codec.Peer{Side: codec.Client, Vendor: model.VendorOpenAI, Model: "m"}),
		Writer:    writer,
		Journal:   model.NewJournal(),
		Restorers: passthrough{},
		Review: func(input stream.ReviewInput) *model.Error {
			reviewed = input.Text
			return nil
		},
		ReviewBytes: 3, ReviewTimeout: time.Hour,
		ResponseID: "chatcmpl-1", ResponseModel: "m", HoldLimit: 1 << 20,
	})
	if failure := feed(t, session, response); failure != nil {
		t.Fatal(failure)
	}
	if reviewed != "ab你" {
		t.Fatalf("reviewed text = %q", reviewed)
	}
}

func TestRawReviewReportsTheJournalItemCount(t *testing.T) {
	writer := &collector{header: http.Header{}}
	items := -1
	session := stream.New(stream.Options{
		Encoder:   chat.New().NewStreamEncoder(model.ResponseMeta{ID: "chatcmpl-1", Model: "m"}, codec.Peer{Side: codec.Client, Vendor: model.VendorOpenAI, Model: "m"}),
		Writer:    writer,
		Journal:   model.NewJournal(),
		Restorers: passthrough{},
		Format:    model.Chat, Raw: true,
		Review: func(input stream.ReviewInput) *model.Error {
			items = input.Items
			return nil
		},
		ReviewBytes: 5, ReviewTimeout: time.Hour,
		ResponseID: "chatcmpl-1", ResponseModel: "m", HoldLimit: 1 << 20,
	})
	frame := codec.Frame{Data: []byte(`{"ok":true}`)}
	events := []model.Event{
		{Kind: model.EventMessageStart, Meta: &model.ResponseMeta{ID: "chatcmpl-1", Model: "m"}},
		{Kind: model.EventItemStart, ItemIndex: 0, Item: &model.Item{Kind: model.ItemMessage, Role: model.RoleAssistant}},
		{Kind: model.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartText}},
		{Kind: model.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "hello"},
		{Kind: model.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartText}},
		{Kind: model.EventItemStop, ItemIndex: 0, Item: &model.Item{Kind: model.ItemMessage, Role: model.RoleAssistant}},
		{Kind: model.EventMessageStop},
	}
	if failure := session.Handle(events, &frame); failure != nil {
		t.Fatal(failure)
	}
	if items != 1 {
		t.Fatalf("raw review item count = %d", items)
	}
}

func TestPruneUndeliveredDropsAHeldSibling(t *testing.T) {
	writer := &collector{header: http.Header{}}
	session := stream.New(stream.Options{
		Encoder:   chat.New().NewStreamEncoder(model.ResponseMeta{ID: "chatcmpl-1", Model: "m"}, codec.Peer{Side: codec.Client, Vendor: model.VendorOpenAI, Model: "m"}),
		Writer:    writer,
		Journal:   model.NewJournal(),
		Restorers: passthrough{},
		Gate: func(item model.Item) *model.Error {
			if item.ToolCall != nil && item.ToolCall.ID == "call_1" {
				return &model.Error{Kind: "blocked", Class: model.ClassPolicy, Code: "tool_call_blocked", Message: "blocked", Status: 403}
			}
			return nil
		},
		Format: model.Chat, ResponseID: "chatcmpl-1", ResponseModel: "m", HoldLimit: 1 << 20,
	})
	events := []model.Event{
		{Kind: model.EventMessageStart, Meta: &model.ResponseMeta{ID: "chatcmpl-1", Model: "m"}},
		{Kind: model.EventItemStart, ItemIndex: 0, Item: &model.Item{Kind: model.ItemMessage, Role: model.RoleAssistant}},
		{Kind: model.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartText}},
		{Kind: model.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: "thinking"},
		{Kind: model.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartText}},
		{Kind: model.EventItemStop, ItemIndex: 0, Item: &model.Item{Kind: model.ItemMessage, Role: model.RoleAssistant}},
		{Kind: model.EventItemStart, ItemIndex: 1, Item: &model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "one"}}},
		{Kind: model.EventPartStart, ItemIndex: 1, PartIndex: 0, Part: &model.Part{Kind: model.PartToolArguments}},
		{Kind: model.EventItemStart, ItemIndex: 2, Item: &model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_2", Name: "two"}}},
		{Kind: model.EventPartStart, ItemIndex: 2, PartIndex: 0, Part: &model.Part{Kind: model.PartToolArguments}},
		{Kind: model.EventPartDelta, ItemIndex: 1, PartIndex: 0, Delta: `{"a":1}`},
		{Kind: model.EventPartDelta, ItemIndex: 2, PartIndex: 0, Delta: `{"b":2}`},
		{Kind: model.EventPartStop, ItemIndex: 1, PartIndex: 0, Part: &model.Part{Kind: model.PartToolArguments}},
		{Kind: model.EventItemStop, ItemIndex: 1},
	}
	var failure *model.Error
	for _, event := range events {
		if failure = session.Handle([]model.Event{event}, nil); failure != nil {
			break
		}
	}
	if failure == nil || failure.Code != "tool_call_blocked" {
		t.Fatalf("the gate must stop the stream: %+v", failure)
	}
	if !session.Committed() {
		t.Fatal("the text must have committed the stream")
	}
	session.PruneUndelivered()
	for _, item := range session.Journal().Response(model.Chat).Items {
		if item.ToolCall != nil && item.ToolCall.ID == "call_2" {
			t.Fatal("a held sibling tool call must not survive the audit projection")
		}
	}
	if strings.Contains(writer.body.String(), "call_2") {
		t.Fatal("the client must not receive the held sibling")
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
