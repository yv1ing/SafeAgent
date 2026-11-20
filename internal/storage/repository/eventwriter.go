package repository

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

const (
	batchInterval = 200 * time.Millisecond
	batchSize     = 500
	queueLimit    = 20_000
	appendTimeout = 2 * time.Second
	flushTimeout  = 15 * time.Second
)

var (
	ErrEventQueueFull = errors.New("the event queue is full")
	ErrWriterClosed   = errors.New("the event writer is closed")
)

// EventWriter batches audit events off the request path. Events of one call reach the database
// either through the flusher or, at Seal, inside the transaction that terminates the call.
type EventWriter struct {
	store    *Store
	mu       sync.Mutex
	changed  chan struct{}
	pending  []entity.Event
	inflight map[string]int
	failed   map[string]error
	stopping bool
	done     chan struct{}
}

func newEventWriter(store *Store) *EventWriter {
	writer := &EventWriter{store: store, changed: make(chan struct{}), inflight: map[string]int{}, failed: map[string]error{}, done: make(chan struct{})}
	go writer.run()
	return writer
}

// signal wakes everyone waiting for queue space or for in-flight batches; callers hold mu.
func (writer *EventWriter) signal() {
	close(writer.changed)
	writer.changed = make(chan struct{})
}

func (writer *EventWriter) Append(event entity.Event) error {
	event.Payload = escapeNUL(event.Payload)
	timeout := time.NewTimer(appendTimeout)
	defer timeout.Stop()
	for {
		writer.mu.Lock()
		if failure, found := writer.failed[event.CallID]; found {
			writer.mu.Unlock()
			return failure
		}
		if writer.stopping {
			writer.mu.Unlock()
			return ErrWriterClosed
		}
		if len(writer.pending) < queueLimit {
			writer.pending = append(writer.pending, event)
			writer.mu.Unlock()
			return nil
		}
		changed := writer.changed
		writer.mu.Unlock()
		select {
		case <-changed:
		case <-timeout.C:
			return ErrEventQueueFull
		}
	}
}

func (writer *EventWriter) run() {
	defer close(writer.done)
	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()
	for {
		<-ticker.C
		writer.mu.Lock()
		stopping := writer.stopping
		batch := writer.pending
		writer.pending = nil
		for _, event := range batch {
			writer.inflight[event.CallID]++
		}
		writer.signal()
		writer.mu.Unlock()
		writer.flush(batch)
		if stopping {
			return
		}
	}
}

func (writer *EventWriter) flush(batch []entity.Event) {
	for start := 0; start < len(batch); start += batchSize {
		end := min(start+batchSize, len(batch))
		writer.insert(batch[start:end])
	}
}

// insert writes a batch. When the batch fails, one bad row must not take the rest with it, so the
// rows are retried one by one, all within one more flushTimeout; rows the retry cannot reach in
// time fail their calls instead of stalling the flusher.
func (writer *EventWriter) insert(batch []entity.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	_, err := writer.store.DB.NewInsert().Model(&batch).Exec(ctx)
	cancel()
	if err == nil {
		for index := range batch {
			writer.settle(batch[index].CallID, nil)
		}
		return
	}
	retry, cancelRetry := context.WithTimeout(context.Background(), flushTimeout)
	defer cancelRetry()
	for index := range batch {
		failure := retry.Err()
		if failure == nil {
			_, failure = writer.store.DB.NewInsert().Model(&batch[index]).Exec(retry)
		}
		writer.settle(batch[index].CallID, failure)
	}
}

func (writer *EventWriter) settle(callID string, failure error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if failure != nil {
		if _, found := writer.failed[callID]; !found {
			writer.failed[callID] = failure
		}
	}
	if writer.inflight[callID]--; writer.inflight[callID] <= 0 {
		delete(writer.inflight, callID)
	}
	writer.signal()
}

// Seal terminates a call: it waits for the call's in-flight batches, then writes the remaining
// events, the final call row and its tool call index in one transaction. When the transaction fails the events go back
// to the queue so the caller may retry or abandon the call.
func (writer *EventWriter) Seal(ctx context.Context, call *entity.Call, toolCallIDs []string, analysis entity.SealAnalysis) error {
	owned, err := writer.claim(ctx, call.ID)
	if err != nil {
		return err
	}
	err = writer.store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		if len(owned) > 0 {
			if _, err := transaction.NewInsert().Model(&owned).Exec(ctx); err != nil {
				return err
			}
		}
		if _, err := transaction.NewUpdate().Model(call).WherePK().Exec(ctx); err != nil {
			return err
		}
		if err := registerTools(ctx, transaction, call, toolCallIDs); err != nil {
			return err
		}
		return writeAnalysis(ctx, transaction, analysis)
	})
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if err != nil {
		writer.pending = append(owned, writer.pending...)
		writer.signal()
		return err
	}
	delete(writer.failed, call.ID)
	return nil
}

func (writer *EventWriter) claim(ctx context.Context, callID string) ([]entity.Event, error) {
	for {
		writer.mu.Lock()
		if failure := writer.failed[callID]; failure != nil {
			delete(writer.failed, callID)
			writer.mu.Unlock()
			return nil, failure
		}
		if writer.inflight[callID] == 0 {
			var owned []entity.Event
			kept := writer.pending[:0]
			for _, event := range writer.pending {
				if event.CallID == callID {
					owned = append(owned, event)
				} else {
					kept = append(kept, event)
				}
			}
			writer.pending = kept
			writer.signal()
			writer.mu.Unlock()
			return owned, nil
		}
		changed := writer.changed
		writer.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Abandon forgets the queued events of a call that could not be sealed.
func (writer *EventWriter) Abandon(callID string) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	kept := writer.pending[:0]
	for _, event := range writer.pending {
		if event.CallID != callID {
			kept = append(kept, event)
		}
	}
	writer.pending = kept
	delete(writer.failed, callID)
	writer.signal()
}

// Flush blocks until everything queued so far has been written.
func (writer *EventWriter) Flush() {
	for {
		writer.mu.Lock()
		idle := len(writer.pending) == 0 && len(writer.inflight) == 0
		changed := writer.changed
		writer.mu.Unlock()
		if idle {
			return
		}
		select {
		case <-changed:
		case <-time.After(batchInterval):
		}
	}
}

// Close flushes what is queued and stops the flusher.
func (writer *EventWriter) Close() {
	writer.mu.Lock()
	writer.stopping = true
	writer.mu.Unlock()
	<-writer.done
}

// escapeNUL rewrites the JSON escape \u0000, which jsonb rejects. An escaped backslash followed by
// the text u0000 is data, not an escape, hence the parity check.
func escapeNUL(payload []byte) []byte {
	const nul = `\u0000`
	result := payload
	copied := false
	for index := 0; index+len(nul) <= len(payload); index++ {
		if payload[index] != '\\' || string(payload[index:index+len(nul)]) != nul {
			continue
		}
		backslashes := 0
		for before := index - 1; before >= 0 && payload[before] == '\\'; before-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			continue
		}
		if !copied {
			result = append([]byte(nil), payload...)
			copied = true
		}
		copy(result[index:], `\ufffd`)
	}
	return result
}

func registerTools(ctx context.Context, transaction bun.Tx, call *entity.Call, toolCallIDs []string) error {
	if len(toolCallIDs) == 0 {
		return nil
	}
	rows := make([]entity.CallTool, 0, len(toolCallIDs))
	for _, id := range toolCallIDs {
		rows = append(rows, entity.CallTool{ProjectID: call.ProjectID, ToolCallID: id, CallID: call.ID, CreatedAt: call.CreatedAt})
	}
	_, err := transaction.NewInsert().Model(&rows).
		On("CONFLICT (project_id, tool_call_id) DO UPDATE").
		Set("call_id = EXCLUDED.call_id, created_at = EXCLUDED.created_at").Exec(ctx)
	return err
}

// writeAnalysis writes the online decisions and the asynchronous outbox job in the same transaction
// that seals the call. Online analysis results do not exist.
func writeAnalysis(ctx context.Context, transaction bun.Tx, seal entity.SealAnalysis) error {
	if len(seal.Decisions) > 0 {
		if _, err := transaction.NewInsert().Model(&seal.Decisions).On("CONFLICT (id) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
	}
	if seal.Snapshot != nil {
		if _, err := transaction.NewInsert().Model(seal.Snapshot).On("CONFLICT (id) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
	}
	if seal.Job != nil {
		now := time.Now().UTC()
		seal.Job.CreatedAt = now
		seal.Job.UpdatedAt = now
		if seal.Job.State == "" {
			seal.Job.State = "pending"
		}
		if seal.Job.MaxAttempts <= 0 {
			seal.Job.MaxAttempts = 5
		}
		if _, err := transaction.NewInsert().Model(seal.Job).On("CONFLICT (dedupe_key) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
