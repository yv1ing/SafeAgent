package pipeline

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observe"
	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// call carries everything one client call needs from admission to its terminal state.
type call struct {
	ctx           context.Context
	pipeline      *Pipeline
	kind          model.Format
	coder         codec.Codec
	peer          codec.Peer
	salt          string
	writer        Writer
	request       *model.Request
	identity      *Identity
	targets       []routing.Target
	endpoints     map[string]endpoint
	available     channels
	candidates    []routing.Candidate
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
	parentSource  string
	offset        int
	// client is the request as the client sent it; analysis and fingerprints are taken from it,
	// never from the redacted copy that goes upstream.
	client          *model.Request
	clientHeader    http.Header
	body            []byte
	stream          bool
	historyExpanded bool
	streamCommitted bool
	streamTruncated bool
	sessionSource   string
	explicitSession string
	sessionConflict *model.Error
	unsealed        bool
	toolKinds       map[string]model.ToolKind
	failureLedger   model.Ledger
	findings        []analysis.Finding
	decisions       []entity.Decision
	redactionRules  []redact.Rule
	reserved        bool
	opened          bool
	blockedRecorded bool
}

func requestHasSignature(request *model.Request) bool {
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

// admit authenticates, decodes, redacts, analyzes, routes, rate-limits and records the call. A
// non-nil state with a refusal means the call was already recorded as rejected.
func (pipeline *Pipeline) admit(ctx context.Context, input Request, writer Writer) (*call, *model.Error) {
	started := time.Now()
	identity, denied := pipeline.Authenticate(ctx, BearerToken(input.Header, input.QueryKey))
	if denied != nil {
		return nil, denied
	}
	coder, found := pipeline.codecs.Get(input.Kind)
	if !found {
		return nil, internal("unsupported_format", "The protocol is not supported.")
	}
	callID := pipeline.ids()
	explicitSession, validSession := namespacedSession(identity.AgentKey.ID, input.Header.Get(SessionHeader))
	if !validSession {
		return nil, invalid("invalid_session", "The X-SafeAgent-Session header is invalid.")
	}
	salt := callID
	peer := codec.Peer{Side: codec.Client, Vendor: input.Kind.DefaultVendor(), Envelope: Envelope{Secret: pipeline.secret}, Salt: salt}
	request, decodeLosses, err := coder.DecodeRequest(input.Body, input.Header, peer)
	if err != nil {
		var failure *model.Error
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
	if refusal := model.RejectUpstreamState(request); refusal != nil {
		return nil, refusal
	}
	if input.ForceStream {
		request.Stream = true
	}
	peer.IncludeUsage = request.StreamOptions != nil && request.StreamOptions.IncludeUsage

	transport := "json"
	if request.Stream {
		transport = "sse"
	}
	state := &call{
		ctx: ctx, pipeline: pipeline, kind: input.Kind, coder: coder, peer: peer, salt: salt, writer: writer,
		request: request, identity: identity, started: started, release: func() {}, clientHeader: input.Header,
		body: input.Body, stream: request.Stream, unsealed: len(unsealLosses) > 0 || requestHasSignature(request),
		explicitSession: explicitSession,
	}
	compiled, version, err := pipeline.compiledPolicy(ctx, identity.Project.ID)
	if err != nil {
		pipeline.logger.Error("project policy could not be compiled", "project", identity.Project.ID, "error", err)
		return nil, internal("policy_unavailable", "The project policy is unavailable.")
	}
	state.compiled, state.policyVersion = compiled, version
	record := &entity.Call{
		ID: callID, ProjectID: identity.Project.ID, AgentKeyID: identity.AgentKey.ID,
		ClientFormat: string(input.Kind), Kind: string(input.Kind), ClientTransport: transport,
		PublicModel: request.Model, State: "running", Stream: request.Stream,
		AnalysisState: "pending", CreatedAt: started,
	}
	state.recorder = NewRecorder(pipeline.repository, pipeline.logger, pipeline.secret, record)

	release, err := pipeline.limiter.Acquire(
		limit.Scoped{Scope: "project:" + identity.Project.ID, Limit: limit.Limit{RequestsPerMinute: identity.Project.RequestsPerMinute, Concurrency: identity.Project.Concurrency}},
		limit.Scoped{Scope: "key:" + identity.AgentKey.ID, Limit: limit.Limit{RequestsPerMinute: identity.AgentKey.RequestsPerMinute, Concurrency: identity.AgentKey.Concurrency}},
	)
	if err != nil {
		return nil, failure("rate_limited", model.ClassRateLimit, "rate_limited", "The request rate or concurrency limit has been exceeded.", http.StatusTooManyRequests)
	}
	state.release = release

	// offset counts the leading items the parent conversation already holds; those are not
	// stored again.
	offset := 0
	var parent *entity.Call
	if request.PreviousResponseID != "" {
		var refusal *model.Error
		if parent, offset, refusal = pipeline.attachHistory(ctx, identity.AgentKey.ID, request); refusal != nil {
			release()
			return nil, refusal
		}
		state.historyExpanded = true
		state.parentSource = "previous_response"
	}
	if parent == nil {
		if linked, prefix, source := pipeline.link(ctx, identity, request); linked != nil {
			if explicitSession == "" || linked.SessionID == explicitSession {
				parent, offset, state.parentSource = linked, prefix, source
			}
		}
	}
	if parent != nil && explicitSession != "" && parent.SessionID != "" && parent.SessionID != explicitSession {
		if state.parentSource == "previous_response" {
			state.sessionConflict = failure("invalid_request", model.ClassInvalid, "session_conflict", "The previous response belongs to a different session.", http.StatusConflict)
		} else {
			parent, offset, state.parentSource = nil, 0, ""
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

func (state *call) open() *model.Error {
	record := state.recorder.Call()
	record.MessageOffset = state.offset
	if state.parent != nil {
		record.ParentCallID = state.parent.ID
		record.SessionID = state.parent.SessionID
	}
	if state.explicitSession != "" {
		record.SessionID = state.explicitSession
		state.sessionSource = "explicit"
	} else if state.parent != nil {
		state.sessionSource = state.parentSource
	}
	if record.SessionID == "" {
		record.SessionID = record.ID
		state.sessionSource = "call_id"
	}
	state.stickyKey = record.SessionID
	if state.kind == model.Responses {
		record.ResponseID = model.ResponseID(state.kind, record.ID, "")
	}
	if err := state.recorder.Open(state.ctx); err != nil {
		state.pipeline.logger.Error("call could not be recorded", "call", record.ID, "error", err)
		return internal("audit_unavailable", "The call could not be recorded.")
	}
	state.opened = true
	return nil
}

// screen redacts, evaluates the online policy, resolves the route and reserves quota. A refusal
// here is recorded as a rejected call so the audit trail shows what was turned away and why.
func (state *call) screen(unsealLosses []model.Loss) *model.Error {
	pipeline, identity, request := state.pipeline, state.identity, state.request
	original := model.CloneRequest(request)
	state.client = original
	if state.sessionConflict != nil {
		state.recordRequest(original, nil)
		return state.sessionConflict
	}
	state.toolKinds = map[string]model.ToolKind{}
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
	state.recordRequestToolResults()
	if refusal := state.evaluateRequest(state.baseTrace()); refusal != nil {
		return refusal
	}
	if results := state.newToolResults(); len(results) > 0 {
		if refusal := state.evaluateToolResults(state.baseTrace(), results); refusal != nil {
			return refusal
		}
	}

	targets, endpoints, available, routeKind, err := pipeline.resolve(state.ctx, identity.Project.ID, request.Model, state.kind)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		pipeline.logger.Error("routes could not be read", "error", err)
		return internal("store_failure", "The model routes could not be loaded.")
	}
	if err != nil || len(targets) == 0 {
		return failure("invalid_request", model.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
	}
	if routeKind != string(entity.ModelGeneration) {
		return failure("invalid_request", model.ClassInvalid, "model_kind_mismatch", "The model is not a generation model.", http.StatusUnprocessableEntity)
	}
	if request.ChoiceCount > 1 && (!state.stream || len(state.hits) > 0 || state.historyExpanded || !state.hasSameFormat(targets)) {
		return model.UnsupportedFeature("unsupported_multi_choice", "Multiple candidates are available only through a same-format channel without redaction or history expansion.")
	}
	state.targets, state.endpoints, state.available = targets, endpoints, available
	state.candidates = pipeline.selector.Plan(targets, available.keys, state.stickyKey, string(state.kind))
	if len(state.candidates) == 0 {
		return failure("upstream_failure", model.ClassNoChannel, "no_available_channel", "No channel is available for this model.", http.StatusServiceUnavailable)
	}

	state.estimate = estimateTokens(request)
	allowed, day, err := pipeline.repository.ReserveTokens(state.ctx, identity.Project.ID, state.estimate, identity.Project.DailyTokenQuota)
	if err != nil {
		return internal("quota_unavailable", "The token quota is unavailable.")
	}
	if !allowed {
		return failure("rate_limited", model.ClassRateLimit, "daily_quota_exceeded", "The daily token quota has been exhausted.", http.StatusTooManyRequests)
	}
	state.day = day
	state.reserved = true
	return nil
}

// hasSameFormat reports whether every candidate can pass the client body through unchanged. A
// multi-choice request is safe only when no target would be re-encoded or converted.
func (state *call) hasSameFormat(targets []routing.Target) bool {
	for _, target := range targets {
		if target.Format != string(state.kind) {
			return false
		}
	}
	return len(targets) > 0
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

// recordRequest stores what the client sent: the messages this call added, in plaintext, the
// parameters and the effective tool definitions. A continuation therefore carries its own tools.
func (state *call) recordRequestToolResults() {
	offset := min(state.offset, len(state.request.Items))
	for index := offset; index < len(state.request.Items); index++ {
		item := &state.request.Items[index]
		if item.Kind != model.ItemToolResult {
			continue
		}
		state.recorder.ToolResult(auditevent.ToolResult{CallID: item.CallID, Name: item.Name, IsError: item.IsError})
	}
}

func (state *call) newToolResults() []policy.ToolResult {
	offset := min(state.offset, len(state.request.Items))
	results := []policy.ToolResult{}
	for index := offset; index < len(state.request.Items); index++ {
		item := &state.request.Items[index]
		if item.Kind != model.ItemToolResult {
			continue
		}
		text := ""
		for _, part := range item.Parts {
			if part.Text != "" {
				text += part.Text
			}
		}
		if len(text) > maxPolicyResultBytes {
			text = text[:maxPolicyResultBytes]
		}
		results = append(results, policy.ToolResult{
			CallID: item.CallID, Name: item.Name, IsError: item.IsError, Text: text, Trust: "untrusted",
		})
	}
	return results
}

const maxPolicyResultBytes = 8 << 10

func (state *call) recordRequest(original *model.Request, losses []model.Loss) {
	params := *original
	params.Items = nil
	params.Tools = nil
	offset := min(state.offset, len(original.Items))
	record := auditevent.Request{
		Items: original.Items[offset:], Params: params, Tools: original.Tools,
		Hits: nonNil(state.hits), Ledger: model.Ledger{Losses: losses}, SessionSource: state.sessionSource,
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
func estimateTokens(request *model.Request) int64 {
	output := int64(defaultMaxOutputTokens)
	if request.MaxOutputTokens != nil {
		output = min(max(*request.MaxOutputTokens, 1), maximumOutputEstimate)
	}
	return int64(len(model.Marshal(request.Items))/4) + output
}

const (
	defaultMaxOutputTokens = 4096
	maximumOutputEstimate  = 1 << 30
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
