package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func (store *Store) AppendEvent(event *entity.Event) error {
	return store.events.Append(*event)
}

// CallByResponseID finds the call a client refers to with previous_response_id.
func (store *Store) CallByResponseID(ctx context.Context, agentKeyID, responseID string) (*entity.Call, error) {
	return store.firstCall(ctx, store.DB.NewSelect().Model((*entity.Call)(nil)).
		Where("agent_key_id = ? AND response_id = ?", agentKeyID, responseID).
		Where("response_deleted_at IS NULL"))
}

// CallByToolCall finds the call that issued a tool call, for a request that only carries its result.
func (store *Store) CallByToolCall(ctx context.Context, projectID, agentKeyID, toolCallID string) (*entity.Call, error) {
	return store.firstCall(ctx, store.DB.NewSelect().Model((*entity.Call)(nil)).
		Where("id = (SELECT call_id FROM call_tools WHERE project_id = ? AND tool_call_id = ?)", projectID, toolCallID).
		Where("agent_key_id = ?", agentKeyID))
}

// CallByConversationHash finds the recent call whose finished conversation equals a request's history.
func (store *Store) CallByConversationHash(ctx context.Context, agentKeyID, hash string) (*entity.Call, error) {
	return store.firstCall(ctx, store.DB.NewSelect().Model((*entity.Call)(nil)).
		Where("agent_key_id = ? AND conversation_hash = ?", agentKeyID, hash).
		Where("created_at > ?", time.Now().UTC().AddDate(0, 0, -7)))
}

func (store *Store) firstCall(ctx context.Context, query *bun.SelectQuery) (*entity.Call, error) {
	call := new(entity.Call)
	if err := query.Order("created_at DESC", "id DESC").Limit(1).Scan(ctx, call); err != nil {
		return nil, missing(err)
	}
	return call, nil
}

func (store *Store) CallEvents(ctx context.Context, projectID, callID string) ([]entity.Event, error) {
	events := []entity.Event{}
	err := store.DB.NewSelect().Model(&events).
		Where("call_id = ? AND project_id = ?", callID, projectID).
		Order("sequence").Scan(ctx)
	if err != nil {
		return events, err
	}
	return events, checkEventVersions(events)
}

// EventsOfCalls returns every event of the given calls in one query, ordered for replay. The
// trajectory reader uses this instead of enumerating kinds, so a new event kind needs no reader
// change.
func (store *Store) EventsOfCalls(ctx context.Context, callIDs []string) ([]entity.Event, error) {
	events := []entity.Event{}
	if len(callIDs) == 0 {
		return events, nil
	}
	err := store.DB.NewSelect().Model(&events).
		Where("call_id IN (?)", bun.In(callIDs)).
		Order("call_id", "sequence").Scan(ctx)
	if err != nil {
		return events, err
	}
	return events, checkEventVersions(events)
}

// SessionEventStamp returns, per call, the highest event sequence stored for it. The conversation
// cache key needs the stamp on every read but never the payloads, so this is one aggregate query.
func (store *Store) SessionEventStamp(ctx context.Context, callIDs []string) (map[string]int64, error) {
	states := map[string]int64{}
	if len(callIDs) == 0 {
		return states, nil
	}
	rows := []struct {
		CallID   string `bun:"call_id"`
		Sequence int64  `bun:"sequence"`
	}{}
	if err := store.DB.NewRaw(`SELECT call_id, max(sequence) AS sequence FROM events
		WHERE call_id IN (?) GROUP BY call_id`, bun.In(callIDs)).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		states[row.CallID] = row.Sequence
	}
	return states, nil
}

// EventsOfKinds returns the given kinds for many calls in one query, ordered for replay.
func (store *Store) EventsOfKinds(ctx context.Context, callIDs []string, kinds ...string) ([]entity.Event, error) {
	events := []entity.Event{}
	if len(callIDs) == 0 {
		return events, nil
	}
	err := store.DB.NewSelect().Model(&events).
		Where("call_id IN (?)", bun.In(callIDs)).
		Where("kind IN (?)", bun.In(kinds)).
		Order("call_id", "sequence").Scan(ctx)
	if err != nil {
		return events, err
	}
	return events, checkEventVersions(events)
}

// checkEventVersions refuses a payload written by a different event contract. The schema
// fingerprint covers the tables; this check covers the JSON stored inside them.
func checkEventVersions(events []entity.Event) error {
	for _, event := range events {
		if event.SchemaVersion != auditevent.SchemaVersion {
			return fmt.Errorf("event %s/%d has unsupported schema version %d", event.CallID, event.Sequence, event.SchemaVersion)
		}
	}
	return nil
}
