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

// responseID is the identifier clients see for a call; it is also what previous_response_id and
// the stream's message id carry, so every code path must build it here.
func responseID(kind protocol.Format, callID string) string {
	switch kind {
	case protocol.Responses:
		return "resp_" + callID
	case protocol.Messages:
		return "msg_" + callID
	case protocol.Gemini:
		return callID
	}
	return "chatcmpl-" + callID
}

// transcript returns the messages a conversation holds up to and including the call: each
// ancestor's added messages followed by its response. The final call's own response is excluded
// because the request being built continues after it.
func (pipeline *Pipeline) transcript(ctx context.Context, callID string) ([]protocol.Message, bool, error) {
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
	var messages []protocol.Message
	truncated := chain[0].ParentCallID != "" || chain[0].MessageOffset > 0
	total := 0
	for _, call := range chain {
		request, found := requests[call.ID]
		if !found {
			return nil, false, repository.ErrNotFound
		}
		if total+len(protocol.Marshal(request.Messages)) > maximumHistoryBytes {
			return messages, true, nil
		}
		total += len(protocol.Marshal(request.Messages))
		messages = append(messages, request.Messages...)
		if response, found := responses[call.ID]; found && len(response.Response.Blocks) > 0 {
			message := protocol.Message{Role: protocol.RoleAssistant, Blocks: response.Response.Blocks}
			if total+len(protocol.Marshal(message)) > maximumHistoryBytes {
				return messages, true, nil
			}
			total += len(protocol.Marshal(message))
			messages = append(messages, message)
		}
	}
	return messages, truncated, nil
}

// attachHistory expands previous_response_id into the stored conversation so the upstream receives
// one self-contained request. It returns the parent call.
func (pipeline *Pipeline) attachHistory(ctx context.Context, agentKeyID string, request *protocol.Request) (*entity.Call, *protocol.Error) {
	parent, err := pipeline.repository.CallByResponseID(ctx, agentKeyID, request.PreviousResponseID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, failure("invalid_request", protocol.ClassInvalid, "previous_response_not_found", "The previous response is unknown or has expired.", 404)
		}
		return nil, internal("store_failure", "The previous response could not be loaded.")
	}
	history, truncated, err := pipeline.transcript(ctx, parent.ID)
	if err != nil || truncated {
		if errors.Is(err, repository.ErrNotFound) || truncated {
			return nil, failure("invalid_request", protocol.ClassInvalid, "previous_response_not_found", "Part of the previous conversation is unavailable.", 404)
		}
		pipeline.logger.Error("previous response could not be read", "error", err)
		return nil, internal("store_failure", "The previous response could not be loaded.")
	}
	request.Messages = append(history, request.Messages...)
	return parent, nil
}

// link finds the call this one continues when the client repeats its history: first by the id of
// the latest tool result, then by the fingerprint of the history the client repeated. The returned
// offset is the number of leading messages the parent conversation already holds; only a verified
// prefix is returned, so recording the tail never drops what the client sent.
func (pipeline *Pipeline) link(ctx context.Context, identity *Identity, request *protocol.Request) (*entity.Call, int) {
	if id := latestToolResult(request); id != "" {
		if found, err := pipeline.repository.CallByToolCall(ctx, identity.Project.ID, identity.AgentKey.ID, id); err == nil {
			if history, truncated, err := pipeline.transcript(ctx, found.ID); err == nil && !truncated && protocol.RepeatsPrefix(request.Messages, history) {
				return found, len(history)
			}
		}
	}
	if prefix := protocol.HistoryPrefix(pipeline.secret, request.Messages); prefix != "" {
		if found, err := pipeline.repository.CallByConversationHash(ctx, identity.AgentKey.ID, prefix); err == nil {
			// The hash is a projection and can ignore blocks, so the projected match alone is
			// not enough. Verify the full transcript before choosing the offset; otherwise a
			// reasoning-only assistant turn would be cut from the stored request.
			if history, truncated, err := pipeline.transcript(ctx, found.ID); err == nil && !truncated && protocol.RepeatsPrefix(request.Messages, history) {
				return found, len(history)
			}
		}
	}
	return nil, 0
}

func latestToolResult(request *protocol.Request) string {
	for index := len(request.Messages) - 1; index >= 0; index-- {
		blocks := request.Messages[index].Blocks
		for position := len(blocks) - 1; position >= 0; position-- {
			if blocks[position].Kind == protocol.KindToolResult && blocks[position].ToolResult != nil {
				return blocks[position].ToolResult.CallID
			}
		}
	}
	return ""
}
