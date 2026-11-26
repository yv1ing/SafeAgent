package pipeline_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/pipeline"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/testkit"
)

func chatReply(received testkit.Received) testkit.Reply {
	return testkit.Reply{Body: `{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`}
}

func TestLaneClassificationThroughAdmission(t *testing.T) {
	t.Run("auxiliary call has no session and no analysis", func(t *testing.T) {
		rig := newRig(t, chatReply)
		count, _, failure := rig.pipeline.CountTokens(context.Background(), pipeline.Request{
			Kind: model.Chat, Operation: classify.CountTokens,
			Header: http.Header{"Authorization": {"Bearer agent-key"}},
			Body:   []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`),
		})
		if failure != nil || count <= 0 {
			t.Fatalf("count=%d failure=%+v", count, failure)
		}
		call := rig.store.Calls[0]
		if call.Lane != classify.LaneAuxiliary || call.Operation != classify.CountTokens || call.Purpose != classify.PurposeCountTokens {
			t.Fatalf("auxiliary classification is wrong: %+v", call)
		}
		if call.SessionID != "" || call.AnalysisState != "skipped" {
			t.Fatalf("an auxiliary call must not join a conversation: %+v", call)
		}
		if call.Outcome != classify.OutcomeOK || call.State != "completed" {
			t.Fatalf("auxiliary outcome is wrong: %+v", call)
		}
		if len(rig.store.Analysis) != 1 {
			t.Fatalf("exactly one seal expected: %+v", rig.store.Analysis)
		}
		seal := rig.store.Analysis[0]
		if seal.Job != nil || seal.Snapshot == nil || seal.RulesVersion == 0 {
			t.Fatalf("an auxiliary call keeps the snapshot but queues no diagnosis: %+v", seal)
		}
	})

	t.Run("auxiliary body hits are parameters", func(t *testing.T) {
		rig := newRig(t, func(testkit.Received) testkit.Reply {
			return testkit.Reply{Body: `{"object":"list","data":[]}`}
		})
		rig.store.Models[0].Kind = entity.ModelEmbedding
		payload, failure := rig.pipeline.Passthrough(context.Background(), pipeline.Request{
			Kind: model.Chat, Operation: classify.Embeddings,
			Header: http.Header{"Authorization": {"Bearer agent-key"}},
			Body:   []byte(`{"model":"gpt-5","input":"mail me@example.com"}`),
		}, provider.AuxiliaryEmbeddings)
		if failure != nil || len(payload) == 0 {
			t.Fatalf("payload=%d failure=%+v", len(payload), failure)
		}
		call := rig.store.Calls[0]
		if call.Lane != classify.LaneAuxiliary || call.Operation != classify.Embeddings || call.SessionID != "" {
			t.Fatalf("embedding call classification is wrong: %+v", call)
		}
		if call.RedactionValues != 1 || len(call.RedactionLabels) != 1 || call.RedactionLabels[0] != "EMAIL" {
			t.Fatalf("the embedding body hit must be counted: %+v", call)
		}
		decoded, err := auditevent.Decode(auditevent.KindRequest, firstEvent(t, rig.store.EventsOf(call.ID), auditevent.KindRequest))
		if err != nil || decoded.Request == nil || len(decoded.Request.Hits) != 1 {
			t.Fatalf("the embedding hit must be stored: %+v %v", decoded.Request, err)
		}
		hit := decoded.Request.Hits[0]
		if hit.Section != "params" || hit.Ordinal != 0 || !strings.HasPrefix(hit.Path, "body") || hit.CallID != call.ID {
			t.Fatalf("an embedding hit belongs to the params section: %+v", hit)
		}
		if len(routingEvents(t, rig)) != 1 {
			t.Fatal("an auxiliary call emits one routing event too")
		}
	})

	t.Run("side call is conversational but not analyzed", func(t *testing.T) {
		rig := newRig(t, chatReply)
		response := rig.send(pipeline.Request{
			Kind: model.Chat, Operation: classify.Generation,
			Header: http.Header{"Authorization": {"Bearer agent-key"}, "X-Openai-Subagent": {"1"}},
			Body:   []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"generate a title"}]}`),
		})
		if response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
		call := rig.store.Calls[0]
		if call.Lane != classify.LaneSide || !call.Purpose.Side() {
			t.Fatalf("side classification is wrong: %+v", call)
		}
		if call.SessionID == "" {
			t.Fatal("a side call writes its conversation")
		}
		if call.AnalysisState != "skipped" || rig.store.Analysis[0].Job != nil {
			t.Fatalf("a side call must not be analyzed: %+v", rig.store.Analysis[0])
		}
		if !call.UsageKnown || call.InputTokens != 7 || call.OutputTokens != 3 || call.CostMicros == nil {
			t.Fatalf("a side call is still counted for tokens and cost: %+v", call)
		}
	})

	t.Run("main call is analyzed and owns its session", func(t *testing.T) {
		rig := newRig(t, chatReply)
		response := rig.chat(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`)
		if response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
		call := rig.store.Calls[0]
		if call.Lane != classify.LaneMain || call.SessionID != call.ID || call.AnalysisState != "pending" {
			t.Fatalf("main classification is wrong: %+v", call)
		}
		seal := rig.store.Analysis[0]
		if seal.Job == nil || seal.Snapshot == nil || seal.Job.DedupeKey != "call:"+call.ID+":async" {
			t.Fatalf("a main call queues the asynchronous diagnosis: %+v", seal)
		}
	})
}

func TestRoutingResolutionOrderAndFallback(t *testing.T) {
	sideRequest := func(name string) pipeline.Request {
		return pipeline.Request{
			Kind: model.Chat, Operation: classify.Generation,
			Header: http.Header{"Authorization": {"Bearer agent-key"}, "X-Openai-Subagent": {"1"}},
			Body:   []byte(`{"model":"` + name + `","messages":[{"role":"user","content":"hi"}]}`),
		}
	}
	mainRequest := func(name string, header http.Header) pipeline.Request {
		if header == nil {
			header = http.Header{}
		}
		header.Set("Authorization", "Bearer agent-key")
		return pipeline.Request{
			Kind: model.Chat, Operation: classify.Generation, Header: header,
			Body: []byte(`{"model":"` + name + `","messages":[{"role":"user","content":"hi"}]}`),
		}
	}

	t.Run("exact route", func(t *testing.T) {
		rig := newRig(t, chatReply)
		if response := rig.send(mainRequest("gpt-5", nil)); response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
		call := rig.store.Calls[0]
		if call.Resolution != "exact" || call.RouteID != "route_1" || call.PublicModel != "gpt-5" {
			t.Fatalf("exact resolution is wrong: %+v", call)
		}
	})

	t.Run("alias route", func(t *testing.T) {
		rig := newRig(t, chatReply)
		rig.store.Aliases = []entity.RouteAlias{{ProjectID: "project_1", Alias: "gpt-5-old", RouteID: "route_1", Source: "renamed"}}
		if response := rig.send(mainRequest("gpt-5-old", nil)); response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
		if call := rig.store.Calls[0]; call.Resolution != "alias" || call.RouteID != "route_1" {
			t.Fatalf("alias resolution is wrong: %+v", call)
		}
	})

	t.Run("explicit header wins", func(t *testing.T) {
		rig := newRig(t, chatReply)
		header := http.Header{}
		header.Set("X-SafeAgent-Model", "gpt-5")
		if response := rig.send(mainRequest("unrouted-model", header)); response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
		call := rig.store.Calls[0]
		if call.Resolution != "explicit" || call.RouteID != "route_1" || call.PublicModel != "unrouted-model" {
			t.Fatalf("explicit resolution is wrong: %+v", call)
		}
	})

	t.Run("side model falls back to the project default route", func(t *testing.T) {
		rig := newRig(t, chatReply)
		rig.store.Proj.DefaultRouteID = "route_1"
		if response := rig.send(sideRequest("background-model")); response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
		call := rig.store.Calls[0]
		if call.Resolution != "fallback" || call.RouteID != "route_1" || call.PublicModel != "background-model" {
			t.Fatalf("fallback resolution is wrong: %+v", call)
		}
		routing := routingEvents(t, rig)
		if len(routing) != 1 || routing[0].Resolution != "fallback" || routing[0].RouteName != "gpt-5" || routing[0].PublicModel != "background-model" {
			t.Fatalf("one fallback routing event expected: %+v", routing)
		}
	})

	t.Run("main call keeps failing without fallback", func(t *testing.T) {
		rig := newRig(t, chatReply)
		rig.store.Proj.DefaultRouteID = "route_1"
		response := rig.send(mainRequest("background-model", nil))
		if response.Code != 404 || !strings.Contains(response.Body.String(), "model_not_found") {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
		call := rig.store.Calls[0]
		if call.Lane != classify.LaneMain || call.State != "failed" || call.Outcome != classify.OutcomeClientError {
			t.Fatalf("a main call must not silently fall back: %+v", call)
		}
		if len(routingEvents(t, rig)) != 0 {
			t.Fatal("no route resolution means no routing event")
		}
	})
}

func TestRoutingEventPrecedesTheFirstAttempt(t *testing.T) {
	rig := newRig(t, chatReply)
	rig.chat(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`)
	events := rig.store.EventsOf(rig.store.Calls[0].ID)
	kinds := eventKinds(events)
	routingAt := -1
	for index, kind := range kinds {
		if kind == auditevent.KindRouting {
			routingAt = index
		}
	}
	if routingAt != 1 || strings.Count(strings.Join(kinds, ","), auditevent.KindRouting) != 1 {
		t.Fatalf("exactly one routing event, right after the request: %v", kinds)
	}
	decoded, err := auditevent.Decode(auditevent.KindRouting, events[routingAt].Payload)
	if err != nil || decoded.Routing == nil || decoded.Routing.RouteID != "route_1" || decoded.Routing.Resolution != "exact" {
		t.Fatalf("routing payload is wrong: %+v %v", decoded.Routing, err)
	}
}

func TestRedactionValuesAnchorToTheStoredItem(t *testing.T) {
	rig := newRig(t, echoEmail)
	rig.chat(`{"model":"gpt-5","messages":[{"role":"user","content":"mail me@example.com and me@example.com"}]}`)
	call := rig.store.Calls[0]
	if call.RedactionValues != 1 || len(call.RedactionLabels) != 1 || call.RedactionLabels[0] != "EMAIL" {
		t.Fatalf("one distinct value is one hit: %+v", call)
	}
	decoded, err := auditevent.Decode(auditevent.KindRequest, firstEvent(t, rig.store.EventsOf(call.ID), auditevent.KindRequest))
	if err != nil || decoded.Request == nil {
		t.Fatalf("request event is unreadable: %v", err)
	}
	if len(decoded.Request.Hits) != 2 {
		t.Fatalf("both occurrences must be stored: %+v", decoded.Request.Hits)
	}
	for _, hit := range decoded.Request.Hits {
		if hit.CallID != call.ID || hit.Section != "request" || hit.Ordinal != 0 || hit.Path != "parts[0].text" {
			t.Fatalf("hit identity is wrong: %+v", hit)
		}
		if hit.ValueID == "" || hit.Start < 0 || hit.End <= hit.Start {
			t.Fatalf("hit ranges are wrong: %+v", hit)
		}
	}
}

func TestContinuedCallDoesNotRestoreAncestorHits(t *testing.T) {
	rig := newRig(t, func(received testkit.Received) testkit.Reply {
		if strings.Contains(received.Body, "previous_response_id") {
			return testkit.Reply{Frames: testkit.ChatText("second answer")}
		}
		return testkit.Reply{Frames: testkit.ChatText("first answer")}
	})
	rig.chat(`{"model":"gpt-5","messages":[{"role":"user","content":"mail ops@corp.example"}]}`)
	first := rig.store.Calls[0]
	if first.RedactionValues != 1 {
		t.Fatalf("the first call owns its hit: %+v", first)
	}
	continued := rig.send(pipeline.Request{
		Kind: model.Responses, Header: http.Header{"Authorization": {"Bearer agent-key"}},
		Body: []byte(`{"model":"gpt-5","previous_response_id":"` + first.ResponseID + `","input":"and then?"}`),
	})
	if continued.Code != 200 {
		t.Fatalf("%d %s", continued.Code, continued.Body)
	}
	second := rig.store.Calls[1]
	if second.RedactionValues != 0 || len(second.RedactionLabels) != 0 {
		t.Fatalf("an ancestor value must not be counted again: %+v", second)
	}
	decoded, err := auditevent.Decode(auditevent.KindRequest, firstEvent(t, rig.store.EventsOf(second.ID), auditevent.KindRequest))
	if err != nil || decoded.Request == nil {
		t.Fatalf("request event is unreadable: %v", err)
	}
	if len(decoded.Request.Hits) != 0 {
		t.Fatalf("an ancestor hit must not be stored again: %+v", decoded.Request.Hits)
	}
}

// routingEvents decodes every routing event of a rig, in order.
func routingEvents(t *testing.T, rig *rig) []auditevent.Routing {
	t.Helper()
	if len(rig.store.Calls) == 0 {
		return nil
	}
	var routing []auditevent.Routing
	for _, event := range rig.store.EventsOf(rig.store.Calls[0].ID) {
		if event.Kind != auditevent.KindRouting {
			continue
		}
		decoded, err := auditevent.Decode(event.Kind, event.Payload)
		if err != nil || decoded.Routing == nil {
			t.Fatalf("routing event is unreadable: %v", err)
		}
		routing = append(routing, *decoded.Routing)
	}
	return routing
}

func TestChannelBaseURLPointsTheChannelAtItsOwnHost(t *testing.T) {
	t.Run("a configured channel host wins over the provider host", func(t *testing.T) {
		rig := newRig(t, chatReply)
		spec, _ := rig.store.Registry.Get("test")
		endpoint := spec.Endpoints[model.Chat]
		endpoint.BaseURL = "http://127.0.0.1:1"
		spec.Endpoints[model.Chat] = endpoint
		rig.store.SetProvider(spec)
		rig.store.Channel[0].BaseURL = rig.upstream.URL
		if response := rig.chat(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`); response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
	})

	t.Run("an empty channel host keeps the provider host", func(t *testing.T) {
		rig := newRig(t, chatReply)
		rig.store.Channel[0].BaseURL = ""
		if response := rig.chat(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`); response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
	})
}
