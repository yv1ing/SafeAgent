package console

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/observation"
	diagnosis "github.com/yv1ing/SafeAgent/internal/service/diagnosis"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

var (
	CallStates     = []string{"running", "completed", "failed", "blocked", "cancelled", "interrupted", "rejected"}
	CallSeverities = []string{analysis.SeverityInfo, analysis.SeverityMedium, analysis.SeverityHigh}
)

type CallService struct {
	store    *repository.Store
	analyzer *diagnosis.Service
}

func NewCallService(store *repository.Store, analyzer *diagnosis.Service) *CallService {
	return &CallService{store: store, analyzer: analyzer}
}

type CallFilter struct {
	State       string
	MinSeverity string
	Model       string
	AgentKeyID  string
	SessionID   string
}

func (filter CallFilter) validate() error {
	if filter.State != "" && !slices.Contains(CallStates, filter.State) {
		return Invalid("invalid_state")
	}
	if filter.MinSeverity != "" && !slices.Contains(CallSeverities, filter.MinSeverity) {
		return Invalid("invalid_severity")
	}
	return nil
}

func (service *CallService) List(ctx context.Context, actor Actor, projectID string, filter CallFilter, cursor Cursor) (Page[entity.Call], error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return Page[entity.Call]{}, err
	}
	if err := filter.validate(); err != nil {
		return Page[entity.Call]{}, err
	}
	if filter.SessionID != "" {
		if _, err := service.store.Conversation(ctx, projectID, filter.SessionID); err != nil {
			return Page[entity.Call]{}, Translate(err)
		}
	}
	rows, err := service.store.Calls(ctx, projectID, repository.CallFilter{
		State: filter.State, SessionID: filter.SessionID, AgentKeyID: filter.AgentKeyID, Model: filter.Model,
		Minimum: filter.MinSeverity, Before: cursor.Before, BeforeID: cursor.BeforeID, Limit: cursor.Fetch(),
	})
	if err != nil {
		return Page[entity.Call]{}, err
	}
	return Paginate(rows, cursor, func(call entity.Call) Position {
		return Position{At: call.CreatedAt, ID: call.ID}
	}), nil
}

type CallAttempt struct {
	ChannelID        string
	ChannelName      string
	UpstreamModel    string
	KeyID            string
	StartOffset      int64
	EndOffset        int64
	FirstTokenOffset int64
	Result           string
	Code             string
	Class            string
}

type CallDetail struct {
	Call       *entity.Call
	Transcript Transcript
	Trace      *observation.Trace
	Analyses   []*diagnosis.AnalysisView
	Decisions  []entity.Decision
	Attempts   []CallAttempt
	Events     []entity.Event
}

// CallDetail assembles the replay of one call: the conversation up to and including it, every analysis
// of the call, the online decisions, the upstream attempts and the raw events.
func (service *CallService) Detail(ctx context.Context, actor Actor, projectID, callID string) (*CallDetail, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	call, err := service.store.Call(ctx, projectID, callID)
	if err != nil {
		return nil, Translate(err)
	}
	transcript, err := service.ConversationTrace(ctx, projectID, call.ID)
	if err != nil {
		return nil, Translate(err)
	}
	detail := &CallDetail{Call: call, Transcript: transcript, Trace: transcript.Trace, Analyses: []*diagnosis.AnalysisView{}, Decisions: []entity.Decision{}}
	analyses, err := service.analyzer.AnalysesOfCall(ctx, projectID, call.ID)
	if err != nil {
		return nil, err
	}
	detail.Analyses = analyses
	decisions, err := service.store.DecisionsOfCall(ctx, call.ID)
	if err != nil {
		return nil, err
	}
	detail.Decisions = decisions
	// Raw events can approach the stream limit. Members receive attempts built from the small
	// attempt events, while the full payload set is loaded only for administrators.
	if actor.Administrator {
		events, err := service.store.CallEvents(ctx, projectID, call.ID)
		if err != nil {
			return nil, err
		}
		detail.Events = events
		detail.Attempts = Attempts(call, events)
		return detail, nil
	}
	attemptEvents, err := service.store.EventsOfKinds(ctx, []string{call.ID},
		auditevent.KindAttempt, auditevent.KindAttemptFailed, auditevent.KindFirstToken)
	if err != nil {
		return nil, err
	}
	detail.Attempts = Attempts(call, attemptEvents)
	return detail, nil
}

type attemptPayload struct {
	ChannelID     string `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	UpstreamModel string `json:"upstream_model"`
	KeyID         string `json:"key_id"`
	Code          string `json:"code"`
	Class         string `json:"class"`
	Milliseconds  int64  `json:"milliseconds"`
}

// Attempts rebuilds the upstream attempt waterfall from the attempt events of a call.
func Attempts(call *entity.Call, events []entity.Event) []CallAttempt {
	attempts := []CallAttempt{}
	open := false
	offset := func(at time.Time) int64 { return max(at.Sub(call.CreatedAt).Milliseconds(), 0) }
	for _, event := range events {
		var payload attemptPayload
		switch event.Kind {
		case auditevent.KindAttempt, auditevent.KindAttemptFailed, auditevent.KindFirstToken:
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				continue
			}
		default:
			continue
		}
		switch event.Kind {
		case auditevent.KindAttempt:
			attempts = append(attempts, CallAttempt{ChannelID: payload.ChannelID, ChannelName: payload.ChannelName, UpstreamModel: payload.UpstreamModel, KeyID: payload.KeyID, StartOffset: offset(event.CreatedAt), Result: "running"})
			open = true
		case auditevent.KindFirstToken:
			if open {
				attempts[len(attempts)-1].FirstTokenOffset = payload.Milliseconds
			}
		case auditevent.KindAttemptFailed:
			if !open {
				attempts = append(attempts, CallAttempt{ChannelID: payload.ChannelID, KeyID: payload.KeyID, StartOffset: offset(event.CreatedAt)})
			}
			last := &attempts[len(attempts)-1]
			last.Result, last.Code, last.Class, last.EndOffset = "failed", payload.Code, payload.Class, offset(event.CreatedAt)
			open = false
		}
	}
	if open {
		last := &attempts[len(attempts)-1]
		last.Result = outcome(call.State)
		last.EndOffset = int64(call.DurationMilliseconds)
		if call.TerminatedAt != nil {
			last.EndOffset = offset(*call.TerminatedAt)
		}
	}
	return attempts
}

func outcome(state string) string {
	if state == "completed" {
		return "succeeded"
	}
	return state
}
