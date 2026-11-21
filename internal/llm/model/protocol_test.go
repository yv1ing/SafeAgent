package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

var secret = []byte("test-secret")

func text(role model.Role, value string) model.Item {
	return model.MessageItem(role, value)
}

func TestObjectReadsRawMessages(t *testing.T) {
	object := model.Object(json.RawMessage(`{"a":1,"b":{"c":"d"}}`))
	if object == nil || model.String(model.Object(object["b"])["c"]) != "d" {
		t.Fatalf("a raw JSON object must decode: %+v", object)
	}
	for _, value := range []any{json.RawMessage(`[1]`), json.RawMessage(`not json`), json.RawMessage(nil), "text", nil, 3} {
		if model.Object(value) != nil {
			t.Fatalf("%#v is not an object", value)
		}
	}
	if model.Object(map[string]any{"k": 1}) == nil {
		t.Fatal("plain maps must pass through")
	}
}

func TestDecodeJSONRejectsTrailingValuesAndKeepsNumbers(t *testing.T) {
	if _, err := model.DecodeJSON([]byte(`{} {}`)); err == nil {
		t.Fatal("two values must be rejected")
	}
	if _, err := model.DecodeJSON([]byte("\"\xff\"")); err == nil {
		t.Fatal("invalid UTF-8 must be rejected")
	}
	object, err := model.DecodeObject([]byte(`{"big":12345678901234567890,"n":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if number, ok := model.Integer(object["n"]); !ok || number != 3 {
		t.Fatalf("%v %v", number, ok)
	}
	if string(model.Marshal(object)) != `{"big":12345678901234567890,"n":3}` {
		t.Fatalf("large numbers must keep their digits: %s", model.Marshal(object))
	}
}

func TestSignatureEnvelopeKeepsItsDigestAndRejectsTampering(t *testing.T) {
	original := model.Signature{Vendor: model.VendorAnthropic, Model: "claude", Blob: "blob", TextDigest: model.TextDigest("plan")}
	sealed, err := model.SealSignature(secret, original)
	if err != nil {
		t.Fatal(err)
	}
	opened, ok := model.OpenSignature(secret, sealed)
	if !ok || opened != original {
		t.Fatalf("the envelope must return exactly what was sealed: %+v %v", opened, ok)
	}
	if _, ok := model.OpenSignature([]byte("other"), sealed); ok {
		t.Fatal("a different secret must not open the envelope")
	}
	tampered := []byte(sealed)
	tampered[len(tampered)-6] ^= 1
	if _, ok := model.OpenSignature(secret, string(tampered)); ok {
		t.Fatal("a modified envelope must not open")
	}
	for _, junk := range []string{"", "AAAA", "not base64!"} {
		if _, ok := model.OpenSignature(secret, junk); ok {
			t.Fatalf("%q is not an envelope", junk)
		}
	}
}

func TestAdaptReasoningStripsForeignSignaturesAndDropsRedactedReasoning(t *testing.T) {
	request := &model.Request{Source: model.Messages, Items: []model.Item{
		{Kind: model.ItemReasoning, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartReasoningText, Text: "mine"}}, Reasoning: &model.Reasoning{Visibility: "full", Signature: &model.Signature{Vendor: model.VendorGoogle, Blob: "g"}}},
		{Kind: model.ItemReasoning, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartReasoningText, Text: "foreign"}}, Reasoning: &model.Reasoning{Visibility: "full", Signature: &model.Signature{Vendor: model.VendorAnthropic, Blob: "a"}}},
		{Kind: model.ItemReasoning, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartRedactedReasoning, Text: "opaque"}}, Reasoning: &model.Reasoning{Visibility: "full"}},
		{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "kept"}}},
	}}
	losses := model.AdaptReasoning(request, model.VendorGoogle, model.Gemini)
	if len(losses) != 2 || len(request.Items[0].Reasoning.Signature.Blob) == 0 || request.Items[1].Reasoning.Signature != nil || len(request.Items[2].Parts) != 0 {
		t.Fatalf("%+v %+v", losses, request.Items)
	}
	if losses[0].Code != model.LossSignatureStripped || losses[1].Code != model.LossFieldDropped {
		t.Fatalf("%+v", losses)
	}
}

func TestCloneRequestIsDeep(t *testing.T) {
	budget := int64(1)
	original := &model.Request{
		Raw:   &model.Raw{Format: model.Chat, Fields: map[string]json.RawMessage{"k": json.RawMessage(`1`)}},
		Tools: []model.Tool{{Name: "t", Server: &model.ServerTool{Type: "web_search"}, Raw: &model.Raw{Fields: map[string]json.RawMessage{"x": json.RawMessage(`1`)}}}},
		Items: []model.Item{{
			Kind: model.ItemReasoning, Role: model.RoleAssistant,
			Parts:     []model.Part{{Kind: model.PartReasoningText, Text: "r", Citations: []model.Citation{{URL: "u"}}}},
			Reasoning: &model.Reasoning{Signature: &model.Signature{Blob: "b"}},
			Raw:       &model.Raw{Fields: map[string]json.RawMessage{"m": json.RawMessage(`1`)}},
		}, {
			Kind: model.ItemToolCall, Role: model.RoleAssistant,
			ToolCall: &model.ToolCall{Name: "n", Signature: &model.Signature{Blob: "s"}},
		}, {
			Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "c",
			Parts: []model.Part{{Kind: model.PartText, Text: "r"}},
		}},
		MaxOutputTokens: &budget,
	}
	clone := model.CloneRequest(original)
	clone.Raw.Fields["k"] = json.RawMessage(`2`)
	clone.Tools[0].Server.Type = "changed"
	clone.Tools[0].Raw.Fields["x"] = json.RawMessage(`2`)
	clone.Items[0].Raw.Fields["m"] = json.RawMessage(`2`)
	clone.Items[0].Reasoning.Signature.Blob = "changed"
	clone.Items[0].Parts[0].Citations[0].URL = "changed"
	clone.Items[1].ToolCall.Signature.Blob = "changed"
	clone.Items[2].Parts[0].Text = "changed"
	if string(original.Raw.Fields["k"]) != "1" || original.Tools[0].Server.Type != "web_search" || string(original.Tools[0].Raw.Fields["x"]) != "1" || string(original.Items[0].Raw.Fields["m"]) != "1" {
		t.Fatal("request-level state was shared with the clone")
	}
	if original.Items[0].Reasoning.Signature.Blob != "b" || original.Items[0].Parts[0].Citations[0].URL != "u" || original.Items[1].ToolCall.Signature.Blob != "s" || original.Items[2].Parts[0].Text != "r" {
		t.Fatal("item state was shared with the clone")
	}
}

func TestMergeItemOverlaysTheFinalSnapshot(t *testing.T) {
	target := model.Item{Kind: model.ItemToolCall, ToolCall: &model.ToolCall{ID: "c", Name: "read", Arguments: `{"p":`}}
	model.MergeItem(&target, model.Item{Kind: model.ItemToolCall, ToolCall: &model.ToolCall{Arguments: `{"p":1}`, Signature: &model.Signature{Blob: "s"}}, Parts: []model.Part{{Kind: model.PartText, Text: "final"}}})
	if target.ToolCall.Arguments != `{"p":1}` || target.ToolCall.ID != "c" || target.ToolCall.Signature == nil || len(target.Parts) != 1 {
		t.Fatalf("%+v", target)
	}
}

func TestHistoryFingerprintIgnoresFormattingButNotContent(t *testing.T) {
	call := func(arguments string) model.Item {
		return model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "c", Name: "read", Arguments: arguments}}
	}
	base := []model.Item{text(model.RoleUser, "hi"), call(`{"a":1,"b":2}`)}
	same := []model.Item{text(model.RoleUser, "hi"), call(`{ "a": 1, "b": 2 }`)}
	other := []model.Item{text(model.RoleUser, "hi!"), call(`{"a":1,"b":2}`)}
	if model.HistoryFingerprint(secret, base) != model.HistoryFingerprint(secret, same) {
		t.Fatal("whitespace in arguments must not change the fingerprint")
	}
	if model.HistoryFingerprint(secret, base) == model.HistoryFingerprint(secret, other) {
		t.Fatal("different text must change the fingerprint")
	}
	if model.HistoryFingerprint(secret, base) == model.HistoryFingerprint([]byte("other"), base) {
		t.Fatal("the fingerprint is keyed")
	}
}

func TestHistoryFingerprintIncludesMediaContent(t *testing.T) {
	image := func(data string) model.Item {
		return model.Item{Kind: model.ItemMessage, Role: model.RoleUser, Parts: []model.Part{
			{Kind: model.PartMedia, Media: &model.Media{Kind: "image", MIMEType: "image/png", Data: data}},
		}}
	}
	if model.HistoryFingerprint(secret, []model.Item{image("AAAA")}) == model.HistoryFingerprint(secret, []model.Item{image("BBBB")}) {
		t.Fatal("different media with the same MIME type must not share a fingerprint")
	}
}

func TestHistoryPrefixMatchesTheFollowUpRequest(t *testing.T) {
	first := &model.Request{Items: []model.Item{text(model.RoleUser, "hi")}}
	response := &model.Response{Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "hello"}}}}}
	issued := model.ConversationFingerprint(secret, first, response)
	followUp := []model.Item{text(model.RoleUser, "hi"), text(model.RoleAssistant, "hello"), text(model.RoleUser, "more")}
	if model.HistoryPrefix(secret, followUp) != issued {
		t.Fatal("the prefix up to the last assistant turn must equal the earlier conversation fingerprint")
	}
	if model.HistoryPrefix(secret, followUp[:1]) != "" {
		t.Fatal("a request without an assistant turn has no prefix")
	}
	if !model.RepeatsPrefix(followUp, followUp[:2]) {
		t.Fatal("a repeated history must match its prefix")
	}
}

func TestRepeatsPrefixIgnoresFieldsClientsDoNotEcho(t *testing.T) {
	history := []model.Item{
		text(model.RoleUser, "hi"),
		{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{
			{Kind: model.PartText, Text: "hello", CacheControl: &model.CacheControl{Type: "ephemeral"}},
		}},
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "c", Name: "read", Arguments: `{"a":1}`, Signature: &model.Signature{Blob: "sig"}}},
	}
	followUp := []model.Item{
		text(model.RoleUser, "hi"),
		{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartText, Text: "hello"}}},
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "c", Name: "read", Arguments: `{ "a": 1 }`}},
		text(model.RoleUser, "more"),
	}
	if !model.RepeatsPrefix(followUp, history) {
		t.Fatal("a continuation whose echoed items omit cache markers and signatures must match")
	}
	followUp[1].Parts[0].Text = "different"
	if model.RepeatsPrefix(followUp, history) {
		t.Fatal("different assistant text must not match")
	}
}

func TestRepairRequestDropsUnansweredCallsAndOrphans(t *testing.T) {
	request := &model.Request{Items: []model.Item{
		{Kind: model.ItemMessage, Role: model.RoleUser, Parts: []model.Part{{Kind: model.PartText, Text: "hi"}}},
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "answering", Name: "read"}},
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "unanswered", Name: "now"}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "answering", Parts: []model.Part{{Kind: model.PartText, Text: "ok"}}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "orphan", Parts: []model.Part{{Kind: model.PartText, Text: "x"}}},
		{Kind: model.ItemMessage, Role: model.RoleAssistant, Parts: nil},
	}}
	repairs := model.RepairRequest(request, model.Messages)
	if len(repairs) != 3 {
		t.Fatalf("expected unanswered, orphan and empty repairs: %+v", repairs)
	}
	if len(request.Items) != 3 {
		t.Fatalf("unexpected items: %+v", request.Items)
	}
}

func TestRepairMovesToolResultsNextToTheirCall(t *testing.T) {
	request := &model.Request{Items: []model.Item{
		text(model.RoleUser, "start"),
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "c", Name: "read"}},
		text(model.RoleUser, "interjection"),
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "c", Parts: []model.Part{{Kind: model.PartText, Text: "ok"}}},
	}}
	repairs := model.RepairRequest(request, model.Chat)
	if len(request.Items) != 4 || request.Items[2].Kind != model.ItemToolResult || request.Items[3].Text() != "interjection" {
		t.Fatalf("results must follow the call: %+v", request.Items)
	}
	if len(repairs) == 0 {
		t.Fatal("the relocation must be recorded")
	}
}

func TestCapabilityValidationIsFatalForCoreMediaAndDegradedForPrivateFields(t *testing.T) {
	request := &model.Request{Source: model.Messages, Items: []model.Item{
		{Kind: model.ItemMessage, Role: model.RoleUser, Parts: []model.Part{{Kind: model.PartMedia, Media: &model.Media{Kind: "image", URL: "https://example.com/a.png"}}}},
	}}
	losses := model.ValidateRequestCapability(request, model.Capability{Format: model.Chat, Media: model.MediaCapability{}})
	if len(losses) != 1 || !losses[0].Fatal() {
		t.Fatalf("%+v", losses)
	}
}

func TestItemIDsAreTargetNative(t *testing.T) {
	if id, changed := model.ItemID(model.Responses, model.ItemMessage, "msg_abc", "call", 0); id != "msg_abc" || changed {
		t.Fatalf("%s %v", id, changed)
	}
	if id, changed := model.ItemID(model.Responses, model.ItemToolCall, "call_1", "call_1", 3); !changed || len(id) < 4 || id[:3] != "fc_" {
		t.Fatalf("%s %v", id, changed)
	}
	if id := model.ResponseID(model.Responses, "call", "chatcmpl-x"); id != "resp_call" {
		t.Fatalf("an incompatible upstream id must be replaced: %s", id)
	}
	if id := model.ResponseID(model.Responses, "call", "resp_upstream"); id != "resp_upstream" {
		t.Fatalf("a compatible upstream id must be preserved: %s", id)
	}
}

func TestFormatsKnowTheirDefaultVendor(t *testing.T) {
	for format, vendor := range map[model.Format]model.Vendor{model.Chat: model.VendorOpenAI, model.Responses: model.VendorOpenAI, model.Messages: model.VendorAnthropic, model.Gemini: model.VendorGoogle} {
		if !format.Valid() || format.DefaultVendor() != vendor {
			t.Fatalf("%s", format)
		}
	}
	if model.Format("other").Valid() {
		t.Fatal("unknown formats are invalid")
	}
}

func TestRepairLiftsResponsesMessagesAboveToolCalls(t *testing.T) {
	request := &model.Request{Items: []model.Item{
		text(model.RoleUser, "read both"),
		text(model.RoleAssistant, "reading"),
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "a", Name: "read"}},
		text(model.RoleAssistant, "again"),
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "b", Name: "read"}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "a", Parts: []model.Part{{Kind: model.PartText, Text: "A"}}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "b", Parts: []model.Part{{Kind: model.PartText, Text: "B"}}},
	}}
	model.RepairRequest(request, model.Responses)
	var order []string
	for _, item := range request.Items {
		switch item.Kind {
		case model.ItemToolCall:
			order = append(order, "call:"+item.ToolCall.ID)
		case model.ItemToolResult:
			order = append(order, "result:"+item.CallID)
		case model.ItemMessage:
			if item.Role == model.RoleAssistant {
				order = append(order, "message")
			}
		}
	}
	got := strings.Join(order, ",")
	if got != "message,message,call:a,call:b,result:a,result:b" {
		t.Fatalf("unexpected order %s", got)
	}
}

func TestRepairMovesResponsesReasoningFirst(t *testing.T) {
	request := &model.Request{Items: []model.Item{
		text(model.RoleUser, "read"),
		{Kind: model.ItemReasoning, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartReasoningText, Text: "plan"}}},
		text(model.RoleAssistant, "reading"),
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "a", Name: "read"}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "a", Parts: []model.Part{{Kind: model.PartText, Text: "A"}}},
	}}
	model.RepairRequest(request, model.Responses)
	var kinds []string
	for _, item := range request.Items {
		if item.Kind == model.ItemReasoning {
			kinds = append(kinds, "reasoning")
		} else if item.Kind == model.ItemMessage && item.Role == model.RoleAssistant {
			kinds = append(kinds, "message")
		} else if item.Kind == model.ItemToolCall {
			kinds = append(kinds, "call")
		}
	}
	if got := strings.Join(kinds, ","); got != "reasoning,message,call" {
		t.Fatalf("reasoning must lead the assistant turn: %s", got)
	}
}

func TestMissingToolReasoningOnlyAppliesWithoutReasoning(t *testing.T) {
	withReasoning := &model.Request{Items: []model.Item{
		{Kind: model.ItemReasoning, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartReasoningText, Text: "plan"}}},
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "a", Name: "read"}},
		{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "a"},
	}}
	if model.MissingToolReasoning(withReasoning) {
		t.Fatal("a reasoning item before the call satisfies replay")
	}
	without := &model.Request{Items: []model.Item{
		text(model.RoleAssistant, "reading"),
		{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: "a", Name: "read"}},
	}}
	if !model.MissingToolReasoning(without) {
		t.Fatal("a tool call without reasoning must be detected")
	}
}
