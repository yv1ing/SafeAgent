package redact_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/redact"
)

var secret = []byte("test-secret")

func engine(t *testing.T, rules ...redact.Rule) *redact.Engine {
	t.Helper()
	compiled, err := redact.NewEngine(rules, secret, "scope")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return compiled
}

func builtin(detector, label string) redact.Rule {
	return redact.Rule{Kind: redact.KindBuiltin, Detector: detector, Label: label, Action: redact.ActionReplace, Enabled: true}
}

func regex(pattern, label string, action redact.Action) redact.Rule {
	return redact.Rule{Kind: redact.KindRegex, Pattern: pattern, Label: label, Action: action, Enabled: true}
}

func userRequest(text string) *model.Request {
	return &model.Request{Items: []model.Item{model.MessageItem(model.RoleUser, text)}}
}

func stream(restorer *redact.Restorer, text string, chunk int) string {
	var restored strings.Builder
	for index := 0; index < len(text); index += chunk {
		restored.WriteString(restorer.Push(text[index:min(len(text), index+chunk)]))
	}
	restored.WriteString(restorer.Flush())
	return restored.String()
}

func TestDetectors(t *testing.T) {
	validCard := "4111111111111111"
	cases := []struct {
		name     string
		detector string
		label    string
		text     string
		redacted bool
	}{
		{"mobile", "cn_mobile", "PHONE", "call 13812345678 now", true},
		{"mobile inside timestamp", "cn_mobile", "PHONE", "1731900000000", false},
		{"mobile inside long number", "cn_mobile", "PHONE", "1234567890123456789", false},
		{"mobile inside date stamp", "cn_mobile", "PHONE", "20241118123045678", false},
		{"mobile inside exponent", "cn_mobile", "PHONE", "1.731900000000e12", false},
		{"identity", "cn_id", "ID_NUMBER", "11010519491231002X", true},
		{"identity bad checksum", "cn_id", "ID_NUMBER", "110105194912310021", false},
		{"email", "email", "EMAIL", "mail ops@example.com please", true},
		{"email asset name", "email", "EMAIL", "icon@2x.png", false},
		{"email git remote", "email", "EMAIL", "git@github.com:org/repo.git", false},
		{"ipv4", "ipv4", "IP_ADDRESS", "from 10.0.0.7 today", true},
		{"version string", "ipv4", "IP_ADDRESS", "go1.27.4", false},
		{"version word", "ipv4", "IP_ADDRESS", "version 1.2.3.4", false},
		{"ipv4 leading zero", "ipv4", "IP_ADDRESS", "01.2.3.4", false},
		{"ipv6 full", "ipv6", "IP_ADDRESS", "at 2001:db8::8a2e:370:7334 up", true},
		{"ipv6 loopback", "ipv6", "IP_ADDRESS", "bind ::1 now", true},
		{"ipv6 unspecified", "ipv6", "IP_ADDRESS", "bind :: now", false},
		{"ipv6 clock time", "ipv6", "IP_ADDRESS", "at 12:30:45 sharp", false},
		{"ipv6 scope operator", "ipv6", "IP_ADDRESS", "std::vector", false},
		{"bank card spaced", "bank_card", "BANK_CARD", "card 4111 1111 1111 1111 end", true},
		{"bank card unseparated", "bank_card", "BANK_CARD", "card " + validCard + " end", true},
		{"bank card mastercard 2-series", "bank_card", "BANK_CARD", "2223003122003222", true},
		{"bank card amex", "bank_card", "BANK_CARD", "378282246310005", true},
		{"bank card unionpay", "bank_card", "BANK_CARD", "6200000000000005", true},
		{"bank card bad luhn", "bank_card", "BANK_CARD", "4111111111111112", false},
		{"bank card millisecond timestamp", "bank_card", "BANK_CARD", "1731900000000", false},
		{"bank card microsecond timestamp", "bank_card", "BANK_CARD", "1731900000000000", false},
		{"bank card unknown prefix", "bank_card", "BANK_CARD", "9111111111111113", false},
		{"bank card repeated digit", "bank_card", "BANK_CARD", "4444444444444", false},
		{"bank card inside longer run", "bank_card", "BANK_CARD", "41111111111111111234", false},
		{"credential assignment", "credential_assignment", "CREDENTIAL", `password = hunter2hunter2hunter2`, true},
		{"credential json", "credential_assignment", "CREDENTIAL", `{"password": "s3cr3t/with+chars!@#abc"}`, true},
		{"credential json compact", "credential_assignment", "CREDENTIAL", `{"api_key":"abcdefghijklmnop"}`, true},
		{"credential query", "credential_assignment", "CREDENTIAL", `https://x.example/?api_key=abcdefghijklmnop&a=1`, true},
		{"credential too short", "credential_assignment", "CREDENTIAL", `password = short`, false},
		{"credential embedded name", "credential_assignment", "CREDENTIAL", `my_password=abcdefghijklmnop`, false},
		{"aws key", "aws_access_key", "AWS_KEY", "AKIAIOSFODNN7EXAMPLE", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			scope := engine(t, builtin(testCase.detector, testCase.label)).NewScope()
			replaced, result := scope.Replace(testCase.text, "text")
			if testCase.redacted {
				if !strings.Contains(replaced, "["+testCase.label+"_") || len(result.Hits) == 0 {
					t.Fatalf("not redacted: %q", replaced)
				}
				if scope.Restore(replaced) != testCase.text {
					t.Fatalf("restore mismatch: %q", scope.Restore(replaced))
				}
				return
			}
			if replaced != testCase.text || len(result.Hits) != 0 {
				t.Fatalf("unexpected redaction: %q %+v", replaced, result.Hits)
			}
		})
	}
}

func TestCredentialJSONKeepsKeyAndQuotes(t *testing.T) {
	scope := engine(t, builtin("credential_assignment", "CREDENTIAL")).NewScope()
	replaced, _ := scope.Replace(`{"password": "hunter2hunter2"}`, "text")
	if !strings.HasPrefix(replaced, `{"password": "[CREDENTIAL_`) || !strings.HasSuffix(replaced, `]"}`) {
		t.Fatalf("JSON shape changed: %q", replaced)
	}
}

func TestRuleCompilation(t *testing.T) {
	cases := []struct {
		name  string
		rules []redact.Rule
		valid bool
	}{
		{"builtin", []redact.Rule{builtin("email", "EMAIL")}, true},
		{"builtin with capture", []redact.Rule{{Kind: redact.KindBuiltin, Detector: "email", Capture: "x", Label: "EMAIL", Action: redact.ActionReplace, Enabled: true}}, false},
		{"unknown detector", []redact.Rule{builtin("nope", "EMAIL")}, false},
		{"regex", []redact.Rule{regex(`\d{4}`, "CODE", redact.ActionReplace)}, true},
		{"regex capture", []redact.Rule{{Kind: redact.KindRegex, Pattern: `id=(?P<value>\d+)`, Capture: "value", Label: "CODE", Action: redact.ActionReplace, Enabled: true}}, true},
		{"regex unknown capture", []redact.Rule{{Kind: redact.KindRegex, Pattern: `id=(\d+)`, Capture: "value", Label: "CODE", Action: redact.ActionReplace, Enabled: true}}, false},
		{"regex matching empty text", []redact.Rule{regex(`\d*`, "CODE", redact.ActionReplace)}, false},
		{"regex invalid", []redact.Rule{regex(`(`, "CODE", redact.ActionReplace)}, false},
		{"regex empty", []redact.Rule{regex(``, "CODE", redact.ActionReplace)}, false},
		{"regex too long", []redact.Rule{regex(strings.Repeat("a", redact.MaximumPattern+1), "CODE", redact.ActionReplace)}, false},
		{"bad label", []redact.Rule{regex(`\d{4}`, "code", redact.ActionReplace)}, false},
		{"bad action", []redact.Rule{regex(`\d{4}`, "CODE", "shred")}, false},
		{"bad kind", []redact.Rule{{Kind: "magic", Label: "CODE", Action: redact.ActionReplace, Enabled: true}}, false},
		{"disabled rules are not compiled", []redact.Rule{{Kind: "magic", Label: "x"}}, true},
		{"too many rules", make([]redact.Rule, redact.MaximumRules+1), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			validateErr := redact.Validate(testCase.rules)
			_, engineErr := redact.NewEngine(testCase.rules, secret, "scope")
			if (validateErr == nil) != testCase.valid || (engineErr == nil) != testCase.valid {
				t.Fatalf("valid=%v, Validate=%v, NewEngine=%v", testCase.valid, validateErr, engineErr)
			}
		})
	}
}

func TestCaptureGroupSelectsRedactedSpan(t *testing.T) {
	rule := redact.Rule{Kind: redact.KindRegex, Pattern: `id=(?P<value>\d+)`, Capture: "value", Label: "CODE", Action: redact.ActionReplace, Enabled: true}
	replaced, _ := engine(t, rule).NewScope().Replace("id=12345 ok", "text")
	if !strings.HasPrefix(replaced, "id=[CODE_") || !strings.HasSuffix(replaced, "] ok") {
		t.Fatalf("capture group ignored: %q", replaced)
	}
}

func TestBlockAndAlertActions(t *testing.T) {
	block := engine(t, builtin("email", "EMAIL"), regex(`\d{4}`, "CODE", redact.ActionBlock))
	scope := block.NewScope()
	text, result := scope.Replace("a@example.com 1234", "text")
	if !result.Blocked || text != "" {
		t.Fatalf("block rule must stop the text: %q %+v", text, result)
	}

	alert := engine(t, regex(`\d{4}`, "CODE", redact.ActionAlert)).NewScope()
	text, result = alert.Replace("code 1234", "text")
	if result.Blocked || text != "code 1234" || len(result.Hits) != 1 || result.Hits[0].Action != redact.ActionAlert || result.Values() != 0 {
		t.Fatalf("alert rule must only record: %q %+v", text, result)
	}
}

func TestHitJSONShape(t *testing.T) {
	_, result := engine(t, builtin("email", "EMAIL")).NewScope().Replace("a@example.com", "messages[0]")
	encoded, err := json.Marshal(result.Hits[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != `{"label":"EMAIL","action":"replace","path":"messages[0]","start":0,"end":13}` {
		t.Fatalf("hit shape changed: %s", encoded)
	}
}

func TestPlaceholdersAreStableAcrossScopesAndDifferPerNamespace(t *testing.T) {
	compiled := engine(t, builtin("email", "EMAIL"))
	first, _ := compiled.NewScope().Replace("a@example.com", "text")
	second, _ := compiled.NewScope().Replace("a@example.com", "text")
	if first != second {
		t.Fatalf("placeholder is not stable: %q vs %q", first, second)
	}
	other, err := redact.NewEngine([]redact.Rule{builtin("email", "EMAIL")}, secret, "other")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	third, _ := other.NewScope().Replace("a@example.com", "text")
	if third == first {
		t.Fatalf("placeholders must differ per namespace")
	}
}

func collidingPair(t *testing.T) (string, string) {
	t.Helper()
	compiled := engine(t, regex(`\d{6}`, "CODE", redact.ActionReplace))
	seen := map[string]string{}
	for number := 0; number < 200000; number++ {
		value := fmt.Sprintf("%06d", number)
		replaced, _ := compiled.NewScope().Replace(value, "text")
		if previous, found := seen[replaced]; found {
			return previous, value
		}
		seen[replaced] = value
	}
	t.Fatal("no short placeholder collision found")
	return "", ""
}

func TestCollidingShortPlaceholdersUpgradeBothValues(t *testing.T) {
	// 32-bit short digests collide rarely, so the test hunts one down instead of faking a hash.
	left, right := collidingPair(t)
	compiled := engine(t, regex(`\d{6}`, "CODE", redact.ActionReplace))

	request := userRequest(left + " " + right + " " + left)
	scope := compiled.NewScope()
	if _, err := scope.ApplyRequest(request); err != nil {
		t.Fatalf("apply: %v", err)
	}
	redacted := request.Items[0].Parts[0].Text
	tokens := strings.Fields(redacted)
	if tokens[0] == tokens[1] || tokens[0] != tokens[2] {
		t.Fatalf("colliding values must get distinct stable placeholders: %q", redacted)
	}
	for _, token := range tokens[:2] {
		if len(token) != len("[CODE_]")+16 {
			t.Fatalf("both colliding values must use the long form: %q", token)
		}
	}
	if scope.Restore(redacted) != left+" "+right+" "+left {
		t.Fatalf("collision broke restoration: %q", scope.Restore(redacted))
	}

	sequential := compiled.NewScope()
	first, _ := sequential.Replace(left, "one")
	second, _ := sequential.Replace(right, "two")
	if first == second || sequential.Restore(first) != left || sequential.Restore(second) != right {
		t.Fatalf("an issued placeholder must stay restorable: %q %q", first, second)
	}
}

func TestRestore(t *testing.T) {
	scope := engine(t, builtin("email", "EMAIL")).NewScope()
	token, _ := scope.Replace("ops@example.com", "text")
	cases := []struct {
		name, input, want string
	}{
		{"plain", "mail " + token + " now", "mail ops@example.com now"},
		{"nested brackets", "[see " + token + "]", "[see ops@example.com]"},
		{"double nested", "[[" + token + "]]", "[[ops@example.com]]"},
		{"unknown placeholder", "[EMAIL_00000000]", "[EMAIL_00000000]"},
		{"plain text untouched", "array[0] and [not a token] and trailing [", "array[0] and [not a token] and trailing ["},
		{"empty", "", ""},
		{"repeated", token + token, "ops@example.com" + "ops@example.com"},
	}
	for _, testCase := range cases {
		if got := scope.Restore(testCase.input); got != testCase.want {
			t.Fatalf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

func TestRestoreNeverRestoresTwice(t *testing.T) {
	// The wrapped value's original text contains another issued placeholder; one pass leaves it literal.
	nestedScope := engine(t, regex(`secret\[[A-Z_0-9a-f]+\]`, "WRAP", redact.ActionReplace), builtin("cn_mobile", "PHONE")).NewScope()
	phoneToken, _ := nestedScope.Replace("13812345678", "text")
	wrapped, _ := nestedScope.Replace("secret"+phoneToken, "text")
	if got := nestedScope.Restore(wrapped); got != "secret"+phoneToken {
		t.Fatalf("restored value was restored again: %q", got)
	}

	unissued := "[PHONE_deadbeef]"
	if got := nestedScope.Restore(phoneToken + unissued); got != "13812345678"+unissued {
		t.Fatalf("unissued placeholder changed: %q", got)
	}
}

func TestStreamingRestoreAcrossChunks(t *testing.T) {
	scope := engine(t, builtin("cn_mobile", "PHONE"), builtin("email", "EMAIL")).NewScope()
	phone, _ := scope.Replace("13812345678", "text")
	email, _ := scope.Replace("ops@example.com", "text")
	payload := strings.Repeat("[", 8) + " items = [\n {\"content\": \"" + strings.Repeat("x", 200) + "\"}\n] 号码 " + phone + " [see " + email + "] 结束 ["
	expected := strings.NewReplacer(phone, "13812345678", email, "ops@example.com").Replace(payload)
	for _, chunk := range []int{1, 2, 3, 7, 16, 41, 43, len(payload)} {
		if got := stream(scope.TextRestorer(), payload, chunk); got != expected {
			t.Fatalf("chunk %d altered text\nwant %q\n got %q", chunk, expected, got)
		}
	}
	if got := scope.Restore(payload); got != expected {
		t.Fatalf("one-shot restore diverged from streaming: %q", got)
	}
}

func TestRestorerHoldsPartialPlaceholderUntilFlush(t *testing.T) {
	scope := engine(t, builtin("email", "EMAIL")).NewScope()
	token, _ := scope.Replace("ops@example.com", "text")
	restorer := scope.TextRestorer()
	if emitted := restorer.Push(token[:6]); emitted != "" {
		t.Fatalf("a possible placeholder prefix must be held back: %q", emitted)
	}
	if flushed := restorer.Flush(); flushed != token[:6] {
		t.Fatalf("flush must release the held text verbatim: %q", flushed)
	}
	if flushed := restorer.Flush(); flushed != "" {
		t.Fatalf("flush must be idempotent: %q", flushed)
	}
}

func TestJSONRestorer(t *testing.T) {
	scope := engine(t, builtin("email", "EMAIL"), regex(`quote-[a-z]+`, "QUOTED", redact.ActionReplace)).NewScope()
	email, _ := scope.Replace("ops@example.com", "text")
	quoted, _ := scope.Replace("quote-abc", "text")
	cases := []struct {
		name, input, want string
	}{
		{"value", `{"to":"` + email + `","count":2}`, `{"to":"ops@example.com","count":2}`},
		{"escaped quote before token", `{"a":"\"` + email + `","b":"` + email + `"}`, `{"a":"\"ops@example.com","b":"ops@example.com"}`},
		{"escaped backslash ends string", `{"a":"\\","b":"` + email + `"}`, `{"a":"\\","b":"ops@example.com"}`},
		{"outside strings untouched", `[` + email + `]`, `[` + email + `]`},
		{"truncated string", `{"to":"` + email + `","note":"[EMAIL_ab`, `{"to":"ops@example.com","note":"[EMAIL_ab`},
		{"truncated escape", `{"to":"` + email + `\`, `{"to":"ops@example.com\`},
		{"truncated structure", `{"to":["` + email, `{"to":["ops@example.com`},
		{"issued token", `{"q":"` + quoted + `"}`, `{"q":"quote-abc"}`},
	}
	for _, testCase := range cases {
		for _, chunk := range []int{1, 5, len(testCase.input)} {
			if got := stream(scope.JSONRestorer(), testCase.input, chunk); got != testCase.want {
				t.Fatalf("%s (chunk %d): got %s, want %s", testCase.name, chunk, got, testCase.want)
			}
		}
	}
}

func TestJSONRestorerEscapesRestoredValue(t *testing.T) {
	scope := engine(t, regex(`<[^>]*>`, "TAG", redact.ActionReplace)).NewScope()
	original := "<a \"b\"\\c\nd\te>"
	token, _ := scope.Replace(original, "text")
	input := `{"v":"` + token + `"}`
	restored := stream(scope.JSONRestorer(), input, 3)
	var decoded struct{ V string }
	if err := json.Unmarshal([]byte(restored), &decoded); err != nil {
		t.Fatalf("restored arguments are not valid JSON: %q: %v", restored, err)
	}
	if decoded.V != original {
		t.Fatalf("value changed: %q", decoded.V)
	}
	if text := scope.Restore(token); text != original {
		t.Fatalf("text mode must not escape: %q", text)
	}
}

func TestApplyRequestCoversEveryTextPosition(t *testing.T) {
	compiled := engine(t, builtin("email", "EMAIL"))
	address := "ops@example.com"
	mediaData := base64.StdEncoding.EncodeToString([]byte("note: " + address))
	request := &model.Request{
		Instructions:   "instructions " + address,
		User:           address,
		PromptCacheKey: "cache-" + address,
		Metadata:       map[string]string{"owner-" + address: "value " + address, "plain": "p"},
		ResponseFormat: &model.ResponseFormat{Kind: "json_schema", Name: "answer", Description: "desc " + address, Schema: []byte(`{"title":"` + address + `"}`)},
		Raw:            &model.Raw{Format: model.Chat, Fields: map[string]json.RawMessage{"x": json.RawMessage(`{"a":"` + address + `"}`)}},
		Items: []model.Item{
			{Kind: model.ItemMessage, Role: model.RoleDeveloper, Parts: []model.Part{{Kind: model.PartText, Text: "dev " + address}}, Raw: &model.Raw{Fields: map[string]json.RawMessage{"m": json.RawMessage(`"` + address + `"`)}}},
			{Kind: model.ItemMessage, Role: model.RoleUser, Parts: []model.Part{
				{Kind: model.PartText, Text: "hi " + address, Raw: &model.Raw{Fields: map[string]json.RawMessage{"b": json.RawMessage(`"` + address + `"`)}}, Citations: []model.Citation{{CitedText: "cited " + address, Title: "title " + address}}},
				{Kind: model.PartMedia, Media: &model.Media{Kind: "document", MIMEType: "text/plain", Data: mediaData}, Raw: &model.Raw{Fields: map[string]json.RawMessage{"e": json.RawMessage(`"` + address + `"`)}}, Citations: []model.Citation{{CitedText: "media " + address}}},
			}},
			{Kind: model.ItemToolResult, Role: model.RoleTool, CallID: "c1", Parts: []model.Part{{Kind: model.PartText, Text: "result " + address}}},
			{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "c1", Name: "send", Arguments: `{"to":"` + address + `"}`}},
			{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolCustom, ID: "c2", Name: "patch", Arguments: "raw " + address}},
		},
		Tools: []model.Tool{{Kind: model.ToolFunction, Name: "send", Description: "tool " + address, Parameters: []byte(`{"description":"` + address + `"}`), Raw: &model.Raw{Fields: map[string]json.RawMessage{"t": json.RawMessage(`"` + address + `"`)}}}},
	}
	scope := compiled.NewScope()
	result, err := scope.ApplyRequest(request)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), address) {
		t.Fatalf("a position was not redacted: %s", encoded)
	}
	if result.Blocked || len(result.Hits) != 23 || result.Values() != 23 {
		t.Fatalf("unexpected result: blocked=%v hits=%d values=%d", result.Blocked, len(result.Hits), result.Values())
	}
	if len(request.Metadata) != 2 || request.Metadata["plain"] != "p" {
		t.Fatalf("metadata keys were not rewritten consistently: %+v", request.Metadata)
	}

	decoded, err := base64.StdEncoding.DecodeString(request.Items[1].Parts[1].Media.Data)
	if err != nil {
		t.Fatalf("media data must stay base64: %v", err)
	}
	if got := scope.Restore(string(decoded)); got != "note: "+address {
		t.Fatalf("media round trip failed: %q", got)
	}
	if got := scope.Restore(request.Items[4].ToolCall.Arguments); got != "raw "+address {
		t.Fatalf("custom tool arguments must be redacted as text: %q", got)
	}
	if got := stream(scope.JSONRestorer(), request.Items[3].ToolCall.Arguments, 4); got != `{"to":"`+address+`"}` {
		t.Fatalf("function tool arguments round trip failed: %q", got)
	}
}

func TestApplyRequestMediaEdgeCases(t *testing.T) {
	compiled := engine(t, builtin("email", "EMAIL"))
	binary := base64.StdEncoding.EncodeToString([]byte("a@example.com"))
	untouched := userRequest("")
	untouched.Items[0].Parts = []model.Part{{Kind: model.PartMedia, Media: &model.Media{Kind: "image", MIMEType: "image/png", Data: binary}}}
	if _, err := compiled.NewScope().ApplyRequest(untouched); err != nil || untouched.Items[0].Parts[0].Media.Data != binary {
		t.Fatalf("binary media must not be touched: %v", err)
	}

	for _, media := range []*model.Media{
		{Kind: "document", MIMEType: "text/plain", Data: "***not base64***"},
		{Kind: "document", URL: "https://example.com/?u=a@example.com"},
		{Kind: "document", Filename: "a@example.com.txt"},
	} {
		request := userRequest("")
		request.Items[0].Parts = []model.Part{{Kind: model.PartMedia, Media: media}}
		if _, err := compiled.NewScope().ApplyRequest(request); !errors.Is(err, redact.ErrImmutablePosition) {
			t.Fatalf("expected immutable position for %+v, got %v", media, err)
		}
	}
}

func TestApplyRequestRejectsImmutablePositions(t *testing.T) {
	compiled := engine(t, builtin("email", "EMAIL"))
	call := func(id, name string) *model.Request {
		return &model.Request{Items: []model.Item{{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: id, Name: name, Arguments: "{}"}}}}
	}
	cases := map[string]*model.Request{
		"tool call id":   call("a@example.com", "read"),
		"tool call name": call("c1", "a@example.com"),
		"tool name":      {Tools: []model.Tool{{Kind: model.ToolFunction, Name: "a@example.com"}}},
		"stop sequence":  {Stop: []string{"a@example.com"}},
		"tool choice":    {ToolChoice: &model.ToolChoice{Mode: "tool", Names: []string{"a@example.com"}}},
		"schema name":    {ResponseFormat: &model.ResponseFormat{Kind: "json_schema", Name: "a@example.com"}},
		"json key":       {Tools: []model.Tool{{Kind: model.ToolFunction, Name: "t", Parameters: []byte(`{"a@example.com":1}`)}}},
		"json number":    {Raw: &model.Raw{Format: model.Chat, Fields: map[string]json.RawMessage{"x": json.RawMessage(`13812345678`)}}},
	}
	numeric := engine(t, builtin("email", "EMAIL"), builtin("cn_mobile", "PHONE"))
	for name, request := range cases {
		if _, err := numeric.NewScope().ApplyRequest(request); !errors.Is(err, redact.ErrImmutablePosition) {
			t.Fatalf("%s: expected immutable position, got %v", name, err)
		}
	}

	alert := engine(t, redact.Rule{Kind: redact.KindBuiltin, Detector: "email", Label: "EMAIL", Action: redact.ActionAlert, Enabled: true})
	if _, err := alert.NewScope().ApplyRequest(call("a@example.com", "read")); err != nil {
		t.Fatalf("alert rules must not reject: %v", err)
	}
	if _, err := compiled.NewScope().ApplyRequest(call("call_20250101123456789", "read")); err != nil {
		t.Fatalf("non-matching identifier rejected: %v", err)
	}

	scope := compiled.NewScope()
	text, _ := scope.Replace("ops@example.com", "text")
	if _, err := scope.ApplyRequest(call(text, "read")); !errors.Is(err, redact.ErrImmutablePosition) {
		t.Fatalf("an identifier equal to an issued placeholder must be rejected: %v", err)
	}
}

func TestApplyRequestBlockedAndEmpty(t *testing.T) {
	blocking := engine(t, redact.Rule{Kind: redact.KindBuiltin, Detector: "email", Label: "EMAIL", Action: redact.ActionBlock, Enabled: true})
	request := userRequest("write to a@example.com")
	result, err := blocking.NewScope().ApplyRequest(request)
	if err != nil || !result.Blocked || len(result.Hits) != 1 || strings.Contains(request.Items[0].Parts[0].Text, "example") {
		t.Fatalf("blocked request must report the hit and clear the text: %v %+v", err, result)
	}

	empty := engine(t)
	request = userRequest("a@example.com")
	result, err = empty.NewScope().ApplyRequest(request)
	if err != nil || result.Blocked || len(result.Hits) != 0 || request.Items[0].Parts[0].Text != "a@example.com" {
		t.Fatalf("an empty engine must leave the request alone: %v %+v", err, result)
	}
}

func TestReasoningSignatureSurvivesRedactionOfTheTextItWasIssuedFor(t *testing.T) {
	compiled := engine(t, builtin("email", "EMAIL"))
	build := func(text, digest string) *model.Request {
		request := userRequest("")
		request.Items = []model.Item{{Kind: model.ItemReasoning, Role: model.RoleAssistant, Parts: []model.Part{{Kind: model.PartReasoningText, Text: text}}, Reasoning: &model.Reasoning{Visibility: "full", Signature: &model.Signature{Blob: "b", TextDigest: digest}}}}
		return request
	}
	shown := "mail a@example.com"
	issued := build(shown, model.TextDigest(shown))
	if _, err := compiled.NewScope().ApplyRequest(issued); err != nil || issued.Items[0].Reasoning.Signature == nil {
		t.Fatalf("re-redacting the shown text restores the provider's text, so the signature stays: %v", err)
	}
	if strings.Contains(issued.Items[0].Text(), "a@example.com") {
		t.Fatalf("the reasoning text must still be redacted: %q", issued.Items[0].Text())
	}
	unproven := build(shown, "stale")
	if _, err := compiled.NewScope().ApplyRequest(unproven); err != nil || unproven.Items[0].Reasoning.Signature != nil {
		t.Fatalf("a rewritten text without proof of origin loses its signature: %v", err)
	}
	unchanged := build("nothing to hide", "stale")
	if _, err := compiled.NewScope().ApplyRequest(unchanged); err != nil || unchanged.Items[0].Reasoning.Signature == nil {
		t.Fatalf("an untouched reasoning text keeps its signature: %v", err)
	}
}

func TestCredentialDetectors(t *testing.T) {
	stripeKey := "sk_" + "live_" + strings.Repeat("a1B2", 3) + strings.Repeat("c3D4", 3)
	cases := []struct {
		detector, text, secret string
	}{
		{"openai_key", "key sk-" + strings.Repeat("a1B2", 12) + " here", "sk-" + strings.Repeat("a1B2", 12)},
		{"google_api_key", "AIzaSyA1234567890abcdefghijklmnopqrstuv", "AIzaSyA1234567890abcdefghijklmnopqrstuv"},
		{"slack_token", "token xoxb-1234567890-abcdefghij", "xoxb-1234567890-abcdefghij"},
		{"stripe_key", stripeKey, stripeKey},
		{"bearer_token", "Authorization: Bearer abcdef0123456789abcdef", "abcdef0123456789abcdef"},
		{"high_entropy_token", "secret=Zx9Qw3Er7Ty1Ui5Op2As6Df8Gh4Jk0LmNb", "Zx9Qw3Er7Ty1Ui5Op2As6Df8Gh4Jk0LmNb"},
	}
	for _, testCase := range cases {
		t.Run(testCase.detector, func(t *testing.T) {
			replaced, result := engine(t, builtin(testCase.detector, "SECRET")).NewScope().Replace(testCase.text, "text")
			if len(result.Hits) != 1 || strings.Contains(replaced, testCase.secret) {
				t.Fatalf("%q was not redacted: %q %+v", testCase.secret, replaced, result.Hits)
			}
		})
	}
	if _, result := engine(t, builtin("high_entropy_token", "SECRET")).NewScope().Replace("the_quick_brown_fox_jumps_over_the_lazy_dog", "text"); len(result.Hits) != 0 {
		t.Fatalf("an identifier made of words is not a secret: %+v", result.Hits)
	}
	if _, result := engine(t, builtin("openai_key", "SECRET")).NewScope().Replace("sk-"+strings.Repeat("a", 60), "text"); len(result.Hits) != 0 {
		t.Fatalf("part of a longer token must not match: %+v", result.Hits)
	}
}

func TestUnterminatedPrivateKeyStopsAtTheKeyMaterial(t *testing.T) {
	text := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASC\nAAAA==\nThanks, see you tomorrow."
	replaced, _ := engine(t, builtin("pem_private_key", "PRIVATE_KEY")).NewScope().Replace(text, "text")
	if strings.Contains(replaced, "MIIEvQ") || !strings.Contains(replaced, "see you tomorrow") {
		t.Fatalf("the key must be redacted and the prose after it kept: %q", replaced)
	}
}

func TestAuxiliaryBodyRedactsValuesAndPreservesModel(t *testing.T) {
	compiled := engine(t, builtin("email", "EMAIL"))
	scope := compiled.NewScope()
	out, result, err := scope.ApplyAuxiliaryBody([]byte(`{"model":"m","input":"me@example.com"}`))
	if err != nil || result.Blocked {
		t.Fatalf("apply: %v %+v", err, result)
	}
	if !strings.Contains(string(out), `"model":"m"`) {
		t.Fatalf("the model must stay intact: %s", out)
	}
	if strings.Contains(string(out), "me@example.com") || !strings.Contains(string(out), "EMAIL") {
		t.Fatalf("the auxiliary value must be redacted: %s", out)
	}
}

func TestAuxiliaryBodyRefusesASensitiveKey(t *testing.T) {
	compiled := engine(t, builtin("email", "EMAIL"))
	scope := compiled.NewScope()
	_, _, err := scope.ApplyAuxiliaryBody([]byte(`{"model":"m","input":{"me@example.com":"value"}}`))
	if !errors.Is(err, redact.ErrImmutablePosition) {
		t.Fatalf("a sensitive object key must be refused, got %v", err)
	}
}

func TestStructuralRequestFieldsAreChecked(t *testing.T) {
	compiled := engine(t, builtin("email", "EMAIL"))
	request := userRequest("hello")
	request.Stop = []string{"me@example.com"}
	if _, err := compiled.NewScope().ApplyRequest(request); !errors.Is(err, redact.ErrImmutablePosition) {
		t.Fatalf("a matched stop sequence must be refused, got %v", err)
	}
}
