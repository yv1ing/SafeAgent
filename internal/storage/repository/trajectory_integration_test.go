//go:build integration

package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

const secret = "0123456789abcdef0123456789abcdef"

func database(t *testing.T) *repository.Store {
	t.Helper()
	dsn := os.Getenv("SAFEAGENT_TEST_DATABASE")
	if dsn == "" {
		t.Skip("SAFEAGENT_TEST_DATABASE is not set")
	}
	store := repository.Open(dsn, []byte(secret))
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.DB.ExecContext(context.Background(), `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := store.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestTrajectoryAppendProjectsStateCallsAndCheckpoints(t *testing.T) {
	ctx := context.Background()
	store := database(t)
	project, err := store.CreateProject(ctx, &entity.Project{Name: "integration", RetentionDays: 30, CoverageThreshold: 0.9, CreatedAt: time.Now().UTC()}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.CreateAgentKey(ctx, project.ID, "agent", "agent-secret", 0, 0, 0, "agent")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := store.EnsureTrajectory(ctx, project.ID, key.ID, "session-1", "test", 0.9)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("hello integration")
	segmentID := trajectory.ContentHash(content)
	now := time.Now().UTC()
	callID := "call_integration"
	_, err = store.Append(ctx, trajectory.Batch{
		TrajectoryID: summary.ID, ProjectID: project.ID,
		Segments: []trajectory.Segment{{ID: segmentID, TrajectoryID: summary.ID, MediaType: "text/plain", BytesRef: segmentID, Size: int64(len(content)), Integrity: trajectory.IntegrityTrusted, Confidentiality: trajectory.ConfidentialityPrivate, SourceEvent: "evt_start", CreatedAt: now}},
		Blobs:    []trajectory.Blob{{Hash: segmentID, Bytes: content, MediaType: "text/plain", CreatedAt: now}},
		Events: []trajectory.Draft{
			{ID: "evt_start", Kind: trajectory.KindTrajectoryStart, Boundary: trajectory.BoundaryRoot, Actor: trajectory.ActorGateway, Principal: key.ID, At: now, Payload: trajectory.Start{ProjectID: project.ID, AgentKeyID: key.ID, SessionID: "session-1", Lane: classify.LaneMain, CoverageThreshold: 0.9, StartedAt: now}},
			{ID: "evt_action", Kind: trajectory.KindActionProposed, Boundary: trajectory.BoundaryAction, Actor: trajectory.ActorGateway, Principal: key.ID, CallID: callID, ActionID: "act_1", At: now, Inputs: []trajectory.Ref{{Segment: segmentID, Role: "prompt"}}, Payload: trajectory.ActionProposed{ActionID: "act_1", ActionType: trajectory.ActionModel, CallID: callID, Operation: classify.Generation, Purpose: classify.PurposeChat, Lane: classify.LaneMain, PublicModel: "gpt-5"}},
			{ID: "evt_observe", Kind: trajectory.KindObservation, Boundary: trajectory.BoundaryIngress, Actor: trajectory.ActorGateway, Principal: key.ID, CallID: callID, At: now, Outputs: []trajectory.Ref{{Segment: segmentID, Role: "observation"}}, Payload: trajectory.Observation{Source: "client", Integrity: trajectory.IntegrityTrusted, Confidentiality: trajectory.ConfidentialityPrivate, CallID: callID}},
			{ID: "evt_gate", Kind: trajectory.KindGateDecision, Boundary: trajectory.BoundaryAction, Actor: trajectory.ActorGateway, Principal: key.ID, CallID: callID, ActionID: "act_1", At: now, Checks: []trajectory.Check{{ID: "check_1", Kind: "deterministic", Status: "finding", Code: "prompt_injection", Severity: "medium", Confidence: 0.8}}, Payload: trajectory.GateDecision{DecisionID: "dec_1", Gate: "action", Action: "block", Blocked: true, CallID: callID, ActionID: "act_1", Checks: []trajectory.Check{{ID: "check_1", Kind: "deterministic", Status: "finding", Code: "prompt_injection", Severity: "medium", Confidence: 0.8}}}},
			{ID: "evt_execute", Kind: trajectory.KindActionExecuted, Boundary: trajectory.BoundaryAction, Actor: trajectory.ActorModel, Principal: key.ID, CallID: callID, ActionID: "act_1", At: now, Payload: trajectory.ActionExecuted{ActionID: "act_1", ActionType: trajectory.ActionModel, CallID: callID, State: "completed", Outcome: classify.OutcomeBlocked, Blocked: true}},
			{ID: "evt_end", Kind: trajectory.KindTrajectoryEnd, Boundary: trajectory.BoundaryRoot, Actor: trajectory.ActorGateway, Principal: key.ID, At: now, Payload: trajectory.TrajectoryEnd{Outcome: "blocked"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	call, err := store.Call(ctx, project.ID, callID)
	if err != nil {
		t.Fatal(err)
	}
	if call.TrajectoryID != summary.ID || call.State != "completed" || call.Outcome != classify.OutcomeBlocked {
		t.Fatalf("call projection=%+v", call)
	}
	if call.FindingCount != 1 || call.MaxSeverity != "medium" {
		t.Fatalf("call finding projection=%+v", call)
	}
	current, err := store.TrajectoryState(ctx, project.ID, summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Coverage <= 0 || current.HeadEvent == "" {
		t.Fatalf("state=%+v", current)
	}
	findings, err := store.FindingsOfTrajectory(ctx, project.ID, summary.ID)
	if err != nil || len(findings) == 0 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
	if report, err := store.VerifyTrajectory(ctx, project.ID, summary.ID); err != nil || report.Status != trajectory.StatusVerified {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	bundle, err := store.EvidenceBundle(ctx, project.ID, summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.TrajectoryID != summary.ID || len(bundle.Events) != 6 {
		t.Fatalf("bundle=%+v", bundle)
	}
	if err := store.RebuildTrajectory(ctx, project.ID, summary.ID); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := store.TrajectoryState(ctx, project.ID, summary.ID)
	if err != nil || rebuilt.HeadEvent != current.HeadEvent {
		t.Fatalf("rebuilt=%+v err=%v", rebuilt, err)
	}
}

func TestInterruptedCallIsClosedOnStartup(t *testing.T) {
	ctx := context.Background()
	store := database(t)
	project, err := store.CreateProject(ctx, &entity.Project{Name: "recover", RetentionDays: 30, CoverageThreshold: 0.9, CreatedAt: time.Now().UTC()}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.CreateAgentKey(ctx, project.ID, "agent", "agent-secret", 0, 0, 0, "agent")
	if err != nil {
		t.Fatal(err)
	}
	summary, _ := store.EnsureTrajectory(ctx, project.ID, key.ID, "", "", 0.9)
	now := time.Now().UTC()
	if _, err := store.Append(ctx, trajectory.Batch{TrajectoryID: summary.ID, ProjectID: project.ID, Events: []trajectory.Draft{
		{Kind: trajectory.KindTrajectoryStart, Boundary: trajectory.BoundaryRoot, Actor: trajectory.ActorGateway, Principal: key.ID, At: now, Payload: trajectory.Start{ProjectID: project.ID, AgentKeyID: key.ID, Lane: classify.LaneMain}},
		{Kind: trajectory.KindActionProposed, Boundary: trajectory.BoundaryAction, Actor: trajectory.ActorGateway, Principal: key.ID, CallID: "call_running", ActionID: "act_running", At: now, Payload: trajectory.ActionProposed{ActionID: "act_running", ActionType: trajectory.ActionModel, CallID: "call_running", Lane: classify.LaneMain, Operation: classify.Generation, Purpose: classify.PurposeChat}},
	}}); err != nil {
		t.Fatal(err)
	}
	interrupted, err := store.InterruptRunning(ctx)
	if err != nil || interrupted != 1 {
		t.Fatalf("interrupted=%d err=%v", interrupted, err)
	}
	call, err := store.Call(ctx, project.ID, "call_running")
	if err != nil || call.State != "interrupted" {
		t.Fatalf("call=%+v err=%v", call, err)
	}
	events, _ := store.TrajectoryEvents(ctx, project.ID, summary.ID)
	found := false
	for _, event := range events {
		if event.Kind == trajectory.KindTrajectoryEnd {
			found = true
		}
	}
	if !found {
		t.Fatal("interruption did not append trajectory.end")
	}
}
