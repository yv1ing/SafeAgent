package repository

import (
	"context"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// SessionCalls returns the calls of one conversation, oldest first. A limit above the cap, or no
// limit, is capped at MaximumSessionCalls.
func (store *Store) SessionCalls(ctx context.Context, projectID, sessionID string, limit int) ([]entity.Call, error) {
	if limit <= 0 || limit > MaximumSessionCalls {
		limit = MaximumSessionCalls
	}
	calls := []entity.Call{}
	err := store.DB.NewSelect().Model(&calls).
		Where("project_id = ? AND session_id = ?", projectID, sessionID).
		Order("created_at", "id").Limit(limit).Scan(ctx)
	return calls, err
}

// MaximumSessionCalls bounds how many calls one conversation view reads.
const MaximumSessionCalls = 1000

// RequestMessages returns, per call, the request event with its payload reduced to the items the
// call added; the rest of the payload (parameters, tool definitions, hits) is not read.
func (store *Store) RequestMessages(ctx context.Context, callIDs []string) ([]entity.Event, error) {
	events := []entity.Event{}
	if len(callIDs) == 0 {
		return events, nil
	}
	err := store.DB.NewRaw(`SELECT call_id, sequence, kind, jsonb_build_object('items', payload->'items') AS payload
		FROM events WHERE kind = 'request' AND call_id IN (?) ORDER BY call_id, sequence`, bun.In(callIDs)).Scan(ctx, &events)
	return events, err
}

// Conversation returns one conversation of the project with its call count and highest severity.
func (store *Store) Conversation(ctx context.Context, projectID, sessionID string) (*entity.Conversation, error) {
	conversation := new(entity.Conversation)
	err := store.DB.NewRaw(`SELECT cv.*, stats.calls, stats.max_severity FROM conversations cv
		CROSS JOIN LATERAL (
			SELECT count(*) AS calls,
				coalesce((array_agg(c.max_severity ORDER BY (CASE c.max_severity WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'info' THEN 1 ELSE 0 END) DESC))[1], '') AS max_severity
			FROM calls c WHERE c.project_id = cv.project_id AND c.session_id = cv.id
		) stats
		WHERE cv.project_id = ? AND cv.id = ?`, projectID, sessionID).Scan(ctx, conversation)
	if err != nil {
		return nil, missing(err)
	}
	return conversation, nil
}
