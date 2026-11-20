package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

var secret = []byte("test-secret")

func text(role protocol.Role, value string) protocol.Message {
	return protocol.Message{Role: role, Blocks: []protocol.Block{{Kind: protocol.KindText, Text: value}}}
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
	request := &protocol.Request{Messages: []protocol.Message{{Role: protocol.RoleAssistant, Blocks: []protocol.Block{
		{Kind: protocol.KindReasoning, Text: "mine", Reasoning: &protocol.Reasoning{Signature: &protocol.Signature{Vendor: protocol.VendorGoogle, Blob: "g"}}},
		{Kind: protocol.KindReasoning, Text: "foreign", Reasoning: &protocol.Reasoning{Signature: &protocol.Signature{Vendor: protocol.VendorAnthropic, Blob: "a"}}},
		{Kind: protocol.KindRedacted, Text: "opaque", Extras: protocol.Extras{Format: protocol.Messages}},
		{Kind: protocol.KindText, Text: ""},
		{Kind: protocol.KindText, Text: "kept"},
	}}}}
	losses := protocol.AdaptReasoning(request, protocol.VendorGoogle)
	blocks := request.Messages[0].Blocks
	if len(losses) != 2 || len(blocks) != 3 {
		t.Fatalf("%+v %+v", losses, blocks)
	}
	if blocks[0].Reasoning.Signature == nil || blocks[1].Reasoning.Signature != nil || blocks[1].Text != "foreign" {
		t.Fatalf("only the foreign signature goes: %+v", blocks)
	}
	if losses[0].Code != protocol.LossSignatureStripped || losses[1].Code != protocol.LossFieldDropped {
		t.Fatalf("%+v", losses)
	}
}

func TestCloneRequestIsDeep(t *testing.T) {
	budget := int64(1)
	original := &protocol.Request{
		Extras: protocol.Extras{Format: protocol.Chat, Fields: map[string]json.RawMessage{"k": json.RawMessage(`1`)}},
		Tools:  []protocol.Tool{{Name: "t", Server: &protocol.ServerTool{Type: "web_search"}, Extras: protocol.Extras{Fields: map[string]json.RawMessage{"x": json.RawMessage(`1`)}}}},
		Messages: []protocol.Message{{Role: protocol.RoleAssistant, Extras: protocol.Extras{Fields: map[string]json.RawMessage{"m": json.RawMessage(`1`)}}, Blocks: []protocol.Block{
			{Kind: protocol.KindReasoning, Reasoning: &protocol.Reasoning{Signature: &protocol.Signature{Blob: "b"}}, CacheControl: &protocol.CacheControl{Type: "ephemeral"}, Citations: []protocol.Citation{{URL: "u"}}},
			{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Name: "n", Signature: &protocol.Signature{Blob: "s"}}},
			{Kind: protocol.KindToolResult, ToolResult: &protocol.ToolResult{Content: []protocol.Block{{Kind: protocol.KindText, Text: "r"}}}},
		}}},
		MaxOutputTokens: &budget,
	}
	clone := protocol.CloneRequest(original)
	clone.Extras.Fields["k"] = json.RawMessage(`2`)
	clone.Tools[0].Server.Type = "changed"
	clone.Tools[0].Extras.Fields["x"] = json.RawMessage(`2`)
	clone.Messages[0].Extras.Fields["m"] = json.RawMessage(`2`)
	clone.Messages[0].Blocks[0].Reasoning.Signature.Blob = "changed"
	clone.Messages[0].Blocks[0].CacheControl.Type = "changed"
	clone.Messages[0].Blocks[0].Citations[0].URL = "changed"
	clone.Messages[0].Blocks[1].ToolCall.Signature.Blob = "changed"
	clone.Messages[0].Blocks[2].ToolResult.Content[0].Text = "changed"
	if string(original.Extras.Fields["k"]) != "1" || original.Tools[0].Server.Type != "web_search" || string(original.Tools[0].Extras.Fields["x"]) != "1" || string(original.Messages[0].Extras.Fields["m"]) != "1" {
		t.Fatal("request-level state was shared with the clone")
	}
	blocks := original.Messages[0].Blocks
	if blocks[0].Reasoning.Signature.Blob != "b" || blocks[0].CacheControl.Type != "ephemeral" || blocks[0].Citations[0].URL != "u" || blocks[1].ToolCall.Signature.Blob != "s" || blocks[2].ToolResult.Content[0].Text != "r" {
		t.Fatal("block state was shared with the clone")
	}
}

func TestMergeBlockOverlaysTheFinalSnapshot(t *testing.T) {
	target := protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{ID: "c", Name: "read", Arguments: `{"p":`}}
	protocol.MergeBlock(&target, protocol.Block{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{Arguments: `{"p":1}`, Signature: &protocol.Signature{Blob: "s"}}, CacheControl: &protocol.CacheControl{Type: "ephemeral"}})
	if target.ToolCall.Arguments != `{"p":1}` || target.ToolCall.ID != "c" || target.ToolCall.Signature == nil || target.CacheControl == nil {
		t.Fatalf("%+v", target.ToolCall)
	}
}

func TestHistoryFingerprintIgnoresFormattingButNotContent(t *testing.T) {
	call := func(arguments string) protocol.Message {
		return protocol.Message{Role: protocol.RoleAssistant, Blocks: []protocol.Block{{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{ID: "c", Name: "read", Arguments: arguments}}}}
	}
	base := []protocol.Message{text(protocol.RoleUser, "hi"), call(`{"a":1,"b":2}`)}
	same := []protocol.Message{text(protocol.RoleUser, "hi"), call(`{ "a": 1, "b": 2 }`)}
	other := []protocol.Message{text(protocol.RoleUser, "hi!"), call(`{"a":1,"b":2}`)}
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

func TestHistoryPrefixMatchesTheFollowUpRequest(t *testing.T) {
	first := &protocol.Request{Messages: []protocol.Message{text(protocol.RoleUser, "hi")}}
	response := &protocol.Response{Blocks: []protocol.Block{{Kind: protocol.KindText, Text: "hello"}}}
	issued := protocol.ConversationFingerprint(secret, first, response)
	followUp := []protocol.Message{text(protocol.RoleUser, "hi"), text(protocol.RoleAssistant, "hello"), text(protocol.RoleUser, "more")}
	if protocol.HistoryPrefix(secret, followUp) != issued {
		t.Fatal("the prefix up to the last assistant turn must equal the earlier conversation fingerprint")
	}
	if protocol.HistoryPrefix(secret, followUp[:1]) != "" {
		t.Fatal("a request without an assistant turn has no prefix")
	}
	split := []protocol.Message{text(protocol.RoleUser, "hi"), text(protocol.RoleAssistant, "hel"), text(protocol.RoleAssistant, "lo"), text(protocol.RoleUser, "more")}
	if protocol.HistoryPrefix(secret, split) != issued {
		t.Fatal("adjacent assistant text must be joined before fingerprinting")
	}
}

func TestRepeatsPrefixIgnoresFieldsClientsDoNotEcho(t *testing.T) {
	history := []protocol.Message{
		text(protocol.RoleUser, "hi"),
		{Role: protocol.RoleAssistant, Blocks: []protocol.Block{
			{Kind: protocol.KindText, Text: "hello", CacheControl: &protocol.CacheControl{Type: "ephemeral"}},
			{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{ID: "c", Name: "read", Arguments: `{"a":1}`, Signature: &protocol.Signature{Blob: "sig"}}},
		}},
	}
	followUp := []protocol.Message{
		text(protocol.RoleUser, "hi"),
		{Role: protocol.RoleAssistant, Blocks: []protocol.Block{
			{Kind: protocol.KindText, Text: "hello"},
			{Kind: protocol.KindToolCall, ToolCall: &protocol.ToolCall{ID: "c", Name: "read", Arguments: `{ "a": 1 }`}},
		}},
		text(protocol.RoleUser, "more"),
	}
	if !protocol.RepeatsPrefix(followUp, history) {
		t.Fatal("a continuation whose echoed blocks omit cache markers and signatures must match")
	}
	followUp[1].Blocks[0].Text = "different"
	if protocol.RepeatsPrefix(followUp, history) {
		t.Fatal("different assistant text must not match")
	}
	if protocol.RepeatsPrefix(history[:1], history) {
		t.Fatal("a request shorter than the history must not match")
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
