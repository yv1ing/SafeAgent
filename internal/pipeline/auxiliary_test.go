package pipeline_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/pipeline"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

func TestCountTokensForwardsToTheUpstream(t *testing.T) {
	rig := newRigFor(t, string(model.Messages), func(testkit.Received) testkit.Reply {
		return testkit.Reply{Status: 200, Body: `{"input_tokens":42}`}
	})
	count, estimated, failure := rig.pipeline.CountTokens(context.Background(), pipeline.Request{
		Kind: model.Messages, Header: http.Header{"Authorization": {"Bearer agent-key"}},
		Body: []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`),
	})
	if failure != nil || estimated || count != 42 {
		t.Fatalf("count=%d estimated=%v failure=%v", count, estimated, failure)
	}
	if path := rig.upstream.Requests()[0].Path; !strings.HasSuffix(path, "/count_tokens") {
		t.Fatalf("the native counting endpoint must be used: %s", path)
	}
	call := rig.store.Calls[0]
	if call.ChannelID != "channel_1" || call.ModelID != "model_1" || call.UpstreamModel != "gpt-5-upstream" || call.EndpointFormat != "anthropic_messages" {
		t.Fatalf("the counting call must name its channel model: %+v", call)
	}
}

func TestCountTokensFallsBackToAnEstimate(t *testing.T) {
	rig := newRig(t, nil)
	count, estimated, failure := rig.pipeline.CountTokens(context.Background(), pipeline.Request{
		Kind: model.Chat, Header: http.Header{"Authorization": {"Bearer agent-key"}},
		Body: []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"a fairly long question"}]}`),
	})
	if failure != nil || !estimated || count <= 0 {
		t.Fatalf("count=%d estimated=%v failure=%v", count, estimated, failure)
	}
	if len(rig.upstream.Requests()) != 0 {
		t.Fatal("an estimate must not call the upstream")
	}
}

func TestEmbeddingsPassThroughAndAreAudited(t *testing.T) {
	rig := newRig(t, func(testkit.Received) testkit.Reply {
		return testkit.Reply{Status: 200, Body: `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}]}`}
	})
	rig.store.Models[0].Kind = entity.ModelEmbedding
	payload, failure := rig.pipeline.Passthrough(context.Background(), pipeline.Request{
		Kind: model.Chat, Header: http.Header{"Authorization": {"Bearer agent-key"}},
		Body: []byte(`{"model":"gpt-5","input":"hello me@example.com"}`),
	}, provider.AuxiliaryEmbeddings)
	if failure != nil || !strings.Contains(string(payload), "0.1") {
		t.Fatalf("payload=%s failure=%v", payload, failure)
	}
	sent := rig.upstream.Requests()[0]
	if !strings.HasSuffix(sent.Path, "/embeddings") {
		t.Fatalf("the same-format auxiliary endpoint must be used: %s", sent.Path)
	}
	if strings.Contains(sent.Body, "me@example.com") {
		t.Fatalf("the auxiliary request must be redacted: %s", sent.Body)
	}
	if !strings.Contains(sent.Body, `"model":"gpt-5-upstream"`) {
		t.Fatalf("the route must name the upstream model: %s", sent.Body)
	}
	if call := rig.store.Calls[0]; call.Kind != "embeddings" || call.State != "completed" {
		t.Fatalf("the auxiliary call must be audited: %+v", call)
	}
	call := rig.store.Calls[0]
	if call.ChannelID != "channel_1" || call.ModelID != "model_1" || call.UpstreamModel != "gpt-5-upstream" || call.EndpointFormat != "openai_chat" {
		t.Fatalf("the auxiliary call must name its channel model: %+v", call)
	}
}

func TestAuxiliaryUsesOnlyTheRoutedFormat(t *testing.T) {
	rig := newRig(t, func(testkit.Received) testkit.Reply { return testkit.Reply{Body: `{"data":[]}`} })
	rig.store.Registry = provider.NewRegistry(provider.ProviderSpec{
		ID: "test", Label: "Test",
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Chat: {
				Format: model.Chat, BaseURL: rig.upstream.URL, Path: "/chat/completions", Auth: provider.AuthBearer,
				Auxiliary: []provider.AuxiliarySpec{{Kind: provider.AuxiliaryEmbeddings, Path: "/embeddings"}},
			},
			model.Messages: {
				Format: model.Messages, BaseURL: rig.upstream.URL, Path: "/messages", Auth: provider.AuthAPIKey,
				Auxiliary: []provider.AuxiliarySpec{{Kind: provider.AuxiliaryEmbeddings, Path: "/messages/embeddings"}},
			},
		},
	})
	rig.store.Channel[0].Endpoints = []entity.ChannelEndpoint{{Format: string(model.Chat)}, {Format: string(model.Messages)}}
	rig.store.Models[0].Kind = entity.ModelEmbedding
	rig.store.Models[0].Formats = []string{string(model.Messages)}
	rig.pipeline = pipeline.New(rig.store, []byte(testkit.InstanceSecret), pipeline.Options{
		Providers: rig.store.Registry, Selector: rig.selector, Limiter: rig.limiter,
	})
	payload, failure := rig.pipeline.Passthrough(context.Background(), pipeline.Request{
		Kind: model.Chat, Header: http.Header{"Authorization": {"Bearer agent-key"}},
		Body: []byte(`{"model":"gpt-5","input":"hello"}`),
	}, provider.AuxiliaryEmbeddings)
	if failure != nil {
		t.Fatalf("failure=%+v", failure)
	}
	if len(payload) == 0 {
		t.Fatal("the auxiliary response is empty")
	}
	requests := rig.upstream.Requests()
	if len(requests) != 1 || !strings.HasSuffix(requests[0].Path, "/messages/embeddings") {
		t.Fatalf("the routed Messages format must serve the call: %+v", requests)
	}
}

func TestEmbeddingModelRequiresAnAuxiliaryFormat(t *testing.T) {
	rig := newRig(t, nil)
	rig.store.Models[0].Kind = entity.ModelEmbedding
	rig.store.Registry = provider.NewRegistry(provider.ProviderSpec{
		ID: "test", Label: "Test",
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Chat: {Format: model.Chat, BaseURL: rig.upstream.URL, Path: "/chat/completions", Auth: provider.AuthBearer},
		},
	})
	rig.pipeline = pipeline.New(rig.store, []byte(testkit.InstanceSecret), pipeline.Options{
		Providers: rig.store.Registry, Selector: rig.selector, Limiter: rig.limiter,
	})
	_, failure := rig.pipeline.Passthrough(context.Background(), pipeline.Request{
		Kind: model.Chat, Header: http.Header{"Authorization": {"Bearer agent-key"}},
		Body: []byte(`{"model":"gpt-5","input":"hello"}`),
	}, provider.AuxiliaryEmbeddings)
	if failure == nil || failure.Code != "model_not_found" {
		t.Fatalf("failure=%+v", failure)
	}
}

func TestStoredResponseLifecycle(t *testing.T) {
	rig := newRig(t, func(testkit.Received) testkit.Reply { return testkit.Reply{Frames: testkit.ChatText("stored answer")} })
	writer := rig.send(pipeline.Request{Kind: model.Responses, Header: http.Header{"Authorization": {"Bearer agent-key"}}, Body: []byte(`{"model":"gpt-5","store":true,"input":"hi"}`)})
	if writer.Code != 200 {
		t.Fatalf("%d %s", writer.Code, writer.Body)
	}
	responseID := rig.store.Calls[0].ResponseID
	if responseID == "" {
		t.Fatal("a stored response must have an id")
	}
	key := pipeline.BearerToken(http.Header{"Authorization": {"Bearer agent-key"}}, "")
	payload, failure := rig.pipeline.RetrieveResponse(context.Background(), key, responseID)
	if failure != nil || len(payload) == 0 {
		t.Fatalf("payload=%d failure=%v", len(payload), failure)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil || decoded["object"] != "response" {
		t.Fatalf("stored response shape: %s", payload)
	}
	items, _, failure := rig.pipeline.InputItems(context.Background(), key, responseID)
	if failure != nil || len(items) == 0 {
		t.Fatalf("items=%d failure=%v", len(items), failure)
	}
	if failure := rig.pipeline.DeleteResponse(context.Background(), key, responseID); failure != nil {
		t.Fatal(failure)
	}
	if _, failure := rig.pipeline.RetrieveResponse(context.Background(), key, responseID); failure == nil {
		t.Fatal("a deleted response must not be retrievable")
	}
}

func TestAuxiliaryRedactionBlockIsAudited(t *testing.T) {
	rig := newRig(t, nil)
	rig.store.RuleList = []entity.Rule{{ID: "rule_block", Kind: "builtin", Detector: "email", Label: "EMAIL", Action: "block", Enabled: true, Version: 1}}
	_, failure := rig.pipeline.Passthrough(context.Background(), pipeline.Request{
		Kind: model.Chat, Header: http.Header{"Authorization": {"Bearer agent-key"}},
		Body: []byte(`{"model":"gpt-5","input":"me@example.com"}`),
	}, provider.AuxiliaryEmbeddings)
	if failure == nil || failure.Code != "sensitive_content_blocked" {
		t.Fatalf("failure=%+v", failure)
	}
	if len(rig.store.Calls) != 1 || rig.store.Calls[0].Kind != "embeddings" || rig.store.Calls[0].State != "blocked" {
		t.Fatalf("a blocked auxiliary call must be recorded: %+v", rig.store.Calls)
	}
	if len(rig.upstream.Requests()) != 0 {
		t.Fatal("a blocked auxiliary call must not reach the upstream")
	}
}

func TestAuxiliaryEgressIsRefusedWhenTheCallCannotBeRecorded(t *testing.T) {
	rig := newRig(t, func(testkit.Received) testkit.Reply {
		return testkit.Reply{Status: 200, Body: `{"data":[]}`}
	})
	rig.store.FailCreate = true
	_, failure := rig.pipeline.Passthrough(context.Background(), pipeline.Request{
		Kind: model.Chat, Header: http.Header{"Authorization": {"Bearer agent-key"}},
		Body: []byte(`{"model":"gpt-5","input":"hello"}`),
	}, provider.AuxiliaryEmbeddings)
	if failure == nil || failure.Code != "audit_unavailable" {
		t.Fatalf("failure=%+v", failure)
	}
	if len(rig.upstream.Requests()) != 0 {
		t.Fatal("content must not reach an upstream without an audit record")
	}
}
