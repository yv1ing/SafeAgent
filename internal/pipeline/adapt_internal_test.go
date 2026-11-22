package pipeline

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/codec/chat"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/gemini"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/messages"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
)

func rawEndpoint(format model.Format, capability model.Capability, compat model.Compat) endpoint {
	return endpoint{format: format, endpoint: provider.EndpointSpec{Capability: capability, Compat: compat}}
}

func rawPointer[T any](value T) *T { return &value }

func TestRawRequestSafetyRejectsCanonicalRewrites(t *testing.T) {
	chatCapability := chat.New().BaseCapability()
	messagesCapability := messages.New().BaseCapability()
	geminiCapability := gemini.New().BaseCapability()
	messagesEndpoint := rawEndpoint(model.Messages, messagesCapability, model.Compat{})
	geminiEndpoint := rawEndpoint(model.Gemini, geminiCapability, model.Compat{})
	strictChat := chatCapability
	strictChat.Tools.Strict = false
	noStreamUsage := chatCapability
	noStreamUsage.StreamUsage = false

	cases := []struct {
		name     string
		request  *model.Request
		endpoint endpoint
		safe     bool
	}{
		{"chat plain", &model.Request{Model: "m"}, rawEndpoint(model.Chat, chatCapability, model.Compat{}), true},
		{"chat store", &model.Request{Model: "m", Store: rawPointer(false)}, rawEndpoint(model.Chat, chatCapability, model.Compat{}), false},
		{"chat stream usage unsupported", &model.Request{Model: "m", StreamOptions: &model.StreamOptions{}}, rawEndpoint(model.Chat, noStreamUsage, model.Compat{}), false},
		{"chat automatic effort", &model.Request{Model: "m", Reasoning: &model.ReasoningConfig{Effort: "auto"}}, rawEndpoint(model.Chat, chatCapability, model.Compat{}), false},
		{"chat maximum effort", &model.Request{Model: "m", Reasoning: &model.ReasoningConfig{Effort: "max"}}, rawEndpoint(model.Chat, chatCapability, model.Compat{}), false},
		{"chat ordinary effort", &model.Request{Model: "m", Reasoning: &model.ReasoningConfig{Effort: "high"}}, rawEndpoint(model.Chat, chatCapability, model.Compat{}), true},
		{"chat strict tool unsupported", &model.Request{Model: "m", Tools: []model.Tool{{Name: "read", Kind: model.ToolFunction, Strict: rawPointer(true)}}}, rawEndpoint(model.Chat, strictChat, model.Compat{}), false},
		{"chat parallel without tools", &model.Request{Model: "m", ParallelToolCalls: rawPointer(false)}, rawEndpoint(model.Chat, chatCapability, model.Compat{}), false},
		{"chat top logprobs without logprobs", &model.Request{Model: "m", TopLogprobs: rawPointer(int64(5))}, rawEndpoint(model.Chat, chatCapability, model.Compat{}), false},
		{"chat top logprobs with logprobs", &model.Request{Model: "m", Logprobs: rawPointer(true), TopLogprobs: rawPointer(int64(5))}, rawEndpoint(model.Chat, chatCapability, model.Compat{}), true},
		{"chat required choice without tools", &model.Request{Model: "m", ToolChoice: &model.ToolChoice{Mode: "required"}}, rawEndpoint(model.Chat, chatCapability, model.Compat{}), false},
		{"messages minimal effort", &model.Request{Model: "m", Reasoning: &model.ReasoningConfig{Effort: "minimal"}}, messagesEndpoint, false},
		{"messages small thinking budget", &model.Request{Model: "m", MaxOutputTokens: rawPointer(int64(4096)), Reasoning: &model.ReasoningConfig{BudgetTokens: rawPointer(int64(512))}}, messagesEndpoint, false},
		{"messages thinking budget inside limit", &model.Request{Model: "m", MaxOutputTokens: rawPointer(int64(4096)), Reasoning: &model.ReasoningConfig{BudgetTokens: rawPointer(int64(2048))}}, messagesEndpoint, true},
		{"messages thinking budget above limit", &model.Request{Model: "m", MaxOutputTokens: rawPointer(int64(1024)), Reasoning: &model.ReasoningConfig{BudgetTokens: rawPointer(int64(2048))}}, messagesEndpoint, false},
		{"messages adaptive budget", &model.Request{Model: "m", MaxOutputTokens: rawPointer(int64(4096)), Reasoning: &model.ReasoningConfig{BudgetTokens: rawPointer(int64(-1))}}, messagesEndpoint, true},
		{"messages allowed tools", &model.Request{Model: "m", Tools: []model.Tool{{Name: "read"}}, ToolChoice: &model.ToolChoice{Mode: "allowed", Names: []string{"read"}}}, messagesEndpoint, false},
		{"messages developer role", &model.Request{Model: "m", Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleDeveloper}}}, messagesEndpoint, false},
		{"messages system media", &model.Request{Model: "m", Items: []model.Item{{Kind: model.ItemMessage, Role: model.RoleSystem, Parts: []model.Part{{Kind: model.PartMedia}}}}}, messagesEndpoint, false},
		{"messages json object format", &model.Request{Model: "m", ResponseFormat: &model.ResponseFormat{Kind: "json_object"}}, messagesEndpoint, false},
		{"messages named schema", &model.Request{Model: "m", ResponseFormat: &model.ResponseFormat{Kind: "json_schema", Name: "out"}}, messagesEndpoint, false},
		{"messages unnamed schema", &model.Request{Model: "m", ResponseFormat: &model.ResponseFormat{Kind: "json_schema"}}, messagesEndpoint, true},
		{"gemini mapped effort", &model.Request{Model: "gemini-2.5-pro", Reasoning: &model.ReasoningConfig{Effort: "low"}}, geminiEndpoint, false},
		{"gemini thinking level", &model.Request{Model: "gemini-3-pro", Reasoning: &model.ReasoningConfig{Effort: "low"}}, geminiEndpoint, true},
		{"gemini budget and effort", &model.Request{Model: "gemini-3-pro", Reasoning: &model.ReasoningConfig{Effort: "low", BudgetTokens: rawPointer(int64(2048))}}, geminiEndpoint, false},
		{"gemini budget", &model.Request{Model: "gemini-3-pro", Reasoning: &model.ReasoningConfig{BudgetTokens: rawPointer(int64(2048))}}, geminiEndpoint, true},
		{"gemini named schema", &model.Request{Model: "m", ResponseFormat: &model.ResponseFormat{Kind: "json_schema", Strict: rawPointer(true)}}, geminiEndpoint, false},
		{"gemini older target with call ids", &model.Request{Model: "gemini-3-pro", Items: []model.Item{{Kind: model.ItemToolCall, ToolCall: &model.ToolCall{ID: "call_1"}}}}, endpoint{format: model.Gemini, upstreamModel: "gemini-2.5-pro", endpoint: provider.EndpointSpec{Capability: geminiCapability}}, false},
		{"gemini current target with call ids", &model.Request{Model: "gemini-3-pro", Items: []model.Item{{Kind: model.ItemToolCall, ToolCall: &model.ToolCall{ID: "call_1"}}}}, endpoint{format: model.Gemini, upstreamModel: "gemini-3-pro", endpoint: provider.EndpointSpec{Capability: geminiCapability}}, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := rawRequestSafe(test.request, test.endpoint); got != test.safe {
				t.Fatalf("rawRequestSafe = %v, want %v", got, test.safe)
			}
		})
	}
}

func TestRawBodySafetyRejectsLegacyShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		kind model.Format
		safe bool
	}{
		{"chat modern", `{"model":"m","max_completion_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, model.Chat, true},
		{"chat legacy limit", `{"model":"m","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, model.Chat, false},
		{"chat legacy tools", `{"model":"m","functions":[],"messages":[{"role":"user","content":"hi"}]}`, model.Chat, false},
		{"chat legacy call", `{"model":"m","messages":[{"role":"assistant","function_call":{"name":"read","arguments":"{}"}}]}`, model.Chat, false},
		{"chat reasoning alias", `{"model":"m","messages":[{"role":"assistant","reasoning":"so"}]}`, model.Chat, false},
		{"chat thinking alias", `{"model":"m","thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hi"}]}`, model.Chat, false},
		{"chat any choice", `{"model":"m","tool_choice":"any","messages":[{"role":"user","content":"hi"}]}`, model.Chat, false},
		{"messages root system", `{"model":"m","system":"rules","messages":[{"role":"user","content":"hi"}]}`, model.Messages, true},
		{"messages inline system", `{"model":"m","messages":[{"role":"system","content":"rules"},{"role":"user","content":"hi"}]}`, model.Messages, false},
		{"messages top-level parallel", `{"model":"m","parallel_tool_calls":false,"messages":[{"role":"user","content":"hi"}]}`, model.Messages, false},
		{"messages extra metadata", `{"model":"m","metadata":{"user_id":"u","trace":"t"},"messages":[{"role":"user","content":"hi"}]}`, model.Messages, false},
		{"messages user metadata", `{"model":"m","metadata":{"user_id":"u"},"messages":[{"role":"user","content":"hi"}]}`, model.Messages, true},
		{"gemini body", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, model.Gemini, true},
		{"gemini legacy schema", `{"generationConfig":{"responseSchema":{"type":"object"}},"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, model.Gemini, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := rawBodySafe([]byte(test.body), test.kind); got != test.safe {
				t.Fatalf("rawBodySafe = %v, want %v", got, test.safe)
			}
		})
	}
}

func TestRawSafeRequiresFormatPrerequisites(t *testing.T) {
	if rawSafe(&model.Request{}, model.Responses) {
		t.Fatal("Responses requests must use the canonical path")
	}
	if rawSafe(&model.Request{}, model.Messages) {
		t.Fatal("Messages raw requests require an output limit")
	}
	if !rawSafe(&model.Request{MaxOutputTokens: rawPointer(int64(1024))}, model.Messages) {
		t.Fatal("Messages raw requests with an output limit are safe")
	}
	if !rawSafe(&model.Request{}, model.Chat) || !rawSafe(&model.Request{}, model.Gemini) {
		t.Fatal("Chat and Gemini raw requests need no extra prerequisite")
	}
}
