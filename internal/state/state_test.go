package state_test

import (
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

func event(t *testing.T, kind trajectory.Kind, payload any) *trajectory.Event {
	t.Helper()
	return &trajectory.Event{ID: trajectory.NewID("evt"), Kind: kind, At: time.Now().UTC(), Payload: trajectory.Marshal(payload)}
}

func TestReducerTracksLabelsAndCoverage(t *testing.T) {
	current := state.New("trj_1", "project_1")
	current, err := state.Reduce(current, event(t, trajectory.KindTrajectoryStart, trajectory.Start{ProjectID: "project_1", AgentKeyID: "key_1", CoverageThreshold: 0.9}))
	if err != nil {
		t.Fatal(err)
	}
	observation := event(t, trajectory.KindObservation, trajectory.Observation{Source: "tool", Integrity: trajectory.IntegrityUntrusted, Confidentiality: trajectory.ConfidentialityPrivate})
	observation.Outputs = []trajectory.Ref{{Segment: "seg_1", Role: "tool_result"}}
	current, err = state.Reduce(current, observation)
	if err != nil {
		t.Fatal(err)
	}
	if label := current.Label("seg_1"); label.Integrity != trajectory.IntegrityUntrusted || label.Confidentiality != trajectory.ConfidentialityPrivate {
		t.Fatalf("label=%+v", label)
	}
	proposed := event(t, trajectory.KindActionProposed, trajectory.ActionProposed{ActionID: "act_1", ActionType: trajectory.ActionTool, Sink: "external_send"})
	current, err = state.Reduce(current, proposed)
	if err != nil {
		t.Fatal(err)
	}
	if current.Coverage >= 1 {
		t.Fatalf("an unexecuted action must lower coverage: %+v", current)
	}
}

func TestObservationKeepsPerSegmentLabels(t *testing.T) {
	current := state.New("trj_1", "project_1")
	observation := event(t, trajectory.KindObservation, trajectory.Observation{
		Source: "client", Integrity: trajectory.IntegrityTrusted, Confidentiality: trajectory.ConfidentialityPrivate,
		Labels: []trajectory.LabelRef{
			{Segment: "seg_user", Integrity: trajectory.IntegrityTrusted, Confidentiality: trajectory.ConfidentialityUserIdentity},
			{Segment: "seg_tool", Integrity: trajectory.IntegrityUntrusted, Confidentiality: trajectory.ConfidentialityPrivate},
		},
	})
	observation.Outputs = []trajectory.Ref{{Segment: "seg_user"}, {Segment: "seg_tool"}}
	var err error
	current, err = state.Reduce(current, observation)
	if err != nil {
		t.Fatal(err)
	}
	if label := current.Label("seg_user"); label.Confidentiality != trajectory.ConfidentialityUserIdentity {
		t.Fatalf("user label=%+v", label)
	}
	if label := current.Label("seg_tool"); label.Integrity != trajectory.IntegrityUntrusted {
		t.Fatalf("tool label=%+v", label)
	}
}

func TestCoverageForCallIgnoresItsOpenActions(t *testing.T) {
	current := state.New("trj_1", "project_1")
	observation := event(t, trajectory.KindObservation, trajectory.Observation{Source: "client", Integrity: trajectory.IntegrityTrusted, Confidentiality: trajectory.ConfidentialityPrivate})
	observation.Outputs = []trajectory.Ref{{Segment: "seg_1"}}
	var err error
	current, err = state.Reduce(current, observation)
	if err != nil {
		t.Fatal(err)
	}
	current, err = state.Reduce(current, event(t, trajectory.KindActionProposed, trajectory.ActionProposed{ActionID: "act_1", ActionType: trajectory.ActionModel, CallID: "call_1"}))
	if err != nil {
		t.Fatal(err)
	}
	if current.Coverage >= 1 {
		t.Fatalf("the open action lowers stored coverage: %+v", current)
	}
	if coverage := current.CoverageForCall("call_1"); coverage != 1 {
		t.Fatalf("the call's own open action must not lower its gate coverage: %v", coverage)
	}
}

func TestReducerApprovalLifecycle(t *testing.T) {
	current := state.New("trj_1", "project_1")
	expiry := time.Now().Add(time.Hour)
	for _, payload := range []struct {
		kind trajectory.Kind
		data any
	}{
		{trajectory.KindApprovalRequested, trajectory.ApprovalRequested{ApprovalID: "ap_1", InvocationHash: "h", ExpiresAt: expiry}},
		{trajectory.KindApprovalGranted, trajectory.ApprovalGranted{ApprovalID: "ap_1", InvocationHash: "h", ExpiresAt: expiry}},
		{trajectory.KindApprovalConsumed, trajectory.ApprovalConsumed{ApprovalID: "ap_1", InvocationHash: "h", ExecutionCount: 1}},
	} {
		var err error
		current, err = state.Reduce(current, event(t, payload.kind, payload.data))
		if err != nil {
			t.Fatal(err)
		}
	}
	approval, found := current.Approval("ap_1")
	if !found || approval.State != "consumed" || approval.ExecutionCount != 1 {
		t.Fatalf("approval=%+v", approval)
	}
}
