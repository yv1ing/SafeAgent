package repository

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// ScopeCalls returns the calls covered by a replay scope, oldest first and hard-capped. Auxiliary
// protocol traffic is never replayed or diagnosed.
func (store *Store) ScopeCalls(ctx context.Context, projectID string, scopeKind, callID, sessionID, agentKeyID string, from, to time.Time, limit int) ([]entity.Call, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	calls := []entity.Call{}
	query := store.DB.NewSelect().Model(&calls).Where("project_id = ?", projectID).
		Where("state <> 'running'").Where("lane <> ?", "auxiliary")
	switch scopeKind {
	case "call":
		query = query.Where("id = ?", callID)
	case "session":
		query = query.Where("session_id = ?", sessionID)
	case "agent_key":
		query = query.Where("agent_key_id = ?", agentKeyID)
	case "window":
		query = query.Where("created_at >= ? AND created_at < ?", from, to)
	default:
		return nil, ErrNotFound
	}
	if err := query.Order("created_at", "id").Limit(limit + 1).Scan(ctx); err != nil {
		return nil, err
	}
	return calls, nil
}

// ScopeBytes returns the stored event payload size of a replay scope before any observation is
// built, so an oversized replay is refused instead of exhausting memory halfway through.
func (store *Store) ScopeBytes(ctx context.Context, projectID string, callIDs []string) (int64, error) {
	if len(callIDs) == 0 {
		return 0, nil
	}
	var size int64
	err := store.DB.NewRaw(`SELECT COALESCE(sum(octet_length(payload)), 0)
		FROM events WHERE project_id = ? AND call_id IN (?)`, projectID, bun.In(callIDs)).Scan(ctx, &size)
	return size, err
}
