package pipeline

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

const CodeAuditFailed = "audit_failed"

type Recorder struct {
	writer            Repository
	logger            *slog.Logger
	call              *entity.Call
	summary           *entity.TrajectorySummary
	principal         string
	trajectoryID      string
	pending           []trajectory.Draft
	segments          map[string]trajectory.Segment
	blobs             map[string]trajectory.Blob
	mu                sync.Mutex
	writeFailure      error
	modelAction       string
	response          string
	sealed            bool
	policyVersion     int64
	policyHash        string
	rulesVersion      int64
	rulesHash         string
	rules             []redact.Rule
	request           string
	observed          []string
	actionID          string
	finishEventID     string
	EndOnFinish       bool
	CoverageThreshold float64
}

func NewRecorder(writer Repository, logger *slog.Logger, call *entity.Call, summary *entity.TrajectorySummary, principal string) *Recorder {
	return &Recorder{
		writer: writer, logger: logger, call: call, summary: summary, principal: principal,
		trajectoryID: summary.ID, modelAction: "act_model_" + call.ID, actionID: "act_model_" + call.ID,
		segments: map[string]trajectory.Segment{}, blobs: map[string]trajectory.Blob{},
	}
}

func (recorder *Recorder) Call() *entity.Call { return recorder.call }

func (recorder *Recorder) TrajectoryID() string { return recorder.trajectoryID }

func (recorder *Recorder) SetAction(actionID string) { recorder.actionID = actionID }

func (recorder *Recorder) ResetAction() { recorder.actionID = recorder.modelAction }

func (recorder *Recorder) CurrentAction() string { return recorder.actionID }

func (recorder *Recorder) InputSegments() []string {
	values := append([]string(nil), recorder.observed...)
	if recorder.request != "" {
		values = append(values, recorder.request)
	}
	return values
}

// Open emits the trajectory start for a new trajectory. It performs no call-level write: the model
// action is proposed after redaction has produced its request segment.
func (recorder *Recorder) Open(ctx context.Context) error {
	if recorder.summary.EventCount == 0 {
		recorder.append(trajectory.Draft{
			ID: trajectory.NewID("evt"), Kind: trajectory.KindTrajectoryStart, Boundary: trajectory.BoundaryRoot,
			Actor: trajectory.ActorGateway, Principal: recorder.principal, At: recorder.call.CreatedAt,
			Capture: captureFor(trajectory.BoundaryRoot),
			Payload: trajectory.Start{
				ProjectID: recorder.call.ProjectID, AgentKeyID: recorder.call.AgentKeyID,
				SessionID: recorder.call.SessionID, Lane: recorder.call.Lane, Model: recorder.call.PublicModel,
				Operation: string(recorder.call.Operation), CoverageThreshold: recorder.CoverageThreshold,
				StartedAt: recorder.call.CreatedAt,
			},
		})
	}
	recorder.Flush(ctx)
	return recorder.writeFailure
}

func (recorder *Recorder) SetPolicy(version int64, hash string, rulesVersion int64, rulesHash string, rules []redact.Rule) {
	recorder.policyVersion, recorder.policyHash = version, hash
	recorder.rulesVersion, recorder.rulesHash, recorder.rules = rulesVersion, rulesHash, rules
	recorder.append(trajectory.Draft{
		ID: trajectory.NewID("evt"), Kind: trajectory.KindPolicyChange, Boundary: trajectory.BoundaryControl,
		Actor: trajectory.ActorGateway, Principal: recorder.principal, At: time.Now().UTC(),
		Capture: captureFor(trajectory.BoundaryControl),
		Payload: trajectory.PolicyChange{Version: version, Hash: hash, RulesVersion: rulesVersion, RulesHash: rulesHash, RedactionRules: trajectory.Marshal(rules)},
	})
}

// Propose opens the model action interval. requestSegment is the canonical client request stored as
// a content-addressed blob, so history can be rebuilt without copying it into the event.
func (recorder *Recorder) Propose(eventID, requestSegment string, inputs []trajectory.Ref) {
	recorder.request = requestSegment
	recorder.append(trajectory.Draft{
		ID: eventID, Kind: trajectory.KindActionProposed, Boundary: trajectory.BoundaryAction,
		Actor: trajectory.ActorGateway, Principal: recorder.principal, CallID: recorder.call.ID, ActionID: recorder.modelAction,
		At: recorder.call.CreatedAt, Inputs: inputs, Capture: captureFor(trajectory.BoundaryAction),
		Payload: trajectory.ActionProposed{
			ActionID: recorder.modelAction, ActionType: trajectory.ActionModel, CallID: recorder.call.ID,
			Operation: recorder.call.Operation, Purpose: recorder.call.Purpose, PurposeSource: recorder.call.PurposeSource,
			Lane: recorder.call.Lane, ClientFormat: recorder.call.ClientFormat, PublicModel: recorder.call.PublicModel,
			Stream: recorder.call.Stream, SessionID: recorder.call.SessionID, ParentCallID: recorder.call.ParentCallID,
			RequestSegment: requestSegment,
		},
	})
}

func (recorder *Recorder) ObserveRequest(items []model.Item, offset int, hits []trajectory.RedactionHit) []trajectory.Ref {
	refs := make([]trajectory.Ref, 0, len(items))
	labels := make([]trajectory.LabelRef, 0, len(items))
	eventID := trajectory.NewID("evt")
	for index := offset; index < len(items); index++ {
		item := items[index]
		integrity, confidentiality := labelsForItem(item)
		for partIndex := range item.Parts {
			part := &item.Parts[partIndex]
			if part.Text == "" {
				continue
			}
			segment := recorder.segment(eventID, []byte(part.Text), "text/plain", integrity, confidentiality)
			if segment != "" {
				refs = append(refs, trajectory.Ref{Segment: segment, Role: "observation"})
				labels = append(labels, trajectory.LabelRef{Segment: segment, Integrity: integrity, Confidentiality: confidentiality})
				recorder.markObserved(segment)
			}
		}
	}
	if len(refs) > 0 || len(hits) > 0 {
		integrity, confidentiality := strongestLabels(labels, trajectory.IntegrityTrusted, trajectory.ConfidentialityPrivate)
		recorder.append(trajectory.Draft{
			ID: eventID, Kind: trajectory.KindObservation, Boundary: trajectory.BoundaryIngress,
			Actor: trajectory.ActorGateway, Principal: recorder.principal, CallID: recorder.call.ID,
			At: time.Now().UTC(), Outputs: refs, Capture: captureFor(trajectory.BoundaryIngress),
			Payload: trajectory.Observation{Source: "client", Integrity: integrity, Confidentiality: confidentiality, Labels: labels, CallID: recorder.call.ID, Redactions: hits},
		})
	}
	return refs
}

func (recorder *Recorder) AssembleTools(tools []model.Tool) {
	if len(tools) == 0 {
		return
	}
	eventID := trajectory.NewID("evt")
	refs := []trajectory.Ref{}
	labels := []trajectory.LabelRef{}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
		if tool.Description == "" {
			continue
		}
		segment := recorder.segment(eventID, []byte(tool.Description), "text/plain", trajectory.IntegrityUntrusted, trajectory.ConfidentialityPublic)
		if segment != "" {
			refs = append(refs, trajectory.Ref{Segment: segment, Role: "tool_description"})
			labels = append(labels, trajectory.LabelRef{Segment: segment, Integrity: trajectory.IntegrityUntrusted, Confidentiality: trajectory.ConfidentialityPublic})
		}
	}
	recorder.append(trajectory.Draft{
		ID: eventID, Kind: trajectory.KindContextAssemble, Boundary: trajectory.BoundaryContext,
		Actor: trajectory.ActorGateway, Principal: recorder.principal, CallID: recorder.call.ID,
		At: time.Now().UTC(), Outputs: refs, Capture: captureFor(trajectory.BoundaryContext),
		Payload: trajectory.ContextAssemble{Kind: "tools", Segments: segmentIDs(refs), Labels: labels, Tools: names, CallID: recorder.call.ID},
	})
}

func (recorder *Recorder) ToolCall(toolCallID, kind, name, arguments string, target, sink string, inputSegments []string, blocked bool) {
	actionID := recorder.toolAction(toolCallID)
	argumentsHash := trajectory.ContentHash([]byte(arguments))
	recorder.append(trajectory.Draft{
		ID: trajectory.NewID("evt"), Kind: trajectory.KindActionProposed, Boundary: trajectory.BoundaryAction,
		Actor: trajectory.ActorModel, Principal: recorder.principal, CallID: recorder.call.ID, ActionID: actionID,
		At: time.Now().UTC(), Capture: captureFor(trajectory.BoundaryAction),
		Payload: trajectory.ActionProposed{
			ActionID: actionID, ActionType: trajectory.ActionTool, CallID: recorder.call.ID,
			ToolCallID: toolCallID, ToolName: name, ToolKind: kind, ArgumentsHash: argumentsHash,
			Target: target, Sink: sink, InputSegments: inputSegments,
			InvocationHash: trajectory.ContentHash([]byte(name + "\x00" + arguments + "\x00" + target)),
		},
	})
	if blocked {
		recorder.GateDecision("action", "block", true, false, "", "tool_call_blocked", "high", "the tool call was blocked", nil)
	}
}

func (recorder *Recorder) ToolResult(actionCallID, toolCallID string, isError bool, item model.Item) {
	eventID := trajectory.NewID("evt")
	segment := ""
	for _, part := range item.Parts {
		if part.Text != "" {
			segment = recorder.segment(eventID, []byte(part.Text), "text/plain", trajectory.IntegrityUntrusted, trajectory.ConfidentialityPrivate)
			break
		}
	}
	actionID := "act_tool_" + actionCallID + "_" + toolCallID
	refs := []trajectory.Ref{}
	if segment != "" {
		refs = append(refs, trajectory.Ref{Segment: segment, Role: "tool_result"})
		recorder.markObserved(segment)
	}
	recorder.append(trajectory.Draft{
		ID: eventID, Kind: trajectory.KindActionExecuted, Boundary: trajectory.BoundaryAction,
		Actor: trajectory.ActorTool, Principal: recorder.principal, CallID: actionCallID, ActionID: actionID,
		At: time.Now().UTC(), Outputs: refs, Capture: captureFor(trajectory.BoundaryAction),
		Payload: trajectory.ActionExecuted{
			ActionID: actionID, ActionType: trajectory.ActionTool, CallID: actionCallID,
			ErrorCode: toolErrorCode(isError), OutputSegments: segmentIDs(refs),
		},
	})
}

func toolErrorCode(isError bool) string {
	if isError {
		return "tool_error"
	}
	return ""
}

func (recorder *Recorder) AttemptProposed(channelID, channelName, modelID, upstreamModel, format string, attempt int) string {
	actionID := recorder.toolAction("attempt_" + time.Now().UTC().Format(time.RFC3339Nano))
	recorder.append(trajectory.Draft{
		ID: trajectory.NewID("evt"), Kind: trajectory.KindActionProposed, Boundary: trajectory.BoundaryAction,
		Actor: trajectory.ActorGateway, Principal: recorder.principal, CallID: recorder.call.ID, ActionID: actionID,
		At: time.Now().UTC(), Capture: captureFor(trajectory.BoundaryAction),
		Payload: trajectory.ActionProposed{
			ActionID: actionID, ActionType: trajectory.ActionProviderAttempt, CallID: recorder.call.ID, Attempt: attempt,
			ChannelID: channelID, ChannelName: channelName, ModelID: modelID, UpstreamModel: upstreamModel, Format: format,
		},
	})
	return actionID
}

func (recorder *Recorder) AttemptExecuted(actionID string, failureCode string) {
	if recorder.sealed {
		return
	}
	recorder.append(trajectory.Draft{
		ID: trajectory.NewID("evt"), Kind: trajectory.KindActionExecuted, Boundary: trajectory.BoundaryAction,
		Actor: trajectory.ActorGateway, Principal: recorder.principal, CallID: recorder.call.ID, ActionID: actionID,
		At: time.Now().UTC(), Capture: captureFor(trajectory.BoundaryAction),
		Payload: trajectory.ActionExecuted{ActionID: actionID, ActionType: trajectory.ActionProviderAttempt, CallID: recorder.call.ID, ErrorCode: failureCode},
	})
}

func (recorder *Recorder) ModelChange(routeID, routeName, resolution, publicModel, channelID, modelID, upstreamModel, format string) {
	if channelID != "" {
		recorder.call.ChannelID, recorder.call.ModelID, recorder.call.UpstreamModel = channelID, modelID, upstreamModel
	}
	if format != "" {
		recorder.call.UpstreamFormat = format
	}
	if routeID != "" {
		recorder.call.RouteID, recorder.call.Resolution = routeID, resolution
	}
	recorder.append(trajectory.Draft{
		ID: trajectory.NewID("evt"), Kind: trajectory.KindModelChange, Boundary: trajectory.BoundaryControl,
		Actor: trajectory.ActorGateway, Principal: recorder.principal, CallID: recorder.call.ID,
		At: time.Now().UTC(), Capture: captureFor(trajectory.BoundaryControl),
		Payload: trajectory.ModelChange{ModelID: modelID, Upstream: upstreamModel, ChannelID: channelID, RouteID: routeID, RouteName: routeName, Resolution: resolution, PublicModel: publicModel, Format: format},
	})
}

func (recorder *Recorder) GateDecision(gate, action string, blocked, terminal bool, ruleID, code, severity, reason string, checks []trajectory.Check) {
	recorder.append(trajectory.Draft{
		ID: trajectory.NewID("evt"), Kind: trajectory.KindGateDecision, Boundary: boundaryForGate(gate),
		Actor: trajectory.ActorGateway, Principal: recorder.principal, CallID: recorder.call.ID, ActionID: recorder.actionID,
		At: time.Now().UTC(), Checks: checks, Capture: captureFor(boundaryForGate(gate)),
		Payload: trajectory.GateDecision{
			DecisionID: trajectory.NewID("dec"), Gate: gate, Action: action, Blocked: blocked, Terminal: terminal,
			RuleID: ruleID, Code: code, Severity: severity, Reason: reason, CallID: recorder.call.ID, ActionID: recorder.actionID,
			CallState: recorder.call.State, Outcome: string(recorder.call.Outcome), ErrorCode: recorder.call.ErrorCode, Checks: checks,
		},
	})
}

func (recorder *Recorder) BindResponse(response *model.Response) string {
	if recorder.response != "" {
		return recorder.response
	}
	if recorder.finishEventID == "" {
		recorder.finishEventID = trajectory.NewID("evt")
	}
	payload, _ := json.Marshal(response)
	recorder.response = recorder.segment(recorder.finishEventID, payload, "application/json", trajectory.IntegrityTrusted, trajectory.ConfidentialityPrivate)
	return recorder.response
}

func (recorder *Recorder) Finish(ctx context.Context, state, outcome, code string, usage *model.Usage, cost *int64, currency string, duration int, firstToken int, losses []model.Loss, repairs []model.Repair, toolCallIDs []string, committed, partial, truncated, blocked bool, responseSegment string) error {
	if recorder.sealed {
		return recorder.writeFailure
	}
	recorder.sealed = true
	executed := trajectory.ActionExecuted{
		ActionID: recorder.modelAction, ActionType: trajectory.ActionModel, CallID: recorder.call.ID,
		State: state, Outcome: classify.Outcome(outcome), ErrorCode: code, DurationMS: duration, FirstTokenMS: firstToken,
		CostMicros: cost, Currency: currency, ResponseID: recorder.call.ResponseID, ResponseStore: recorder.call.ResponseStore,
		ConversationHash: recorder.call.ConversationHash, Losses: trajectoryLosses(losses), Repairs: trajectoryRepairs(repairs), ToolCallIDs: toolCallIDs,
		Committed: committed, Partial: partial, Truncated: truncated, Blocked: blocked, ResponseSegment: responseSegment,
	}
	if usage != nil {
		executed.Usage = trajectory.Usage{Input: usage.Input, Output: usage.Output, CacheRead: usage.CacheRead, CacheWrite: usage.CacheWrite, Reasoning: usage.Reasoning, Known: true}
	}
	outputs := []trajectory.Ref{}
	if responseSegment != "" {
		outputs = append(outputs, trajectory.Ref{Segment: responseSegment, Role: "response"})
	}
	finishEventID := recorder.finishEventID
	if finishEventID == "" {
		finishEventID = trajectory.NewID("evt")
	}
	recorder.append(trajectory.Draft{
		ID: finishEventID, Kind: trajectory.KindActionExecuted, Boundary: trajectory.BoundaryAction,
		Actor: trajectory.ActorModel, Principal: recorder.principal, CallID: recorder.call.ID, ActionID: recorder.modelAction,
		At: time.Now().UTC(), Outputs: outputs, Capture: captureFor(trajectory.BoundaryAction), Payload: executed,
	})
	if recorder.EndOnFinish || recorder.call.SessionID == "" {
		recorder.append(trajectory.Draft{
			ID: trajectory.NewID("evt"), Kind: trajectory.KindTrajectoryEnd, Boundary: trajectory.BoundaryRoot,
			Actor: trajectory.ActorGateway, Principal: recorder.principal, At: time.Now().UTC(),
			Capture: captureFor(trajectory.BoundaryRoot),
			Payload: trajectory.TrajectoryEnd{Outcome: outcome},
		})
	}
	recorder.Flush(ctx)
	return recorder.writeFailure
}

// Flush persists every queued event, segment and blob. A failure is sticky: no later path may
// report the action as recorded.
func (recorder *Recorder) Flush(ctx context.Context) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.writeFailure != nil || len(recorder.pending) == 0 {
		return
	}
	batch := trajectory.Batch{TrajectoryID: recorder.trajectoryID, ProjectID: recorder.call.ProjectID, Events: recorder.pending}
	for _, segment := range recorder.segments {
		batch.Segments = append(batch.Segments, segment)
	}
	for _, blob := range recorder.blobs {
		batch.Blobs = append(batch.Blobs, blob)
	}
	result, err := recorder.writer.Append(ctx, batch)
	if err != nil {
		recorder.logger.Error("trajectory events could not be appended", "trajectory", recorder.trajectoryID, "call", recorder.call.ID, "error", err)
		recorder.writeFailure = err
		return
	}
	recorder.summary = &result.Summary
	recorder.pending = nil
	recorder.segments = map[string]trajectory.Segment{}
	recorder.blobs = map[string]trajectory.Blob{}
}

func (recorder *Recorder) WriteFailure() error { return recorder.writeFailure }

func (recorder *Recorder) append(draft trajectory.Draft) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.pending = append(recorder.pending, draft)
}

func (recorder *Recorder) segment(eventID string, content []byte, mediaType, integrity, confidentiality string) string {
	if len(content) == 0 {
		return ""
	}
	id := trajectory.ContentHash(content)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if existing, found := recorder.segments[id]; found {
		if integrityRank(integrity) > integrityRank(existing.Integrity) {
			existing.Integrity = integrity
		}
		if confidentialityRank(confidentiality) > confidentialityRank(existing.Confidentiality) {
			existing.Confidentiality = confidentiality
		}
		recorder.segments[id] = existing
		return id
	}
	recorder.segments[id] = trajectory.Segment{
		ID: id, TrajectoryID: recorder.trajectoryID, MediaType: mediaType, BytesRef: id, Size: int64(len(content)),
		Integrity: integrity, Confidentiality: confidentiality, SourceEvent: eventID, CreatedAt: time.Now().UTC(),
	}
	recorder.blobs[id] = trajectory.Blob{Hash: id, Bytes: content, MediaType: mediaType, CreatedAt: time.Now().UTC()}
	return id
}

func (recorder *Recorder) Segment(eventID string, content []byte, mediaType, integrity, confidentiality string) string {
	return recorder.segment(eventID, content, mediaType, integrity, confidentiality)
}

func (recorder *Recorder) toolAction(toolCallID string) string {
	return "act_tool_" + recorder.call.ID + "_" + toolCallID
}

func captureFor(boundary trajectory.Boundary) trajectory.Capture {
	return trajectory.Capture{Recorder: "gateway", Placement: "gateway", Attested: false, Coverage: []trajectory.Boundary{boundary}}
}

func boundaryForGate(gate string) trajectory.Boundary {
	switch gate {
	case "ingress":
		return trajectory.BoundaryIngress
	case "release":
		return trajectory.BoundaryAction
	case "recovery":
		return trajectory.BoundaryRecovery
	default:
		return trajectory.BoundaryAction
	}
}

func labelsForItem(item model.Item) (string, string) {
	switch item.Kind {
	case model.ItemToolResult:
		return trajectory.IntegrityUntrusted, trajectory.ConfidentialityPrivate
	case model.ItemToolCall:
		return trajectory.IntegrityTrusted, trajectory.ConfidentialityPublic
	}
	switch item.Role {
	case model.RoleSystem, model.RoleDeveloper:
		return trajectory.IntegrityTrusted, trajectory.ConfidentialityPrivate
	case model.RoleUser:
		return trajectory.IntegrityTrusted, trajectory.ConfidentialityUserIdentity
	case model.RoleAssistant:
		return trajectory.IntegrityTrusted, trajectory.ConfidentialityPrivate
	}
	return trajectory.IntegrityUnknown, trajectory.ConfidentialityPrivate
}

func strongestLabels(labels []trajectory.LabelRef, integrity, confidentiality string) (string, string) {
	for _, label := range labels {
		if integrityRank(label.Integrity) > integrityRank(integrity) {
			integrity = label.Integrity
		}
		if confidentialityRank(label.Confidentiality) > confidentialityRank(confidentiality) {
			confidentiality = label.Confidentiality
		}
	}
	return integrity, confidentiality
}

func integrityRank(value string) int {
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

func confidentialityRank(value string) int {
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

func trajectoryLosses(losses []model.Loss) []trajectory.Loss {
	converted := make([]trajectory.Loss, 0, len(losses))
	for _, loss := range losses {
		converted = append(converted, trajectory.Loss{
			Path: loss.Path, Code: string(loss.Code), Class: string(loss.Class), Detail: loss.Detail,
			From: string(loss.From), To: string(loss.To), ItemKey: loss.ItemKey,
		})
	}
	return converted
}

func trajectoryRepairs(repairs []model.Repair) []trajectory.Repair {
	converted := make([]trajectory.Repair, 0, len(repairs))
	for _, repair := range repairs {
		converted = append(converted, trajectory.Repair{Kind: repair.Kind, Path: repair.Path, Detail: repair.Detail, ItemKey: repair.ItemKey})
	}
	return converted
}

func (recorder *Recorder) markObserved(segment string) {
	for _, existing := range recorder.observed {
		if existing == segment {
			return
		}
	}
	recorder.observed = append(recorder.observed, segment)
}

func segmentIDs(refs []trajectory.Ref) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.Segment)
	}
	return ids
}

func (recorder *Recorder) RequestApproval(actionID, invocationHash, reason string) string {
	approvalID := trajectory.NewID("apv")
	recorder.append(trajectory.Draft{
		ID: trajectory.NewID("evt"), Kind: trajectory.KindApprovalRequested, Boundary: trajectory.BoundaryAction,
		Actor: trajectory.ActorGateway, Principal: recorder.principal, CallID: recorder.call.ID, ActionID: actionID,
		At: time.Now().UTC(), Capture: captureFor(trajectory.BoundaryAction),
		Payload: trajectory.ApprovalRequested{ApprovalID: approvalID, ActionID: actionID, InvocationHash: invocationHash, ExpiresAt: time.Now().UTC().Add(15 * time.Minute), Scope: "tool_call", Reason: reason},
	})
	return approvalID
}

func (recorder *Recorder) ConsumeApproval(approvalID, invocationHash string, executionCount int64) {
	recorder.append(trajectory.Draft{
		ID: trajectory.NewID("evt"), Kind: trajectory.KindApprovalConsumed, Boundary: trajectory.BoundaryAction,
		Actor: trajectory.ActorGateway, Principal: recorder.principal, CallID: recorder.call.ID, ActionID: recorder.actionID,
		At: time.Now().UTC(), Capture: captureFor(trajectory.BoundaryAction),
		Payload: trajectory.ApprovalConsumed{ApprovalID: approvalID, InvocationHash: invocationHash, ExecutionCount: executionCount},
	})
}

func (recorder *Recorder) Authorize(actionID, approvalID, invocationHash string) {
	recorder.append(trajectory.Draft{
		ID: trajectory.NewID("evt"), Kind: trajectory.KindActionAuthorized, Boundary: trajectory.BoundaryAction,
		Actor: trajectory.ActorGateway, Principal: recorder.principal, CallID: recorder.call.ID, ActionID: actionID,
		At: time.Now().UTC(), Capture: captureFor(trajectory.BoundaryAction),
		Auth: &trajectory.Auth{Approval: approvalID, InvocationHash: invocationHash, ExecutionCount: 1},
		Payload: trajectory.ActionAuthorized{
			ActionID: actionID, Decision: "allow", ApprovalID: approvalID,
			InvocationHash: invocationHash, ExecutionCount: 1,
		},
	})
}
