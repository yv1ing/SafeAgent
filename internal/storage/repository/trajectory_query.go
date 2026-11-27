package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/evidence"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/threat"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type TrajectoryFilter struct {
	State       string
	AgentKeyID  string
	SessionID   string
	MinSeverity string
	Since       time.Time
	Before      time.Time
	BeforeID    string
	Limit       int
}

func (store *Store) Trajectory(ctx context.Context, projectID, trajectoryID string) (*entity.TrajectorySummary, error) {
	summary := new(entity.TrajectorySummary)
	if err := store.DB.NewSelect().Model(summary).Where("id = ? AND project_id = ?", trajectoryID, projectID).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return summary, nil
}

func (store *Store) Trajectories(ctx context.Context, projectID string, filter TrajectoryFilter) ([]entity.TrajectorySummary, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	query := store.DB.NewSelect().Model((*entity.TrajectorySummary)(nil)).Where("project_id = ?", projectID)
	if filter.State != "" {
		query = query.Where("state = ?", filter.State)
	}
	if filter.AgentKeyID != "" {
		query = query.Where("agent_key_id = ?", filter.AgentKeyID)
	}
	if filter.SessionID != "" {
		query = query.Where("session_id = ?", filter.SessionID)
	}
	if filter.MinSeverity != "" {
		query = query.Where("(CASE max_severity WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'info' THEN 1 ELSE 0 END) >= ?", threat.SeverityRank(filter.MinSeverity))
	}
	if !filter.Since.IsZero() {
		query = query.Where("last_event_at >= ?", filter.Since)
	}
	if !filter.Before.IsZero() {
		if filter.BeforeID == "" {
			query = query.Where("last_event_at < ?", filter.Before)
		} else {
			query = query.Where("(last_event_at, id) < (?, ?)", filter.Before, filter.BeforeID)
		}
	}
	rows := []entity.TrajectorySummary{}
	if err := query.Order("last_event_at DESC", "id DESC").Limit(filter.Limit).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (store *Store) TrajectoryEvents(ctx context.Context, projectID, trajectoryID string) ([]trajectory.Event, error) {
	rows := []entity.TrajectoryEvent{}
	if err := store.DB.NewSelect().Model(&rows).
		Where("trajectory_id = ? AND project_id = ?", trajectoryID, projectID).
		Order("seq ASC").Scan(ctx); err != nil {
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

func (store *Store) TrajectoryState(ctx context.Context, projectID, trajectoryID string) (*state.State, error) {
	row := new(entity.TrajectoryStateRow)
	if err := store.DB.NewSelect().Model(row).Where("trajectory_id = ? AND project_id = ?", trajectoryID, projectID).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	current := state.New(trajectoryID, projectID)
	if err := json.Unmarshal(row.State, current); err != nil {
		return nil, err
	}
	return current, nil
}

func (store *Store) SegmentsOfTrajectory(ctx context.Context, projectID, trajectoryID string) ([]entity.Segment, error) {
	rows := []entity.Segment{}
	if err := store.DB.NewRaw(`SELECT s.* FROM segments s
			WHERE s.trajectory_id = ? AND EXISTS (
				SELECT 1 FROM trajectory_summaries t WHERE t.id = s.trajectory_id AND t.project_id = ?
			) ORDER BY s.created_at ASC`, trajectoryID, projectID).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (store *Store) Segment(ctx context.Context, projectID, trajectoryID, segmentID string) (*entity.Segment, []byte, error) {
	row := new(entity.Segment)
	if err := store.DB.NewRaw(`SELECT s.* FROM segments s
			WHERE s.trajectory_id = ? AND s.id = ? AND EXISTS (
				SELECT 1 FROM trajectory_summaries t WHERE t.id = s.trajectory_id AND t.project_id = ?
			)`, trajectoryID, segmentID, projectID).Scan(ctx, row); err != nil {
		return nil, nil, missing(err)
	}
	content, _, err := store.SegmentContent(ctx, trajectoryID, segmentID)
	if err != nil {
		return nil, nil, err
	}
	return row, content, nil
}

func (store *Store) SegmentContent(ctx context.Context, trajectoryID, segmentID string) ([]byte, string, error) {
	row := new(entity.Segment)
	if err := store.DB.NewSelect().Model(row).Where("trajectory_id = ? AND id = ?", trajectoryID, segmentID).Scan(ctx); err != nil {
		return nil, "", missing(err)
	}
	blob := new(entity.Blob)
	if err := store.DB.NewSelect().Model(blob).Where("hash = ?", row.BytesRef).Scan(ctx); err != nil {
		return nil, "", missing(err)
	}
	content := blob.Bytes
	if blob.Encrypted {
		plain, err := secure.Open(store.Secret, string(blob.Bytes))
		if err != nil {
			return nil, "", err
		}
		content = plain
	}
	return content, row.MediaType, nil
}

func (store *Store) EdgesOfTrajectory(ctx context.Context, projectID, trajectoryID string) ([]trajectory.Edge, error) {
	rows := []entity.Edge{}
	if err := store.DB.NewRaw(`SELECT e.* FROM edges e
			WHERE e.trajectory_id = ? AND EXISTS (
				SELECT 1 FROM trajectory_summaries t WHERE t.id = e.trajectory_id AND t.project_id = ?
			) ORDER BY e.relation ASC`, trajectoryID, projectID).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	edges := make([]trajectory.Edge, 0, len(rows))
	for _, row := range rows {
		edges = append(edges, trajectory.Edge{TrajectoryID: row.TrajectoryID, From: row.From, To: row.To, Relation: row.Relation, EventID: row.EventID, Confidence: row.Confidence})
	}
	return edges, nil
}

func (store *Store) FindingsOfTrajectory(ctx context.Context, projectID, trajectoryID string) ([]entity.Finding, error) {
	rows := []entity.Finding{}
	if err := store.DB.NewSelect().Model(&rows).Where("project_id = ? AND trajectory_id = ?", projectID, trajectoryID).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

func (store *Store) CheckpointsOfTrajectory(ctx context.Context, trajectoryID string) ([]entity.Checkpoint, error) {
	rows := []entity.Checkpoint{}
	if err := store.DB.NewSelect().Model(&rows).Where("trajectory_id = ?", trajectoryID).Order("to_seq ASC").Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

func (store *Store) VerifyTrajectory(ctx context.Context, projectID, trajectoryID string) (trajectory.ChainReport, error) {
	summary, err := store.Trajectory(ctx, projectID, trajectoryID)
	if err != nil {
		return trajectory.ChainReport{Status: trajectory.StatusUnavailable, Error: "trajectory_unavailable"}, err
	}
	events, err := store.TrajectoryEvents(ctx, projectID, trajectoryID)
	if err != nil {
		return trajectory.ChainReport{Status: trajectory.StatusUnavailable, Error: "events_unavailable"}, err
	}
	if len(events) == 0 {
		return trajectory.ChainReport{Status: trajectory.StatusIncomplete, Error: "events_missing"}, nil
	}
	if events[0].Seq != 1 {
		return trajectory.ChainReport{Status: trajectory.StatusIncomplete, Events: int64(len(events)), Error: "pruned_range"}, nil
	}
	checkpoints, err := store.CheckpointsOfTrajectory(ctx, trajectoryID)
	if err != nil || len(checkpoints) == 0 {
		return trajectory.ChainReport{Status: trajectory.StatusIncomplete, Events: int64(len(events)), Error: "checkpoint_missing"}, nil
	}
	last := checkpoints[len(checkpoints)-1]
	checkpoint := evidence.Checkpoint{
		ID: last.ID, TrajectoryID: last.TrajectoryID, FromSeq: last.FromSeq, ToSeq: last.ToSeq,
		MerkleRoot: last.MerkleRoot, Signature: last.Signature, PrevHash: last.PrevHash,
		WitnessRef: last.WitnessRef, CreatedAt: last.CreatedAt,
	}
	verification := evidence.VerifyRange(events, checkpoint, store.Secret)
	if !verification.Valid {
		return trajectory.ChainReport{Status: trajectory.StatusBroken, Events: int64(len(events)), Error: verification.Failures[0]}, nil
	}
	report := trajectory.ChainReport{Status: trajectory.StatusVerified, Events: int64(len(events)), Head: events[len(events)-1].Hash}
	if summary.State == "open" {
		report.Status = trajectory.StatusInProgress
	}
	return report, nil
}

// RebuildTrajectory deletes every projection of a trajectory and replays the log. It is the only
// write path for projections outside Append, and it never touches trajectory_events.
func (store *Store) RebuildTrajectory(ctx context.Context, projectID, trajectoryID string) error {
	events, err := store.TrajectoryEvents(ctx, projectID, trajectoryID)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return ErrTrajectoryNotFound
	}
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*entity.Finding)(nil)).Where("trajectory_id = ?", trajectoryID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*entity.Edge)(nil)).Where("trajectory_id = ?", trajectoryID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*entity.Call)(nil)).Where("trajectory_id = ?", trajectoryID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*entity.TrajectoryStateRow)(nil)).Where("trajectory_id = ?", trajectoryID).Exec(ctx); err != nil {
			return err
		}
		summary := new(entity.TrajectorySummary)
		if err := tx.NewSelect().Model(summary).Where("id = ?", trajectoryID).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		current := state.New(trajectoryID, projectID)
		calls := map[string]*entity.Call{}
		findings := []threat.Finding{}
		for index := range events {
			before := current
			after, err := state.Reduce(current, &events[index])
			if err != nil {
				return err
			}
			current = after
			found := threat.EvaluateInvariants(before, after, &events[index])
			findings = append(findings, found...)
			findings = append(findings, checkedFindings(&events[index])...)
			call, err := applyCallProjection(ctx, tx, calls, summary, events[index], after)
			if err != nil {
				return err
			}
			if call != nil {
				calls[call.ID] = call
			}
			for _, edge := range edgesFor(events[index]) {
				if _, err := tx.NewInsert().Model(&entity.Edge{TrajectoryID: edge.TrajectoryID, From: edge.From, To: edge.To, Relation: edge.Relation, EventID: edge.EventID, Confidence: edge.Confidence}).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
					return err
				}
			}
		}
		if err := saveState(ctx, tx, current); err != nil {
			return err
		}
		if err := saveCalls(ctx, tx, calls); err != nil {
			return err
		}
		if err := saveFindings(ctx, tx, projectID, events, findings); err != nil {
			return err
		}
		if err := refreshCallFindings(ctx, tx, callIDs(calls)); err != nil {
			return err
		}
		return updateSummary(ctx, tx, summary, current)
	})
}

// EvidenceBundle returns a portable bundle for an event range. Segment content is included so a
// verifier can recompute every segment id without the database.
func (store *Store) EvidenceBundle(ctx context.Context, projectID, trajectoryID string) (evidence.Bundle, error) {
	events, err := store.TrajectoryEvents(ctx, projectID, trajectoryID)
	if err != nil {
		return evidence.Bundle{}, err
	}
	if len(events) == 0 {
		return evidence.Bundle{}, ErrTrajectoryNotFound
	}
	segments, err := store.SegmentsOfTrajectory(ctx, projectID, trajectoryID)
	if err != nil {
		return evidence.Bundle{}, err
	}
	bundleSegments := make([]evidence.Segment, 0, len(segments))
	for _, segment := range segments {
		content, _, err := store.SegmentContent(ctx, trajectoryID, segment.ID)
		if err != nil {
			return evidence.Bundle{}, fmt.Errorf("segment %s: %w", segment.ID, err)
		}
		bundleSegments = append(bundleSegments, evidence.Segment{ID: segment.ID, MediaType: segment.MediaType, Content: content, Integrity: segment.Integrity, Confidentiality: segment.Confidentiality})
	}
	edges, err := store.EdgesOfTrajectory(ctx, projectID, trajectoryID)
	if err != nil {
		return evidence.Bundle{}, err
	}
	checkpoints, err := store.CheckpointsOfTrajectory(ctx, trajectoryID)
	if err != nil || len(checkpoints) == 0 {
		return evidence.Bundle{}, errors.New("the trajectory has no checkpoint")
	}
	checkpoint := checkpoints[len(checkpoints)-1]
	current, _ := store.TrajectoryState(ctx, projectID, trajectoryID)
	approvals := []evidence.Approval{}
	if current != nil {
		for _, approval := range current.Approvals {
			approvals = append(approvals, evidence.Approval{ID: approval.ID, InvocationHash: approval.InvocationHash, State: approval.State, ExpiresAt: approval.ExpiresAt, ExecutionCount: approval.ExecutionCount})
		}
	}
	sort.Slice(approvals, func(left, right int) bool { return approvals[left].ID < approvals[right].ID })
	policy := evidence.Policy{}
	if current != nil && current.PolicyVersion > 0 {
		policy.Version, policy.Hash = current.PolicyVersion, current.PolicyHash
		if version, err := store.PolicyVersion(ctx, projectID, current.PolicyVersion); err == nil {
			policy.Rules = version.Document
		}
	}
	return evidence.Build(trajectoryID, events, bundleSegments, edges, current,
		evidence.Checkpoint{ID: checkpoint.ID, TrajectoryID: checkpoint.TrajectoryID, FromSeq: checkpoint.FromSeq, ToSeq: checkpoint.ToSeq, MerkleRoot: checkpoint.MerkleRoot, Signature: checkpoint.Signature, PrevHash: checkpoint.PrevHash, WitnessRef: checkpoint.WitnessRef, CreatedAt: checkpoint.CreatedAt},
		approvals, policy), nil
}

func (store *Store) AppendApproval(ctx context.Context, projectID, trajectoryID, actor, eventKind string, payload any) error {
	kind := trajectory.Kind(eventKind)
	_, err := store.Append(ctx, trajectory.Batch{
		TrajectoryID: trajectoryID, ProjectID: projectID,
		Events: []trajectory.Draft{{
			Kind: kind, Boundary: trajectory.BoundaryAction, Actor: trajectory.ActorHuman,
			Principal: actor, At: time.Now().UTC(), Payload: payload,
			Capture: trajectory.Capture{Recorder: "console", Placement: "console"},
		}},
	})
	return err
}

type TrajectoryAudit struct {
	ProjectID    string
	TrajectoryID string
	Report       trajectory.ChainReport
}

func (store *Store) VerifyTrajectories(ctx context.Context, projectID, trajectoryID string, limit int) ([]TrajectoryAudit, error) {
	summaries := []entity.TrajectorySummary{}
	query := store.DB.NewSelect().Model(&summaries).Order("last_event_at DESC")
	if projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}
	if trajectoryID != "" {
		query = query.Where("id = ?", trajectoryID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	reports := make([]TrajectoryAudit, 0, len(summaries))
	for _, summary := range summaries {
		report, err := store.VerifyTrajectory(ctx, summary.ProjectID, summary.ID)
		if err != nil {
			report = trajectory.ChainReport{Status: trajectory.StatusUnavailable, Error: err.Error()}
		}
		reports = append(reports, TrajectoryAudit{ProjectID: summary.ProjectID, TrajectoryID: summary.ID, Report: report})
	}
	return reports, nil
}
