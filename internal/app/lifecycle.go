package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/provider/builtin"
	"github.com/yv1ing/SafeAgent/internal/platform/config"
	"github.com/yv1ing/SafeAgent/internal/policy"
	"github.com/yv1ing/SafeAgent/internal/service/control"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// Run serves until ctx ends or the instance lease is lost. Shutdown stops accepting and drains
// in-flight calls before the lease is released. A call that is still running when the drain period
// ends is marked interrupted by the next start.
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
	if err := control.SeedProviders(ctx, database, builtin.All()); err != nil {
		return fmt.Errorf("seed the built-in providers: %w", err)
	}
	providers, err := control.NewRegistry(ctx, database)
	if err != nil {
		return fmt.Errorf("load the provider catalog: %w", err)
	}
	if health, healthErr := control.Health(ctx, database, providers); healthErr != nil {
		return fmt.Errorf("evaluate the configuration: %w", healthErr)
	} else {
		for _, warning := range health.Warnings {
			slog.Warn("configuration is degraded", "code", warning.Code, "subject", warning.Subject, "detail", warning.Message)
		}
	}

	server := NewServer(configuration, database, providers, frontend)
	if err := validatePolicies(ctx, database); err != nil {
		return err
	}
	workers, stop := context.WithCancel(ctx)
	defer stop()
	var background sync.WaitGroup
	background.Add(2)
	go func() { defer background.Done(); monitorLease(workers, lease, stop) }()
	go func() { defer background.Done(); maintain(workers, database, configuration.OperationLogDays) }()

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
	prune := time.NewTicker(time.Minute)
	defer prune.Stop()
	rollup := time.NewTicker(5 * time.Minute)
	defer rollup.Stop()
	pruneNow, rollupNow := true, true
	for {
		if pruneNow {
			if err := database.Prune(ctx, operationLogDays); err != nil && ctx.Err() == nil {
				slog.Error("retention failed", "error_class", "database")
			}
			pruneNow = false
		}
		if rollupNow {
			if err := database.Rollup(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
				slog.Error("traffic rollup failed", "error_class", "database")
			}
			rollupNow = false
		}
		select {
		case <-ctx.Done():
			return
		case <-prune.C:
			pruneNow = true
		case <-rollup.C:
			rollupNow = true
		}
	}
}

func validatePolicies(ctx context.Context, database *repository.Store) error {
	policies, err := database.ActivePolicies(ctx)
	if err != nil {
		return err
	}
	for _, version := range policies {
		var document policy.Document
		if err := json.Unmarshal(version.Document, &document); err != nil {
			return fmt.Errorf("project %s policy %d is unreadable: %w", version.ProjectID, version.Version, err)
		}
		if _, err := policy.Compile(document); err != nil {
			return fmt.Errorf("project %s policy %d does not compile: %w", version.ProjectID, version.Version, err)
		}
	}
	return nil
}
