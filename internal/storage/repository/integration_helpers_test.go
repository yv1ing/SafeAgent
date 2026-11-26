//go:build integration

package repository_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

var testInstanceKey = bytes.Repeat([]byte{7}, 32)

// open connects to SAFEAGENT_TEST_DATABASE. Every test drops and recreates the public schema, so
// the variable must name a database reserved for tests.
func open(t *testing.T) *repository.Store {
	t.Helper()
	dsn := os.Getenv("SAFEAGENT_TEST_DATABASE")
	if dsn == "" {
		t.Fatal("SAFEAGENT_TEST_DATABASE is not set; use make lab-integration")
	}
	database := repository.Open(dsn, testInstanceKey)
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
	return seedWithAuxiliary(t, database, retentionDays, 3)
}

func seedWithAuxiliary(t *testing.T, database *repository.Store, retentionDays, auxiliaryRetentionDays int) fixture {
	t.Helper()
	ctx := context.Background()
	project, err := database.CreateProject(ctx, &entity.Project{
		Name: "p", RetentionDays: retentionDays, AuxiliaryRetentionDays: auxiliaryRetentionDays,
	}, nil, nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	key, err := database.CreateAgentKey(ctx, project.ID, "key", "secret", 0, 0, 0, "agent")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	return fixture{project: project, key: key}
}

// call is one main-lane generation call.
func (seeded fixture) call(id, session string, at time.Time) *entity.Call {
	return &entity.Call{
		ID: id, ProjectID: seeded.project.ID, AgentKeyID: seeded.key.ID,
		ClientFormat: string(model.Chat), Operation: classify.Generation,
		Purpose: classify.PurposeChat, PurposeSource: classify.SourceDefault,
		Lane: classify.LaneMain, State: "running", SessionID: session, CreatedAt: at,
	}
}

// sideCall is one background generation call: it keeps the conversation but is not diagnosed and
// does not contribute to traffic counters.
func (seeded fixture) sideCall(id, session string, at time.Time) *entity.Call {
	call := seeded.call(id, session, at)
	call.Purpose = classify.PurposeCompaction
	call.Lane = classify.LaneSide
	return call
}

// auxiliaryCall is one protocol helper call: no conversation, no diagnosis, no counters.
func (seeded fixture) auxiliaryCall(id string, at time.Time) *entity.Call {
	return &entity.Call{
		ID: id, ProjectID: seeded.project.ID, AgentKeyID: seeded.key.ID,
		ClientFormat: string(model.Chat), Operation: classify.CountTokens,
		Purpose: classify.PurposeCountTokens, PurposeSource: classify.SourceDefault,
		Lane: classify.LaneAuxiliary, State: "running", SessionID: "ignored", CreatedAt: at,
	}
}

// seedProvider inserts one built-in-shaped provider with a Chat endpoint.
func seedProvider(t *testing.T, database *repository.Store, id string) {
	t.Helper()
	spec := provider.ProviderSpec{
		ID: id, Label: id,
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Chat: {
				Format: model.Chat, BaseURL: "https://example.com", Path: "/v1/chat/completions",
				Auth: provider.AuthBearer,
			},
		},
	}
	endpoints, err := provider.MarshalEndpoints(spec)
	if err != nil {
		t.Fatalf("marshal endpoints: %v", err)
	}
	if err := database.SeedProviders(context.Background(), []repository.ProviderSeed{{ID: spec.ID, Label: spec.Label, Endpoints: endpoints}}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
}

// seedChannel creates one enabled channel on a provider.
func seedChannel(t *testing.T, database *repository.Store, providerID, name string) *repository.ChannelRecord {
	t.Helper()
	channel, err := database.CreateChannel(context.Background(), &entity.Channel{
		Name: name, ProviderID: providerID, Enabled: true, Headers: map[string]string{},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return channel
}

// seedModel creates one enabled generation model on a provider.
func seedModel(t *testing.T, database *repository.Store, providerID, upstreamName string) *entity.Model {
	t.Helper()
	created, err := database.CreateModel(context.Background(), &entity.Model{
		ProviderID: providerID, UpstreamName: upstreamName, Kind: entity.ModelGeneration,
		Currency: "USD", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	return created
}

// seedRoute creates one route with a single target and returns it.
func seedRoute(t *testing.T, database *repository.Store, projectID, name, channelID, modelID string) *repository.RouteRecord {
	t.Helper()
	created, err := database.CreateRoute(context.Background(),
		&entity.Route{ProjectID: projectID, Name: name, Enabled: true},
		[]entity.RouteTarget{{ChannelID: channelID, ModelID: modelID, Weight: 1, Enabled: true}}, nil)
	if err != nil {
		t.Fatalf("create route: %v", err)
	}
	return created
}
