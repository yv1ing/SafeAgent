package threat_test

import (
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/threat"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

func event(t *testing.T, kind trajectory.Kind, payload any) *trajectory.Event {
	t.Helper()
	return &trajectory.Event{ID: trajectory.NewID("evt"), Kind: kind, At: time.Now().UTC(), Payload: trajectory.Marshal(payload)}
}

func TestUntrustedInputToExternalSendRequiresApproval(t *testing.T) {
	before := state.New("trj_1", "project_1")
	before.Labels["seg_1"] = state.Label{Integrity: trajectory.IntegrityUntrusted, Confidentiality: trajectory.ConfidentialityPrivate}
	after := *before
	event := event(t, trajectory.KindActionProposed, trajectory.ActionProposed{ActionID: "act_1", ActionType: trajectory.ActionTool, Sink: "external_send", InputSegments: []string{"seg_1"}})
	findings := threat.EvaluateInvariants(before, &after, event)
	if !has(findings, threat.CodeUntrustedToExternalSend) {
		t.Fatalf("findings=%+v", findings)
	}
}

func TestPrivateContentToPublicSink(t *testing.T) {
	before := state.New("trj_1", "project_1")
	before.Labels["seg_1"] = state.Label{Integrity: trajectory.IntegrityTrusted, Confidentiality: trajectory.ConfidentialityUserIdentity}
	after := *before
	event := event(t, trajectory.KindActionProposed, trajectory.ActionProposed{ActionID: "act_1", ActionType: trajectory.ActionTool, Sink: "external_send", InputSegments: []string{"seg_1"}})
	if !has(threat.EvaluateInvariants(before, &after, event), threat.CodePrivateToPublicSink) {
		t.Fatal("private content to a public sink was not found")
	}
}

func TestCompactionConstraintLoss(t *testing.T) {
	state := state.New("trj_1", "project_1")
	event := event(t, trajectory.KindContextCompact, trajectory.ContextCompact{DroppedConstraints: []string{"no-share"}})
	if !has(threat.EvaluateInvariants(state, state, event), threat.CodeConstraintLost) {
		t.Fatal("constraint loss was not found")
	}
}

func TestMemoryAuthorityCollapse(t *testing.T) {
	before := state.New("trj_1", "project_1")
	before.Labels["seg_1"] = state.Label{Integrity: trajectory.IntegrityTrusted}
	after := *before
	event := event(t, trajectory.KindMemoryWrite, trajectory.MemoryWrite{EntryID: "m1", SourceSegments: []string{"seg_1"}, Authority: trajectory.IntegrityUntrusted})
	if !has(threat.EvaluateInvariants(before, &after, event), threat.CodeMemoryAuthorityCollapse) {
		t.Fatal("authority collapse was not found")
	}
}

func TestRollbackOpaqueIsIncomplete(t *testing.T) {
	state := state.New("trj_1", "project_1")
	event := event(t, trajectory.KindRollback, trajectory.RollbackPayload{Snapshot: trajectory.Snapshot{Session: "s", Opaque: true}, Verified: true})
	if !has(threat.EvaluateInvariants(state, state, event), threat.CodeRollbackMismatch) {
		t.Fatal("opaque cache rollback was reported verified")
	}
}

func TestExecutionWithoutAuthorizationIsUnattributed(t *testing.T) {
	before := state.New("trj_1", "project_1")
	before.Actions["act_1"] = state.Action{ID: "act_1", Type: trajectory.ActionTool, Status: "proposed"}
	event := event(t, trajectory.KindActionExecuted, trajectory.ActionExecuted{ActionID: "act_1", ActionType: trajectory.ActionTool})
	if !has(threat.EvaluateInvariants(before, before, event), threat.CodeUnattributedAction) {
		t.Fatal("an executed tool call without authorization was not found")
	}
}

func TestApprovalReplayIsFlagged(t *testing.T) {
	current := state.New("trj_1", "project_1")
	event := event(t, trajectory.KindApprovalConsumed, trajectory.ApprovalConsumed{ApprovalID: "ap_1", InvocationHash: "h", ExecutionCount: 2})
	if !has(threat.EvaluateInvariants(current, current, event), threat.CodeApprovalReplay) {
		t.Fatal("a second approval consumption was not found")
	}
}

func has(findings []threat.Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
