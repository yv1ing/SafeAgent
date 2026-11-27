package threat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

const InvariantVersion = "2026.11.3"

const (
	ControlAllow           = "allow"
	ControlBlock           = "block"
	ControlRequireApproval = "require_approval"
	ControlCompensate      = "compensate"
	ControlQuarantine      = "quarantine"
	ControlRecord          = "record"
)

type Threat struct {
	ID        string
	Transfer  trajectory.Kind
	Failure   string
	Invariant string
	Control   string
	Severity  string
}

var Threats = []Threat{
	{"ASI01.untrusted_external", trajectory.KindActionProposed, "admit", "I1", ControlRequireApproval, SeverityHigh},
	{"ASI02.private_public_sink", trajectory.KindActionProposed, "admit", "I2", ControlBlock, SeverityHigh},
	{"ASI03.capability_escalation", trajectory.KindDelegationSpawn, "rewrite", "I3", ControlBlock, SeverityHigh},
	{"ASI04.constraint_loss", trajectory.KindContextCompact, "rewrite", "I4", ControlBlock, SeverityHigh},
	{"ASI06.memory_authority_collapse", trajectory.KindMemoryWrite, "rewrite", "I5", ControlBlock, SeverityHigh},
	{"ASI06.rollback_mismatch", trajectory.KindRollback, "restore", "I6", ControlBlock, SeverityHigh},
	{"ASI09.approval_replay", trajectory.KindApprovalConsumed, "admit", "I7", ControlBlock, SeverityHigh},
	{"ASI08.capture_gap", trajectory.KindTrajectoryEnd, "admit", "I8", ControlRecord, SeverityMedium},
	{"ASI07.delegation_escalation", trajectory.KindDelegationSpawn, "rewrite", "I9", ControlBlock, SeverityHigh},
	{"ASI10.unattributed_action", trajectory.KindActionExecuted, "admit", "I10", ControlBlock, SeverityMedium},
}

var threatByInvariant = func() map[string]Threat {
	values := make(map[string]Threat, len(Threats))
	for _, threat := range Threats {
		values[threat.Invariant] = threat
	}
	return values
}()

func threatFor(invariant string) Threat { return threatByInvariant[invariant] }

// EvaluateInvariants checks the ten trajectory invariants against the state as it existed before
// event and the event itself. The result is deterministic and independent of any model.
func EvaluateInvariants(before *state.State, after *state.State, event *trajectory.Event) []Finding {
	if before == nil || after == nil {
		return nil
	}
	findings := make([]Finding, 0, 4)
	switch event.Kind {
	case trajectory.KindActionProposed:
		findings = append(findings, actionInvariants(after, event)...)
	case trajectory.KindContextCompact:
		if payload, err := trajectory.DecodePayload[trajectory.ContextCompact](event); err == nil && len(payload.DroppedConstraints) > 0 {
			findings = append(findings, invariantFinding(threatFor("I4"), event, payload.DroppedConstraints, ControlBlock,
				"compaction dropped a pinned constraint"))
		}
	case trajectory.KindMemoryWrite:
		findings = append(findings, memoryInvariant(after, event)...)
	case trajectory.KindMemoryConsolidate:
		findings = append(findings, consolidationInvariant(after, event)...)
	case trajectory.KindRollback:
		if payload, err := trajectory.DecodePayload[trajectory.RollbackPayload](event); err == nil {
			if payload.Snapshot.Opaque || (payload.Snapshot.Cache == "" && payload.Snapshot.Session != "") || !payload.Verified {
				findings = append(findings, invariantFinding(threatFor("I6"), event, nil, ControlBlock,
					"rollback cache state is opaque or mismatched"))
			}
		}
	case trajectory.KindDelegationSpawn:
		findings = append(findings, delegationInvariants(after, event)...)
	case trajectory.KindApprovalConsumed:
		if payload, err := trajectory.DecodePayload[trajectory.ApprovalConsumed](event); err == nil {
			if payload.ExecutionCount > 1 {
				findings = append(findings, invariantFinding(threatFor("I7"), event, nil, ControlBlock,
					"approval was consumed more than once"))
			}
		}
	case trajectory.KindActionExecuted:
		findings = append(findings, attributionInvariants(before, event)...)
	case trajectory.KindCaptureHeartbeat:
		if payload, err := trajectory.DecodePayload[trajectory.CaptureHeartbeat](event); err == nil && payload.Gap > 0 {
			findings = append(findings, invariantFinding(threatFor("I8"), event, nil, ControlRecord,
				"recorder sequence reported a capture gap"))
		}
	case trajectory.KindTrajectoryEnd:
		if after.Coverage+1e-9 < after.CoverageThreshold {
			findings = append(findings, invariantFinding(threatFor("I8"), event, nil, ControlRecord,
				"trajectory coverage is below the configured threshold"))
		}
	}
	return NormalizeFindings(findings)
}

func actionInvariants(current *state.State, event *trajectory.Event) []Finding {
	payload, err := trajectory.DecodePayload[trajectory.ActionProposed](event)
	if err != nil || payload.ActionType == trajectory.ActionProviderAttempt {
		return nil
	}
	sink := observation.Sink(payload.Sink)
	findings := []Finding{}
	hasUntrusted, hasPrivate := false, false
	segments := make([]string, 0, len(payload.InputSegments))
	for _, id := range payload.InputSegments {
		label := current.Labels[id]
		segments = append(segments, id)
		if label.Integrity == trajectory.IntegrityUntrusted {
			hasUntrusted = true
		}
		if label.Confidentiality == trajectory.ConfidentialityPrivate || label.Confidentiality == trajectory.ConfidentialityUserIdentity {
			hasPrivate = true
		}
	}
	if sink.HasSideEffect() && hasUntrusted {
		if approval := approvalFor(current, payload.InvocationHash, payload.ActionID, event.At); approval == "" {
			finding := invariantFinding(threatFor("I1"), event, segments, ControlRequireApproval,
				"untrusted content reached an external action without an approval")
			finding.Sink = sink
			findings = append(findings, finding)
		}
	}
	if isPublicSink(sink) && hasPrivate {
		finding := invariantFinding(threatFor("I2"), event, segments, ControlBlock,
			"private content reached a public sink")
		finding.Sink = sink
		findings = append(findings, finding)
	}
	if len(payload.InputSegments) == 0 && payload.ActionType == trajectory.ActionTool {
		findings = append(findings, invariantFinding(threatFor("I10"), event, nil, ControlRecord,
			"tool action has no segment lineage"))
	}
	if current.CoverageForCall(payload.CallID)+1e-9 < current.CoverageThreshold && sink.HasSideEffect() {
		findings = append(findings, invariantFinding(threatFor("I8"), event, nil, ControlRequireApproval,
			"coverage is below threshold for an irreversible action"))
	}
	return findings
}

func memoryInvariant(current *state.State, event *trajectory.Event) []Finding {
	payload, err := trajectory.DecodePayload[trajectory.MemoryWrite](event)
	if err != nil {
		return nil
	}
	sourceRank := 0
	for _, id := range payload.SourceSegments {
		label := current.Labels[id]
		sourceRank = max(sourceRank, integrityRank(label.Integrity))
	}
	if sourceRank > integrityRank(payload.Authority) {
		return []Finding{invariantFinding(threatFor("I5"), event, payload.SourceSegments, ControlBlock,
			"memory entry authority is below its source segments")}
	}
	return nil
}

func consolidationInvariant(current *state.State, event *trajectory.Event) []Finding {
	payload, err := trajectory.DecodePayload[trajectory.MemoryConsolidate](event)
	if err != nil || payload.Authority == "" {
		return nil
	}
	segments := []string{}
	sourceRank := 0
	for _, id := range payload.After {
		entry, found := current.Memories[id]
		if !found {
			continue
		}
		for _, segment := range entry.SourceSegments {
			segments = append(segments, segment)
			sourceRank = max(sourceRank, integrityRank(current.Labels[segment].Integrity))
		}
	}
	if sourceRank > integrityRank(payload.Authority) {
		return []Finding{invariantFinding(threatFor("I5"), event, segments, ControlBlock,
			"consolidated memory authority is below its source segments")}
	}
	return nil
}

func delegationInvariants(current *state.State, event *trajectory.Event) []Finding {
	payload, err := trajectory.DecodePayload[trajectory.DelegationSpawn](event)
	if err != nil {
		return nil
	}
	granted := map[string]bool{}
	for _, capability := range current.Capabilities {
		if !capability.Delegated {
			granted[capability.ID] = true
		}
	}
	if len(granted) == 0 {
		return nil
	}
	for _, capability := range payload.Capabilities {
		if !granted[capability] {
			return []Finding{invariantFinding(threatFor("I3"), event, nil, ControlBlock,
				"delegation granted a capability the parent did not hold")}
		}
	}
	return nil
}

func attributionInvariants(current *state.State, event *trajectory.Event) []Finding {
	payload, err := trajectory.DecodePayload[trajectory.ActionExecuted](event)
	if err != nil || payload.ActionType != trajectory.ActionTool {
		return nil
	}
	action, found := current.Actions[payload.ActionID]
	if !found {
		return []Finding{invariantFinding(threatFor("I10"), event, nil, ControlRecord,
			"executed action has no proposal")}
	}
	switch action.Status {
	case "proposed", "awaiting_approval":
		return []Finding{invariantFinding(threatFor("I10"), event, nil, ControlBlock,
			"action executed without an authorization")}
	case "blocked":
		return []Finding{invariantFinding(threatFor("I10"), event, nil, ControlBlock,
			"action executed after the gate blocked it")}
	}
	return nil
}

func approvalFor(current *state.State, invocation, actionID string, at time.Time) string {
	for id, approval := range current.Approvals {
		if approval.State == "granted" && (invocation == "" || approval.InvocationHash == invocation) &&
			(approval.ExpiresAt.IsZero() || approval.ExpiresAt.After(at)) {
			return id
		}
	}
	if action, found := current.Actions[actionID]; found && action.ApprovalID != "" {
		return action.ApprovalID
	}
	return ""
}

func isPublicSink(sink observation.Sink) bool {
	switch sink {
	case observation.SinkExternalSend, observation.SinkPayment, observation.SinkFileWrite:
		return true
	}
	return false
}

func integrityRank(value string) int {
	switch value {
	case trajectory.IntegrityTrusted:
		return 3
	case trajectory.IntegrityUnknown:
		return 2
	case trajectory.IntegrityUntrusted:
		return 1
	}
	return 0
}

func invariantFinding(threat Threat, event *trajectory.Event, segments []string, control, detail string) Finding {
	finding := Finding{
		Code: findingCode(threat), Severity: threat.Severity, Confidence: 1,
		Origin: observation.OriginEnvironment, Trust: observation.Untrusted, Sink: observation.SinkNone,
		Detector: DetectorRef{ID: "invariant", Version: InvariantVersion},
		Evidence: []Evidence{{
			Section: "event", Path: event.ID, View: "event", Quote: detail,
		}},
		EventID: event.ID, Invariant: threat.Invariant, Transfer: string(threat.Transfer), Control: control,
		Segments: segments,
	}
	payload, _ := json.Marshal(struct {
		ID        string `json:"id"`
		At        int64  `json:"at"`
		Code      string `json:"code"`
		Invariant string `json:"invariant"`
	}{event.ID, event.At.UnixMicro(), finding.Code, finding.Invariant})
	sum := sha256.Sum256(payload)
	finding.Fingerprint = hex.EncodeToString(sum[:16])
	return finding
}

func findingCode(value Threat) string {
	switch value.Invariant {
	case "I1":
		return CodeUntrustedToExternalSend
	case "I2":
		return CodePrivateToPublicSink
	case "I3", "I9":
		return CodeCapabilityEscalation
	case "I4":
		return CodeConstraintLost
	case "I5":
		return CodeMemoryAuthorityCollapse
	case "I6":
		return CodeRollbackMismatch
	case "I7":
		return CodeApprovalReplay
	case "I8":
		return CodeCaptureGap
	case "I10":
		return CodeUnattributedAction
	}
	return CodeProvenanceAuthMismatch
}
