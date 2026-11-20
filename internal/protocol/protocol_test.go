package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

var secret = []byte("test-secret")

func text(role protocol.Role, value string) protocol.Item {
	return protocol.MessageItem(role, value)
}

func TestObjectReadsRawMessages(t *testing.T) {
	object := protocol.Object(json.RawMessage(`{"a":1,"b":{"c":"d"}}`))
	if object == nil || protocol.String(protocol.Object(object["b"])["c"]) != "d" {
		t.Fatalf("a raw JSON object must decode: %+v", object)
	}
	for _, value := range []any{json.RawMessage(`[1]`), json.RawMessage(`not json`), json.RawMessage(nil), "text", nil, 3} {
		if protocol.Object(value) != nil {
			t.Fatalf("%#v is not an object", value)
		}
	}
	if protocol.Object(map[string]any{"k": 1}) == nil {
		t.Fatal("plain maps must pass through")
	}
}

func TestDecodeJSONRejectsTrailingValuesAndKeepsNumbers(t *testing.T) {
	if _, err := protocol.DecodeJSON([]byte(`{} {}`)); err == nil {
		t.Fatal("two values must be rejected")
	}
	if _, err := protocol.DecodeJSON([]byte("\"\xff\"")); err == nil {
		t.Fatal("invalid UTF-8 must be rejected")
	}
	object, err := protocol.DecodeObject([]byte(`{"big":12345678901234567890,"n":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if number, ok := protocol.Integer(object["n"]); !ok || number != 3 {
		t.Fatalf("%v %v", number, ok)
	}
	if string(protocol.Marshal(object)) != `{"big":12345678901234567890,"n":3}` {
		t.Fatalf("large numbers must keep their digits: %s", protocol.Marshal(object))
	}
}

func TestSignatureEnvelopeKeepsItsDigestAndRejectsTampering(t *testing.T) {
	original := protocol.Signature{Vendor: protocol.VendorAnthropic, Model: "claude", Blob: "blob", TextDigest: protocol.TextDigest("plan")}
	sealed, err := protocol.SealSignature(secret, original)
	if err != nil {
		t.Fatal(err)
	}
	opened, ok := protocol.OpenSignature(secret, sealed)
	if !ok || opened != original {
		t.Fatalf("the envelope must return exactly what was sealed: %+v %v", opened, ok)
	}
	if _, ok := protocol.OpenSignature([]byte("other"), sealed); ok {
		t.Fatal("a different secret must not open the envelope")
	}
	tampered := []byte(sealed)
	tampered[len(tampered)-6] ^= 1
	if _, ok := protocol.OpenSignature(secret, string(tampered)); ok {
		t.Fatal("a modified envelope must not open")
	}
	for _, junk := range []string{"", "AAAA", "not base64!"} {
		if _, ok := protocol.OpenSignature(secret, junk); ok {
			t.Fatalf("%q is not an envelope", junk)
		}
	}
}

func TestAdaptReasoningStripsForeignSignaturesAndDropsRedactedReasoning(t *testing.T) {
	request := &protocol.Request{Source: protocol.Messages, Items: []protocol.Item{
		{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartReasoningText, Text: "mine"}}, Reasoning: &protocol.Reasoning{Visibility: "full", Signature: &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: "g"}}},
		{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartReasoningText, Text: "foreign"}}, Reasoning: &protocol.Reasoning{Visibility: "full", Signature: &protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: "a"}}},
		{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartRedactedReasoning, Text: "opaque"}}, Reasoning: &protocol.Reasoning{Visibility: "full"}},
		{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartText, Text: "kept"}}},
	}}
	losses := protocol.AdaptReasoning(request, protocol.VendorGoogle, protocol.Gemini)
	if len(losses) != 2 || len(request.Items[0].Reasoning.Signature.Blob) == 0 || request.Items[1].Reasoning.Signature != nil || len(request.Items[2].Parts) != 0 {
		t.Fatalf("%+v %+v", losses, request.Items)
	}
	if losses[0].Code != protocol.LossSignatureStripped || losses[1].Code != protocol.LossFieldDropped {
		t.Fatalf("%+v", losses)
	}
}

func TestCloneRequestIsDeep(t *testing.T) {
	budget := int64(1)
	original := &protocol.Request{
		Raw:   &protocol.Raw{Format: protocol.Chat, Fields: map[string]json.RawMessage{"k": json.RawMessage(`1`)}},
		Tools: []protocol.Tool{{Name: "t", Server: &protocol.ServerTool{Type: "web_search"}, Raw: &protocol.Raw{Fields: map[string]json.RawMessage{"x": json.RawMessage(`1`)}}}},
		Items: []protocol.Item{{
			Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant,
			Parts:     []protocol.Part{{Kind: protocol.PartReasoningText, Text: "r", Citations: []protocol.Citation{{URL: "u"}}}},
			Reasoning: &protocol.Reasoning{Signature: &protocol.Signature{Blob: "b"}},
			Raw:       &protocol.Raw{Fields: map[string]json.RawMessage{"m": json.RawMessage(`1`)}},
		}, {
			Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant,
			ToolCall: &protocol.ToolCall{Name: "n", Signature: &protocol.Signature{Blob: "s"}},
		}, {
			Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "c",
			Parts: []protocol.Part{{Kind: protocol.PartText, Text: "r"}},
		}},
		MaxOutputTokens: &budget,
	}
	clone := protocol.CloneRequest(original)
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
	target := protocol.Item{Kind: protocol.ItemToolCall, ToolCall: &protocol.ToolCall{ID: "c", Name: "read", Arguments: `{"p":`}}
	protocol.MergeItem(&target, protocol.Item{Kind: protocol.ItemToolCall, ToolCall: &protocol.ToolCall{Arguments: `{"p":1}`, Signature: &protocol.Signature{Blob: "s"}}, Parts: []protocol.Part{{Kind: protocol.PartText, Text: "final"}}})
	if target.ToolCall.Arguments != `{"p":1}` || target.ToolCall.ID != "c" || target.ToolCall.Signature == nil || len(target.Parts) != 1 {
		t.Fatalf("%+v", target)
	}
}

func TestHistoryFingerprintIgnoresFormattingButNotContent(t *testing.T) {
	call := func(arguments string) protocol.Item {
		return protocol.Item{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "c", Name: "read", Arguments: arguments}}
	}
	base := []protocol.Item{text(protocol.RoleUser, "hi"), call(`{"a":1,"b":2}`)}
	same := []protocol.Item{text(protocol.RoleUser, "hi"), call(`{ "a": 1, "b": 2 }`)}
	other := []protocol.Item{text(protocol.RoleUser, "hi!"), call(`{"a":1,"b":2}`)}
	if protocol.HistoryFingerprint(secret, base) != protocol.HistoryFingerprint(secret, same) {
		t.Fatal("whitespace in arguments must not change the fingerprint")
	}
	if protocol.HistoryFingerprint(secret, base) == protocol.HistoryFingerprint(secret, other) {
		t.Fatal("different text must change the fingerprint")
	}
	if protocol.HistoryFingerprint(secret, base) == protocol.HistoryFingerprint([]byte("other"), base) {
		t.Fatal("the fingerprint is keyed")
	}
}

func TestHistoryFingerprintIncludesMediaContent(t *testing.T) {
	image := func(data string) protocol.Item {
		return protocol.Item{Kind: protocol.ItemMessage, Role: protocol.RoleUser, Parts: []protocol.Part{
			{Kind: protocol.PartMedia, Media: &protocol.Media{Kind: "image", MIMEType: "image/png", Data: data}},
		}}
	}
	if protocol.HistoryFingerprint(secret, []protocol.Item{image("AAAA")}) == protocol.HistoryFingerprint(secret, []protocol.Item{image("BBBB")}) {
		t.Fatal("different media with the same MIME type must not share a fingerprint")
	}
}

func TestHistoryPrefixMatchesTheFollowUpRequest(t *testing.T) {
	first := &protocol.Request{Items: []protocol.Item{text(protocol.RoleUser, "hi")}}
	response := &protocol.Response{Items: []protocol.Item{{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartText, Text: "hello"}}}}}
	issued := protocol.ConversationFingerprint(secret, first, response)
	followUp := []protocol.Item{text(protocol.RoleUser, "hi"), text(protocol.RoleAssistant, "hello"), text(protocol.RoleUser, "more")}
	if protocol.HistoryPrefix(secret, followUp) != issued {
		t.Fatal("the prefix up to the last assistant turn must equal the earlier conversation fingerprint")
	}
	if protocol.HistoryPrefix(secret, followUp[:1]) != "" {
		t.Fatal("a request without an assistant turn has no prefix")
	}
	if !protocol.RepeatsPrefix(followUp, followUp[:2]) {
		t.Fatal("a repeated history must match its prefix")
	}
}

func TestRepeatsPrefixIgnoresFieldsClientsDoNotEcho(t *testing.T) {
	history := []protocol.Item{
		text(protocol.RoleUser, "hi"),
		{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant, Parts: []protocol.Part{
			{Kind: protocol.PartText, Text: "hello", CacheControl: &protocol.CacheControl{Type: "ephemeral"}},
		}},
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "c", Name: "read", Arguments: `{"a":1}`, Signature: &protocol.Signature{Blob: "sig"}}},
	}
	followUp := []protocol.Item{
		text(protocol.RoleUser, "hi"),
		{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartText, Text: "hello"}}},
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "c", Name: "read", Arguments: `{ "a": 1 }`}},
		text(protocol.RoleUser, "more"),
	}
	if !protocol.RepeatsPrefix(followUp, history) {
		t.Fatal("a continuation whose echoed items omit cache markers and signatures must match")
	}
	followUp[1].Parts[0].Text = "different"
	if protocol.RepeatsPrefix(followUp, history) {
		t.Fatal("different assistant text must not match")
	}
}

func TestRepairRequestDropsUnansweredCallsAndOrphans(t *testing.T) {
	request := &protocol.Request{Items: []protocol.Item{
		{Kind: protocol.ItemMessage, Role: protocol.RoleUser, Parts: []protocol.Part{{Kind: protocol.PartText, Text: "hi"}}},
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "answering", Name: "read"}},
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "unanswered", Name: "now"}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "answering", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "ok"}}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "orphan", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "x"}}},
		{Kind: protocol.ItemMessage, Role: protocol.RoleAssistant, Parts: nil},
	}}
	repairs := protocol.RepairRequest(request, protocol.Messages)
	if len(repairs) != 3 {
		t.Fatalf("expected unanswered, orphan and empty repairs: %+v", repairs)
	}
	if len(request.Items) != 3 {
		t.Fatalf("unexpected items: %+v", request.Items)
	}
}

func TestRepairMovesToolResultsNextToTheirCall(t *testing.T) {
	request := &protocol.Request{Items: []protocol.Item{
		text(protocol.RoleUser, "start"),
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "c", Name: "read"}},
		text(protocol.RoleUser, "interjection"),
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "c", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "ok"}}},
	}}
	repairs := protocol.RepairRequest(request, protocol.Chat)
	if len(request.Items) != 4 || request.Items[2].Kind != protocol.ItemToolResult || request.Items[3].Text() != "interjection" {
		t.Fatalf("results must follow the call: %+v", request.Items)
	}
	if len(repairs) == 0 {
		t.Fatal("the relocation must be recorded")
	}
}

func TestCapabilityValidationIsFatalForCoreMediaAndDegradedForPrivateFields(t *testing.T) {
	request := &protocol.Request{Source: protocol.Messages, Items: []protocol.Item{
		{Kind: protocol.ItemMessage, Role: protocol.RoleUser, Parts: []protocol.Part{{Kind: protocol.PartMedia, Media: &protocol.Media{Kind: "image", URL: "https://example.com/a.png"}}}},
	}}
	losses := protocol.ValidateRequestCapability(request, protocol.Capability{Format: protocol.Chat, Media: protocol.MediaCapability{}})
	if len(losses) != 1 || !losses[0].Fatal() {
		t.Fatalf("%+v", losses)
	}
}

func TestItemIDsAreTargetNative(t *testing.T) {
	if id, changed := protocol.ItemID(protocol.Responses, protocol.ItemMessage, "msg_abc", "call", 0); id != "msg_abc" || changed {
		t.Fatalf("%s %v", id, changed)
	}
	if id, changed := protocol.ItemID(protocol.Responses, protocol.ItemToolCall, "call_1", "call_1", 3); !changed || len(id) < 4 || id[:3] != "fc_" {
		t.Fatalf("%s %v", id, changed)
	}
	if id := protocol.ResponseID(protocol.Responses, "call", "chatcmpl-x"); id != "resp_call" {
		t.Fatalf("an incompatible upstream id must be replaced: %s", id)
	}
	if id := protocol.ResponseID(protocol.Responses, "call", "resp_upstream"); id != "resp_upstream" {
		t.Fatalf("a compatible upstream id must be preserved: %s", id)
	}
}

func TestFormatsKnowTheirDefaultVendor(t *testing.T) {
	for format, vendor := range map[protocol.Format]protocol.Vendor{protocol.Chat: protocol.VendorOpenAI, protocol.Responses: protocol.VendorOpenAI, protocol.Messages: protocol.VendorAnthropic, protocol.Gemini: protocol.VendorGoogle} {
		if !format.Valid() || format.DefaultVendor() != vendor {
			t.Fatalf("%s", format)
		}
	}
	if protocol.Format("other").Valid() {
		t.Fatal("unknown formats are invalid")
	}
}

func TestRepairGroupsResponsesToolOutputsAfterTheirCalls(t *testing.T) {
	request := &protocol.Request{Items: []protocol.Item{
		text(protocol.RoleUser, "read both"),
		text(protocol.RoleAssistant, "reading"),
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "a", Name: "read"}},
		text(protocol.RoleAssistant, "again"),
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "b", Name: "read"}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "a", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "A"}}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "b", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "B"}}},
	}}
	protocol.RepairRequest(request, protocol.Responses)
	var order []string
	for _, item := range request.Items {
		switch item.Kind {
		case protocol.ItemToolCall:
			order = append(order, "call:"+item.ToolCall.ID)
		case protocol.ItemToolResult:
			order = append(order, "result:"+item.CallID)
		}
	}
	got := strings.Join(order, ",")
	if got != "call:a,call:b,result:a,result:b" {
		t.Fatalf("unexpected order %s", got)
	}
}

func TestRepairMovesResponsesReasoningFirst(t *testing.T) {
	request := &protocol.Request{Items: []protocol.Item{
		text(protocol.RoleUser, "read"),
		{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartReasoningText, Text: "plan"}}},
		text(protocol.RoleAssistant, "reading"),
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "a", Name: "read"}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "a", Parts: []protocol.Part{{Kind: protocol.PartText, Text: "A"}}},
	}}
	protocol.RepairRequest(request, protocol.Responses)
	var kinds []string
	for _, item := range request.Items {
		if item.Kind == protocol.ItemReasoning {
			kinds = append(kinds, "reasoning")
		} else if item.Kind == protocol.ItemMessage && item.Role == protocol.RoleAssistant {
			kinds = append(kinds, "message")
		} else if item.Kind == protocol.ItemToolCall {
			kinds = append(kinds, "call")
		}
	}
	if got := strings.Join(kinds, ","); got != "reasoning,message,call" {
		t.Fatalf("reasoning must lead the assistant turn: %s", got)
	}
}

func TestMissingToolReasoningOnlyAppliesWithoutReasoning(t *testing.T) {
	withReasoning := &protocol.Request{Items: []protocol.Item{
		{Kind: protocol.ItemReasoning, Role: protocol.RoleAssistant, Parts: []protocol.Part{{Kind: protocol.PartReasoningText, Text: "plan"}}},
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "a", Name: "read"}},
		{Kind: protocol.ItemToolResult, Role: protocol.RoleTool, CallID: "a"},
	}}
	if protocol.MissingToolReasoning(withReasoning) {
		t.Fatal("a reasoning item before the call satisfies replay")
	}
	without := &protocol.Request{Items: []protocol.Item{
		text(protocol.RoleAssistant, "reading"),
		{Kind: protocol.ItemToolCall, Role: protocol.RoleAssistant, ToolCall: &protocol.ToolCall{ID: "a", Name: "read"}},
	}}
	if !protocol.MissingToolReasoning(without) {
		t.Fatal("a tool call without reasoning must be detected")
	}
}
