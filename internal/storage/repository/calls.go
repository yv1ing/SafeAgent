package repository

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/threat"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type CallFilter struct {
	State            string
	SessionID        string
	AgentKeyID       string
	Model            string
	Minimum          string
	IncludeAuxiliary bool
	Lanes            []classify.Lane
	Operations       []classify.Operation
	Purposes         []classify.Purpose
	Outcomes         []classify.Outcome
	Before           time.Time
	BeforeID         string
	Limit            int
}

func (store *Store) Call(ctx context.Context, projectID, callID string) (*entity.Call, error) {
	call := new(entity.Call)
	if err := store.DB.NewSelect().Model(call).Where("project_id = ? AND id = ?", projectID, callID).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return call, nil
}

func (store *Store) Calls(ctx context.Context, projectID string, filter CallFilter) ([]entity.Call, error) {
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	query := store.DB.NewSelect().Model((*entity.Call)(nil)).Where("project_id = ?", projectID)
	if filter.State != "" {
		query = query.Where("state = ?", filter.State)
	}
	if filter.SessionID != "" {
		query = query.Where("session_id = ?", filter.SessionID)
	}
	if filter.AgentKeyID != "" {
		query = query.Where("agent_key_id = ?", filter.AgentKeyID)
	}
	if filter.Model != "" {
		query = query.Where("(public_model = ? OR upstream_model = ?)", filter.Model, filter.Model)
	}
	if filter.Minimum != "" {
		query = query.Where("(CASE max_severity WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'info' THEN 1 ELSE 0 END) >= ?", threat.SeverityRank(filter.Minimum))
	}
	if len(filter.Lanes) > 0 {
		query = query.Where("lane IN (?)", bunIn(filter.Lanes))
	} else if !filter.IncludeAuxiliary {
		query = query.Where("lane <> 'auxiliary'")
	}
	if len(filter.Operations) > 0 {
		query = query.Where("operation IN (?)", bunIn(filter.Operations))
	}
	if len(filter.Purposes) > 0 {
		query = query.Where("purpose IN (?)", bunIn(filter.Purposes))
	}
	if len(filter.Outcomes) > 0 {
		query = query.Where("outcome IN (?)", bunIn(filter.Outcomes))
	}
	if !filter.Before.IsZero() {
		if filter.BeforeID == "" {
			query = query.Where("created_at < ?", filter.Before)
		} else {
			query = query.Where("(created_at, id) < (?, ?)", filter.Before, filter.BeforeID)
		}
	}
	rows := []entity.Call{}
	if err := query.Order("created_at DESC", "id DESC").Limit(filter.Limit).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func bunIn[T ~string](values []T) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func (store *Store) CallByResponseID(ctx context.Context, agentKeyID, responseID string) (*entity.Call, error) {
	call := new(entity.Call)
	if err := store.DB.NewSelect().Model(call).Where("agent_key_id = ? AND response_id = ?", agentKeyID, responseID).Order("created_at DESC").Limit(1).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return call, nil
}

func (store *Store) CallByToolCall(ctx context.Context, projectID, agentKeyID, toolCallID string) (*entity.Call, error) {
	call := new(entity.Call)
	if err := store.DB.NewSelect().Model(call).Where("project_id = ? AND agent_key_id = ? AND tool_call_ids ? ?", projectID, agentKeyID, toolCallID).Order("created_at DESC").Limit(1).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return call, nil
}

func (store *Store) CallByConversationHash(ctx context.Context, agentKeyID, hash string) (*entity.Call, error) {
	call := new(entity.Call)
	if err := store.DB.NewSelect().Model(call).Where("agent_key_id = ? AND conversation_hash = ?", agentKeyID, hash).Order("created_at DESC").Limit(1).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return call, nil
}

func (store *Store) CallChain(ctx context.Context, callID string) ([]entity.Call, error) {
	rows := []entity.Call{}
	if err := store.DB.NewRaw(`WITH RECURSIVE chain AS (
			SELECT * FROM calls WHERE id = ?
			UNION ALL
			SELECT c.* FROM calls c JOIN chain p ON c.id = p.parent_call_id
		) SELECT * FROM chain ORDER BY created_at ASC`, callID).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (store *Store) SessionCalls(ctx context.Context, projectID, sessionID string, limit int) ([]entity.Call, error) {
	if limit <= 0 {
		limit = 200
	}
	rows := []entity.Call{}
	if err := store.DB.NewSelect().Model(&rows).
		Where("project_id = ? AND session_id = ?", projectID, sessionID).
		Order("created_at ASC", "id ASC").Limit(limit).Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

func (store *Store) CallsOfTrajectory(ctx context.Context, projectID, trajectoryID string) ([]entity.Call, error) {
	rows := []entity.Call{}
	if err := store.DB.NewSelect().Model(&rows).
		Where("project_id = ? AND trajectory_id = ?", projectID, trajectoryID).
		Order("call_seq ASC", "created_at ASC").Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

func (store *Store) DeleteResponse(ctx context.Context, agentKeyID, responseID string) error {
	_, err := store.DB.NewUpdate().Model((*entity.Call)(nil)).
		Set("response_deleted_at = ?", time.Now().UTC()).
		Where("agent_key_id = ? AND response_id = ?", agentKeyID, responseID).Exec(ctx)
	return err
}

func (store *Store) MarkCallCancelled(ctx context.Context, callID string) error {
	now := time.Now().UTC()
	_, err := store.DB.NewUpdate().Model((*entity.Call)(nil)).
		Set("state = 'cancelled'").Set("outcome = 'cancelled'").Set("cancelled_at = ?", now).Set("terminated_at = ?", now).
		Where("id = ? AND state = 'running'", callID).Exec(ctx)
	return err
}

func (store *Store) CallEvents(ctx context.Context, projectID, callID string) ([]trajectory.Event, error) {
	rows := []entity.TrajectoryEvent{}
	if err := store.DB.NewSelect().Model(&rows).Where("project_id = ? AND call_id = ?", projectID, callID).Order("seq ASC").Scan(ctx); err != nil {
		return nil, err
	}
	events := make([]trajectory.Event, 0, len(rows))
	for index := range rows {
		event, err := fromEntityEvent(&rows[index])
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (store *Store) EventsOfCalls(ctx context.Context, projectID string, callIDs []string) ([]trajectory.Event, error) {
	if len(callIDs) == 0 {
		return []trajectory.Event{}, nil
	}
	rows := []entity.TrajectoryEvent{}
	if err := store.DB.NewSelect().Model(&rows).Where("project_id = ? AND call_id IN (?)", projectID, bunIn(callIDs)).Order("seq ASC").Scan(ctx); err != nil {
		return nil, err
	}
	events := make([]trajectory.Event, 0, len(rows))
	for index := range rows {
		event, err := fromEntityEvent(&rows[index])
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (store *Store) FindingsOfCalls(ctx context.Context, projectID string, callIDs []string) ([]entity.Finding, error) {
	if len(callIDs) == 0 {
		return []entity.Finding{}, nil
	}
	rows := []entity.Finding{}
	if err := store.DB.NewSelect().Model(&rows).Where("project_id = ? AND call_id IN (?)", projectID, bunIn(callIDs)).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

func (store *Store) HighRiskCalls(ctx context.Context, projectIDs []string, since time.Time, limit int) ([]entity.Call, error) {
	if len(projectIDs) == 0 {
		return []entity.Call{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	rows := []entity.Call{}
	if err := store.DB.NewSelect().Model(&rows).
		Where("project_id IN (?) AND created_at >= ? AND max_severity = 'high'", bunIn(projectIDs), since).
		Order("created_at DESC").Limit(limit).Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

// InterruptRunning closes the trajectories and calls a previous process left running. It appends a
// trajectory.end event, so the interruption is part of the log rather than a hidden column change.
func (store *Store) InterruptRunning(ctx context.Context) (int64, error) {
	rows := []struct {
		TrajectoryID string `bun:"trajectory_id"`
		ProjectID    string `bun:"project_id"`
	}{}
	if err := store.DB.NewRaw(`SELECT DISTINCT trajectory_id, project_id FROM calls WHERE state = 'running'`).Scan(ctx, &rows); err != nil {
		return 0, err
	}
	trajectories := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.TrajectoryID == "" {
			continue
		}
		trajectories = append(trajectories, row.TrajectoryID)
		_, err := store.Append(ctx, trajectory.Batch{
			TrajectoryID: row.TrajectoryID, ProjectID: row.ProjectID,
			Events: []trajectory.Draft{{
				Kind: trajectory.KindTrajectoryEnd, Boundary: trajectory.BoundaryRoot,
				Actor: trajectory.ActorGateway, At: time.Now().UTC(),
				Capture: trajectory.Capture{Recorder: "startup", Placement: "gateway"},
				Payload: trajectory.TrajectoryEnd{Outcome: "interrupted", Reason: "process_interrupted"},
			}},
		})
		if err != nil {
			return 0, err
		}
	}
	if len(trajectories) > 0 {
		if _, err := store.DB.NewUpdate().Model((*entity.TrajectorySummary)(nil)).
			Set("state = 'interrupted'").
			Where("id IN (?) AND state = 'ended'", bun.In(trajectories)).Exec(ctx); err != nil {
			return 0, err
		}
	}
	result, err := store.DB.NewUpdate().Model((*entity.Call)(nil)).
		Set("state = 'interrupted'").Set("outcome = 'cancelled'").Set("terminated_at = now()").
		Where("state = 'running'").Exec(ctx)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
