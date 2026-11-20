package gateway

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const maximumHistoryBytes = 32 << 20

// transcript returns the items a conversation holds up to and including the call: each ancestor's
// added items followed by its response. The final call's own response is excluded because the
// request being built continues after it.
func (pipeline *Pipeline) transcript(ctx context.Context, callID string) ([]protocol.Item, bool, error) {
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
		switch event.Kind {
		case auditevent.KindRequest:
			var record auditevent.Request
			if err := json.Unmarshal(event.Payload, &record); err != nil {
				return nil, false, err
			}
			requests[event.CallID] = record
		case auditevent.KindResponse:
			var record auditevent.Response
			if err := json.Unmarshal(event.Payload, &record); err != nil {
				return nil, false, err
			}
			responses[event.CallID] = record
		}
	}
	var items []protocol.Item
	truncated := chain[0].ParentCallID != "" || chain[0].MessageOffset > 0
	total := 0
	for _, call := range chain {
		request, found := requests[call.ID]
		if !found {
			return nil, false, repository.ErrNotFound
		}
		if total+len(protocol.Marshal(request.Items)) > maximumHistoryBytes {
			return items, true, nil
		}
		total += len(protocol.Marshal(request.Items))
		items = append(items, request.Items...)
		if response, found := responses[call.ID]; found && len(response.Response.Items) > 0 {
			if total+len(protocol.Marshal(response.Response.Items)) > maximumHistoryBytes {
				return items, true, nil
			}
			total += len(protocol.Marshal(response.Response.Items))
			items = append(items, response.Response.Items...)
		}
	}
	return items, truncated, nil
}

// attachHistory expands previous_response_id into the stored conversation so the upstream receives
// one self-contained request. It returns the parent call.
func (pipeline *Pipeline) attachHistory(ctx context.Context, agentKeyID string, request *protocol.Request) (*entity.Call, int, *protocol.Error) {
	parent, err := pipeline.repository.CallByResponseID(ctx, agentKeyID, request.PreviousResponseID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, 0, failure("invalid_request", protocol.ClassInvalid, "previous_response_not_found", "The previous response is unknown or has expired.", 404)
		}
		return nil, 0, internal("store_failure", "The previous response could not be loaded.")
	}
	history, truncated, err := pipeline.transcript(ctx, parent.ID)
	if err != nil || truncated {
		if errors.Is(err, repository.ErrNotFound) || truncated {
			return nil, 0, failure("invalid_request", protocol.ClassInvalid, "previous_response_not_found", "Part of the previous conversation is unavailable.", 404)
		}
		pipeline.logger.Error("previous response could not be read", "error", err)
		return nil, 0, internal("store_failure", "The previous response could not be loaded.")
	}
	offset := len(history)
	if !protocol.RepeatsPrefix(request.Items, history) {
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
	return parent, offset, nil
}

// parentSettings returns the effective tools and instructions of a stored call. A continuation that
// names a previous response but repeats only the new input still needs the tools and system prompt
// the earlier turn used.
func (pipeline *Pipeline) parentSettings(ctx context.Context, callID string) (auditevent.Request, bool) {
	events, err := pipeline.repository.EventsOfKinds(ctx, []string{callID}, auditevent.KindRequest)
	if err != nil || len(events) == 0 {
		return auditevent.Request{}, false
	}
	var record auditevent.Request
	if err := json.Unmarshal(events[len(events)-1].Payload, &record); err != nil {
		return auditevent.Request{}, false
	}
	return record, true
}

// link finds the call this one continues when the client repeats its history: first by the id of
// the latest tool result, then by the fingerprint of the history the client repeated. The returned
// offset is the number of leading items the parent conversation already holds; only a verified
// prefix is returned, so recording the tail never drops what the client sent.
func (pipeline *Pipeline) link(ctx context.Context, identity *Identity, request *protocol.Request) (*entity.Call, int) {
	if id := latestToolResult(request); id != "" {
		if found, err := pipeline.repository.CallByToolCall(ctx, identity.Project.ID, identity.AgentKey.ID, id); err == nil {
			if history, truncated, err := pipeline.transcript(ctx, found.ID); err == nil && !truncated && protocol.RepeatsPrefix(request.Items, history) {
				return found, len(history)
			}
		}
	}
	if prefix := protocol.HistoryPrefix(pipeline.secret, request.Items); prefix != "" {
		if found, err := pipeline.repository.CallByConversationHash(ctx, identity.AgentKey.ID, prefix); err == nil {
			if history, truncated, err := pipeline.transcript(ctx, found.ID); err == nil && !truncated && protocol.RepeatsPrefix(request.Items, history) {
				return found, len(history)
			}
		}
	}
	return nil, 0
}

func latestToolResult(request *protocol.Request) string {
	for index := len(request.Items) - 1; index >= 0; index-- {
		item := &request.Items[index]
		if item.Kind == protocol.ItemToolResult && item.CallID != "" {
			return item.CallID
		}
	}
	return ""
}
