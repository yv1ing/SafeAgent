package pipeline

import (
	"context"
	"errors"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observation/adapter"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// conversation loads the calls of one conversation, oldest first, with the request items each call
// added, its response and the id of the call that stored them. It is the only reader of ancestor
// conversation on the request path; the adapter turns the entries into history items and origins,
// which is the same expansion the replay path performs.
func (pipeline *Pipeline) conversation(ctx context.Context, callID string) ([]adapter.Ancestor, bool, error) {
	chain, err := pipeline.repository.CallChain(ctx, callID)
	if err != nil {
		return nil, false, err
	}
	ids := make([]string, len(chain))
	for index, call := range chain {
		ids[index] = call.ID
	}
	events, err := pipeline.repository.EventsOfKinds(ctx, ids, auditevent.KindRequest, auditevent.KindResponse)
	if err != nil {
		return nil, false, err
	}
	requests := map[string]auditevent.Request{}
	responses := map[string]auditevent.Response{}
	for _, event := range events {
		decoded, err := auditevent.Decode(event.Kind, event.Payload)
		if err != nil {
			return nil, false, err
		}
		switch event.Kind {
		case auditevent.KindRequest:
			if decoded.Request != nil {
				requests[event.CallID] = *decoded.Request
			}
		case auditevent.KindResponse:
			if decoded.Response != nil {
				responses[event.CallID] = *decoded.Response
			}
		}
	}
	truncated := chain[0].ParentCallID != "" || chain[0].MessageOffset > 0
	entries := make([]adapter.Ancestor, 0, len(chain))
	for _, call := range chain {
		request, found := requests[call.ID]
		if !found {
			return nil, false, repository.ErrNotFound
		}
		entry := adapter.Ancestor{Found: true, CallID: call.ID, Request: request}
		if response, found := responses[call.ID]; found {
			copied := response
			entry.Response = &copied
		}
		entries = append(entries, entry)
	}
	entries, trimmed := adapter.TrimAncestors(entries)
	return entries, truncated || trimmed, nil
}

// historyOf returns the flattened ancestor conversation of a call together with one origin per
// item. A non-empty truncated flag means the history is incomplete and a continuation must not
// pretend otherwise.
func (pipeline *Pipeline) historyOf(ctx context.Context, callID string) ([]model.Item, []observation.Locator, bool, error) {
	entries, truncated, err := pipeline.conversation(ctx, callID)
	if err != nil {
		return nil, nil, false, err
	}
	if truncated {
		return nil, nil, true, nil
	}
	history, origins, missing := adapter.AncestorItems(entries)
	if missing {
		return nil, nil, true, nil
	}
	return history, origins, false, nil
}

// attachHistory expands previous_response_id into the stored conversation so the upstream receives
// one self-contained request. It returns the parent call and the origins aligned with the leading
// items the client did not add itself.
func (pipeline *Pipeline) attachHistory(ctx context.Context, agentKeyID string, request *model.Request) (*entity.Call, int, []observation.Locator, *model.Error) {
	parent, err := pipeline.repository.CallByResponseID(ctx, agentKeyID, request.PreviousResponseID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, 0, nil, failure("invalid_request", model.ClassInvalid, "previous_response_not_found", "The previous response is unknown or has expired.", 404)
		}
		return nil, 0, nil, internal("store_failure", "The previous response could not be loaded.")
	}
	history, origins, truncated, err := pipeline.historyOf(ctx, parent.ID)
	if err != nil || truncated {
		if errors.Is(err, repository.ErrNotFound) || truncated {
			return nil, 0, nil, failure("invalid_request", model.ClassInvalid, "previous_response_not_found", "Part of the previous conversation is unavailable.", 404)
		}
		pipeline.logger.Error("previous response could not be read", "error", err)
		return nil, 0, nil, internal("store_failure", "The previous response could not be loaded.")
	}
	offset := len(history)
	if !model.RepeatsPrefix(request.Items, history) {
		request.Items = append(history, request.Items...)
	}
	for index := range request.Items {
		request.Items[index].Ordinal = index
	}
	if settings, ok := pipeline.parentSettings(ctx, parent.ID); ok {
		if len(request.Tools) == 0 {
			request.Tools = settings.Tools
		}
		if request.Instructions == "" {
			request.Instructions = settings.Params.Instructions
		}
	}
	return parent, offset, origins, nil
}

// parentSettings returns the effective tools and instructions of a stored call. A continuation that
// names a previous response but repeats only the new input still needs the tools and system prompt
// the earlier turn used.
func (pipeline *Pipeline) parentSettings(ctx context.Context, callID string) (auditevent.Request, bool) {
	events, err := pipeline.repository.EventsOfKinds(ctx, []string{callID}, auditevent.KindRequest)
	if err != nil || len(events) == 0 {
		return auditevent.Request{}, false
	}
	decoded, err := auditevent.Decode(events[len(events)-1].Kind, events[len(events)-1].Payload)
	if err != nil || decoded.Request == nil {
		return auditevent.Request{}, false
	}
	return *decoded.Request, true
}

// link finds the call this one continues when the client repeats its history: first by the id of
// the latest tool result, then by the fingerprint of the history the client repeated. The returned
// offset is the number of leading items the parent conversation already holds; only a verified
// prefix is returned, so recording the tail never drops what the client sent.
func (pipeline *Pipeline) link(ctx context.Context, identity *Identity, request *model.Request) (*entity.Call, []observation.Locator, int, string) {
	if id := latestToolResult(request); id != "" {
		if found, err := pipeline.repository.CallByToolCall(ctx, identity.Project.ID, identity.AgentKey.ID, id); err == nil {
			if history, origins, truncated, err := pipeline.historyOf(ctx, found.ID); err == nil && !truncated && model.RepeatsPrefix(request.Items, history) {
				return found, origins, len(history), "tool_call"
			}
		}
	}
	if prefix := model.HistoryPrefix(pipeline.secret, request.Items); prefix != "" {
		if found, err := pipeline.repository.CallByConversationHash(ctx, identity.AgentKey.ID, prefix); err == nil {
			if history, origins, truncated, err := pipeline.historyOf(ctx, found.ID); err == nil && !truncated && model.RepeatsPrefix(request.Items, history) {
				return found, origins, len(history), "conversation_prefix"
			}
		}
	}
	return nil, nil, 0, ""
}

func latestToolResult(request *model.Request) string {
	for index := len(request.Items) - 1; index >= 0; index-- {
		item := &request.Items[index]
		if item.Kind == model.ItemToolResult && item.CallID != "" {
			return item.CallID
		}
	}
	return ""
}
