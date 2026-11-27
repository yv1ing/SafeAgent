// Package state reduces a trajectory event log into the materialised security state: labels,
// constraints, capabilities, obligations, approvals, actions and coverage.
package state

import (
	"encoding/json"
	"math"
	"time"

	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type Label struct {
	Integrity       string `json:"integrity"`
	Confidentiality string `json:"confidentiality"`
}

type Constraint struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
	Text string `json:"text,omitempty"`
}

type Capability struct {
	ID          string    `json:"id"`
	Action      string    `json:"action"`
	Resource    string    `json:"resource"`
	MaxRisk     float64   `json:"max_risk"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Parent      string    `json:"parent,omitempty"`
	Delegated   bool      `json:"delegated,omitempty"`
	SourceEvent string    `json:"source_event,omitempty"`
}

type Obligation struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	Requires string `json:"requires"`
	State    string `json:"state"`
	Deadline string `json:"deadline,omitempty"`
}

type Approval struct {
	ID             string    `json:"id"`
	InvocationHash string    `json:"invocation_hash"`
	Scope          string    `json:"scope,omitempty"`
	State          string    `json:"state"`
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
	ExecutionCount int64     `json:"execution_count"`
}

type Action struct {
	ID             string                `json:"id"`
	Type           trajectory.ActionType `json:"type"`
	CallID         string                `json:"call_id,omitempty"`
	ToolCallID     string                `json:"tool_call_id,omitempty"`
	ToolName       string                `json:"tool_name,omitempty"`
	Sink           string                `json:"sink,omitempty"`
	Target         string                `json:"target,omitempty"`
	InputSegments  []string              `json:"input_segments,omitempty"`
	Required       []string              `json:"required_capabilities,omitempty"`
	InvocationHash string                `json:"invocation_hash,omitempty"`
	Status         string                `json:"status"`
	ApprovalID     string                `json:"approval_id,omitempty"`
	ExternalEffect bool                  `json:"external_effect,omitempty"`
	ExecutedAt     *time.Time            `json:"executed_at,omitempty"`
	Observed       bool                  `json:"observed,omitempty"`
}

type Memory struct {
	ID             string   `json:"id"`
	Authority      string   `json:"authority"`
	Verified       bool     `json:"verified"`
	SourceSegments []string `json:"source_segments,omitempty"`
	ContentHash    string   `json:"content_hash,omitempty"`
	Revoked        bool     `json:"revoked,omitempty"`
}

type Risk struct {
	Score       float64 `json:"score"`
	MaxSeverity string  `json:"max_severity,omitempty"`
}

type State struct {
	TrajectoryID      string              `json:"trajectory_id"`
	ProjectID         string              `json:"project_id"`
	AgentKeyID        string              `json:"agent_key_id"`
	SessionID         string              `json:"session_id,omitempty"`
	Goal              string              `json:"goal,omitempty"`
	Version           int64               `json:"version"`
	HeadEvent         string              `json:"head_event"`
	Open              bool                `json:"open"`
	CoverageThreshold float64             `json:"coverage_threshold"`
	Constraints       []Constraint        `json:"constraints"`
	Labels            map[string]Label    `json:"labels"`
	Capabilities      []Capability        `json:"capabilities"`
	Obligations       []Obligation        `json:"obligations"`
	Approvals         map[string]Approval `json:"approvals"`
	Actions           map[string]Action   `json:"actions"`
	Memories          map[string]Memory   `json:"memories"`
	Risk              Risk                `json:"risk"`
	Coverage          float64             `json:"coverage"`
	Expected          int64               `json:"expected_boundaries"`
	Observed          int64               `json:"observed_boundaries"`
	Gaps              int64               `json:"capture_gaps"`
	PolicyVersion     int64               `json:"policy_version,omitempty"`
	PolicyHash        string              `json:"policy_hash,omitempty"`
	RulesVersion      int64               `json:"rules_version,omitempty"`
	RulesHash         string              `json:"rules_hash,omitempty"`
	RedactionRules    json.RawMessage     `json:"redaction_rules,omitempty"`
	Model             string              `json:"model,omitempty"`
	NextCallSeq       int64               `json:"next_call_seq,omitempty"`
	LastRecorder      string              `json:"last_recorder,omitempty"`
	LastSequence      int64               `json:"last_sequence,omitempty"`
	UpdatedAt         time.Time           `json:"updated_at,omitempty"`
}

func New(trajectoryID, projectID string) *State {
	return &State{
		TrajectoryID: trajectoryID, ProjectID: projectID, Open: true,
		CoverageThreshold: 0.9, Coverage: 1,
		Labels: map[string]Label{}, Approvals: map[string]Approval{},
		Actions: map[string]Action{}, Memories: map[string]Memory{},
	}
}

// Reduce applies one event. The reducer is deterministic: the same range always produces the same
// state, so rebuilding projections is safe.
func Reduce(previous *State, event *trajectory.Event) (*State, error) {
	state := clone(previous)
	state.Version = event.Seq
	state.HeadEvent = event.ID
	state.UpdatedAt = event.At
	switch event.Kind {
	case trajectory.KindTrajectoryStart:
		start, err := trajectory.DecodePayload[trajectory.Start](event)
		if err != nil {
			return nil, err
		}
		state.ProjectID, state.AgentKeyID, state.SessionID = start.ProjectID, start.AgentKeyID, start.SessionID
		state.Goal, state.Model, state.Open = start.Goal, start.Model, true
		if start.CoverageThreshold > 0 {
			state.CoverageThreshold = start.CoverageThreshold
		}
		if start.PolicyVersion > 0 {
			state.PolicyVersion, state.PolicyHash = start.PolicyVersion, start.PolicyHash
		}
	case trajectory.KindObservation:
		payload, err := trajectory.DecodePayload[trajectory.Observation](event)
		if err != nil {
			return nil, err
		}
		state.Expected++
		state.Observed++
		for _, ref := range append(append([]trajectory.Ref{}, event.Inputs...), event.Outputs...) {
			integrity, confidentiality := eventLabel(payload.Labels, ref.Segment, payload.Integrity, payload.Confidentiality)
			mergeLabel(state, ref.Segment, integrity, confidentiality)
		}
		if event.ActionID != "" {
			markObserved(state, event.ActionID)
		}
	case trajectory.KindContextAssemble:
		payload, err := trajectory.DecodePayload[trajectory.ContextAssemble](event)
		if err != nil {
			return nil, err
		}
		state.Expected++
		state.Observed++
		for _, ref := range event.Inputs {
			integrity, confidentiality := eventLabel(payload.Labels, ref.Segment, trajectory.IntegrityTrusted, trajectory.ConfidentialityPrivate)
			mergeLabel(state, ref.Segment, integrity, confidentiality)
		}
		for _, ref := range event.Outputs {
			integrity, confidentiality := eventLabel(payload.Labels, ref.Segment, trajectory.IntegrityUntrusted, trajectory.ConfidentialityPublic)
			mergeLabel(state, ref.Segment, integrity, confidentiality)
		}
	case trajectory.KindContextCompact:
		payload, err := trajectory.DecodePayload[trajectory.ContextCompact](event)
		if err != nil {
			return nil, err
		}
		state.Expected++
		state.Observed++
		if len(payload.DroppedConstraints) == 0 {
			break
		}
		for _, id := range payload.DroppedConstraints {
			state.Obligations = append(state.Obligations, Obligation{
				ID: "obl_" + event.ID + "_" + id, Kind: "constraint_survival", Subject: id,
				Requires: "repair", State: "violated", Deadline: "trajectory.end",
			})
		}
	case trajectory.KindMemoryWrite:
		payload, err := trajectory.DecodePayload[trajectory.MemoryWrite](event)
		if err != nil {
			return nil, err
		}
		state.Expected++
		state.Observed++
		state.Memories[payload.EntryID] = Memory{
			ID: payload.EntryID, Authority: payload.Authority, Verified: payload.Verified,
			SourceSegments: payload.SourceSegments, ContentHash: payload.ContentHash,
		}
	case trajectory.KindMemoryConsolidate:
		payload, err := trajectory.DecodePayload[trajectory.MemoryConsolidate](event)
		if err != nil {
			return nil, err
		}
		state.Expected++
		state.Observed++
		for _, id := range payload.Revoked {
			entry := state.Memories[id]
			entry.Revoked = true
			state.Memories[id] = entry
		}
		for _, id := range payload.After {
			if entry, found := state.Memories[id]; found && payload.Authority != "" {
				entry.Authority = payload.Authority
				state.Memories[id] = entry
			}
		}
	case trajectory.KindMemoryRead:
		payload, err := trajectory.DecodePayload[trajectory.MemoryRead](event)
		if err != nil {
			return nil, err
		}
		state.Expected++
		state.Observed++
		for _, id := range payload.Segments {
			if payload.Integrity != "" {
				mergeLabel(state, id, payload.Integrity, "")
			}
		}
	case trajectory.KindActionProposed:
		payload, err := trajectory.DecodePayload[trajectory.ActionProposed](event)
		if err != nil {
			return nil, err
		}
		if payload.ActionType == trajectory.ActionProviderAttempt {
			break
		}
		state.Expected++
		action := Action{
			ID: payload.ActionID, Type: payload.ActionType, CallID: payload.CallID,
			ToolCallID: payload.ToolCallID, ToolName: payload.ToolName, Sink: payload.Sink,
			Target: payload.Target, InputSegments: payload.InputSegments,
			Required: payload.RequiredCapabilities, InvocationHash: payload.InvocationHash,
			Status: "proposed",
		}
		if payload.ActionType == trajectory.ActionModel && payload.CallID != "" {
			state.NextCallSeq++
		}
		state.Actions[payload.ActionID] = action
	case trajectory.KindActionAuthorized:
		payload, err := trajectory.DecodePayload[trajectory.ActionAuthorized](event)
		if err != nil {
			return nil, err
		}
		action, found := state.Actions[payload.ActionID]
		if !found {
			break
		}
		if payload.Decision == "block" {
			action.Status = "blocked"
			markObserved(state, payload.ActionID)
		} else if payload.Decision == "require_approval" {
			action.Status = "awaiting_approval"
		} else {
			action.Status = "authorized"
			action.ApprovalID = payload.ApprovalID
		}
		state.Actions[payload.ActionID] = action
	case trajectory.KindActionExecuted:
		payload, err := trajectory.DecodePayload[trajectory.ActionExecuted](event)
		if err != nil {
			return nil, err
		}
		if payload.ActionType == trajectory.ActionProviderAttempt {
			break
		}
		action, found := state.Actions[payload.ActionID]
		if !found {
			break
		}
		action.Status = "executed"
		if payload.Blocked {
			action.Status = "blocked"
		}
		action.ExternalEffect = payload.ExternalEffect
		executed := event.At
		action.ExecutedAt = &executed
		state.Actions[payload.ActionID] = action
		markObserved(state, payload.ActionID)
		if payload.Blocked {
			state.Risk.Score += 0.5
		}
		if payload.ErrorCode != "" {
			state.Risk.Score += 0.05
		}
	case trajectory.KindExternalEffect:
		payload, err := trajectory.DecodePayload[trajectory.ExternalEffect](event)
		if err != nil {
			return nil, err
		}
		action, found := state.Actions[payload.ActionID]
		if !found {
			break
		}
		action.ExternalEffect = true
		action.Status = "external_effect"
		state.Actions[payload.ActionID] = action
		state.Expected++
		state.Observed++
	case trajectory.KindRollback:
		payload, err := trajectory.DecodePayload[trajectory.RollbackPayload](event)
		if err != nil {
			return nil, err
		}
		state.Expected++
		state.Observed++
		if !payload.Verified {
			state.Obligations = append(state.Obligations, Obligation{
				ID: "obl_" + event.ID + "_rollback", Kind: "rollback_consistency", Subject: event.ID,
				Requires: "repair", State: "violated",
			})
		}
	case trajectory.KindDelegationSpawn:
		payload, err := trajectory.DecodePayload[trajectory.DelegationSpawn](event)
		if err != nil {
			return nil, err
		}
		state.Expected++
		state.Observed++
		for _, label := range payload.Labels {
			mergeLabel(state, label.Segment, label.Integrity, label.Confidentiality)
		}
		for _, capability := range payload.Capabilities {
			state.Capabilities = append(state.Capabilities, Capability{
				ID: capability, Delegated: true, SourceEvent: event.ID,
			})
		}
	case trajectory.KindDelegationReturn:
		state.Expected++
		state.Observed++
	case trajectory.KindToolRegistryChange:
		state.Expected++
		state.Observed++
	case trajectory.KindPolicyChange:
		payload, err := trajectory.DecodePayload[trajectory.PolicyChange](event)
		if err != nil {
			return nil, err
		}
		state.PolicyVersion, state.PolicyHash = payload.Version, payload.Hash
		state.RulesVersion, state.RulesHash = payload.RulesVersion, payload.RulesHash
		if len(payload.RedactionRules) > 0 {
			state.RedactionRules = payload.RedactionRules
		}
	case trajectory.KindModelChange:
		payload, err := trajectory.DecodePayload[trajectory.ModelChange](event)
		if err != nil {
			return nil, err
		}
		state.Model = payload.PublicModel
	case trajectory.KindApprovalRequested:
		payload, err := trajectory.DecodePayload[trajectory.ApprovalRequested](event)
		if err != nil {
			return nil, err
		}
		state.Approvals[payload.ApprovalID] = Approval{
			ID: payload.ApprovalID, InvocationHash: payload.InvocationHash, Scope: payload.Scope,
			State: "pending", ExpiresAt: payload.ExpiresAt,
		}
	case trajectory.KindApprovalGranted:
		payload, err := trajectory.DecodePayload[trajectory.ApprovalGranted](event)
		if err != nil {
			return nil, err
		}
		approval, found := state.Approvals[payload.ApprovalID]
		if !found {
			break
		}
		approval.ID = payload.ApprovalID
		approval.InvocationHash = payload.InvocationHash
		approval.Scope = payload.Scope
		approval.State = "granted"
		approval.ExpiresAt = payload.ExpiresAt
		state.Approvals[payload.ApprovalID] = approval
	case trajectory.KindApprovalDenied:
		payload, err := trajectory.DecodePayload[trajectory.ApprovalDenied](event)
		if err != nil {
			return nil, err
		}
		approval, found := state.Approvals[payload.ApprovalID]
		if !found {
			break
		}
		approval.ID = payload.ApprovalID
		approval.State = "denied"
		state.Approvals[payload.ApprovalID] = approval
	case trajectory.KindApprovalConsumed:
		payload, err := trajectory.DecodePayload[trajectory.ApprovalConsumed](event)
		if err != nil {
			return nil, err
		}
		approval, found := state.Approvals[payload.ApprovalID]
		if !found {
			break
		}
		approval.ID = payload.ApprovalID
		approval.State = "consumed"
		approval.ExecutionCount = payload.ExecutionCount
		state.Approvals[payload.ApprovalID] = approval
	case trajectory.KindCaptureHeartbeat:
		payload, err := trajectory.DecodePayload[trajectory.CaptureHeartbeat](event)
		if err != nil {
			return nil, err
		}
		state.LastRecorder, state.LastSequence = payload.Recorder, payload.Sequence
		if payload.Gap > 0 {
			state.Gaps += payload.Gap
		}
	case trajectory.KindGateDecision:
		payload, err := trajectory.DecodePayload[trajectory.GateDecision](event)
		if err != nil {
			return nil, err
		}
		if payload.Blocked {
			state.Risk.Score += 0.5
		}
		if payload.ActionID != "" {
			if action, found := state.Actions[payload.ActionID]; found {
				if payload.Blocked {
					action.Status = "blocked"
					markObserved(state, payload.ActionID)
				} else if action.Status == "proposed" {
					action.Status = "authorized"
				}
				state.Actions[payload.ActionID] = action
			}
		}
		if payload.Terminal && payload.ActionID != "" {
			markObserved(state, payload.ActionID)
		}
		for _, check := range payload.Checks {
			if check.Severity == "high" {
				state.Risk.MaxSeverity = "high"
				state.Risk.Score += 1
			} else if check.Severity == "medium" && state.Risk.MaxSeverity != "high" {
				state.Risk.MaxSeverity = "medium"
				state.Risk.Score += 0.3
			}
		}
	case trajectory.KindTrajectoryEnd:
		if _, err := trajectory.DecodePayload[trajectory.TrajectoryEnd](event); err != nil {
			return nil, err
		}
		state.Open = false
	}
	state.Coverage = coverageOf(state)
	if state.Risk.Score > 10 {
		state.Risk.Score = 10
	}
	return state, nil
}

func clone(state *State) *State {
	clone := *state
	clone.Constraints = append([]Constraint(nil), state.Constraints...)
	clone.Capabilities = append([]Capability(nil), state.Capabilities...)
	clone.Obligations = append([]Obligation(nil), state.Obligations...)
	clone.Labels = make(map[string]Label, len(state.Labels))
	for key, value := range state.Labels {
		clone.Labels[key] = value
	}
	clone.Approvals = make(map[string]Approval, len(state.Approvals))
	for key, value := range state.Approvals {
		clone.Approvals[key] = value
	}
	clone.Actions = make(map[string]Action, len(state.Actions))
	for key, value := range state.Actions {
		clone.Actions[key] = value
	}
	clone.Memories = make(map[string]Memory, len(state.Memories))
	for key, value := range state.Memories {
		clone.Memories[key] = value
	}
	return &clone
}

func mergeLabel(state *State, segment, integrity, confidentiality string) {
	if segment == "" {
		return
	}
	label := state.Labels[segment]
	if integrity != "" {
		if label.Integrity == "" || rankIntegrity(integrity) > rankIntegrity(label.Integrity) {
			label.Integrity = integrity
		}
	}
	if confidentiality != "" {
		if label.Confidentiality == "" || rankConfidentiality(confidentiality) > rankConfidentiality(label.Confidentiality) {
			label.Confidentiality = confidentiality
		}
	}
	state.Labels[segment] = label
}

func rankIntegrity(value string) int {
	switch value {
	case trajectory.IntegrityUntrusted:
		return 3
	case trajectory.IntegrityUnknown:
		return 2
	case trajectory.IntegrityTrusted:
		return 1
	}
	return 0
}

func rankConfidentiality(value string) int {
	switch value {
	case trajectory.ConfidentialityUserIdentity:
		return 3
	case trajectory.ConfidentialityPrivate:
		return 2
	case trajectory.ConfidentialityPublic:
		return 1
	}
	return 0
}

func markObserved(state *State, actionID string) {
	action, found := state.Actions[actionID]
	if !found || action.Observed {
		return
	}
	action.Observed = true
	state.Actions[actionID] = action
	state.Observed++
}

// CoverageForCall is the capture coverage a gate may act on while a call is still running. Actions
// that the current call has proposed but not yet completed are excluded from the denominator; an
// unfinished action from an earlier call stays in it.
func (state *State) CoverageForCall(callID string) float64 {
	open := int64(0)
	for _, action := range state.Actions {
		if action.CallID == callID && (action.Status == "proposed" || action.Status == "awaiting_approval") {
			open++
		}
	}
	expected := state.Expected + state.Gaps - open
	if expected <= 0 {
		return 1
	}
	return math.Max(0, math.Min(1, float64(state.Observed)/float64(expected)))
}

func eventLabel(labels []trajectory.LabelRef, segment, integrity, confidentiality string) (string, string) {
	for _, label := range labels {
		if label.Segment == segment {
			return label.Integrity, label.Confidentiality
		}
	}
	return integrity, confidentiality
}

func coverageOf(state *State) float64 {
	expected := state.Expected + state.Gaps
	if expected <= 0 {
		return 1
	}
	coverage := float64(state.Observed) / float64(expected)
	return math.Max(0, math.Min(1, coverage))
}

// Hash returns the canonical state hash. It excludes the timestamp so two reductions of the same
// event range compare equal.
func Hash(state *State) string {
	clone := *state
	clone.UpdatedAt = time.Time{}
	payload, _ := json.Marshal(clone)
	canonical, err := trajectory.Canonicalize(payload)
	if err != nil {
		return ""
	}
	return trajectory.ContentHash(canonical)
}

func (state *State) Label(segment string) Label { return state.Labels[segment] }

func (state *State) Action(id string) (Action, bool) {
	action, found := state.Actions[id]
	return action, found
}

func (state *State) Approval(id string) (Approval, bool) {
	approval, found := state.Approvals[id]
	return approval, found
}
