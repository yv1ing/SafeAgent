package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/config"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// Run serves until ctx ends or the instance lease is lost. Shutdown runs in a fixed order: stop
// accepting and drain in-flight calls, then flush the audit queue, then release the lease. A call
// that is still running when the drain period ends is marked interrupted by the next start.
func Run(ctx context.Context, configuration config.Config, database *repository.Store, frontend fs.FS) error {
	// The advisory lock comes first: two processes must not race to create the schema.
	lease, err := database.AcquireInstance(ctx)
	if err != nil {
		return err
	}
	defer lease.Close()
	if err := database.Prepare(ctx); err != nil {
		return err
	}
	if err := database.ResetReservations(ctx); err != nil {
		return err
	}
	if err := Bootstrap(ctx, database); err != nil {
		return err
	}
	if interrupted, err := database.InterruptRunning(ctx); err != nil {
		return err
	} else if interrupted > 0 {
		slog.Warn("calls left running by a previous process were marked interrupted", "calls", interrupted)
	}

	server, diagnosisService := NewServer(configuration, database, frontend)
	if err := diagnosisService.ValidatePolicies(ctx); err != nil {
		return err
	}
	workers, stop := context.WithCancel(ctx)
	defer stop()
	var background sync.WaitGroup
	background.Add(3)
	go func() { defer background.Done(); monitorLease(workers, lease, stop) }()
	go func() { defer background.Done(); maintain(workers, database, configuration.OperationLogDays) }()
	go func() {
		defer background.Done()
		diagnosisService.RunWorker(workers, configuration.AnalysisConcurrency)
	}()

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-workers.Done()
		grace, cancel := context.WithTimeout(context.Background(), time.Duration(configuration.ShutdownSeconds)*time.Second)
		defer cancel()
		if err := server.Shutdown(grace); err != nil {
			slog.Error("in-flight calls did not finish before the shutdown deadline", "error", err)
			_ = server.Close()
		}
	}()
	slog.Info("SafeAgent listening", "address", configuration.Listen)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		stop()
		background.Wait()
		return fmt.Errorf("serve: %w", err)
	}
	<-drained
	background.Wait()
	database.FlushEvents()
	if ctx.Err() == nil {
		return errors.New("server stopped after losing its database lease")
	}
	return nil
}

type leaseOwner interface {
	Check(context.Context) error
}

func monitorLease(ctx context.Context, owner leaseOwner, stop context.CancelFunc) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := owner.Check(check)
			cancel()
			if err != nil {
				slog.Error("instance lease lost; stopping service")
				stop()
				return
			}
		}
	}
}

func maintain(ctx context.Context, database *repository.Store, operationLogDays int) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := database.Prune(ctx, operationLogDays); err != nil && ctx.Err() == nil {
			slog.Error("maintenance failed", "error_class", "database")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
