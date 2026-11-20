package repository

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// EnqueueJob inserts a job unless one with the same dedupe key already exists. The dedupe key is
// what makes the seal-time outbox and a later repair pass idempotent.
func (store *Store) EnqueueJob(ctx context.Context, job *entity.AnalysisJob) error {
	if job.ID == "" {
		job.ID = "job_" + secure.Random(12)
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 5
	}
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	if job.State == "" {
		job.State = "pending"
	}
	_, err := store.DB.NewInsert().Model(job).On("CONFLICT (dedupe_key) DO NOTHING").Exec(ctx)
	return err
}

// CreateBatchWithJobs writes a batch and all of its jobs in one transaction, so a replay cannot
// leave a running batch with fewer jobs than its total.
func (store *Store) CreateBatchWithJobs(ctx context.Context, batch *entity.AnalysisBatch, jobs []*entity.AnalysisJob) error {
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		if _, err := transaction.NewInsert().Model(batch).Exec(ctx); err != nil {
			return err
		}
		if len(jobs) == 0 {
			return nil
		}
		_, err := transaction.NewInsert().Model(&jobs).Exec(ctx)
		return err
	})
}

// ClaimJobs leases up to limit pending jobs. SKIP LOCKED lets several workers (or several restarted
// processes) share the table without double-processing.
func (store *Store) ClaimJobs(ctx context.Context, worker string, limit int, lease time.Duration) ([]entity.AnalysisJob, error) {
	if limit <= 0 || limit > 32 {
		limit = 8
	}
	now := time.Now().UTC()
	expires := now.Add(lease)
	jobs := []entity.AnalysisJob{}
	// The CTE form is clearer than a separate SELECT ... FOR UPDATE SKIP LOCKED followed by UPDATE,
	// and keeps the claim atomic.
	err := store.DB.NewRaw(`WITH claimed AS (
			SELECT id FROM analysis_jobs
			WHERE state = 'pending' AND run_after <= ? AND attempts < max_attempts
			ORDER BY priority DESC, run_after, created_at
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		UPDATE analysis_jobs j SET state = 'running', locked_by = ?, lease_expires_at = ?,
			attempts = attempts + 1, updated_at = ?
		FROM claimed WHERE j.id = claimed.id
		RETURNING j.*`, now, limit, worker, expires, now).Scan(ctx, &jobs)
	return jobs, err
}

// Heartbeat extends a lease while a long replay is still making progress.
func (store *Store) HeartbeatJob(ctx context.Context, id, worker string, lease time.Duration) error {
	_, err := store.DB.NewUpdate().Model((*entity.AnalysisJob)(nil)).
		Set("lease_expires_at = ?", time.Now().UTC().Add(lease)).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ? AND locked_by = ? AND state = 'running'", id, worker).Exec(ctx)
	return err
}

func (store *Store) CompleteJob(ctx context.Context, id, worker string) error {
	now := time.Now().UTC()
	result, err := store.DB.NewUpdate().Model((*entity.AnalysisJob)(nil)).
		Set("state = 'done'").Set("lease_expires_at = NULL").Set("updated_at = ?", now).
		Where("id = ? AND locked_by = ? AND state = 'running'", id, worker).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// FailJob retries with exponential backoff until max_attempts, then leaves the job failed so the
// console can show it. Failures are never silently dropped.
func (store *Store) FailJob(ctx context.Context, id, worker, message string) error {
	now := time.Now().UTC()
	// Decide and write in one statement so a lease reclaimed between a SELECT and an UPDATE cannot
	// let a stale worker requeue a job that another worker now owns.
	result, err := store.DB.NewRaw(`UPDATE analysis_jobs SET
			state = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
			last_error = ?, lease_expires_at = NULL, locked_by = '', updated_at = ?,
			run_after = CASE WHEN attempts >= max_attempts THEN ? ELSE ? + make_interval(secs => ((1 << LEAST(attempts, 8)) * 30)::int) END
		WHERE id = ? AND locked_by = ? AND state = 'running'`, message, now, now, now, id, worker).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// RecoverExpiredJobs returns jobs whose worker disappeared to the pending pool. A job that has used
// its attempts becomes failed instead, and a failed batch member is counted so the batch can close.
func (store *Store) RecoverExpiredJobs(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	var recovered int
	err := store.DB.NewRaw(`WITH expired AS (
			UPDATE analysis_jobs SET
				state = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
				locked_by = '', lease_expires_at = NULL, updated_at = ?,
				last_error = CASE WHEN attempts >= max_attempts THEN 'lease expired' ELSE last_error END
			WHERE state = 'running' AND lease_expires_at < ?
			RETURNING id, state, scope
		), failed_batches AS (
			SELECT scope->>'batch_id' AS batch_id, count(*) AS count
			FROM expired
			WHERE state = 'failed' AND COALESCE(scope->>'batch_id', '') <> ''
			GROUP BY 1
		), updated_batches AS (
			UPDATE analysis_batches b SET
				failed = b.failed + f.count,
				state = CASE WHEN b.completed + b.failed + f.count >= b.total THEN 'completed' ELSE b.state END,
				finished_at = CASE WHEN b.completed + b.failed + f.count >= b.total THEN ? ELSE b.finished_at END
			FROM failed_batches f WHERE b.id = f.batch_id
			RETURNING b.id
		)
		SELECT count(*) FROM expired`, now, now, now).Scan(ctx, &recovered)
	return recovered, err
}

// RepairAnalysisJobs re-enqueues sealed calls that never produced an asynchronous run: a crash
// between seal and enqueue, or an old process that stopped before its worker ran.
func (store *Store) RepairAnalysisJobs(ctx context.Context) (int, error) {
	result, err := store.DB.NewRaw(`INSERT INTO analysis_jobs (id, project_id, kind, scope, state,
			attempts, max_attempts, priority, run_after, locked_by, last_error, dedupe_key, created_at, updated_at)
		SELECT 'job_' || substr(md5(random()::text || c.id), 1, 20), c.project_id, 'analyze_call',
			jsonb_build_object('kind', 'call', 'project_id', c.project_id, 'call_id', c.id),
			'pending', 0, 5, 10, now(), '', '', 'call:' || c.id || ':async', now(), now()
		FROM calls c
		WHERE c.analysis_state = 'pending' AND c.state <> 'running'
			AND NOT EXISTS (SELECT 1 FROM analyses a WHERE a.call_id = c.id)
			AND NOT EXISTS (SELECT 1 FROM analysis_jobs j
				WHERE j.kind = 'analyze_call' AND j.scope->>'call_id' = c.id
					AND j.state IN ('pending', 'running'))
		ON CONFLICT (dedupe_key) DO NOTHING`).Exec(ctx)
	if err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	return int(affected), nil
}
