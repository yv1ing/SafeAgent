package pipeline

import (
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/policy"
	security "github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/threat"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

// evaluateRequest runs the ingress gate: the schema and content detectors, then request-phase rules.
func (state *call) evaluateRequest(trace *observation.Trace) *model.Error {
	return state.evaluate(trace, "ingress", policy.PhaseRequest, []string{"schema", "content"}, observation.ToolCall{}, observation.ToolTags{}, nil, policy.Response{})
}

func (state *call) evaluateToolResults(trace *observation.Trace, results []policy.ToolResult) *model.Error {
	return state.evaluate(trace, "ingress", policy.PhaseToolResult, []string{"content"}, observation.ToolCall{}, observation.ToolTags{}, results, policy.Response{})
}

func (state *call) evaluateResponse(trace *observation.Trace, response policy.Response) *model.Error {
	return state.evaluate(trace, "release", policy.PhaseResponse, []string{"content"}, observation.ToolCall{}, observation.ToolTags{}, nil, response)
}

// evaluateToolCall runs the action gate. The tool call has not been released to the client yet, so a
// block prevents the side effect instead of reporting it.
func (state *call) evaluateToolCall(trace *observation.Trace, tool observation.ToolCall, tags observation.ToolTags) *model.Error {
	if refusal := state.evaluate(trace, "action", policy.PhaseToolCall, []string{"schema", "content", "flow"}, tool, tags, nil, policy.Response{}); refusal != nil {
		return refusal
	}
	if refusal := state.enforceToolInvariants(tool); refusal != nil {
		return refusal
	}
	return state.vetoToolCall(trace, tool)
}

func (state *call) evaluate(trace *observation.Trace, gate, phase string, detectors []string, tool observation.ToolCall, tags observation.ToolTags, results []policy.ToolResult, response policy.Response) *model.Error {
	result := state.pipeline.threat.Evaluate(state.ctx, trace, threat.Options{Only: detectors})
	state.findings = result.Findings
	checks := checksFromFindings(result.Findings)
	if len(result.Errors) > 0 {
		return state.gateFailure(gate, phase, result.Errors[0], checks)
	}
	outcome, err := state.compiled.Evaluate(state.ctx, policy.Input{
		Phase: phase, Principal: trace.Principal, Tool: tool, Tags: tags,
		Trace: trace, Findings: state.findings, Results: results, Response: response,
	})
	if err != nil {
		state.pipeline.logger.Error("policy evaluation failed", "call", state.record.ID, "phase", phase, "error", err)
		return state.gateFailure(gate, phase, err.Error(), checks)
	}
	for _, decision := range outcome.Decisions {
		if decision.Action == policy.ActionRecord {
			state.recordGate(gate, decision, false, false, checks)
		}
	}
	if outcome.Block == nil {
		if len(outcome.Decisions) == 0 {
			state.recorder.GateDecision(gate, policy.ActionRecord, false, false, "", "", threat.SeverityInfo, "the gate allowed the event", checks)
		}
		return nil
	}
	terminal := phase == policy.PhaseRequest || phase == policy.PhaseToolResult
	state.recordGate(gate, *outcome.Block, true, terminal, checks)
	code := "request_blocked"
	switch phase {
	case policy.PhaseToolCall:
		code = "tool_call_blocked"
	case policy.PhaseToolResult:
		code = "tool_result_blocked"
	case policy.PhaseResponse:
		code = "response_blocked"
	}
	message := "The policy blocked the request."
	if reason := firstNonEmpty(outcome.Block.Reason, outcome.Block.RuleID); reason != "" {
		message = sentence("The policy blocked the request: " + reason)
	}
	return blocked(code, message, http.StatusForbidden)
}

// vetoToolCall asks the decision model about a side-effecting call that the deterministic policy
// has already allowed. The model can only add a block.
func (state *call) vetoToolCall(trace *observation.Trace, tool observation.ToolCall) *model.Error {
	if state.pipeline.veto == nil || !tool.Sink.HasSideEffect() {
		return nil
	}
	answer, err := state.pipeline.veto.Authorize(state.ctx, trace, tool)
	if err != nil {
		state.pipeline.logger.Warn("decision model could not authorize a tool call", "call", state.record.ID, "error", err)
		state.recorder.GateDecision("action", "record", false, false, "decision_model_unavailable", "decision_model_unavailable", threat.SeverityInfo, "the decision model was unavailable", nil)
		return nil
	}
	threshold := state.pipeline.veto.Threshold(state.ctx, state.identity.Project.ID)
	if (!answer.ConfidenceKnown || answer.Confidence >= 0.5) && answer.Probability < threshold {
		checks := []trajectory.Check{{
			ID: "decision_model", Kind: "semantic", Status: "finding", Code: "decision_model_veto",
			Severity: threat.SeverityHigh, Confidence: answer.Confidence,
			Detail: "the decision model vetoed the tool call",
		}}
		state.recorder.GateDecision("action", "block", true, false, "decision_model_veto", "decision_model_veto", threat.SeverityHigh, "the decision model blocked the tool call", checks)
		return blocked("tool_call_vetoed", "The decision model blocked the tool call.", http.StatusForbidden)
	}
	state.recorder.GateDecision("action", "record", false, false, "decision_model_authorized", "decision_model_authorized", threat.SeverityInfo, "the decision model allowed the tool call", nil)
	return nil
}

func (state *call) gateFailure(gate, phase, detail string, checks []trajectory.Check) *model.Error {
	if state.compiled.HasBlockingRule(phase) {
		state.recorder.GateDecision(gate, "block", true, phase == policy.PhaseRequest || phase == policy.PhaseToolResult, "guard_unavailable", threat.CodeAnalysisIncomplete, threat.SeverityHigh, "the safety check could not complete", checks)
		return blocked("guard_unavailable", "The safety check could not complete, so the request was blocked.", http.StatusServiceUnavailable)
	}
	state.recorder.GateDecision(gate, "record", false, false, "guard_unavailable", threat.CodeAnalysisIncomplete, threat.SeverityMedium, "the safety check could not complete; no blocking rule applies", checks)
	state.pipeline.logger.Warn("guard could not complete; continuing because no blocking rule applies", "phase", phase, "detail", detail)
	return nil
}

func (state *call) recordRedactionBlock(code, reason string) {
	state.recorder.GateDecision("ingress", "block", true, true, code, code, threat.SeverityHigh, reason, nil)
}

func (state *call) recordGate(gate string, decision policy.DecisionResult, blockedCall, terminal bool, checks []trajectory.Check) {
	severity := decision.Severity
	if severity == "" {
		severity = threat.SeverityMedium
	}
	state.recorder.GateDecision(gate, decision.Action, blockedCall, terminal, decision.RuleID, decision.RuleID, severity, decision.Reason, checks)
}

func checksFromFindings(findings []threat.Finding) []trajectory.Check {
	checks := make([]trajectory.Check, 0, len(findings))
	for _, finding := range findings {
		detail := ""
		if len(finding.Evidence) > 0 {
			detail = finding.Evidence[0].Quote
		}
		checks = append(checks, trajectory.Check{
			ID: finding.Fingerprint, Kind: "deterministic", Status: "finding", Code: finding.Code,
			Severity: finding.Severity, Confidence: finding.Confidence, Detail: detail,
		})
	}
	return checks
}

// enforceToolInvariants applies the irreversible-action defaults that do not depend on a project
// rule. A high-severity flow from external content or a secret into a side-effecting tool is
// blocked; a project can only relax it by not declaring the tool as side-effecting.
func (state *call) enforceToolInvariants(tool observation.ToolCall) *model.Error {
	if !tool.Sink.HasSideEffect() {
		return nil
	}
	invocationHash := toolInvocationHash(tool)
	current, err := state.pipeline.repository.TrajectoryState(state.ctx, state.identity.Project.ID, state.record.TrajectoryID)
	if err != nil {
		state.pipeline.logger.Error("approval state could not be read", "call", state.record.ID, "error", err)
		return internal("approval_unavailable", "The approval state is unavailable.")
	}
	if approval, found := grantedApproval(current, invocationHash, time.Now().UTC()); found {
		state.recorder.ConsumeApproval(approval.ID, invocationHash, approval.ExecutionCount+1)
		state.approvalID = approval.ID
		return nil
	}
	if current.CoverageForCall(state.record.ID)+1e-9 < current.CoverageThreshold {
		return state.requireApproval(current, invocationHash, "coverage_below_threshold",
			"trajectory coverage is below the configured threshold", nil)
	}
	for _, finding := range state.findings {
		if finding.Severity != threat.SeverityHigh {
			continue
		}
		switch finding.Code {
		case threat.CodeUntrustedValueReuse, threat.CodeExternalInstructionFollowed, threat.CodeSecretFlow:
			return state.requireApproval(current, invocationHash, finding.Code,
				"an invariant requires approval for this tool call", checksFromFindings(state.findings))
		}
	}
	return nil
}

func (state *call) requireApproval(current *security.State, invocationHash, code, reason string, checks []trajectory.Check) *model.Error {
	if _, found := pendingApproval(current, invocationHash, time.Now().UTC()); !found {
		state.recorder.RequestApproval(state.recorder.CurrentAction(), invocationHash, code)
	}
	state.recorder.Flush(state.ctx)
	if state.recorder.WriteFailure() != nil {
		return internal("audit_unavailable", "The approval request could not be recorded.")
	}
	state.recorder.GateDecision("action", "block", true, false, code, "approval_required", threat.SeverityHigh, reason, checks)
	return blocked("approval_required", "This tool call requires approval before it can run.", http.StatusForbidden)
}

func toolInvocationHash(tool observation.ToolCall) string {
	target := tool.Target.Value
	if tool.TargetShown.Kind != "" {
		target = tool.TargetShown.Value
	}
	return trajectory.ContentHash([]byte(tool.Name + "\x00" + tool.Arguments.Raw + "\x00" + target))
}

func grantedApproval(current *security.State, invocationHash string, now time.Time) (security.Approval, bool) {
	for _, approval := range current.Approvals {
		if approval.State == "granted" && approval.InvocationHash == invocationHash &&
			(approval.ExpiresAt.IsZero() || approval.ExpiresAt.After(now)) {
			return approval, true
		}
	}
	return security.Approval{}, false
}

func pendingApproval(current *security.State, invocationHash string, now time.Time) (security.Approval, bool) {
	for _, approval := range current.Approvals {
		if approval.State == "pending" && approval.InvocationHash == invocationHash &&
			(approval.ExpiresAt.IsZero() || approval.ExpiresAt.After(now)) {
			return approval, true
		}
	}
	return security.Approval{}, false
}
