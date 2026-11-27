package console

import (
	"context"
	"errors"
	"time"

	"github.com/yv1ing/SafeAgent/internal/evidence"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
	"github.com/yv1ing/SafeAgent/internal/threat"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type TrajectoryService struct {
	store *repository.Store
}

func NewTrajectoryService(store *repository.Store) *TrajectoryService {
	return &TrajectoryService{store: store}
}

type TrajectoryFilter struct {
	State       string
	AgentKeyID  string
	SessionID   string
	MinSeverity string
	Before      time.Time
	BeforeID    string
	Limit       int
}

func (service *TrajectoryService) List(ctx context.Context, actor access.Actor, projectID string, filter TrajectoryFilter) ([]entity.TrajectorySummary, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if filter.State != "" && filter.State != "open" && filter.State != "ended" && filter.State != "interrupted" {
		return nil, access.Invalid("invalid_state")
	}
	if filter.MinSeverity != "" && threat.SeverityRank(filter.MinSeverity) == 0 {
		return nil, access.Invalid("invalid_severity")
	}
	return service.store.Trajectories(ctx, projectID, repository.TrajectoryFilter{
		State: filter.State, AgentKeyID: filter.AgentKeyID, SessionID: filter.SessionID,
		MinSeverity: filter.MinSeverity, Before: filter.Before, BeforeID: filter.BeforeID, Limit: filter.Limit,
	})
}

type TrajectoryDetail struct {
	Summary   *entity.TrajectorySummary
	State     *state.State
	Verdict   threat.Verdict
	Calls     []entity.Call
	Events    []trajectory.Event
	Findings  []entity.Finding
	Segments  []entity.Segment
	Edges     []trajectory.Edge
	Integrity trajectory.ChainReport
}

func (service *TrajectoryService) Get(ctx context.Context, actor access.Actor, projectID, trajectoryID string) (*TrajectoryDetail, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	summary, err := service.store.Trajectory(ctx, projectID, trajectoryID)
	if err != nil {
		return nil, access.Translate(err)
	}
	current, err := service.store.TrajectoryState(ctx, projectID, trajectoryID)
	if err != nil {
		return nil, access.Translate(err)
	}
	calls, err := service.store.CallsOfTrajectory(ctx, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	events, err := service.store.TrajectoryEvents(ctx, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	findings, err := service.store.FindingsOfTrajectory(ctx, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	segments, err := service.store.SegmentsOfTrajectory(ctx, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	edges, err := service.store.EdgesOfTrajectory(ctx, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	integrity, err := service.store.VerifyTrajectory(ctx, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	return &TrajectoryDetail{
		Summary: summary, State: current, Verdict: deriveVerdict(findings, current),
		Calls: calls, Events: events, Findings: findings, Segments: segments, Edges: edges, Integrity: integrity,
	}, nil
}

func deriveVerdict(findings []entity.Finding, current *state.State) threat.Verdict {
	values := make([]threat.Finding, 0, len(findings))
	for _, finding := range findings {
		values = append(values, threat.Finding{Code: finding.Code, Severity: finding.Severity})
	}
	coverage := threat.Coverage{Complete: current.Coverage+1e-9 >= current.CoverageThreshold}
	return threat.DeriveVerdict(values, coverage, nil, threat.ModelDisabled)
}

func (service *TrajectoryService) Timeline(ctx context.Context, actor access.Actor, projectID, trajectoryID string) ([]trajectory.Event, error) {
	detail, err := service.Get(ctx, actor, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	return detail.Events, nil
}

func (service *TrajectoryService) Coverage(ctx context.Context, actor access.Actor, projectID, trajectoryID string) (*state.State, error) {
	detail, err := service.Get(ctx, actor, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	return detail.State, nil
}

// Why returns the causal edges that lead to an event, walking backwards over the derived graph.
func (service *TrajectoryService) Why(ctx context.Context, actor access.Actor, projectID, trajectoryID, eventID string) ([]trajectory.Edge, error) {
	detail, err := service.Get(ctx, actor, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	found := false
	anchor := eventID
	for _, event := range detail.Events {
		if event.ID == eventID {
			found = true
			if event.ActionID != "" {
				anchor = event.ActionID
			}
			break
		}
	}
	if !found {
		return nil, access.Translate(repository.ErrNotFound)
	}
	result := []trajectory.Edge{}
	seen := map[string]bool{}
	var walk func(string)
	walk = func(target string) {
		if seen[target] {
			return
		}
		seen[target] = true
		for _, edge := range detail.Edges {
			if edge.To == target {
				result = append(result, edge)
				walk(edge.From)
				continue
			}
			if edge.From == target && (edge.Relation == trajectory.RelationAuthorizedBy || edge.Relation == trajectory.RelationExecutedBy) {
				result = append(result, edge)
				walk(edge.To)
			}
		}
	}
	walk(anchor)
	return result, nil
}

// Blast returns the actions and effects reachable from a segment.
func (service *TrajectoryService) Blast(ctx context.Context, actor access.Actor, projectID, trajectoryID, segmentID string) ([]trajectory.Edge, error) {
	detail, err := service.Get(ctx, actor, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	result := []trajectory.Edge{}
	seen := map[string]bool{}
	var walk func(string)
	walk = func(source string) {
		if seen[source] {
			return
		}
		seen[source] = true
		for _, edge := range detail.Edges {
			if edge.From == source {
				result = append(result, edge)
				walk(edge.To)
			}
		}
	}
	walk(segmentID)
	return result, nil
}

func (service *TrajectoryService) Segment(ctx context.Context, actor access.Actor, projectID, trajectoryID, segmentID string) (*entity.Segment, []byte, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, nil, err
	}
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, nil, err
	}
	return service.store.Segment(ctx, projectID, trajectoryID, segmentID)
}

func (service *TrajectoryService) Bundle(ctx context.Context, actor access.Actor, projectID, trajectoryID string) (evidence.Bundle, error) {
	if err := actor.RequireAdmin(); err != nil {
		return evidence.Bundle{}, err
	}
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return evidence.Bundle{}, err
	}
	return service.store.EvidenceBundle(ctx, projectID, trajectoryID)
}

func (service *TrajectoryService) Approve(ctx context.Context, actor access.Actor, projectID, trajectoryID, approvalID string) error {
	if err := actor.RequireAdmin(); err != nil {
		return err
	}
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return err
	}
	current, err := service.store.TrajectoryState(ctx, projectID, trajectoryID)
	if err != nil {
		return access.Translate(err)
	}
	approval, found := current.Approval(approvalID)
	if !found {
		return access.Translate(repository.ErrNotFound)
	}
	if approval.State != "pending" {
		return access.Conflict("approval_not_pending")
	}
	if !approval.ExpiresAt.IsZero() && !approval.ExpiresAt.After(time.Now().UTC()) {
		return access.Conflict("approval_expired")
	}
	payload := trajectory.ApprovalGranted{
		ApprovalID: approvalID, Approver: actor.UserID, Scope: approval.Scope,
		Nonce: trajectory.NewID("nonce"), SingleUse: true, ExpiresAt: approval.ExpiresAt,
		InvocationHash: approval.InvocationHash,
	}
	if err := service.store.AppendApproval(ctx, projectID, trajectoryID, actor.UserID, string(trajectory.KindApprovalGranted), payload); err != nil {
		if errors.Is(err, repository.ErrApprovalNotPending) {
			return access.Conflict("approval_not_pending")
		}
		return err
	}
	return nil
}

func (service *TrajectoryService) Deny(ctx context.Context, actor access.Actor, projectID, trajectoryID, approvalID, reason string) error {
	if err := actor.RequireAdmin(); err != nil {
		return err
	}
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return err
	}
	current, err := service.store.TrajectoryState(ctx, projectID, trajectoryID)
	if err != nil {
		return access.Translate(err)
	}
	approval, found := current.Approval(approvalID)
	if !found {
		return access.Translate(repository.ErrNotFound)
	}
	if approval.State != "pending" {
		return access.Conflict("approval_not_pending")
	}
	if err := service.store.AppendApproval(ctx, projectID, trajectoryID, actor.UserID, string(trajectory.KindApprovalDenied), trajectory.ApprovalDenied{ApprovalID: approvalID, Reason: reason}); err != nil {
		if errors.Is(err, repository.ErrApprovalNotPending) {
			return access.Conflict("approval_not_pending")
		}
		return err
	}
	return nil
}

func (service *TrajectoryService) Rebuild(ctx context.Context, actor access.Actor, projectID, trajectoryID string) error {
	if err := actor.RequireAdmin(); err != nil {
		return err
	}
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return err
	}
	return service.store.RebuildTrajectory(ctx, projectID, trajectoryID)
}
