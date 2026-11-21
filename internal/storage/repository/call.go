package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// MaximumChainLength bounds how many ancestors a transcript walk follows.
const MaximumChainLength = 2000

// CreateCall inserts the call and registers it with its conversation; the call row must exist
// before any event that references it is queued.
func (store *Store) CreateCall(ctx context.Context, call *entity.Call) error {
	if call.AnalysisState == "" {
		call.AnalysisState = "pending"
	}
	if call.Losses == nil {
		call.Losses = []entity.Loss{}
	}
	if call.Repairs == nil {
		call.Repairs = []entity.Repair{}
	}
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		if _, err := transaction.NewInsert().Model(call).Exec(ctx); err != nil {
			return err
		}
		conversation := &entity.Conversation{ID: call.SessionID, ProjectID: call.ProjectID, FirstCallID: call.ID, LastCallID: call.ID, CreatedAt: call.CreatedAt, LastAt: call.CreatedAt}
		_, err := transaction.NewInsert().Model(conversation).
			On("CONFLICT (id) DO UPDATE").
			Set("last_call_id = CASE WHEN EXCLUDED.last_at >= cv.last_at THEN EXCLUDED.last_call_id ELSE cv.last_call_id END").
			Set("last_at = GREATEST(cv.last_at, EXCLUDED.last_at)").
			Exec(ctx)
		return err
	})
}

// SealCall moves a call to a terminal state together with every event queued for it and the tool
// call ids it issued.
func (store *Store) SealCall(ctx context.Context, call *entity.Call, state, code string, toolCallIDs []string, analysis entity.SealAnalysis) error {
	call.State = state
	call.ErrorCode = code
	terminated := time.Now().UTC()
	call.TerminatedAt = &terminated
	return store.events.Seal(ctx, call, toolCallIDs, analysis)
}

// FailCall ends a call that is still running without its events, after its seal failed.
func (store *Store) FailCall(ctx context.Context, callID, code string) error {
	_, err := store.DB.NewUpdate().Model((*entity.Call)(nil)).
		Set("state = 'failed'").Set("error_code = ?", code).Set("terminated_at = ?", time.Now().UTC()).
		Where("id = ? AND state = 'running'", callID).Exec(ctx)
	return err
}

// AbandonCall drops the events queued for a call that could not be sealed.
func (store *Store) AbandonCall(callID string) { store.events.Abandon(callID) }

type CallFilter struct {
	State      string
	SessionID  string
	AgentKeyID string
	Model      string
	Minimum    string
	Before     time.Time
	BeforeID   string
	Limit      int
}

func pageLimit(limit int) int {
	if limit <= 0 || limit > 200 {
		return 50
	}
	return limit
}

func (store *Store) Calls(ctx context.Context, projectID string, filter CallFilter) ([]entity.Call, error) {
	calls := []entity.Call{}
	query := store.DB.NewSelect().Model(&calls).Where("project_id = ?", projectID)
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
		query = query.Where("public_model = ?", filter.Model)
	}
	if filter.Minimum != "" {
		query = query.Where("(CASE max_severity WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'info' THEN 1 ELSE 0 END) >= (CASE ? WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'info' THEN 1 ELSE 0 END)", filter.Minimum)
	}
	if !filter.Before.IsZero() {
		query = query.Where("(created_at, id) < (?, ?)", filter.Before, filter.BeforeID)
	}
	if err := query.Order("created_at DESC", "id DESC").Limit(pageLimit(filter.Limit)).Scan(ctx); err != nil {
		return nil, err
	}
	return calls, nil
}

func (store *Store) Call(ctx context.Context, projectID, callID string) (*entity.Call, error) {
	call := new(entity.Call)
	if err := store.DB.NewSelect().Model(call).Where("id = ? AND project_id = ?", callID, projectID).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return call, nil
}

// CallChain returns the call and its ancestors, oldest first. A walk that ends at a call whose
// parent no longer exists (pruned) is reported through the first element's ParentCallID.
func (store *Store) CallChain(ctx context.Context, callID string) ([]entity.Call, error) {
	calls := []entity.Call{}
	err := store.DB.NewRaw(`SELECT c.* FROM calls c JOIN (
			WITH RECURSIVE chain(id, parent_call_id, depth) AS (
				SELECT id, parent_call_id, 1 FROM calls WHERE id = ?
				UNION ALL
				SELECT c.id, c.parent_call_id, chain.depth + 1 FROM calls c JOIN chain ON c.id = chain.parent_call_id WHERE chain.depth < ?
			) SELECT id, depth FROM chain
		) link ON link.id = c.id ORDER BY link.depth DESC`, callID, MaximumChainLength).Scan(ctx, &calls)
	if err != nil {
		return nil, err
	}
	if len(calls) == 0 {
		return nil, ErrNotFound
	}
	return calls, nil
}

// DeleteResponse soft-deletes a stored response: retrieve and previous_response_id both stop
// working while the audit record stays.
func (store *Store) DeleteResponse(ctx context.Context, agentKeyID, responseID string) error {
	result, err := store.DB.NewUpdate().Model((*entity.Call)(nil)).
		Set("response_deleted_at = ?", time.Now().UTC()).
		Where("agent_key_id = ? AND response_id = ? AND response_deleted_at IS NULL", agentKeyID, responseID).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkCallCancelled records that a client canceled a running call.
func (store *Store) MarkCallCancelled(ctx context.Context, callID string) {
	_, _ = store.DB.NewUpdate().Model((*entity.Call)(nil)).
		Set("cancelled_at = ?", time.Now().UTC()).
		Where("id = ? AND state = 'running'", callID).Exec(ctx)
}

// Conversations lists the newest conversations of a project, keyed on (last_at, id).
func (store *Store) Conversations(ctx context.Context, projectID string, before time.Time, beforeID string, limit int) ([]entity.Conversation, error) {
	conversations := []entity.Conversation{}
	query := `SELECT cv.*, stats.calls, stats.max_severity FROM conversations cv
		CROSS JOIN LATERAL (
			SELECT count(*) AS calls,
				coalesce((array_agg(c.max_severity ORDER BY (CASE c.max_severity WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'info' THEN 1 ELSE 0 END) DESC))[1], '') AS max_severity
			FROM calls c WHERE c.project_id = cv.project_id AND c.session_id = cv.id
		) stats
		WHERE cv.project_id = ?`
	arguments := []any{projectID}
	if !before.IsZero() {
		query += ` AND (cv.last_at, cv.id) < (?, ?)`
		arguments = append(arguments, before, beforeID)
	}
	query += ` ORDER BY cv.last_at DESC, cv.id DESC LIMIT ?`
	arguments = append(arguments, pageLimit(limit))
	if err := store.DB.NewRaw(query, arguments...).Scan(ctx, &conversations); err != nil {
		return nil, err
	}
	return conversations, nil
}

func (store *Store) InterruptRunning(ctx context.Context) (int, error) {
	result, err := store.DB.NewUpdate().Model((*entity.Call)(nil)).
		Set("state = 'interrupted'").Set("error_code = 'process_restarted'").Set("terminated_at = ?", time.Now().UTC()).
		Where("state = 'running'").Exec(ctx)
	if err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	return int(affected), nil
}

func missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
