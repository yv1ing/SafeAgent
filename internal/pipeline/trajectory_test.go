package pipeline_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/pipeline"
	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/policy"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/testkit"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type rig struct {
	store    *testkit.Memory
	upstream *testkit.Upstream
	pipeline *pipeline.Pipeline
	selector *routing.Selector
	breakers *routing.Breakers
	limiter  *limit.Limiter
	format   string
}

func newRig(t *testing.T, script func(testkit.Received) testkit.Reply) *rig {
	return newRigFor(t, "openai_chat", script)
}

func newRigFor(t *testing.T, upstreamFormat string, script func(testkit.Received) testkit.Reply) *rig {
	upstream := testkit.NewUpstream(t, script)
	store := testkit.NewMemory(upstream.URL, upstreamFormat)
	store.Registry = testkit.ProviderRegistryWithTransport(upstreamFormat, upstream.URL, provider.TransportAuto)
	store.UseProvider(store.Registry)
	breakers := routing.NewBreakers(nil)
	selector := routing.NewSelector(breakers, routing.NewKeyPool(nil), routing.NewSticky(nil), 3)
	limiter := limit.New(nil)
	settings := pipeline.Settings{MaxRequestBytes: 8 << 20, MaxResponseBytes: 32 << 20, MaxStreamBytes: 64 << 20, StreamIdle: 5 * time.Second}
	var sequence int
	var mu sync.Mutex
	service := pipeline.New(store, []byte(testkit.InstanceSecret), pipeline.Options{
		Settings: settings, Providers: store.Registry, Selector: selector, Limiter: limiter,
		IDs: func() string {
			mu.Lock()
			defer mu.Unlock()
			sequence++
			return "call" + string(rune('a'+sequence))
		},
	})
	return &rig{store: store, upstream: upstream, pipeline: service, selector: selector, breakers: breakers, limiter: limiter, format: upstreamFormat}
}

type recorder struct{ *httptest.ResponseRecorder }

func (recorder) Flush() {}

func (rig *rig) chat(body string) *recorder {
	writer := &recorder{httptest.NewRecorder()}
	rig.pipeline.Serve(context.Background(), pipeline.Request{
		Kind: "openai_chat", Header: http.Header{"Authorization": {"Bearer agent-key"}}, Body: []byte(body),
	}, writer)
	return writer
}

func echoEmail(received testkit.Received) testkit.Reply {
	placeholder := "[EMAIL_" + testkit.Between(received.Body, "[EMAIL_", "]") + "]"
	half := len(placeholder) / 2
	return testkit.Reply{Frames: testkit.ChatText("hi "+placeholder[:half], placeholder[half:])}
}

const mailRequest = `{"model":"gpt-5","messages":[{"role":"user","content":"mail me@example.com"}]}`

func TestPipelineRecordsOneTrajectoryAndRestoresRedaction(t *testing.T) {
	rig := newRig(t, echoEmail)
	response := rig.chat(mailRequest)
	if response.Code != 200 {
		t.Fatalf("status %d: %s", response.Code, response.Body)
	}
	if sent := rig.upstream.Requests()[0].Body; !strings.Contains(sent, "[EMAIL_") || strings.Contains(sent, "me@example.com") {
		t.Fatalf("upstream saw %s", sent)
	}
	if !strings.Contains(response.Body.String(), "me@example.com") || strings.Contains(response.Body.String(), "[EMAIL_") {
		t.Fatalf("response was not restored: %s", response.Body)
	}
	call := rig.store.Calls[0]
	if call.State != "completed" || call.Outcome != classify.OutcomeOK || call.TrajectoryID == "" {
		t.Fatalf("call projection is wrong: %+v", call)
	}
	events := rig.store.EventsOfTrajectory(call.TrajectoryID)
	kinds := map[trajectory.Kind]bool{}
	for _, event := range events {
		kinds[event.Kind] = true
	}
	for _, required := range []trajectory.Kind{trajectory.KindTrajectoryStart, trajectory.KindActionProposed, trajectory.KindObservation, trajectory.KindGateDecision, trajectory.KindActionExecuted, trajectory.KindTrajectoryEnd} {
		if !kinds[required] {
			t.Fatalf("missing %s in %v", required, eventKinds(events))
		}
	}
	if report := trajectory.VerifyChain(events, []byte(testkit.InstanceSecret)); report.Status != trajectory.StatusVerified {
		t.Fatalf("chain is broken: %+v", report)
	}
	if call.RedactionValues == 0 {
		t.Fatalf("the redaction was not projected: %+v events=%+v", call, events)
	}
}

func TestSessionCallsShareOneTrajectory(t *testing.T) {
	rig := newRig(t, func(testkit.Received) testkit.Reply { return testkit.Reply{Frames: testkit.ChatText("ok")} })
	first := &recorder{httptest.NewRecorder()}
	second := &recorder{httptest.NewRecorder()}
	header := http.Header{}
	header.Set("Authorization", "Bearer agent-key")
	header.Set("X-SafeAgent-Session", "session-1")
	rig.pipeline.Serve(context.Background(), pipeline.Request{Kind: "openai_chat", Header: header, Body: []byte(mailRequest)}, first)
	rig.pipeline.Serve(context.Background(), pipeline.Request{Kind: "openai_chat", Header: header, Body: []byte(mailRequest)}, second)
	if len(rig.store.Calls) != 2 {
		t.Fatalf("calls=%d", len(rig.store.Calls))
	}
	if rig.store.Calls[0].TrajectoryID != rig.store.Calls[1].TrajectoryID {
		t.Fatalf("session calls disagree on trajectory: %s %s sessions=%q/%q all=%+v", rig.store.Calls[0].TrajectoryID, rig.store.Calls[1].TrajectoryID, rig.store.Calls[0].SessionID, rig.store.Calls[1].SessionID, rig.store.Trajectories)
	}
}

func TestPolicyBlocksRequest(t *testing.T) {
	rig := newRig(t, func(testkit.Received) testkit.Reply { return testkit.Reply{Frames: testkit.ChatText("should not run")} })
	document := policy.Document{Rules: []policy.Rule{{
		ID: "block", Name: "block", Enabled: true, Phase: policy.PhaseRequest,
		When: "true", Action: policy.ActionBlock, Severity: "high", Reason: "blocked for test",
	}}}
	compiled, err := policy.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(document)
	rig.store.Policy = &entity.PolicyVersion{ID: "p", ProjectID: "project_1", Version: 1, Document: encoded, Hash: compiled.Hash()}
	response := rig.chat(mailRequest)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
	if len(rig.upstream.Requests()) != 0 {
		t.Fatal("a blocked request reached the upstream")
	}
	call := rig.store.Calls[0]
	if call.Outcome != classify.OutcomeBlocked || call.State != "completed" {
		t.Fatalf("call=%+v", call)
	}
}

func TestConcurrencyLimitRejectsSecondCall(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	rig := newRig(t, func(testkit.Received) testkit.Reply {
		started <- struct{}{}
		<-release
		return testkit.Reply{Frames: testkit.ChatText("ok")}
	})
	rig.store.Proj.Concurrency = 1
	done := make(chan struct{})
	go func() {
		rig.chat(mailRequest)
		close(done)
	}()
	<-started
	if second := rig.chat(mailRequest); second.Code != http.StatusTooManyRequests {
		t.Fatalf("second call status=%d", second.Code)
	}
	close(release)
	<-done
}

func eventKinds(events []trajectory.Event) []trajectory.Kind {
	kinds := make([]trajectory.Kind, len(events))
	for index, event := range events {
		kinds[index] = event.Kind
	}
	return kinds
}
