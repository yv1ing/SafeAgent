package pipeline

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observation/adapter"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type storedExchange struct {
	Request  *model.Request
	Response *model.Response
}

// conversation loads the calls of one conversation, oldest first, from the canonical request and
// response segments referenced by their trajectory events.
func (pipeline *Pipeline) conversation(ctx context.Context, callID string) ([]adapter.Ancestor, bool, error) {
	chain, err := pipeline.repository.CallChain(ctx, callID)
	if err != nil {
		return nil, false, err
	}
	if len(chain) == 0 {
		return nil, false, repository.ErrNotFound
	}
	ids := make([]string, len(chain))
	projectID := chain[0].ProjectID
	for index, call := range chain {
		ids[index] = call.ID
	}
	events, err := pipeline.repository.EventsOfCalls(ctx, projectID, ids)
	if err != nil {
		return nil, false, err
	}
	exchanges := map[string]storedExchange{}
	for index := range events {
		event := &events[index]
		switch event.Kind {
		case trajectory.KindActionProposed:
			proposed, err := trajectory.DecodePayload[trajectory.ActionProposed](event)
			if err != nil || proposed.ActionType != trajectory.ActionModel || proposed.RequestSegment == "" {
				continue
			}
			payload, _, err := pipeline.repository.SegmentContent(ctx, event.TrajectoryID, proposed.RequestSegment)
			if err != nil {
				continue
			}
			request := new(model.Request)
			if err := json.Unmarshal(payload, request); err != nil {
				continue
			}
			exchange := exchanges[event.CallID]
			exchange.Request = request
			exchanges[event.CallID] = exchange
		case trajectory.KindActionExecuted:
			executed, err := trajectory.DecodePayload[trajectory.ActionExecuted](event)
			if err != nil || executed.ActionType != trajectory.ActionModel || executed.ResponseSegment == "" {
				continue
			}
			payload, _, err := pipeline.repository.SegmentContent(ctx, event.TrajectoryID, executed.ResponseSegment)
			if err != nil {
				continue
			}
			response := new(model.Response)
			if err := json.Unmarshal(payload, response); err != nil {
				continue
			}
			exchange := exchanges[event.CallID]
			exchange.Response = response
			exchanges[event.CallID] = exchange
		}
	}
	truncated := chain[0].ParentCallID != "" || chain[0].MessageOffset > 0
	entries := make([]adapter.Ancestor, 0, len(chain))
	for _, call := range chain {
		exchange, found := exchanges[call.ID]
		if !found || exchange.Request == nil {
			return nil, false, repository.ErrNotFound
		}
		entries = append(entries, adapter.Ancestor{Found: true, CallID: call.ID, Request: *exchange.Request, Response: exchange.Response})
	}
	entries, trimmed := adapter.TrimAncestors(entries)
	return entries, truncated || trimmed, nil
}

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
			request.Instructions = settings.Instructions
		}
	}
	return parent, offset, origins, nil
}

func (pipeline *Pipeline) parentSettings(ctx context.Context, callID string) (*model.Request, bool) {
	call, err := pipeline.repository.CallChain(ctx, callID)
	if err != nil || len(call) == 0 {
		return nil, false
	}
	events, err := pipeline.repository.EventsOfCalls(ctx, call[0].ProjectID, []string{callID})
	if err != nil {
		return nil, false
	}
	for index := len(events) - 1; index >= 0; index-- {
		event := &events[index]
		if event.Kind != trajectory.KindActionProposed {
			continue
		}
		proposed, err := trajectory.DecodePayload[trajectory.ActionProposed](event)
		if err != nil || proposed.RequestSegment == "" {
			continue
		}
		payload, _, err := pipeline.repository.SegmentContent(ctx, event.TrajectoryID, proposed.RequestSegment)
		if err != nil {
			continue
		}
		request := new(model.Request)
		if err := json.Unmarshal(payload, request); err != nil {
			continue
		}
		return request, true
	}
	return nil, false
}

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

func (pipeline *Pipeline) storedExchange(ctx context.Context, identity *Identity, call *entity.Call) (*storedExchange, error) {
	events, err := pipeline.repository.EventsOfCalls(ctx, identity.Project.ID, []string{call.ID})
	if err != nil {
		return nil, err
	}
	exchange := new(storedExchange)
	for index := range events {
		event := &events[index]
		switch event.Kind {
		case trajectory.KindActionProposed:
			proposed, err := trajectory.DecodePayload[trajectory.ActionProposed](event)
			if err != nil || proposed.RequestSegment == "" {
				continue
			}
			payload, _, err := pipeline.repository.SegmentContent(ctx, event.TrajectoryID, proposed.RequestSegment)
			if err != nil {
				continue
			}
			exchange.Request = new(model.Request)
			if err := json.Unmarshal(payload, exchange.Request); err != nil {
				exchange.Request = nil
			}
		case trajectory.KindActionExecuted:
			executed, err := trajectory.DecodePayload[trajectory.ActionExecuted](event)
			if err != nil || executed.ResponseSegment == "" {
				continue
			}
			payload, _, err := pipeline.repository.SegmentContent(ctx, event.TrajectoryID, executed.ResponseSegment)
			if err != nil {
				continue
			}
			exchange.Response = new(model.Response)
			if err := json.Unmarshal(payload, exchange.Response); err != nil {
				exchange.Response = nil
			}
		}
	}
	return exchange, nil
}
