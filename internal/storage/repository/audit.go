package repository

import (
	"context"
	"time"

	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type AuditScope struct {
	ProjectID string
	CallID    string
	Since     time.Time
	Limit     int
}

type AuditReport struct {
	CallID    string                 `json:"call_id"`
	ProjectID string                 `json:"project_id"`
	State     string                 `json:"state"`
	Report    auditevent.ChainReport `json:"report"`
}

// LoadAuditSecret loads the instance secret without creating or modifying the schema. A stale
// fingerprint is an error, so an offline verification never runs against another build's data.
func (store *Store) LoadAuditSecret(ctx context.Context) error {
	state, err := store.loadState(ctx)
	if err != nil {
		return err
	}
	if len(state.HMACSecret) != 32 {
		return errMissingAuditKey
	}
	var incompatible int
	if err := store.DB.NewRaw(`SELECT count(*) FROM events WHERE schema_version <> ?`, auditevent.SchemaVersion).Scan(ctx, &incompatible); err != nil {
		return err
	}
	if incompatible > 0 {
		return errStaleEvents
	}
	store.Secret = state.HMACSecret
	return nil
}

func (store *Store) VerifyCallAudit(ctx context.Context, projectID, callID string) (auditevent.ChainReport, error) {
	if len(store.Secret) != 32 {
		return auditevent.ChainReport{Status: auditevent.StatusUnavailable, Error: "audit key unavailable"}, nil
	}
	call, err := store.Call(ctx, projectID, callID)
	if err != nil {
		return auditevent.ChainReport{}, err
	}
	rows, err := store.CallEvents(ctx, projectID, callID)
	if err != nil {
		return auditevent.ChainReport{Status: auditevent.StatusBroken, Error: err.Error()}, nil
	}
	events := chainEvents(rows)
	report := auditevent.VerifyChain(events, store.Secret)
	if report.Status != auditevent.StatusVerified {
		return report, nil
	}
	if call.State == "running" {
		report.Status = auditevent.StatusInProgress
		return report, nil
	}
	if report.Events != call.AuditEvents || report.Head != call.AuditHead {
		report.Status = auditevent.StatusIncomplete
		report.Error = "call anchor does not match its event chain"
		return report, nil
	}
	if len(rows) == 0 {
		report.Status = auditevent.StatusIncomplete
		report.Error = "call has no audit events"
		return report, nil
	}
	last := rows[len(rows)-1]
	terminal, err := auditevent.Decode(last.Kind, last.Payload)
	if err != nil || terminal.Terminal == nil {
		report.Status = auditevent.StatusBroken
		report.Error = "call has no terminal event"
		return report, nil
	}
	if terminal.Terminal.State != call.State {
		report.Status = auditevent.StatusBroken
		report.Error = "terminal state does not match the call"
		return report, nil
	}
	return report, nil
}

func (store *Store) VerifyAudit(ctx context.Context, scope AuditScope) ([]AuditReport, error) {
	if len(store.Secret) != 32 {
		return []AuditReport{{Report: auditevent.ChainReport{Status: auditevent.StatusUnavailable, Error: "audit key unavailable"}}}, nil
	}
	limit := scope.Limit
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}
	calls := []entity.Call{}
	query := store.DB.NewSelect().Model(&calls).Order("created_at DESC", "id DESC").Limit(limit)
	if scope.ProjectID != "" {
		query = query.Where("project_id = ?", scope.ProjectID)
	}
	if scope.CallID != "" {
		query = query.Where("id = ?", scope.CallID)
	}
	if !scope.Since.IsZero() {
		query = query.Where("created_at >= ?", scope.Since.UTC())
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	reports := make([]AuditReport, 0, len(calls))
	for index := range calls {
		call := &calls[index]
		report, err := store.VerifyCallAudit(ctx, call.ProjectID, call.ID)
		if err != nil {
			return nil, err
		}
		reports = append(reports, AuditReport{CallID: call.ID, ProjectID: call.ProjectID, State: call.State, Report: report})
	}
	return reports, nil
}

func chainEvents(rows []entity.Event) []auditevent.ChainEvent {
	events := make([]auditevent.ChainEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, auditevent.ChainEvent{
			CallID: row.CallID, Sequence: row.Sequence, ProjectID: row.ProjectID,
			Kind: row.Kind, SchemaVersion: row.SchemaVersion, Payload: row.Payload,
			CreatedAt: row.CreatedAt, PrevHash: row.PrevHash, Hash: row.Hash,
		})
	}
	return events
}
