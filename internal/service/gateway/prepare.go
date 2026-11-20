package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observe"
	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// Request is one client call, already read off the wire by the HTTP layer.
type Request struct {
	Kind   protocol.Format
	Header http.Header
	// QueryKey is the agent key passed as ?key=, which only Gemini clients do.
	QueryKey    string
	Body        []byte
	Model       string
	ForceStream bool
}

// call carries everything one client call needs from admission to its terminal state.
type call struct {
	ctx           context.Context
	pipeline      *Pipeline
	kind          protocol.Format
	codec         format.Codec
	peer          format.Peer
	salt          string
	writer        Writer
	request       *protocol.Request
	identity      *Identity
	candidates    []routing.Candidate
	available     channels
	recorder      *Recorder
	scope         *redact.Scope
	compiled      *policy.Compiled
	policyVersion int64
	estimate      int64
	day           time.Time
	started       time.Time
	stickyKey     string
	release       func()
	hits          []redact.Hit
	redacted      int
	attempts      int
	parent        *entity.Call
	offset        int
	// client is the request as the client sent it; analysis and fingerprints are taken from it,
	// never from the redacted copy that goes upstream.
	client          *protocol.Request
	clientHeader    http.Header
	body            []byte
	targetFormat    protocol.Format
	historyExpanded bool
	unsealed        bool
	headersSent     bool
	toolKinds       map[string]protocol.ToolKind
	failureLedger   protocol.Ledger
	findings        []analysis.Finding
	decisions       []entity.Decision
	redactionRules  []redact.Rule
	reserved        bool
	opened          bool
	blockedRecorded bool
}

func requestHasSignature(request *protocol.Request) bool {
	for index := range request.Items {
		item := &request.Items[index]
		if item.Reasoning != nil && item.Reasoning.Signature != nil {
			return true
		}
		if item.ToolCall != nil && item.ToolCall.Signature != nil {
			return true
		}
	}
	return false
}

// Serve runs one client call end to end and writes the protocol-shaped response or error. Every
// path writes something: an admitted call ends in settle, a refused one in writeError.
func (pipeline *Pipeline) Serve(ctx context.Context, input Request, writer Writer) {
	state, refusal := pipeline.admit(ctx, input, writer)
	if refusal != nil {
		pipeline.refuse(state, input.Kind, writer, refusal)
		return
	}
	defer state.release()
	state.run()
}

// admit authenticates, decodes, redacts, analyzes, routes, rate-limits and records the call. A
// non-nil state with a refusal means the call was already recorded as rejected.
func (pipeline *Pipeline) admit(ctx context.Context, input Request, writer Writer) (*call, *protocol.Error) {
	started := time.Now()
	identity, denied := pipeline.Authenticate(ctx, BearerToken(input.Header, input.QueryKey))
	if denied != nil {
		return nil, denied
	}
	codec, found := pipeline.codecs.Get(input.Kind)
	if !found {
		return nil, internal("unsupported_format", "The protocol is not supported.")
	}
	callID := pipeline.ids()
	salt := callID
	peer := format.Peer{Side: format.Client, Vendor: input.Kind.DefaultVendor(), Envelope: Envelope{Secret: pipeline.secret}, Salt: salt}
	request, decodeLosses, err := codec.DecodeRequest(input.Body, input.Header, peer)
	if err != nil {
		var failure *protocol.Error
		if errors.As(err, &failure) {
			return nil, failure
		}
		return nil, invalid("invalid_request", "The request is invalid: "+sentence(err.Error()))
	}
	unsealLosses := unsealRequest(peer, request)
	losses := append(decodeLosses, unsealLosses...)
	if input.Model != "" {
		request.Model = input.Model
	}
	if request.Model == "" {
		return nil, invalid("model_required", "The model field is required.")
	}
	if refusal := protocol.RejectUpstreamState(request); refusal != nil {
		return nil, refusal
	}
	if input.ForceStream {
		request.Stream = true
	}

	state := &call{ctx: ctx, pipeline: pipeline, kind: input.Kind, codec: codec, peer: peer, salt: salt, writer: writer, request: request, identity: identity, started: started, release: func() {}, clientHeader: input.Header, body: input.Body, unsealed: len(unsealLosses) > 0 || requestHasSignature(request)}
	compiled, version, err := pipeline.compiledPolicy(ctx, identity.Project.ID)
	if err != nil {
		pipeline.logger.Error("project policy could not be compiled", "project", identity.Project.ID, "error", err)
		return nil, internal("policy_unavailable", "The project policy is unavailable.")
	}
	state.compiled, state.policyVersion = compiled, version
	record := &entity.Call{
		ID: callID, ProjectID: identity.Project.ID, AgentKeyID: identity.AgentKey.ID,
		ClientFormat: string(input.Kind), PublicModel: request.Model, State: "running", Stream: request.Stream,
		AnalysisState: "pending", CreatedAt: started,
	}
	state.recorder = NewRecorder(pipeline.repository, pipeline.logger, record)

	release, err := pipeline.limiter.Acquire(
		limit.Scoped{Scope: "project:" + identity.Project.ID, Limit: limit.Limit{RequestsPerMinute: identity.Project.RequestsPerMinute, Concurrency: identity.Project.Concurrency}},
		limit.Scoped{Scope: "key:" + identity.AgentKey.ID, Limit: limit.Limit{RequestsPerMinute: identity.AgentKey.RequestsPerMinute, Concurrency: identity.AgentKey.Concurrency}},
	)
	if err != nil {
		return nil, failure("rate_limited", protocol.ClassRateLimit, "rate_limited", "The request rate or concurrency limit has been exceeded.", http.StatusTooManyRequests)
	}
	state.release = release

	// offset counts the leading items the parent conversation already holds; those are not
	// stored again.
	offset := 0
	var parent *entity.Call
	if request.PreviousResponseID != "" {
		var refusal *protocol.Error
		if parent, offset, refusal = pipeline.attachHistory(ctx, identity.AgentKey.ID, request); refusal != nil {
			release()
			return nil, refusal
		}
		state.historyExpanded = true
	}
	if parent == nil {
		if linked, prefix := pipeline.link(ctx, identity, request); linked != nil {
			parent, offset = linked, prefix
		}
	}
	state.parent, state.offset = parent, offset

	if refusal := state.open(); refusal != nil {
		return state, refusal
	}
	if refusal := state.screen(losses); refusal != nil {
		return state, refusal
	}
	return state, nil
}

func (state *call) open() *protocol.Error {
	record := state.recorder.Call()
	record.MessageOffset = state.offset
	if state.parent != nil {
		record.ParentCallID = state.parent.ID
		record.SessionID = state.parent.SessionID
	}
	if record.SessionID == "" {
		record.SessionID = record.ID
	}
	state.stickyKey = record.SessionID
	if err := state.recorder.Open(state.ctx); err != nil {
		state.pipeline.logger.Error("call could not be recorded", "call", record.ID, "error", err)
		return internal("audit_unavailable", "The call could not be recorded.")
	}
	state.opened = true
	return nil
}

// screen redacts, evaluates the online policy, resolves the route and reserves quota. A refusal
// here is recorded as a rejected call so the audit trail shows what was turned away and why.
func (state *call) screen(unsealLosses []protocol.Loss) *protocol.Error {
	pipeline, identity, request := state.pipeline, state.identity, state.request
	original := protocol.CloneRequest(request)
	state.client = original
	state.toolKinds = map[string]protocol.ToolKind{}
	for _, tool := range original.Tools {
		state.toolKinds[tool.Name] = tool.Kind
	}
	engine, rules, err := pipeline.redactionEngine(state.ctx, identity.Project)
	if err != nil {
		state.recordRequest(original, nil)
		return internal("rules_unavailable", "The redaction rules are unavailable.")
	}
	state.scope = engine.NewScope()
	state.redactionRules = rules
	result, err := state.scope.ApplyRequest(request)
	if result != nil {
		state.hits = result.Hits
		state.redacted = result.Values()
	}
	state.recorder.Call().RedactionHits = state.redacted
	if err != nil {
		state.recordRequest(original, nil)
		state.recordRedactionBlock("redaction_immutable", "Sensitive content appears in a position that cannot be rewritten safely.")
		return blocked("redaction_immutable", "Sensitive content appears in a position that cannot be redacted safely.", http.StatusUnprocessableEntity)
	}
	if result.Blocked {
		state.recordRequest(original, nil)
		reason := "The request matches a blocking redaction rule."
		for _, hit := range result.Hits {
			if hit.Action == redact.ActionBlock {
				reason = "The request matches the blocking redaction rule " + hit.Label + "."
				break
			}
		}
		state.recordRedactionBlock("sensitive_content_blocked", reason)
		return blocked("sensitive_content_blocked", reason, http.StatusForbidden)
	}

	state.recordRequest(original, unsealLosses)
	if refusal := state.evaluateRequest(state.baseTrace()); refusal != nil {
		return refusal
	}

	targets, available, err := pipeline.resolve(state.ctx, identity.Project.ID, request.Model)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		pipeline.logger.Error("routes could not be read", "error", err)
		return internal("store_failure", "The model routes could not be loaded.")
	}
	if err != nil || len(targets) == 0 {
		return failure("invalid_request", protocol.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
	}
	state.available = available
	state.candidates = pipeline.selector.Plan(targets, available.keys, state.stickyKey)
	if len(state.candidates) == 0 {
		return failure("upstream_failure", protocol.ClassNoChannel, "no_available_channel", "No channel is available for this model.", http.StatusServiceUnavailable)
	}

	state.estimate = estimateTokens(request)
	allowed, day, err := pipeline.repository.ReserveTokens(state.ctx, identity.Project.ID, state.estimate, identity.Project.DailyTokenQuota)
	if err != nil {
		return internal("quota_unavailable", "The token quota is unavailable.")
	}
	if !allowed {
		return failure("rate_limited", protocol.ClassRateLimit, "daily_quota_exceeded", "The daily token quota has been exhausted.", http.StatusTooManyRequests)
	}
	state.day = day
	state.reserved = true
	return nil
}

// observeOptions builds the adapter configuration shared by every online evaluation. The redactor
// uses the same scope as the request walker, so placeholders in the shown view match what the
// upstream received.
func (state *call) observeOptions() observe.Options {
	principal := observation.Principal{
		ProjectID: state.identity.Project.ID, AgentKeyID: state.identity.AgentKey.ID,
		TrustLevel: state.identity.AgentKey.TrustLevel, Role: state.identity.AgentKey.Role,
	}
	return observe.Options{
		TraceID:   state.recorder.Call().ID,
		Principal: principal,
		Redactor:  state.redactor(),
	}
}

func (state *call) redactor() observe.Redactor {
	return func(text, path string) (string, []observation.Span) {
		if state.scope == nil {
			return text, nil
		}
		shown, result := state.scope.Replace(text, path)
		if result == nil {
			return shown, nil
		}
		if result.Blocked {
			return "[blocked]", []observation.Span{{Start: 0, End: len(text), Label: "blocked"}}
		}
		spans := make([]observation.Span, 0, len(result.Hits))
		for _, hit := range result.Hits {
			spans = append(spans, observation.Span{Start: hit.Start, End: hit.End, Label: hit.Label, Secret: hit.Secret})
		}
		return shown, spans
	}
}

// baseTrace is the client request as an observation; online phases append their items to it.
func (state *call) baseTrace() *observation.Trace {
	trace := observe.Request(state.client, state.observeOptions())
	if trace.PreviousCallID == "" && state.parent != nil {
		trace.PreviousCallID = state.parent.ID
	}
	return trace
}

// evaluateRequest runs the request checkpoint: the schema and content detectors, then the rules of
// the request phase. A blocking decision aborts the call; a detector or rule error is fail-closed
// only when the project has a blocking rule for that phase.
func (state *call) evaluateRequest(trace *observation.Trace) *protocol.Error {
	return state.evaluate(trace, policy.PhaseRequest, []string{"schema", "content"}, observation.ToolCall{}, observation.ToolTags{})
}

// evaluateToolCall runs the tool checkpoint. The tool call has not been released to the client yet,
// so a block prevents the side effect instead of reporting it.
func (state *call) evaluateToolCall(trace *observation.Trace, tool observation.ToolCall, tags observation.ToolTags) *protocol.Error {
	if refusal := state.evaluate(trace, policy.PhaseToolCall, []string{"schema", "content", "flow"}, tool, tags); refusal != nil {
		return refusal
	}
	return state.vetoToolCall(trace, tool)
}

// vetoToolCall asks the decision model about a side-effecting call that the deterministic policy
// already allowed. The model can only add a block; a timeout, error or malformed answer leaves the
// deterministic decision untouched and is recorded as evaluated-but-unavailable.
func (state *call) vetoToolCall(trace *observation.Trace, tool observation.ToolCall) *protocol.Error {
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
	state.decisions = append(state.decisions, entity.Decision{
		ID: "dec_" + secure.Random(10), ProjectID: state.identity.Project.ID, CallID: state.recorder.Call().ID,
		PolicyVersion: state.policyVersion, Phase: phase, Action: action, Blocked: blockedCall,
		Code: "decision_model", Severity: analysis.SeverityInfo, Reason: reason,
		Attributes: attributes, CreatedAt: time.Now().UTC(),
	})
}

func (state *call) evaluate(trace *observation.Trace, phase string, detectors []string, tool observation.ToolCall, tags observation.ToolTags) *protocol.Error {
	result := state.pipeline.analysis.Evaluate(state.ctx, trace, analysis.Options{Only: detectors})
	// Each evaluation sees the whole trace, so replace rather than append. Appending would count
	// a request finding once per tool call and change count or threshold rules.
	state.findings = result.Findings
	if len(result.Errors) > 0 {
		return state.gateFailure(phase, result.Errors[0])
	}
	outcome, err := state.compiled.Evaluate(state.ctx, policy.Input{
		Phase: phase, Principal: trace.Principal, Tool: tool, Tags: tags,
		Trace: trace, Findings: state.findings,
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
	if phase == policy.PhaseToolCall {
		code = "tool_call_blocked"
	}
	message := "The analysis policy blocked the request."
	if reason := firstNonEmpty(outcome.Block.Reason, outcome.Block.RuleID); reason != "" {
		message = sentence("The analysis policy blocked the request: " + reason)
	}
	return blocked(code, message, http.StatusForbidden)
}

func (state *call) gateFailure(phase, detail string) *protocol.Error {
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
	state.decisions = append(state.decisions, entity.Decision{
		ID: "dec_" + secure.Random(10), ProjectID: state.identity.Project.ID, CallID: state.recorder.Call().ID,
		PolicyVersion: state.policyVersion, Phase: phase, Action: decision.Action,
		Blocked: blockedCall, RuleID: decision.RuleID, Code: decision.RuleID,
		Severity: decision.Severity, Reason: decision.Reason, Attributes: attributes,
		CreatedAt: time.Now().UTC(),
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// recordRequest stores what the client sent: the messages this call added, in plaintext, the
// parameters and the effective tool definitions. A continuation therefore carries its own tools.
func (state *call) recordRequest(original *protocol.Request, losses []protocol.Loss) {
	params := *original
	params.Items = nil
	params.Tools = nil
	offset := min(state.offset, len(original.Items))
	record := auditevent.Request{
		Items: original.Items[offset:], Params: params, Tools: original.Tools,
		Hits: nonNil(state.hits), Ledger: protocol.Ledger{Losses: losses},
		Principal: &auditevent.Principal{AgentKeyID: state.identity.AgentKey.ID, Role: state.identity.AgentKey.Role, TrustLevel: state.identity.AgentKey.TrustLevel},
	}
	state.recorder.Event(auditevent.KindRequest, record)
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

// estimateTokens is a deliberately rough reservation: a quarter of the JSON size plus the output
// ceiling the client asked for, clamped so a hostile ceiling can neither shrink nor overflow it.
func estimateTokens(request *protocol.Request) int64 {
	output := int64(defaultMaxOutputTokens)
	if request.MaxOutputTokens != nil {
		output = min(max(*request.MaxOutputTokens, 1), maximumOutputEstimate)
	}
	return int64(len(protocol.Marshal(request.Items))/4) + output
}

const (
	defaultMaxOutputTokens = 4096
	maximumOutputEstimate  = 1 << 30
)
