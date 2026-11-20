//go:build integration

package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// open connects to SAFEAGENT_TEST_DATABASE. Every test drops and recreates the public schema, so
// the variable must name a database reserved for tests.
func open(t *testing.T) *repository.Store {
	t.Helper()
	dsn := os.Getenv("SAFEAGENT_TEST_DATABASE")
	if dsn == "" {
		t.Skip("SAFEAGENT_TEST_DATABASE is not set")
	}
	database := repository.Open(dsn)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func reset(t *testing.T, database *repository.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := database.DB.ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
}

func prepared(t *testing.T) *repository.Store {
	t.Helper()
	database := open(t)
	reset(t, database)
	if err := database.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return database
}

type fixture struct {
	project *entity.Project
	key     *entity.AgentKey
}

func seed(t *testing.T, database *repository.Store, retentionDays int) fixture {
	t.Helper()
	ctx := context.Background()
	project, err := database.CreateProject(ctx, &entity.Project{Name: "p", RetentionDays: retentionDays}, nil, nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	key, err := database.CreateAgentKey(ctx, project.ID, "key", "secret", 0, 0, 0, "agent")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	return fixture{project: project, key: key}
}

func (seeded fixture) call(id, session string, at time.Time) *entity.Call {
	return &entity.Call{ID: id, ProjectID: seeded.project.ID, AgentKeyID: seeded.key.ID, State: "running", SessionID: session, CreatedAt: at}
}
