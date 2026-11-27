package pipeline

import (
	"context"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/responses"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// RetrieveResponse returns a stored Responses response and audits the lifecycle call.
func (pipeline *Pipeline) RetrieveResponse(ctx context.Context, key, responseID string) ([]byte, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, nil, key)
	if denied != nil {
		return nil, denied
	}
	defer release()
	auxiliary, auditFailure := pipeline.beginLifecycle(ctx, identity, classify.ResponsesRetrieve, responseID)
	if auditFailure != nil {
		return nil, auditFailure
	}
	response, _, failure := pipeline.storedResponse(ctx, identity, responseID)
	if failure == nil {
		var payload []byte
		payload, failure = pipeline.EncodeStoredResponse(response)
		if failure == nil {
			if refusal := pipeline.endAuxiliary(ctx, auxiliary, nil); refusal != nil {
				return nil, refusal
			}
			return payload, nil
		}
	}
	return nil, pipeline.endAuxiliary(ctx, auxiliary, failure)
}

// InputItems returns the stored input items and the losses of the flat encoding.
func (pipeline *Pipeline) InputItems(ctx context.Context, key, responseID string) ([]any, []model.Loss, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, nil, key)
	if denied != nil {
		return nil, nil, denied
	}
	defer release()
	auxiliary, auditFailure := pipeline.beginLifecycle(ctx, identity, classify.ResponsesInputItems, responseID)
	if auditFailure != nil {
		return nil, nil, auditFailure
	}
	_, items, failure := pipeline.storedResponse(ctx, identity, responseID)
	if failure != nil {
		return nil, nil, pipeline.endAuxiliary(ctx, auxiliary, failure)
	}
	encoded, losses, err := responses.EncodeInputItems(items)
	if err != nil {
		failure := internal("encode_failed", "The input items could not be encoded.")
		return nil, nil, pipeline.endAuxiliary(ctx, auxiliary, failure)
	}
	if refusal := pipeline.endAuxiliary(ctx, auxiliary, nil); refusal != nil {
		return nil, nil, refusal
	}
	return encoded, losses, nil
}

// DeleteResponse soft-deletes a stored response.
func (pipeline *Pipeline) DeleteResponse(ctx context.Context, key, responseID string) *model.Error {
	identity, release, denied := pipeline.authorize(ctx, nil, key)
	if denied != nil {
		return denied
	}
	defer release()
	auxiliary, auditFailure := pipeline.beginLifecycle(ctx, identity, classify.ResponsesDelete, responseID)
	if auditFailure != nil {
		return auditFailure
	}
	if err := pipeline.repository.DeleteResponse(ctx, identity.AgentKey.ID, responseID); err != nil {
		return pipeline.endAuxiliary(ctx, auxiliary, responseNotFound())
	}
	return pipeline.endAuxiliary(ctx, auxiliary, nil)
}

// CancelResponse aborts an in-flight response.
func (pipeline *Pipeline) CancelResponse(ctx context.Context, key, responseID string) (bool, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, nil, key)
	if denied != nil {
		return false, denied
	}
	defer release()
	auxiliary, auditFailure := pipeline.beginLifecycle(ctx, identity, classify.ResponsesCancel, responseID)
	if auditFailure != nil {
		return false, auditFailure
	}
	call, err := pipeline.repository.CallByResponseID(ctx, identity.AgentKey.ID, responseID)
	if err != nil {
		return false, pipeline.endAuxiliary(ctx, auxiliary, responseNotFound())
	}
	if call.State != "running" {
		return false, pipeline.endAuxiliary(ctx, auxiliary, nil)
	}
	cancelled := pipeline.inflight.Cancel(call.ID)
	if cancelled {
		if err := pipeline.repository.MarkCallCancelled(ctx, call.ID); err != nil {
			pipeline.logger.Error("cancelled call could not be marked", "call", call.ID, "error", err)
		}
	}
	if failure := pipeline.endAuxiliary(ctx, auxiliary, nil); failure != nil {
		return cancelled, failure
	}
	return cancelled, nil
}

// registerInflight lets CancelResponse reach a running call.
func (state *call) registerInflight(cancel context.CancelFunc) {
	state.pipeline.inflight.register(state.recorder.Call().ID, cancel)
}

// storedResponse reads the request and response payloads of a stored Responses call.
func (pipeline *Pipeline) storedResponse(ctx context.Context, identity *Identity, responseID string) (*model.Response, []model.Item, *model.Error) {
	call, err := pipeline.repository.CallByResponseID(ctx, identity.AgentKey.ID, responseID)
	if err != nil {
		return nil, nil, responseNotFound()
	}
	if !call.ResponseStore {
		return nil, nil, responseNotFound()
	}
	exchange, err := pipeline.storedExchange(ctx, identity, call)
	if err != nil {
		return nil, nil, internal("store_failure", "The response could not be loaded.")
	}
	if exchange.Response == nil {
		return nil, nil, responseNotFound()
	}
	var input []model.Item
	if exchange.Request != nil {
		input = exchange.Request.Items
	}
	return exchange.Response, input, nil
}

// EncodeStoredResponse renders a stored response in the Responses wire shape.
func (pipeline *Pipeline) EncodeStoredResponse(response *model.Response) ([]byte, *model.Error) {
	coder, found := pipeline.codecs.Get(model.Responses)
	if !found {
		return nil, internal("unsupported_format", "The protocol is not supported.")
	}
	peer := codec.Peer{Side: codec.Client, Vendor: model.VendorOpenAI, Model: response.Meta.Model, Envelope: Envelope{Secret: pipeline.secret}}
	payload, _, err := coder.EncodeResponse(response, peer)
	if err != nil {
		return nil, internal("encode_failed", "The response could not be encoded.")
	}
	return payload, nil
}
