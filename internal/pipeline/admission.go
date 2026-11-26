package pipeline

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observation/adapter"
	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
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
	lane          classify.Lane
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
	callHits      []auditevent.Hit
	// redactedOccurrences counts every redaction applied to the forwarded request, including the
	// ones relocated to ancestor items. Raw pass-through is only safe when it is zero.
	redactedOccurrences int
	origins             []observation.Locator
	attempts            int
	parent              *entity.Call
	parentSource        string
	offset              int
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
	hasSignatures   bool
	reviewBytes     int
	reviewTimeout   time.Duration
	reviewBlocked   bool
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

// admit authenticates, decodes, classifies, redacts, analyzes, routes, rate-limits and records the
// call. A non-nil state with a refusal means the call was already recorded as rejected.
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
	operation := input.Operation
	if !operation.Valid() {
		operation = classify.Generation
	}
	classification := classify.Classify(operation, requestHints(input.Header, request))
	state := &call{
		ctx: ctx, pipeline: pipeline, kind: input.Kind, coder: coder, peer: peer, salt: salt, writer: writer,
		request: request, identity: identity, started: started, release: func() {}, clientHeader: input.Header,
		body: input.Body, stream: request.Stream, hasSignatures: len(unsealLosses) > 0 || requestHasSignature(request),
		explicitSession: explicitSession, reviewBytes: identity.Project.StreamReviewBytes,
		reviewTimeout: time.Duration(identity.Project.StreamReviewTimeoutMS) * time.Millisecond,
		lane:          classification.Lane,
	}
	compiled, version, err := pipeline.compiledPolicy(ctx, identity.Project.ID)
	if err != nil {
		pipeline.logger.Error("project policy could not be compiled", "project", identity.Project.ID, "error", err)
		return nil, internal("policy_unavailable", "The project policy is unavailable.")
	}
	state.compiled, state.policyVersion = compiled, version
	record := &entity.Call{
		ID: callID, ProjectID: identity.Project.ID, AgentKeyID: identity.AgentKey.ID,
		ClientFormat: string(input.Kind), Operation: classification.Operation,
		Purpose: classification.Purpose, PurposeSource: classification.PurposeSource, Lane: classification.Lane,
		ClientTransport: transport, PublicModel: request.Model, State: "running", Stream: request.Stream,
		AnalysisState: analysisStateOf(classification.Lane), CreatedAt: started, UpdatedAt: started,
	}
	state.recorder = NewRecorder(pipeline.repository, pipeline.logger, pipeline.secret, record)
	state.origins = nil

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
	if classification.Lane.Conversational() && request.PreviousResponseID != "" {
		var origins []observation.Locator
		var refusal *model.Error
		if parent, offset, origins, refusal = pipeline.attachHistory(ctx, identity.AgentKey.ID, request); refusal != nil {
			release()
			return nil, refusal
		}
		state.historyExpanded = true
		state.parentSource = "previous_response"
		state.origins = origins
	}
	if parent == nil && classification.Lane.Conversational() {
		if linked, origins, prefix, source := pipeline.link(ctx, identity, request); linked != nil {
			if explicitSession == "" || linked.SessionID == explicitSession {
				parent, offset, state.parentSource = linked, prefix, source
				state.origins = origins
			}
		}
	}
	if parent != nil && explicitSession != "" && parent.SessionID != "" && parent.SessionID != explicitSession {
		if state.parentSource == "previous_response" {
			state.sessionConflict = failure("invalid_request", model.ClassInvalid, "session_conflict", "The previous response belongs to a different session.", http.StatusConflict)
		} else {
			parent, offset, state.parentSource, state.origins = nil, 0, "", nil
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

// analysisStateOf is the initial diagnosis projection of a lane. Only a main call is queued for
// analysis; side calls are counted in traffic but never diagnosed.
func analysisStateOf(lane classify.Lane) string {
	if lane.Analyzed() {
		return "pending"
	}
	return "skipped"
}

// requestHints gathers the request facts the classifier may read. The prompt is the leading
// system or user text, lowercased and bounded so the inference stays cheap.
func requestHints(header http.Header, request *model.Request) classify.Hints {
	return classify.Hints{
		Header:          header,
		ToolCount:       len(request.Tools),
		MaxOutputTokens: request.MaxOutputTokens,
		Prompt:          promptHint(request),
	}
}

const promptHintBytes = 512

func promptHint(request *model.Request) string {
	text := request.Instructions
	if text == "" {
		text = request.User
	}
	if text == "" {
		text = firstConversationText(request.Items)
	}
	return strings.ToLower(truncateBytes(text, promptHintBytes))
}

// truncateBytes clips a hint at a UTF-8 boundary so lower-casing cannot see an invalid sequence.
func truncateBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func firstConversationText(items []model.Item) string {
	for _, item := range items {
		if item.Kind != model.ItemMessage {
			continue
		}
		if item.Role != model.RoleSystem && item.Role != model.RoleDeveloper && item.Role != model.RoleUser {
			continue
		}
		for _, part := range item.Parts {
			if part.Text != "" {
				return part.Text
			}
		}
	}
	return ""
}

func (state *call) open() *model.Error {
	record := state.recorder.Call()
	record.MessageOffset = state.offset
	if state.lane.Conversational() {
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
	} else {
		record.SessionID = ""
		record.ParentCallID = ""
		state.sessionSource = ""
		state.stickyKey = ""
	}
	if state.kind == model.Responses && state.lane.Conversational() {
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
		state.callHits = state.storedHits(result)
		state.redactedOccurrences = len(result.Hits)
		applyRedactionSummary(state.recorder.Call(), state.callHits)
	} else {
		state.callHits = nil
		state.redactedOccurrences = 0
		applyRedactionSummary(state.recorder.Call(), nil)
	}
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

	plan, failure := pipeline.routeFor(state.ctx, identity.Project.ID, request.Model, state.lane, state.clientHeader, state.kind, entity.ModelGeneration)
	if plan.resolution.Route.ID != "" {
		pipeline.recordRouting(state.recorder, plan, request.Model)
		state.endpoints, state.available = plan.endpoints, plan.available
	}
	if failure != nil {
		return failure
	}
	if request.ChoiceCount > 1 && (!state.stream || state.redactedOccurrences > 0 || state.historyExpanded || !state.hasSameFormat(plan.targets)) {
		return model.UnsupportedFeature("unsupported_multi_choice", "Multiple candidates are available only through a same-format channel without redaction or history expansion.")
	}
	state.candidates = pipeline.selector.Plan(plan.targets, plan.available.keys, state.stickyKey, string(state.kind))
	if len(state.candidates) == 0 {
		return unavailableChannel()
	}

	state.estimate = estimateTokens(request)
	allowed, day, err := pipeline.repository.ReserveTokens(state.ctx, identity.Project.ID, state.estimate, identity.Project.DailyTokenQuota)
	if err != nil {
		return internal("quota_unavailable", "The token quota is unavailable.")
	}
	if !allowed {
		return quotaExceeded()
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
// upstream received. The call id is the identity of every item this call adds; Origins carries the
// call and ordinal of the history prefix an expanded request already holds.
func (state *call) observeOptions() adapter.Options {
	principal := observation.Principal{
		ProjectID: state.identity.Project.ID, AgentKeyID: state.identity.AgentKey.ID,
		TrustLevel: state.identity.AgentKey.TrustLevel, Role: state.identity.AgentKey.Role,
	}
	return adapter.Options{
		TraceID:   state.recorder.Call().ID,
		CallID:    state.recorder.Call().ID,
		Origins:   state.origins,
		Principal: principal,
		Redactor:  adapter.ScopeRedactor(state.scope),
	}
}

// redactor is the online side of the one redaction-to-observation bridge. A nil scope keeps the
// views identical, matching a project without redaction rules.
func (state *call) redactor() adapter.Redactor {
	redactor := adapter.ScopeRedactor(state.scope)
	if redactor == nil {
		return func(text, _ string) observation.Value {
			return observation.NewValue(text, text, nil, nil)
		}
	}
	return redactor
}

// baseTrace is the client request as an observation; online phases append their items to it. The
// adapter, the identities and the shown ranges are the same ones the replay path builds, so an
// online verdict and a stored finding describe the same node.
func (state *call) baseTrace() *observation.Trace {
	trace := adapter.Request(state.client, state.observeOptions())
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
			text = truncatePolicyText(text, maxPolicyResultBytes)
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
		Hits: nonNil(state.callHits), Ledger: model.Ledger{Losses: losses}, SessionSource: state.sessionSource,
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
	return int64(len(model.Marshal(request))/4) + output
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
