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
