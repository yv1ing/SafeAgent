package repository

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

//go:embed schema.sql
var schemaFiles embed.FS

// Baseline returns the exact DDL this build expects. The console's `schema` command prints it.
func (store *Store) Baseline() (string, error) {
	payload, err := schemaFiles.ReadFile("schema.sql")
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func schemaFingerprint(baseline string) string {
	return secure.Digest(strings.ReplaceAll(baseline, "\r\n", "\n"))
}

// Initialize creates the baseline on an empty database and records its fingerprint. The instance
// key is supplied by the operator and is deliberately not stored here. schema.sql is idempotent, so
// a partially created database is completed rather than guessed at.
func (store *Store) Initialize(ctx context.Context) error {
	baseline, err := store.Baseline()
	if err != nil {
		return err
	}
	var existing int
	if err := store.DB.NewRaw(`SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE' AND table_name <> 'system_state'`).Scan(ctx, &existing); err != nil {
		return err
	}
	var recorded int
	if err := store.DB.NewRaw(`SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'system_state'`).Scan(ctx, &recorded); err != nil {
		return err
	}
	if existing > 0 && recorded == 0 {
		return errors.New("the database has tables but no schema fingerprint; recreate the database")
	}
	if existing > 0 {
		var anchored int
		if err := store.DB.NewRaw(`SELECT count(*) FROM system_state WHERE id = 1`).Scan(ctx, &anchored); err != nil {
			return err
		}
		if anchored == 0 {
			return errors.New("the database has tables but no schema fingerprint; recreate the database")
		}
	}
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		if _, err := transaction.ExecContext(ctx, baseline); err != nil {
			return err
		}
		_, err := transaction.NewInsert().Model(&entity.SystemState{ID: 1, SchemaFingerprint: schemaFingerprint(baseline)}).
			On("CONFLICT (id) DO NOTHING").Exec(ctx)
		return err
	})
}

// Prepare creates the schema on an empty database, then verifies that the stored schema matches this
// build. A mismatch is fatal: this project intentionally has no migration path, and running new code
// against an old schema would corrupt the audit trail.
func (store *Store) Prepare(ctx context.Context) error {
	if err := store.Initialize(ctx); err != nil {
		return err
	}
	return store.VerifySchema(ctx)
}

var errStaleEvents = errors.New("the database contains events with a different schema version; recreate the database")

// VerifySchema checks the stored DDL fingerprint and every stored event's schema version without
// creating or modifying anything. It is used at startup and by the offline audit command.
func (store *Store) VerifySchema(ctx context.Context) error {
	if _, err := store.loadState(ctx); err != nil {
		return err
	}
	var incompatible int
	if err := store.DB.NewRaw(`SELECT count(*) FROM events WHERE schema_version <> ?`, auditevent.SchemaVersion).Scan(ctx, &incompatible); err != nil {
		return err
	}
	if incompatible > 0 {
		return errStaleEvents
	}
	return nil
}

// loadState reads the schema anchor without creating or modifying anything.
func (store *Store) loadState(ctx context.Context) (*entity.SystemState, error) {
	baseline, err := store.Baseline()
	if err != nil {
		return nil, err
	}
	expected := schemaFingerprint(baseline)
	state := new(entity.SystemState)
	if err := store.DB.NewSelect().Model(state).Where("id = 1").Scan(ctx); err != nil {
		return nil, errors.New("the schema state is unreadable; recreate the database")
	}
	if state.SchemaFingerprint != expected {
		return nil, fmt.Errorf("the database schema does not match this build (stored %s, expected %s); recreate the database", state.SchemaFingerprint, expected)
	}
	return state, nil
}
