package pipeline

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// evaluateRequest runs the request checkpoint: the schema and content detectors, then the rules of
// the request phase. A blocking decision aborts the call; a detector or rule error is fail-closed
// only when the project has a blocking rule for that phase.
func (state *call) evaluateRequest(trace *observation.Trace) *model.Error {
	return state.evaluate(trace, policy.PhaseRequest, []string{"schema", "content"}, observation.ToolCall{}, observation.ToolTags{}, nil, policy.Response{})
}

func (state *call) evaluateToolResults(trace *observation.Trace, results []policy.ToolResult) *model.Error {
	return state.evaluate(trace, policy.PhaseToolResult, []string{"content"}, observation.ToolCall{}, observation.ToolTags{}, results, policy.Response{})
}

func (state *call) evaluateResponse(trace *observation.Trace, response policy.Response) *model.Error {
	return state.evaluate(trace, policy.PhaseResponse, []string{"content"}, observation.ToolCall{}, observation.ToolTags{}, nil, response)
}

// evaluateToolCall runs the tool checkpoint. The tool call has not been released to the client yet,
// so a block prevents the side effect instead of reporting it.
func (state *call) evaluateToolCall(trace *observation.Trace, tool observation.ToolCall, tags observation.ToolTags) *model.Error {
	if refusal := state.evaluate(trace, policy.PhaseToolCall, []string{"schema", "content", "flow"}, tool, tags, nil, policy.Response{}); refusal != nil {
		return refusal
	}
	return state.vetoToolCall(trace, tool)
}

// vetoToolCall asks the decision model about a side-effecting call that the deterministic policy
// already allowed. The model can only add a block; a timeout, error or malformed answer leaves the
// deterministic decision untouched and is recorded as evaluated-but-unavailable.
func (state *call) vetoToolCall(trace *observation.Trace, tool observation.ToolCall) *model.Error {
	if state.pipeline.veto == nil || !tool.Sink.HasSideEffect() {
		return nil
	}
	answer, err := state.pipeline.veto.Authorize(state.ctx, trace, tool)
	if err != nil {
		state.pipeline.logger.Warn("decision model could not authorize a tool call", "call", state.recorder.Call().ID, "error", err)
		state.recordModelDecision(policy.PhaseToolCall, policy.ActionRecord, false, "decision_model_unavailable", 0, 0)
		return nil
	}
	threshold := state.pipeline.veto.Threshold(state.ctx, state.identity.Project.ID)
	if (!answer.ConfidenceKnown || answer.Confidence >= 0.5) && answer.Probability < threshold {
		state.recordModelDecision(policy.PhaseToolCall, policy.ActionBlock, true, "decision_model_veto", answer.Probability, answer.Confidence)
		state.recordBlocked(auditevent.Decision{
			Stage: policy.PhaseToolCall, Action: policy.ActionBlock, Blocked: true,
			Code: tool.Name, Reason: "decision_model_veto",
		})
		return blocked("tool_call_vetoed", "The decision model blocked the tool call.", http.StatusForbidden)
	}
	state.recordModelDecision(policy.PhaseToolCall, policy.ActionRecord, false, "decision_model_authorized", answer.Probability, answer.Confidence)
	return nil
}

func (state *call) recordModelDecision(phase, action string, blockedCall bool, reason string, probability, confidence float64) {
	attributes, _ := json.Marshal(map[string]any{"probability": probability, "confidence": confidence})
	state.appendDecision(entity.Decision{
		ID: "dec_" + secure.Random(10), ProjectID: state.identity.Project.ID, CallID: state.recorder.Call().ID,
		PolicyVersion: state.policyVersion, Phase: phase, Action: action, Blocked: blockedCall,
		Code: "decision_model", Severity: analysis.SeverityInfo, Reason: reason,
		Attributes: attributes, CreatedAt: time.Now().UTC(),
	})
}

func (state *call) evaluate(trace *observation.Trace, phase string, detectors []string, tool observation.ToolCall, tags observation.ToolTags, results []policy.ToolResult, response policy.Response) *model.Error {
	result := state.pipeline.analysis.Evaluate(state.ctx, trace, analysis.Options{Only: detectors})
	// Each evaluation sees the whole trace, so replace rather than append. Appending would count
	// a request finding once per tool call and change count or threshold rules.
	state.findings = result.Findings
	if len(result.Errors) > 0 {
		return state.gateFailure(phase, result.Errors[0])
	}
	outcome, err := state.compiled.Evaluate(state.ctx, policy.Input{
		Phase: phase, Principal: trace.Principal, Tool: tool, Tags: tags,
		Trace: trace, Findings: state.findings, Results: results, Response: response,
	})
	if err != nil {
		state.pipeline.logger.Error("policy evaluation failed", "call", state.recorder.Call().ID, "phase", phase, "error", err)
		return state.gateFailure(phase, err.Error())
	}
	for _, decision := range outcome.Decisions {
		if decision.Action == policy.ActionRecord {
			state.recordDecision(phase, decision, false)
		}
	}
	if outcome.Block == nil {
		return nil
	}
	state.recordDecision(phase, *outcome.Block, true)
	state.recordBlocked(auditevent.Decision{
		Stage: phase, Action: outcome.Block.Action, Blocked: true,
		RuleID: outcome.Block.RuleID, Code: outcome.Block.RuleID, Reason: outcome.Block.Reason,
	})
	code := "request_blocked"
	switch phase {
	case policy.PhaseToolCall:
		code = "tool_call_blocked"
	case policy.PhaseToolResult:
		code = "tool_result_blocked"
	case policy.PhaseResponse:
		code = "response_blocked"
	}
	message := "The analysis policy blocked the request."
	if reason := firstNonEmpty(outcome.Block.Reason, outcome.Block.RuleID); reason != "" {
		message = sentence("The analysis policy blocked the request: " + reason)
	}
	return blocked(code, message, http.StatusForbidden)
}

func (state *call) gateFailure(phase, detail string) *model.Error {
	if state.compiled.HasBlockingRule(phase) {
		state.recordDecision(phase, policy.DecisionResult{Action: policy.ActionBlock, Reason: "guard_unavailable", Severity: analysis.SeverityHigh}, true)
		state.recordBlocked(auditevent.Decision{
			Stage: phase, Action: policy.ActionBlock, Blocked: true, Reason: "guard_unavailable", Code: analysis.CodeAnalysisIncomplete,
		})
		return blocked("guard_unavailable", "The analysis check could not complete, so the request was blocked.", http.StatusServiceUnavailable)
	}
	state.recordDecision(phase, policy.DecisionResult{Action: policy.ActionRecord, Reason: "guard_unavailable", Severity: analysis.SeverityMedium}, false)
	state.pipeline.logger.Warn("guard could not complete; continuing because no blocking rule applies", "phase", phase, "detail", detail)
	return nil
}

// recordBlocked writes the one blocked event of a call. Several layers observe the same refusal
// (the detector pass, the tool gate, the terminal settle); the first record carries the most
// specific reason and later ones would only duplicate it.
func (state *call) recordRedactionBlock(code, reason string) {
	state.recordDecision(policy.PhaseRequest, policy.DecisionResult{Action: policy.ActionBlock, RuleID: code, Reason: reason, Severity: analysis.SeverityHigh}, true)
	state.recordBlocked(auditevent.Decision{Stage: policy.PhaseRequest, Action: policy.ActionBlock, Blocked: true, RuleID: code, Code: code, Reason: reason})
}

func (state *call) recordBlocked(decision auditevent.Decision) {
	if state.blockedRecorded {
		return
	}
	state.blockedRecorded = true
	state.recorder.Event(auditevent.KindBlocked, decision)
}

func (state *call) recordDecision(phase string, decision policy.DecisionResult, blockedCall bool) {
	attributes, _ := json.Marshal(map[string]any{"phase": phase})
	state.appendDecision(entity.Decision{
		ID: "dec_" + secure.Random(10), ProjectID: state.identity.Project.ID, CallID: state.recorder.Call().ID,
		PolicyVersion: state.policyVersion, Phase: phase, Action: decision.Action,
		Blocked: blockedCall, RuleID: decision.RuleID, Code: decision.RuleID,
		Severity: decision.Severity, Reason: decision.Reason, Attributes: attributes,
		CreatedAt: time.Now().UTC(),
	})
}

func (state *call) appendDecision(decision entity.Decision) {
	state.decisions = append(state.decisions, decision)
	state.recorder.Event(auditevent.KindDecision, auditevent.Decision{
		ID: decision.ID, PolicyVersion: decision.PolicyVersion, Stage: decision.Phase,
		Action: decision.Action, Blocked: decision.Blocked, RuleID: decision.RuleID,
		Code: decision.Code, Severity: decision.Severity, Reason: decision.Reason,
	})
}
