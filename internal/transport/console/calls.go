package console

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/threat"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type CallView struct {
	ID                     string     `json:"id" required:"true"`
	TrajectoryID           string     `json:"trajectory_id" required:"true"`
	CallSeq                int64      `json:"call_seq" required:"true"`
	AgentKeyID             string     `json:"agent_key_id" required:"true"`
	ClientFormat           string     `json:"client_format" required:"true"`
	Operation              string     `json:"operation" required:"true" enum:"generation,count_tokens,models,model,responses_retrieve,responses_input_items,responses_cancel,responses_delete,embeddings,moderations"`
	Purpose                string     `json:"purpose" required:"true" enum:"chat,subagent,compaction,title,memory,count_tokens,model_list,embedding,moderation,unknown"`
	PurposeSource          string     `json:"purpose_source" required:"true" enum:"declared,heuristic,default"`
	Lane                   string     `json:"lane" required:"true" enum:"main,side,auxiliary"`
	PublicModel            string     `json:"public_model" required:"true"`
	ModelID                string     `json:"model_id" required:"true"`
	UpstreamModel          string     `json:"upstream_model" required:"true"`
	ChannelID              string     `json:"channel_id" required:"true"`
	RouteID                string     `json:"route_id" required:"true"`
	Resolution             string     `json:"resolution,omitempty" enum:"exact,alias,explicit,fallback"`
	State                  string     `json:"state" required:"true" enum:"running,completed,failed,cancelled,interrupted"`
	Outcome                string     `json:"outcome,omitempty" enum:"ok,blocked,rejected,client_error,upstream_error,unsupported,not_found,rate_limited,cancelled"`
	ErrorCode              string     `json:"error_code" required:"true"`
	Stream                 bool       `json:"stream" required:"true"`
	FirstTokenMilliseconds int        `json:"first_token_ms" required:"true"`
	DurationMilliseconds   int        `json:"duration_ms" required:"true"`
	InputTokens            int64      `json:"input_tokens" required:"true"`
	OutputTokens           int64      `json:"output_tokens" required:"true"`
	CacheReadTokens        int64      `json:"cache_read_tokens" required:"true"`
	CacheWriteTokens       int64      `json:"cache_write_tokens" required:"true"`
	ReasoningTokens        int64      `json:"reasoning_tokens" required:"true"`
	UsageKnown             bool       `json:"usage_known" required:"true"`
	CostMicros             *int64     `json:"cost_micros" required:"true"`
	Currency               string     `json:"currency" required:"true"`
	RedactionValues        int        `json:"redaction_values" required:"true"`
	RedactionLabels        []string   `json:"redaction_labels" required:"true"`
	FindingCount           int        `json:"finding_count" required:"true"`
	MaxSeverity            string     `json:"max_severity" required:"true"`
	Losses                 []LossView `json:"losses" required:"true"`
	Attempts               int        `json:"attempts" required:"true"`
	SessionID              string     `json:"session_id" required:"true"`
	ParentCallID           string     `json:"parent_call_id" required:"true"`
	CreatedAt              string     `json:"created_at" required:"true"`
	TerminatedAt           string     `json:"terminated_at" required:"true"`
}

type LossView struct {
	Path    string `json:"path" required:"true"`
	Code    string `json:"code" required:"true"`
	Class   string `json:"class,omitempty"`
	Detail  string `json:"detail" required:"true"`
	From    string `json:"from" required:"true"`
	To      string `json:"to" required:"true"`
	ItemKey string `json:"item_key,omitempty"`
}

type CallPage struct {
	Items []CallView  `json:"items" required:"true"`
	Next  *CursorView `json:"next,omitempty"`
}

type AttemptView struct {
	Index          int    `json:"index" required:"true"`
	ChannelID      string `json:"channel_id" required:"true"`
	ChannelName    string `json:"channel_name" required:"true"`
	ModelID        string `json:"model_id" required:"true"`
	UpstreamModel  string `json:"upstream_model" required:"true"`
	Format         string `json:"format" required:"true"`
	Result         string `json:"result" required:"true" enum:"running,succeeded,failed,cancelled"`
	Code           string `json:"code" required:"true"`
	DurationMillis int    `json:"duration_ms" required:"true"`
	FirstTokenMS   int    `json:"first_token_ms" required:"true"`
}

type CallEventView struct {
	ID         string          `json:"id" required:"true"`
	Seq        int64           `json:"seq" required:"true"`
	Kind       string          `json:"kind" required:"true"`
	Boundary   string          `json:"boundary" required:"true"`
	Actor      string          `json:"actor" required:"true"`
	At         string          `json:"at" required:"true"`
	CallID     string          `json:"call_id" required:"true"`
	ActionID   string          `json:"action_id" required:"true"`
	Summary    string          `json:"summary" required:"true"`
	Code       string          `json:"code" required:"true"`
	Payload    json.RawMessage `json:"payload" required:"true"`
	Inputs     []string        `json:"inputs" required:"true"`
	Outputs    []string        `json:"outputs" required:"true"`
	Checks     int             `json:"checks" required:"true"`
	OccurredAt string          `json:"occurred_at" required:"true"`
}

type FindingView struct {
	ID          string          `json:"id" required:"true"`
	Fingerprint string          `json:"fingerprint" required:"true"`
	Code        string          `json:"code" required:"true"`
	Origin      string          `json:"origin" required:"true"`
	Trust       string          `json:"trust" required:"true"`
	Sink        string          `json:"sink" required:"true"`
	Severity    string          `json:"severity" required:"true"`
	Confidence  float64         `json:"confidence" required:"true"`
	Invariant   string          `json:"invariant" required:"true"`
	Transfer    string          `json:"transfer" required:"true"`
	Control     string          `json:"control" required:"true"`
	EventID     string          `json:"event_id" required:"true"`
	CallID      string          `json:"call_id" required:"true"`
	Evidence    json.RawMessage `json:"evidence" required:"true"`
	Segments    json.RawMessage `json:"segments" required:"true"`
	CreatedAt   string          `json:"created_at" required:"true"`
}

type CallDetailView struct {
	Call      CallView        `json:"call" required:"true"`
	Attempts  []AttemptView   `json:"attempts" required:"true"`
	Events    []CallEventView `json:"events" required:"true"`
	Findings  []FindingView   `json:"findings" required:"true"`
	Integrity IntegrityView   `json:"integrity" required:"true"`
}

type IntegrityView struct {
	Status   string `json:"status" required:"true"`
	Events   int64  `json:"events" required:"true"`
	Head     string `json:"head" required:"true"`
	BrokenAt int64  `json:"broken_at,omitempty"`
	Error    string `json:"error,omitempty"`
}

type listCallsInput struct {
	projectPath
	pageQuery
	State      string `query:"state" json:"-"`
	AgentKeyID string `query:"agent_key_id" json:"-"`
	Model      string `query:"model" json:"-"`
	SessionID  string `query:"session_id" json:"-"`
	Lane       string `query:"lane" json:"-"`
	Operation  string `query:"operation" json:"-"`
	Purpose    string `query:"purpose" json:"-"`
	Outcome    string `query:"outcome" json:"-"`
	Severity   string `query:"min_severity" json:"-"`
	Auxiliary  bool   `query:"include_auxiliary" json:"-"`
}

type callPath struct {
	ProjectID string `path:"project_id" json:"-" required:"true"`
	CallID    string `path:"call_id" json:"-" required:"true"`
}

func callView(call entity.Call) CallView {
	losses := make([]LossView, 0, len(call.Losses))
	for _, loss := range call.Losses {
		losses = append(losses, LossView{Path: loss.Path, Code: loss.Code, Class: loss.Class, Detail: loss.Detail, From: loss.From, To: loss.To, ItemKey: loss.ItemKey})
	}
	return CallView{
		ID: call.ID, TrajectoryID: call.TrajectoryID, CallSeq: call.CallSeq, AgentKeyID: call.AgentKeyID,
		ClientFormat: call.ClientFormat, Operation: string(call.Operation), Purpose: string(call.Purpose),
		PurposeSource: string(call.PurposeSource), Lane: string(call.Lane), PublicModel: call.PublicModel,
		ModelID: call.ModelID, UpstreamModel: call.UpstreamModel, ChannelID: call.ChannelID, RouteID: call.RouteID,
		Resolution: call.Resolution, State: call.State, Outcome: string(call.Outcome), ErrorCode: call.ErrorCode,
		Stream: call.Stream, FirstTokenMilliseconds: call.FirstTokenMilliseconds, DurationMilliseconds: call.DurationMilliseconds,
		InputTokens: call.InputTokens, OutputTokens: call.OutputTokens, CacheReadTokens: call.CacheReadTokens,
		CacheWriteTokens: call.CacheWriteTokens, ReasoningTokens: call.ReasoningTokens, UsageKnown: call.UsageKnown,
		CostMicros: call.CostMicros, Currency: call.Currency, RedactionValues: call.RedactionValues,
		RedactionLabels: nonNil(call.RedactionLabels), FindingCount: call.FindingCount, MaxSeverity: call.MaxSeverity,
		Losses: losses, Attempts: call.Attempts, SessionID: call.SessionID, ParentCallID: call.ParentCallID,
		CreatedAt: stamp(call.CreatedAt), TerminatedAt: stampOptional(call.TerminatedAt),
	}
}

func storedFindingView(f entity.Finding) FindingView {
	return FindingView{
		ID: f.ID, Fingerprint: f.Fingerprint, Code: f.Code, Origin: f.Origin, Trust: f.Trust, Sink: f.Sink,
		Severity: f.Severity, Confidence: f.Confidence, Invariant: f.Invariant, Transfer: f.Transfer,
		Control: f.Control, EventID: f.EventID, CallID: f.CallID, Evidence: f.Evidence, Segments: f.Segments,
		CreatedAt: stamp(f.CreatedAt),
	}
}

func eventView(event trajectory.Event, administrator bool) CallEventView {
	view := CallEventView{
		ID: event.ID, Seq: event.Seq, Kind: string(event.Kind), Boundary: string(event.Boundary),
		Actor: string(event.Actor), At: stamp(event.At), CallID: event.CallID, ActionID: event.ActionID,
		Summary: eventSummary(event), Code: eventCode(event), Inputs: refIDs(event.Inputs),
		Outputs: refIDs(event.Outputs), Checks: len(event.Checks), OccurredAt: stamp(event.At),
	}
	if administrator {
		view.Payload = event.Payload
	}
	return view
}

func refIDs(refs []trajectory.Ref) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.Segment)
	}
	return ids
}

func eventSummary(event trajectory.Event) string {
	switch event.Kind {
	case trajectory.KindActionProposed:
		payload, err := trajectory.DecodePayload[trajectory.ActionProposed](&event)
		if err == nil {
			return firstNonEmpty(payload.ToolName, payload.PublicModel, string(payload.ActionType))
		}
	case trajectory.KindActionExecuted:
		payload, err := trajectory.DecodePayload[trajectory.ActionExecuted](&event)
		if err == nil {
			return firstNonEmpty(payload.ErrorCode, string(payload.Outcome), payload.State)
		}
	case trajectory.KindGateDecision:
		payload, err := trajectory.DecodePayload[trajectory.GateDecision](&event)
		if err == nil {
			return firstNonEmpty(payload.Reason, payload.Code, payload.Gate)
		}
	case trajectory.KindObservation:
		payload, err := trajectory.DecodePayload[trajectory.Observation](&event)
		if err == nil {
			return firstNonEmpty(payload.Role, payload.Source)
		}
	}
	return string(event.Kind)
}

func eventCode(event trajectory.Event) string {
	switch event.Kind {
	case trajectory.KindGateDecision:
		payload, _ := trajectory.DecodePayload[trajectory.GateDecision](&event)
		return payload.Code
	case trajectory.KindActionExecuted:
		payload, _ := trajectory.DecodePayload[trajectory.ActionExecuted](&event)
		return payload.ErrorCode
	}
	return ""
}

func (console *Console) registerCalls() {
	console.add("GET", "/projects/{project_id}/calls", "listCalls", listCallsInput{}, CallPage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		position, err := cursor(ctx)
		if err != nil {
			return nil, err
		}
		page, err := console.Calls.List(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), service.CallFilter{
			State: ctx.Query("state"), MinSeverity: ctx.Query("min_severity"), Model: ctx.Query("model"),
			AgentKeyID: ctx.Query("agent_key_id"), SessionID: ctx.Query("session_id"), Lane: ctx.Query("lane"),
			Operation: ctx.Query("operation"), Purpose: ctx.Query("purpose"), Outcome: ctx.Query("outcome"),
			IncludeAuxiliary: ctx.Query("include_auxiliary") == "true",
		}, position)
		if err != nil {
			return nil, err
		}
		return CallPage{Items: viewAll(page.Items, func(call *entity.Call) CallView { return callView(*call) }), Next: cursorView(page.Next)}, nil
	})
	console.add("GET", "/projects/{project_id}/calls/{call_id}", "getCall", callPath{}, CallDetailView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		detail, err := console.Calls.Detail(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("call_id"))
		if err != nil {
			return nil, err
		}
		return CallDetailView{
			Call: callView(*detail.Call),
			Attempts: viewAll(service.Attempts(detail.Events), func(attempt *service.CallAttempt) AttemptView {
				return AttemptView{Index: attempt.Index, ChannelID: attempt.ChannelID, ChannelName: attempt.ChannelName, ModelID: attempt.ModelID, UpstreamModel: attempt.UpstreamModel, Format: attempt.Format, Result: attempt.Result, Code: attempt.Code, DurationMillis: attempt.DurationMillis, FirstTokenMS: attempt.FirstTokenMillis}
			}),
			Events:    viewAll(detail.Events, func(event *trajectory.Event) CallEventView { return eventView(*event, identity.User.Administrator) }),
			Findings:  viewAll(detail.Findings, func(finding *entity.Finding) FindingView { return storedFindingView(*finding) }),
			Integrity: IntegrityView{Status: detail.Integrity.Status, Events: detail.Integrity.Events, Head: detail.Integrity.Head, BrokenAt: detail.Integrity.BrokenAt, Error: detail.Integrity.Error},
		}, nil
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

func threatFindingView(f threat.Finding) FindingView {
	return FindingView{
		Fingerprint: f.Fingerprint, Code: f.Code, Origin: string(f.Origin), Trust: string(f.Trust),
		Sink: string(f.Sink), Severity: f.Severity, Confidence: f.Confidence,
		Invariant: f.Invariant, Transfer: f.Transfer, Control: f.Control, EventID: f.EventID,
	}
}
