package diagnosis

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	workerLease        = 2 * time.Minute
	workerHeartbeat    = 30 * time.Second
	analysisTimeout    = 10 * time.Minute
	workerIdle         = 2 * time.Second
	rollupInterval     = time.Minute
	maximumReplayBytes = 64 << 20
)

// RunWorker starts the in-process analysis worker. It is a loop over a PostgreSQL job table rather
// than an external queue: one instance needs no broker, and the outbox written with the call seal
// guarantees no sealed call is left unanalyzed.
func (service *Service) RunWorker(ctx context.Context, concurrency int) {
	if concurrency <= 0 {
		concurrency = 2
	}
	if concurrency > 16 {
		concurrency = 16
	}
	worker := "worker_" + service.ids()
	if recovered, err := service.store.RecoverExpiredJobs(ctx); err != nil {
		service.logger.Error("expired analysis jobs could not be recovered", "error", err)
	} else if recovered > 0 {
		service.logger.Warn("analysis jobs recovered from expired leases", "jobs", recovered)
	}
	if err := service.store.Rollup(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
		service.logger.Error("initial metric rollup failed", "error", err)
	}
	if repaired, err := service.store.RepairAnalysisJobs(ctx); err != nil {
		service.logger.Error("missing analysis jobs could not be repaired", "error", err)
	} else if repaired > 0 {
		service.logger.Warn("analysis jobs repaired", "jobs", repaired)
	}
	slots := make(chan struct{}, concurrency)
	var active sync.WaitGroup
	ticker := time.NewTicker(workerIdle)
	rollupTicker := time.NewTicker(rollupInterval)
	defer ticker.Stop()
	defer rollupTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			active.Wait()
			return
		case <-rollupTicker.C:
			if err := service.store.Rollup(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
				service.logger.Error("metric rollup failed", "error", err)
			}
		case <-ticker.C:
			if recovered, err := service.store.RecoverExpiredJobs(ctx); err == nil && recovered > 0 {
				service.logger.Warn("analysis jobs recovered", "jobs", recovered)
			}
			jobs, err := service.store.ClaimJobs(ctx, worker, concurrency, workerLease)
			if err != nil {
				if ctx.Err() == nil {
					service.logger.Error("analysis jobs could not be claimed", "error", err)
				}
				continue
			}
			for index := range jobs {
				job := jobs[index]
				slots <- struct{}{}
				active.Add(1)
				go func() {
					defer active.Done()
					defer func() { <-slots }()
					service.runJob(ctx, worker, &job)
				}()
			}
		}
	}
}

func (service *Service) runJob(parent context.Context, worker string, job *entity.AnalysisJob) {
	ctx, cancel := context.WithTimeout(parent, analysisTimeout)
	defer cancel()
	stop := make(chan struct{})
	heartbeatDone := make(chan struct{})
	lostLease := make(chan error, 1)
	go func() {
		defer close(heartbeatDone)
		heartbeat := time.NewTicker(workerHeartbeat)
		defer heartbeat.Stop()
		for {
			select {
			case <-stop:
				return
			case <-heartbeat.C:
				err := service.store.HeartbeatJob(context.WithoutCancel(ctx), job.ID, worker, workerLease)
				if err == nil {
					continue
				}
				if !errors.Is(err, repository.ErrLeaseLost) {
					service.logger.Error("analysis job heartbeat failed", "job", job.ID, "error", err)
					continue
				}
				lostLease <- err
				cancel()
				return
			}
		}
	}()
	err := service.handle(ctx, job)
	close(stop)
	<-heartbeatDone
	select {
	case leaseErr := <-lostLease:
		service.logger.Error("analysis job stopped after losing its lease", "job", job.ID, "error", leaseErr)
		return
	default:
	}
	if err != nil {
		service.logger.Error("analysis job failed", "job", job.ID, "kind", job.Kind, "error", err)
		if markErr := service.store.FailJob(context.WithoutCancel(ctx), job.ID, worker, err.Error()); markErr != nil {
			service.logger.Error("analysis job failure could not be recorded", "job", job.ID, "error", markErr)
			if errors.Is(markErr, repository.ErrLeaseLost) {
				return
			}
		}
		if job.Attempts >= job.MaxAttempts {
			if batchID, callID := batchScope(job); batchID != "" {
				_ = service.store.MarkBatchCall(context.WithoutCancel(ctx), batchID, callID, true)
			}
		}
		return
	}
	if err := service.store.CompleteJob(ctx, job.ID, worker); err != nil {
		service.logger.Error("analysis job completion could not be recorded", "job", job.ID, "error", err)
	}
}

type analyzeScope struct {
	CallID  string `json:"call_id"`
	BatchID string `json:"batch_id,omitempty"`
}

func batchScope(job *entity.AnalysisJob) (string, string) {
	var scope analyzeScope
	if err := json.Unmarshal(job.Scope, &scope); err != nil {
		return "", ""
	}
	return scope.BatchID, scope.CallID
}

func (service *Service) handle(ctx context.Context, job *entity.AnalysisJob) error {
	switch job.Kind {
	case "analyze_call":
		var scope analyzeScope
		if err := json.Unmarshal(job.Scope, &scope); err != nil || scope.CallID == "" {
			return errors.New("analysis job has no call id")
		}
		if _, err := service.analyzeCall(ctx, job.ProjectID, scope.CallID, scope.BatchID); err != nil {
			return err
		}
		if scope.BatchID != "" {
			return service.store.MarkBatchCall(context.WithoutCancel(ctx), scope.BatchID, scope.CallID, false)
		}
		return nil
	default:
		return errors.New("unknown analysis job kind " + job.Kind)
	}
}

// ReplayScope is the hard-capped subject of a re-analysis batch.
type ReplayScope struct {
	Kind       string    `json:"kind"`
	CallID     string    `json:"call_id,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	AgentKeyID string    `json:"agent_key_id,omitempty"`
	From       time.Time `json:"from,omitempty"`
	To         time.Time `json:"to,omitempty"`
	Limit      int       `json:"limit,omitempty"`
}

// Replay creates a batch over a bounded set of calls and queues one analysis job per call. A batch
// only groups work and reports progress; the analysis itself is per call.
func (service *Service) Replay(ctx context.Context, actor access.Actor, projectID string, scope ReplayScope) (*entity.AnalysisBatch, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if scope.Kind == "" {
		scope.Kind = "call"
	}
	limit := scope.Limit
	if limit <= 0 || limit > ScopeLimitCalls {
		limit = ScopeLimitCalls
	}
	calls, err := service.store.ScopeCalls(ctx, projectID, scope.Kind, scope.CallID, scope.SessionID, scope.AgentKeyID, scope.From, scope.To, limit)
	if err != nil {
		return nil, err
	}
	if len(calls) == 0 {
		return nil, repository.ErrNotFound
	}
	if len(calls) > limit {
		return nil, ErrScopeTooLarge
	}
	ids := make([]string, 0, len(calls))
	for _, call := range calls {
		ids = append(ids, call.ID)
	}
	size, err := service.store.ScopeBytes(ctx, projectID, ids)
	if err != nil {
		return nil, err
	}
	if size > maximumReplayBytes {
		return nil, ErrScopeTooLarge
	}
	batch := &entity.AnalysisBatch{
		ID: "batch_" + secure.Random(12), ProjectID: projectID, Kind: scope.Kind,
		State: "running", Total: len(calls), CreatedAt: time.Now().UTC(),
	}
	jobs := make([]*entity.AnalysisJob, 0, len(calls))
	for _, call := range calls {
		jobScope, _ := json.Marshal(analyzeScope{CallID: call.ID, BatchID: batch.ID})
		jobs = append(jobs, &entity.AnalysisJob{
			ID: "job_" + secure.Random(12), ProjectID: projectID, Kind: "analyze_call",
			Scope: jobScope, State: "pending", MaxAttempts: 5, Priority: 5,
			RunAfter: time.Now().UTC(), DedupeKey: "batch:" + batch.ID + ":call:" + call.ID,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}
	if err := service.store.CreateBatchWithJobs(ctx, batch, jobs); err != nil {
		return nil, err
	}
	return batch, nil
}
