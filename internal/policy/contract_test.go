package policy_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/policy"
	"github.com/yv1ing/SafeAgent/internal/threat"
)

var contractDetector = threat.DetectorRef{ID: "content", Version: "2"}

func TestTypedCELRejectsUnknownFieldsAndFires(t *testing.T) {
	document := policy.Document{Rules: []policy.Rule{{
		ID: "low-trust", Name: "low trust", Enabled: true, Phase: policy.PhaseRequest,
		When: "principal.trust_level < 2 && target.host == 'evil.example'", Action: policy.ActionBlock,
		Severity: threat.SeverityHigh, Reason: "untrusted caller",
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

func TestPolicyDocumentsAreBounded(t *testing.T) {
	rule := func(id, when string) policy.Rule {
		return policy.Rule{ID: id, Enabled: true, Phase: policy.PhaseRequest, When: when, Action: policy.ActionRecord}
	}
	rules := make([]policy.Rule, policy.MaximumRules)
	for index := range rules {
		rules[index] = rule("rule_"+strconv.Itoa(index), "true")
	}
	if _, err := policy.Compile(policy.Document{Rules: rules}); err != nil {
		t.Fatalf("a document at the rule limit must compile: %v", err)
	}
	over := append(append([]policy.Rule{}, rules...), rule("over", "true"))
	if _, err := policy.Compile(policy.Document{Rules: over}); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("a document above the rule limit must be rejected by the rule check: %v", err)
	}
	atConditionLimit := "true" + strings.Repeat(" ", policy.MaximumConditionBytes-len("true"))
	if _, err := policy.Compile(policy.Document{Rules: []policy.Rule{rule("long", atConditionLimit)}}); err != nil {
		t.Fatalf("a condition at the byte limit must compile: %v", err)
	}
	if _, err := policy.Compile(policy.Document{Rules: []policy.Rule{rule("long", atConditionLimit+" ")}}); err == nil || !strings.Contains(err.Error(), "condition exceeds") {
		t.Fatalf("a condition above the byte limit must be rejected by the condition check: %v", err)
	}
	if _, err := policy.Compile(policy.Document{Note: strings.Repeat("x", policy.MaximumDocumentBytes)}); err == nil || !strings.Contains(err.Error(), "document exceeds") {
		t.Fatalf("a document above the byte limit must be rejected by the document check: %v", err)
	}
}

func TestPolicyEvaluationHonorsDeadlines(t *testing.T) {
	compiled, err := policy.Compile(policy.Document{Rules: []policy.Rule{{
		ID: "record", Enabled: true, Phase: policy.PhaseRequest, When: "true", Action: policy.ActionRecord,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if _, err := compiled.Evaluate(ctx, policy.Input{Phase: policy.PhaseRequest}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an expired evaluation must report its deadline: %v", err)
	}
}

func TestFlowConditionsReadFindings(t *testing.T) {
	document := policy.Document{Rules: []policy.Rule{{
		ID: "shell", Enabled: true, Phase: policy.PhaseToolCall,
		When:   `findings.exists(f, f.code == "dangerous_command" && f.sink == "shell_exec")`,
		Action: policy.ActionBlock, Severity: threat.SeverityHigh,
	}}}
	compiled, err := policy.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	finding := threat.NewFinding(contractDetector, threat.CodeDangerousCommand, observation.OriginAssistant, observation.SinkShellExec, "", 1, "0/1", 0)
	outcome, err := compiled.Evaluate(context.Background(), policy.Input{
		Phase: policy.PhaseToolCall, Findings: []threat.Finding{finding},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Block == nil {
		t.Fatal("a finding-based rule did not fire")
	}
}

func TestPolicyReadsTheShownTarget(t *testing.T) {
	tool := observation.ToolCall{
		Target:      observation.Target{Kind: "url", Value: "https://secret.example/collect"},
		TargetShown: observation.Target{Kind: "url", Value: "https://[SECRET_1]/collect"},
	}
	document := policy.Document{Rules: []policy.Rule{{
		ID: "shown-target", Enabled: true, Phase: policy.PhaseToolCall,
		When:   `target.value == 'https://[SECRET_1]/collect'`,
		Action: policy.ActionBlock, Severity: threat.SeverityHigh,
	}}}
	compiled, err := policy.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := compiled.Evaluate(context.Background(), policy.Input{Phase: policy.PhaseToolCall, Tool: tool})
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
	outcome, err = compiled.Evaluate(context.Background(), policy.Input{Phase: policy.PhaseToolCall, Tool: tool})
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
		Action: policy.ActionBlock, Severity: threat.SeverityHigh,
	}}}
	compiled, err := policy.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := compiled.Evaluate(context.Background(), policy.Input{
		Phase: policy.PhaseToolCall,
		Tool:  observation.ToolCall{Name: "run", Input: map[string]any{"token": "secret", "user": map[string]any{"name": "alice"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Block == nil {
		t.Fatal("flattened arguments did not match")
	}
}
