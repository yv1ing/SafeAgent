package main

import (
	"strings"
	"testing"
)

func TestValidateStreamAcceptsTextAndTerminalEvent(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"OK"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":1}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	got := validateStream("chat", strings.NewReader(body))
	if !got.ok {
		t.Fatalf("expected a valid stream, got %s: %s", got.kind, got.detail)
	}
	if got.promptTokens != 8 || got.completionTokens != 1 {
		t.Fatalf("usage = %d/%d, want 8/1", got.promptTokens, got.completionTokens)
	}
}

func TestValidateStreamRejectsMissingTerminalEvent(t *testing.T) {
	body := `data: {"choices":[{"delta":{"content":"OK"},"finish_reason":null}]}` + "\n"
	got := validateStream("chat", strings.NewReader(body))
	if got.ok || got.kind != "protocol" {
		t.Fatalf("expected a protocol failure, got %+v", got)
	}
	if !strings.Contains(got.detail, "terminal") {
		t.Fatalf("detail = %q, want a terminal-event message", got.detail)
	}
}

func TestValidateStreamRejectsMalformedFrame(t *testing.T) {
	got := validateStream("chat", strings.NewReader("data: {not json}\n"))
	if got.ok || got.kind != "protocol" {
		t.Fatalf("expected a protocol failure, got %+v", got)
	}
}

func TestValidateJSONRequiresText(t *testing.T) {
	body := `{"choices":[{"message":{"content":""}}]}`
	got := validateJSON("chat", strings.NewReader(body))
	if got.ok || got.kind != "protocol" {
		t.Fatalf("expected a protocol failure, got %+v", got)
	}
}

func TestSanitizeEndpointRemovesQuerySecrets(t *testing.T) {
	got := sanitizeEndpoint("https://provider.example/v1/chat?key=sk-secret&model=m")
	if strings.Contains(got, "sk-secret") || !strings.Contains(got, "REDACTED") {
		t.Fatalf("sanitized endpoint = %q", got)
	}
	got = sanitizeEndpoint("https://alice:secret@provider.example/v1/chat")
	if strings.Contains(got, "alice") || strings.Contains(got, "secret") || !strings.Contains(got, "REDACTED") {
		t.Fatalf("sanitized endpoint with user info = %q", got)
	}
}

func TestPercentileUsesNearestRank(t *testing.T) {
	values := []float64{10, 20, 30, 40}
	if got := percentile(values, 95); got != 40 {
		t.Fatalf("p95 = %v, want 40", got)
	}
	if got := percentile(values, 50); got != 20 {
		t.Fatalf("p50 = %v, want 20", got)
	}
	if got := percentile(nil, 95); got != 0 {
		t.Fatalf("empty p95 = %v, want 0", got)
	}
}

func TestEvaluateAcceptsRelativeBudget(t *testing.T) {
	baseline := loadReport{
		Stream: true, Requests: 120, Concurrency: 4, SuccessRate: 1,
		LatencyMS: latency{P95: 1000}, ThroughputRPS: 5,
		ErrorCounts: map[string]int{},
	}
	candidate := loadReport{
		Stream: true, Requests: 120, Concurrency: 4, SuccessRate: 0.995,
		LatencyMS: latency{P95: 1200}, ThroughputRPS: 4.6,
		ErrorCounts: map[string]int{},
	}
	for _, item := range evaluate(baseline, candidate, thresholds{
		maxP95Ratio: 1.2, maxP95AddMS: 100, minThroughput: 0.9, maxErrorDeltaPct: 0.5,
	}) {
		if !item.pass {
			t.Fatalf("%s failed: %s", item.name, item.detail)
		}
	}
}

func TestEvaluateRejectsLatencyAndThroughputRegression(t *testing.T) {
	baseline := loadReport{
		Stream: false, Requests: 120, Concurrency: 4, SuccessRate: 1,
		LatencyMS: latency{P95: 1000}, ThroughputRPS: 5,
		ErrorCounts: map[string]int{},
	}
	candidate := loadReport{
		Stream: false, Requests: 120, Concurrency: 4, SuccessRate: 1,
		LatencyMS: latency{P95: 2000}, ThroughputRPS: 3,
		ErrorCounts: map[string]int{},
	}
	checks := evaluate(baseline, candidate, thresholds{
		maxP95Ratio: 1.2, maxP95AddMS: 100, minThroughput: 0.9, maxErrorDeltaPct: 0.5,
	})
	failed := map[string]bool{}
	for _, item := range checks {
		if !item.pass {
			failed[item.name] = true
		}
	}
	if !failed["p95 latency"] || !failed["throughput"] {
		t.Fatalf("expected p95 and throughput failures, got %v", failed)
	}
}

func TestEvaluateRejectsInvalidBaseline(t *testing.T) {
	baseline := loadReport{
		Stream: false, Requests: 120, Concurrency: 4, SuccessRate: 0.95,
		LatencyMS: latency{P95: 1000}, ThroughputRPS: 5,
		ErrorCounts: map[string]int{"http_429": 6},
	}
	candidate := baseline
	checks := evaluate(baseline, candidate, thresholds{
		maxP95Ratio: 1.2, maxP95AddMS: 100, minThroughput: 0.9, maxErrorDeltaPct: 0.5,
	})
	for _, item := range checks {
		if item.name == "baseline valid" && item.pass {
			t.Fatal("a baseline with 429 responses must be invalid")
		}
	}
}
