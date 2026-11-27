package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

// auxiliaryCall is the event stream of one auxiliary endpoint call. It is a standalone trajectory:
// it never joins an agent session and ends with the call.
type auxiliaryCall struct {
	recorder  *Recorder
	project   *entity.Project
	principal string
	rules     []redact.Rule
	scope     *redact.Scope
	started   time.Time
}

func (pipeline *Pipeline) beginAuxiliary(ctx context.Context, identity *Identity, operation classify.Operation, input Request, publicModel string) (*auxiliaryCall, *model.Error) {
	if !operation.Valid() {
		return nil, internal("unsupported_operation", "The auxiliary operation is not recognized.")
	}
	classification := classify.Classify(operation, classify.Hints{Header: input.Header})
	callID := pipeline.ids()
	now := time.Now().UTC()
	record := &entity.Call{
		ID: callID, ProjectID: identity.Project.ID, AgentKeyID: identity.AgentKey.ID,
		ClientFormat: string(input.Kind), Operation: classification.Operation,
		Purpose: classification.Purpose, PurposeSource: classification.PurposeSource, Lane: classification.Lane,
		ClientTransport: "json", PublicModel: publicModel, State: "running", CreatedAt: now, UpdatedAt: now,
	}
	summary, err := pipeline.repository.EnsureTrajectory(ctx, identity.Project.ID, identity.AgentKey.ID, "", "", identity.Project.CoverageThreshold)
	if err != nil {
		pipeline.logger.Error("auxiliary trajectory could not be opened", "operation", operation, "error", err)
		return nil, internal("audit_unavailable", "The call could not be recorded.")
	}
	record.TrajectoryID = summary.ID
	recorder := NewRecorder(pipeline.repository, pipeline.logger, record, summary, identity.AgentKey.ID)
	recorder.EndOnFinish = true
	recorder.CoverageThreshold = identity.Project.CoverageThreshold
	if err := recorder.Open(ctx); err != nil {
		pipeline.logger.Error("auxiliary call could not be recorded", "operation", operation, "error", err)
		return nil, internal("audit_unavailable", "The call could not be recorded.")
	}
	engine, rules, err := pipeline.redactionEngine(ctx, identity.Project)
	if err != nil {
		failure := internal("rules_unavailable", "The redaction rules are unavailable.")
		_ = pipeline.endAuxiliary(ctx, &auxiliaryCall{recorder: recorder, project: identity.Project, started: now}, failure)
		return nil, failure
	}
	policy, version, err := pipeline.compiledPolicy(ctx, identity.Project.ID)
	if err == nil {
		rulesJSON, _ := json.Marshal(rules)
		recorder.SetPolicy(version, policy.Hash(), identity.Project.RulesVersion, trajectory.ContentHash(rulesJSON), rules)
	}
	return &auxiliaryCall{
		recorder: recorder, project: identity.Project, rules: rules, scope: engine.NewScope(),
		principal: identity.AgentKey.ID, started: now,
	}, nil
}

func (auxiliary *auxiliaryCall) recordRequest(items []model.Item, params model.Request, body []byte, hits []trajectory.RedactionHit) {
	eventID := trajectory.NewID("evt")
	var payload []byte
	mediaType := "application/json"
	if len(body) > 0 {
		payload = body
	} else {
		payload, _ = json.Marshal(params)
	}
	segment := auxiliary.recorder.Segment(eventID, payload, mediaType, trajectory.IntegrityTrusted, trajectory.ConfidentialityPrivate)
	inputs := []trajectory.Ref{}
	if segment != "" {
		inputs = append(inputs, trajectory.Ref{Segment: segment, Role: "request"})
	}
	auxiliary.recorder.Propose(eventID, segment, inputs)
	auxiliary.recorder.ObserveRequest(items, 0, hits)
	auxiliary.recorder.AssembleTools(params.Tools)
}

func (pipeline *Pipeline) beginLifecycle(ctx context.Context, identity *Identity, operation classify.Operation, responseID string) (*auxiliaryCall, *model.Error) {
	auxiliary, failure := pipeline.beginAuxiliary(ctx, identity, operation, Request{Kind: model.Responses, Operation: operation}, "")
	if failure != nil {
		return nil, failure
	}
	body := model.Marshal(map[string]string{"response_id": responseID})
	auxiliary.recordRequest(nil, model.Request{Source: model.Responses}, body, nil)
	return auxiliary, nil
}

func (pipeline *Pipeline) endAuxiliary(ctx context.Context, auxiliary *auxiliaryCall, failure *model.Error) *model.Error {
	if auxiliary == nil {
		return failure
	}
	record := auxiliary.recorder.Call()
	settled := settlementFor(failure, classify.LaneAuxiliary)
	record.Outcome = settled.outcome
	record.State = settled.state
	record.DurationMilliseconds = int(time.Since(auxiliary.started).Milliseconds())
	code := ""
	if failure != nil {
		code = failure.Code
		record.ErrorCode = code
	}
	err := auxiliary.recorder.Finish(ctx, settled.state, string(settled.outcome), code, nil, nil, "", record.DurationMilliseconds, 0, nil, nil, nil, false, false, false, failure != nil && failure.Class == model.ClassPolicy, "")
	if err != nil {
		pipeline.logger.Error("auxiliary call could not be recorded", "call", record.ID, "error", err)
		return internal("audit_unavailable", "The auxiliary call could not be recorded.")
	}
	return failure
}

func (pipeline *Pipeline) recordAuxiliary(ctx context.Context, identity *Identity, operation classify.Operation, input Request, items []model.Item, modelName string, body []byte, failure *model.Error) *model.Error {
	auxiliary, auditFailure := pipeline.beginAuxiliary(ctx, identity, operation, input, modelName)
	if auditFailure != nil {
		return auditFailure
	}
	auxiliary.recordRequest(items, model.Request{Source: input.Kind, Model: modelName}, body, nil)
	return pipeline.endAuxiliary(ctx, auxiliary, failure)
}

func rejectAuxiliary(code, message string) *model.Error {
	return failure("unsupported", model.ClassInvalid, code, message, http.StatusUnprocessableEntity)
}

func responseNotFound() *model.Error {
	return failure("invalid_request", model.ClassInvalid, "response_not_found", "The response is unknown or has expired.", http.StatusNotFound)
}
