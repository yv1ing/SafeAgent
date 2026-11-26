package pipeline

import (
	"context"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// auxiliaryCall is the audit record of one auxiliary endpoint call. An auxiliary call is protocol
// traffic, not agent activity: it never joins a conversation, is never analyzed, and its redaction
// snapshot is still written so a replay shows what the endpoint received.
type auxiliaryCall struct {
	recorder  *Recorder
	project   *entity.Project
	principal auditevent.Principal
	rules     []redact.Rule
	scope     *redact.Scope
}

// beginAuxiliary classifies and opens the audit record before any upstream egress. A failure to
// record refuses the call, matching the generation path.
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
		ClientTransport: "json", PublicModel: publicModel, State: "running",
		AnalysisState: "skipped", CreatedAt: now, UpdatedAt: now,
	}
	recorder := NewRecorder(pipeline.repository, pipeline.logger, pipeline.secret, record)
	if err := recorder.Open(ctx); err != nil {
		pipeline.logger.Error("auxiliary call could not be recorded", "operation", operation, "error", err)
		return nil, internal("audit_unavailable", "The call could not be recorded.")
	}
	engine, rules, err := pipeline.redactionEngine(ctx, identity.Project)
	if err != nil {
		failure := internal("rules_unavailable", "The redaction rules are unavailable.")
		_ = pipeline.endAuxiliary(ctx, &auxiliaryCall{recorder: recorder, project: identity.Project}, failure)
		return nil, failure
	}
	return &auxiliaryCall{
		recorder: recorder, project: identity.Project, rules: rules, scope: engine.NewScope(),
		principal: auditevent.Principal{AgentKeyID: identity.AgentKey.ID, Role: identity.AgentKey.Role, TrustLevel: identity.AgentKey.TrustLevel},
	}, nil
}

// recordRequest stores the request payload of an auxiliary call with the hits that belong to it.
func (auxiliary *auxiliaryCall) recordRequest(items []model.Item, params model.Request, body []byte, hits []auditevent.Hit) {
	auxiliary.recorder.Event(auditevent.KindRequest, auditevent.Request{
		Items: nonNil(items), Params: params, Body: body, Hits: nonNil(hits),
		Principal: &auxiliary.principal,
	})
}

// beginLifecycle opens the audit record of a Responses lifecycle endpoint. The endpoints take no body,
// but they still name the response they address.
func (pipeline *Pipeline) beginLifecycle(ctx context.Context, identity *Identity, operation classify.Operation, responseID string) (*auxiliaryCall, *model.Error) {
	auxiliary, failure := pipeline.beginAuxiliary(ctx, identity, operation, Request{Kind: model.Responses, Operation: operation}, "")
	if failure != nil {
		return nil, failure
	}
	body := model.Marshal(map[string]string{"response_id": responseID})
	auxiliary.recordRequest(nil, model.Request{Source: model.Responses}, body, nil)
	return auxiliary, nil
}

// endAuxiliary seals an auxiliary call with the one settlement mapping. The HTTP protocol error, if
// any, is still returned by the caller: the call row and the response tell the same story.
func (pipeline *Pipeline) endAuxiliary(ctx context.Context, auxiliary *auxiliaryCall, failure *model.Error) *model.Error {
	if auxiliary == nil {
		return failure
	}
	record := auxiliary.recorder.Call()
	settled := settlementFor(failure, classify.LaneAuxiliary)
	record.Outcome = settled.outcome
	code := ""
	if failure != nil {
		code = failure.Code
	}
	auxiliary.recorder.SetAnalysis(sealAnalysis(record, classify.LaneAuxiliary, auxiliary.project, auxiliary.rules, nil))
	if err := auxiliary.recorder.Seal(ctx, settled.state, code); err != nil {
		pipeline.logger.Error("auxiliary call could not be sealed", "call", record.ID, "error", err)
		return internal("audit_unavailable", "The auxiliary call could not be recorded.")
	}
	return failure
}

// recordAuxiliary opens, records and seals a pure bookkeeping auxiliary call (a model listing or a
// model probe). It performs no redaction because it carries no content beyond the model name.
func (pipeline *Pipeline) recordAuxiliary(ctx context.Context, identity *Identity, operation classify.Operation, input Request, items []model.Item, modelName string, body []byte, failure *model.Error) *model.Error {
	auxiliary, auditFailure := pipeline.beginAuxiliary(ctx, identity, operation, input, modelName)
	if auditFailure != nil {
		return auditFailure
	}
	auxiliary.recordRequest(items, model.Request{Source: input.Kind, Model: modelName}, body, nil)
	return pipeline.endAuxiliary(ctx, auxiliary, failure)
}

// rejectAuxiliary records the refusal of an auxiliary endpoint for a reason the endpoint itself
// reports and returns the protocol error unchanged.
func rejectAuxiliary(code, message string) *model.Error {
	return failure("unsupported", model.ClassInvalid, code, message, http.StatusUnprocessableEntity)
}

func responseNotFound() *model.Error {
	return failure("invalid_request", model.ClassInvalid, "response_not_found", "The response is unknown or has expired.", http.StatusNotFound)
}
