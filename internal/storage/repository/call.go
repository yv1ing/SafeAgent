package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// MaximumChainLength bounds how many ancestors a transcript walk follows.
const MaximumChainLength = 2000

// CreateCall inserts the call and, for a conversational lane, registers it with its conversation.
// An auxiliary call never joins a conversation and is never diagnosed: the operation is not part of
// the agent's conversation, even when its transport carries a session header.
func (store *Store) CreateCall(ctx context.Context, call *entity.Call) error {
	if call.Operation == "" {
		call.Operation = classify.Generation
	}
	if call.Purpose == "" {
		call.Purpose = classify.PurposeUnknown
	}
	if call.PurposeSource == "" {
		call.PurposeSource = classify.SourceDefault
	}
	if call.Lane == "" {
		call.Lane = classify.LaneMain
	}
	switch {
	case call.Lane == classify.LaneAuxiliary:
		call.SessionID = ""
		call.AnalysisState = "skipped"
	case call.Lane == classify.LaneSide:
		if call.AnalysisState == "" {
			call.AnalysisState = "skipped"
		}
	case call.AnalysisState == "":
		call.AnalysisState = "pending"
	}
	if call.CreatedAt.IsZero() {
		call.CreatedAt = time.Now().UTC()
	}
	if call.UpdatedAt.IsZero() {
		call.UpdatedAt = call.CreatedAt
	}
	if call.Losses == nil {
		call.Losses = []entity.Loss{}
	}
	if call.Repairs == nil {
		call.Repairs = []entity.Repair{}
	}
	if call.RedactionLabels == nil {
		call.RedactionLabels = []string{}
	}
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		if _, err := transaction.NewInsert().Model(call).Exec(ctx); err != nil {
			return err
		}
		if !call.Lane.Conversational() || call.SessionID == "" {
			return nil
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
// call ids it issued. The seal is the only writer of UpdatedAt, so a call's row always reflects its
// last lifecycle transition.
func (store *Store) SealCall(ctx context.Context, call *entity.Call, state, code string, toolCallIDs []string, analysis entity.SealAnalysis) error {
	call.State = state
	call.ErrorCode = code
	if call.Lane != classify.LaneMain {
		call.AnalysisState = "skipped"
	}
	if call.Lane == classify.LaneAuxiliary {
		call.SessionID = ""
	}
	terminated := time.Now().UTC()
	call.TerminatedAt = &terminated
	call.UpdatedAt = terminated
	return store.events.Seal(ctx, call, toolCallIDs, analysis)
}

// FailCall ends a call that never sealed because its event transaction failed. The row is written
// from the caller's call so the operation, lane and outcome survive the failed seal.
func (store *Store) FailCall(ctx context.Context, call *entity.Call, code string) error {
	result, err := store.DB.NewUpdate().Model((*entity.Call)(nil)).
		Set("state = 'failed'").Set("error_code = ?", code).Set("outcome = ?", call.Outcome).
		Set("terminated_at = ?", time.Now().UTC()).Set("updated_at = ?", time.Now().UTC()).
		Where("id = ? AND state = 'running'", call.ID).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil
	}
	return nil
}

// AbandonCall drops the events queued for a call that could not be sealed.
func (store *Store) AbandonCall(callID string) { store.events.Abandon(callID) }

// CallFilter narrows the console's call listing. Auxiliary calls are hidden by default because they
// are protocol traffic, not agent activity; an explicit lane filter overrides that default.
type CallFilter struct {
	State            string
	SessionID        string
	AgentKeyID       string
	Model            string
	Minimum          string
	Lanes            []classify.Lane
	Operations       []classify.Operation
	Purposes         []classify.Purpose
	Outcomes         []classify.Outcome
	IncludeAuxiliary bool
	Before           time.Time
	BeforeID         string
	Limit            int
}

func pageLimit(limit int) int {
	if limit <= 0 || limit > 200 {
		return 50
	}
	return limit
}

func textValues[Value ~string](values []Value) []string {
	names := make([]string, 0, len(values))
	for _, value := range values {
		names = append(names, string(value))
	}
	return names
}

func (store *Store) Calls(ctx context.Context, projectID string, filter CallFilter) ([]entity.Call, error) {
	calls := []entity.Call{}
	query := store.DB.NewSelect().Model(&calls).Where("project_id = ?", projectID)
	switch {
	case len(filter.Lanes) > 0:
		query = query.Where("lane IN (?)", bun.In(textValues(filter.Lanes)))
	case !filter.IncludeAuxiliary:
		query = query.Where("lane <> ?", classify.LaneAuxiliary)
	}
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
	if len(filter.Operations) > 0 {
		query = query.Where("operation IN (?)", bun.In(textValues(filter.Operations)))
	}
	if len(filter.Purposes) > 0 {
		query = query.Where("purpose IN (?)", bun.In(textValues(filter.Purposes)))
	}
	if len(filter.Outcomes) > 0 {
		query = query.Where("outcome IN (?)", bun.In(textValues(filter.Outcomes)))
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
		Set("updated_at = ?", time.Now().UTC()).
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
	now := time.Now().UTC()
	_, _ = store.DB.NewUpdate().Model((*entity.Call)(nil)).
		Set("cancelled_at = ?", now).Set("updated_at = ?", now).
		Where("id = ? AND state = 'running'", callID).Exec(ctx)
}

// Conversations lists the newest conversations of a project, keyed on (last_at, id). The trailing
// statistics count only conversational lanes.
func (store *Store) Conversations(ctx context.Context, projectID string, before time.Time, beforeID string, limit int) ([]entity.Conversation, error) {
	conversations := []entity.Conversation{}
	query := conversationStatsQuery + ` WHERE cv.project_id = ?`
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

// InterruptRunning terminates the calls a previous process left running. Each call receives a
// signed terminal event before its state changes, so an interrupted call still verifies as a
// complete chain instead of an incomplete one. The anchor columns are written only by the seal, so
// a call interrupted mid-flight has flushed events but an empty anchor; the next sequence and
// previous hash therefore come from the stored chain, not from the call row.
func (store *Store) InterruptRunning(ctx context.Context) (int, error) {
	running := []entity.Call{}
	if err := store.DB.NewSelect().Model(&running).
		Column("id", "project_id").
		Where("state = 'running'").Scan(ctx); err != nil {
		return 0, err
	}
	if len(running) == 0 {
		return 0, nil
	}
	terminated := time.Now().UTC()
	interrupted := 0
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for index := range running {
			call := &running[index]
			var state string
			if err := tx.NewRaw(`SELECT state FROM calls WHERE id = ? FOR UPDATE`, call.ID).Scan(ctx, &state); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
			if state != "running" {
				continue
			}
			var anchor struct {
				Sequence int64  `bun:"sequence"`
				Hash     string `bun:"hash"`
			}
			if err := tx.NewRaw(`SELECT sequence, hash FROM events WHERE call_id = ? ORDER BY sequence DESC LIMIT 1`, call.ID).Scan(ctx, &anchor); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			payload, err := json.Marshal(auditevent.Terminal{State: "interrupted", Code: "process_restarted"})
			if err != nil {
				return err
			}
			event := &auditevent.ChainEvent{
				CallID: call.ID, Sequence: anchor.Sequence + 1, ProjectID: call.ProjectID,
				Kind: auditevent.KindTerminal, SchemaVersion: auditevent.SchemaVersion,
				Payload: payload, CreatedAt: terminated, PrevHash: anchor.Hash,
			}
			if err := auditevent.Sign(event, store.Secret); err != nil {
				return err
			}
			if _, err := tx.NewInsert().Model(&entity.Event{
				CallID: event.CallID, Sequence: event.Sequence, ProjectID: event.ProjectID,
				Kind: event.Kind, SchemaVersion: event.SchemaVersion, Payload: event.Payload,
				CreatedAt: event.CreatedAt, PrevHash: event.PrevHash, Hash: event.Hash,
			}).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewUpdate().Model((*entity.Call)(nil)).
				Set("state = 'interrupted'").Set("outcome = 'cancelled'").
				Set("error_code = 'process_restarted'").
				Set("terminated_at = ?", terminated).Set("updated_at = ?", terminated).
				Set("audit_head = ?", event.Hash).Set("audit_events = ?", event.Sequence).
				Where("id = ?", call.ID).Exec(ctx); err != nil {
				return err
			}
			interrupted++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return interrupted, nil
}

func missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
