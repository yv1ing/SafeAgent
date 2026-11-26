package console

import (
	"context"
	"slices"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	diagnosis "github.com/yv1ing/SafeAgent/internal/service/diagnosis"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

var (
	CallStates     = []string{"running", "completed", "failed", "cancelled", "interrupted"}
	CallSeverities = []string{analysis.SeverityInfo, analysis.SeverityMedium, analysis.SeverityHigh}
)

type CallService struct {
	store    *repository.Store
	analyzer *diagnosis.Service
}

func NewCallService(store *repository.Store, analyzer *diagnosis.Service) *CallService {
	return &CallService{store: store, analyzer: analyzer}
}

// CallFilter is the query of the call listing. Lane, operation, purpose and outcome are validated
// against internal/classify; the auxiliary lane is hidden unless the caller asks for it explicitly.
type CallFilter struct {
	State            string
	MinSeverity      string
	Model            string
	AgentKeyID       string
	SessionID        string
	Lane             string
	Operation        string
	Purpose          string
	Outcome          string
	IncludeAuxiliary bool
}

func (filter CallFilter) validate() error {
	if filter.State != "" && !slices.Contains(CallStates, filter.State) {
		return access.Invalid("invalid_state")
	}
	if filter.MinSeverity != "" && !slices.Contains(CallSeverities, filter.MinSeverity) {
		return access.Invalid("invalid_severity")
	}
	if filter.Lane != "" && !classify.Lane(filter.Lane).Valid() {
		return access.Invalid("invalid_lane")
	}
	if filter.Operation != "" && !classify.Operation(filter.Operation).Valid() {
		return access.Invalid("invalid_operation")
	}
	if filter.Purpose != "" && !classify.Purpose(filter.Purpose).Valid() {
		return access.Invalid("invalid_purpose")
	}
	if filter.Outcome != "" && !classify.Outcome(filter.Outcome).Valid() {
		return access.Invalid("invalid_outcome")
	}
	return nil
}

func (filter CallFilter) repositoryFilter(cursor Cursor) repository.CallFilter {
	converted := repository.CallFilter{
		State: filter.State, SessionID: filter.SessionID, AgentKeyID: filter.AgentKeyID, Model: filter.Model,
		Minimum: filter.MinSeverity, IncludeAuxiliary: filter.IncludeAuxiliary && filter.Lane == "",
		Before: cursor.Before, BeforeID: cursor.BeforeID, Limit: cursor.Fetch(),
	}
	if filter.Lane != "" {
		converted.Lanes = []classify.Lane{classify.Lane(filter.Lane)}
	}
	if filter.Operation != "" {
		converted.Operations = []classify.Operation{classify.Operation(filter.Operation)}
	}
	if filter.Purpose != "" {
		converted.Purposes = []classify.Purpose{classify.Purpose(filter.Purpose)}
	}
	if filter.Outcome != "" {
		converted.Outcomes = []classify.Outcome{classify.Outcome(filter.Outcome)}
	}
	return converted
}

func (service *CallService) List(ctx context.Context, actor access.Actor, projectID string, filter CallFilter, cursor Cursor) (Page[entity.Call], error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return Page[entity.Call]{}, err
	}
	if err := filter.validate(); err != nil {
		return Page[entity.Call]{}, err
	}
	if filter.SessionID != "" {
		if _, err := service.store.Conversation(ctx, projectID, filter.SessionID); err != nil {
			return Page[entity.Call]{}, access.Translate(err)
		}
	}
	rows, err := service.store.Calls(ctx, projectID, filter.repositoryFilter(cursor))
	if err != nil {
		return Page[entity.Call]{}, err
	}
	return Paginate(rows, cursor, func(call entity.Call) Position {
		return Position{At: call.CreatedAt, ID: call.ID}
	}), nil
}

// Transcript is the rebuilt conversation of one call, including its ancestors. Findings and
// redaction spans already carry the identity of the call that stored each item.
type Transcript struct {
	Trace     *observation.Trace
	Truncated bool
}

type CallDetail struct {
	Call       *entity.Call
	Transcript Transcript
	Analyses   []*diagnosis.AnalysisView
	Decisions  []entity.Decision
	Attempts   []CallAttempt
	Events     []entity.Event
	Timeline   []auditevent.Entry
	Integrity  auditevent.ChainReport
	Hits       []RedactionValue
}

// Detail assembles the replay of one call: the conversation up to and including it, every analysis
// of the call, the online decisions, the upstream attempts, the call's own redaction values and the
// raw events.
func (service *CallService) Detail(ctx context.Context, actor access.Actor, projectID, callID string) (*CallDetail, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	call, err := service.store.Call(ctx, projectID, callID)
	if err != nil {
		return nil, access.Translate(err)
	}
	detail := &CallDetail{
		Call: call, Analyses: []*diagnosis.AnalysisView{}, Decisions: []entity.Decision{}, Hits: []RedactionValue{},
		Transcript: Transcript{Trace: &observation.Trace{ID: call.ID, Principal: observation.Principal{ProjectID: projectID, AgentKeyID: call.AgentKeyID}}},
	}
	events, err := service.store.CallEvents(ctx, projectID, call.ID)
	if err != nil {
		return nil, err
	}
	records := make([]auditevent.Record, 0, len(events))
	for _, event := range events {
		records = append(records, auditevent.Record{
			CallID: event.CallID, Sequence: event.Sequence, ProjectID: event.ProjectID,
			Kind: event.Kind, SchemaVersion: event.SchemaVersion, Payload: event.Payload,
			CreatedAt: event.CreatedAt, PrevHash: event.PrevHash, Hash: event.Hash,
		})
	}
	if detail.Timeline, err = auditevent.BuildTimeline(records); err != nil {
		return nil, err
	}
	detail.Attempts, _ = BuildAttempts(call, detail.Timeline)
	// A running or interrupted call may not have a sealed trace yet; the detail must still show its
	// attempts and timeline. Any other state has all of its events, so a rebuild failure is a defect
	// that must surface rather than be hidden.
	if rebuild, rebuildErr := service.analyzer.BuildTrace(ctx, call); rebuildErr == nil {
		detail.Transcript = Transcript{Trace: rebuild.Trace, Truncated: rebuild.Truncated}
		detail.Hits = CallRedactions(call, rebuild.Hits)
	} else if call.State != "running" && call.State != "interrupted" {
		return nil, rebuildErr
	}
	analyses, err := service.analyzer.AnalysesOfCall(ctx, actor, projectID, call.ID)
	if err != nil {
		return nil, err
	}
	detail.Analyses = analyses
	decisions, err := service.store.DecisionsOfCall(ctx, call.ID)
	if err != nil {
		return nil, err
	}
	detail.Decisions = decisions
	detail.Integrity, err = service.store.VerifyCallAudit(ctx, projectID, call.ID)
	if err != nil {
		detail.Integrity = auditevent.ChainReport{Status: auditevent.StatusUnavailable, Error: "verification unavailable"}
	}
	// Raw events can approach the stream limit; only administrators receive the payloads.
	if actor.Administrator {
		detail.Events = events
	}
	return detail, nil
}
