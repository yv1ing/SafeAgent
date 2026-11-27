package console

import (
	"context"
	"slices"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
	"github.com/yv1ing/SafeAgent/internal/threat"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

var (
	CallStates     = []string{"running", "completed", "failed", "cancelled", "interrupted"}
	CallSeverities = []string{threat.SeverityInfo, threat.SeverityMedium, threat.SeverityHigh}
)

type CallService struct {
	store *repository.Store
}

func NewCallService(store *repository.Store) *CallService { return &CallService{store: store} }

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
	rows, err := service.store.Calls(ctx, projectID, filter.repositoryFilter(cursor))
	if err != nil {
		return Page[entity.Call]{}, err
	}
	return Paginate(rows, cursor, func(call entity.Call) Position {
		return Position{At: call.CreatedAt, ID: call.ID}
	}), nil
}

type CallDetail struct {
	Call       *entity.Call
	Trajectory *entity.TrajectorySummary
	Events     []trajectory.Event
	Findings   []entity.Finding
	Integrity  trajectory.ChainReport
}

func (service *CallService) Detail(ctx context.Context, actor access.Actor, projectID, callID string) (*CallDetail, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	call, err := service.store.Call(ctx, projectID, callID)
	if err != nil {
		return nil, access.Translate(err)
	}
	detail := &CallDetail{Call: call}
	if call.TrajectoryID != "" {
		summary, err := service.store.Trajectory(ctx, projectID, call.TrajectoryID)
		if err != nil {
			return nil, access.Translate(err)
		}
		detail.Trajectory = summary
		events, err := service.store.TrajectoryEvents(ctx, projectID, call.TrajectoryID)
		if err != nil {
			return nil, err
		}
		detail.Events = make([]trajectory.Event, 0, len(events))
		for _, event := range events {
			if event.CallID == callID {
				detail.Events = append(detail.Events, event)
			}
		}
		if detail.Integrity, err = service.store.VerifyTrajectory(ctx, projectID, call.TrajectoryID); err != nil {
			return nil, err
		}
	}
	findings, err := service.store.FindingsOfCalls(ctx, projectID, []string{callID})
	if err != nil {
		return nil, err
	}
	detail.Findings = findings
	return detail, nil
}

type CallAttempt struct {
	Index            int
	ChannelID        string
	ChannelName      string
	ModelID          string
	UpstreamModel    string
	Format           string
	Result           string
	Code             string
	DurationMillis   int
	FirstTokenMillis int
}

func Attempts(events []trajectory.Event) []CallAttempt {
	attempts := []CallAttempt{}
	current := map[string]int{}
	for _, event := range events {
		switch event.Kind {
		case trajectory.KindActionProposed:
			payload, err := trajectory.DecodePayload[trajectory.ActionProposed](&event)
			if err != nil || payload.ActionType != trajectory.ActionProviderAttempt {
				continue
			}
			current[payload.ActionID] = len(attempts)
			attempts = append(attempts, CallAttempt{
				Index: len(attempts), ChannelID: payload.ChannelID, ChannelName: payload.ChannelName,
				ModelID: payload.ModelID, UpstreamModel: payload.UpstreamModel, Format: payload.Format, Result: "running",
			})
		case trajectory.KindActionExecuted:
			payload, err := trajectory.DecodePayload[trajectory.ActionExecuted](&event)
			if err != nil || payload.ActionType != trajectory.ActionProviderAttempt {
				continue
			}
			index, found := current[payload.ActionID]
			if !found {
				continue
			}
			attempts[index].Result = "succeeded"
			attempts[index].Code = payload.ErrorCode
			if payload.ErrorCode != "" {
				attempts[index].Result = "failed"
			}
			attempts[index].DurationMillis = payload.DurationMS
			attempts[index].FirstTokenMillis = payload.FirstTokenMS
		}
	}
	return attempts
}
