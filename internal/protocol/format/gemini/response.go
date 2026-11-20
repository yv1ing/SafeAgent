package gemini

import (
	"errors"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

var responseKeys = []string{"candidates", "usageMetadata", "responseId", "modelVersion", "promptFeedback"}

func (Codec) DecodeResponse(body []byte, peer format.Peer) (*protocol.Response, []protocol.Loss, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("response must be a JSON object")
	}
	if failure := protocol.Object(root["error"]); failure != nil {
		return nil, nil, &format.StreamError{Code: protocol.String(failure["status"]), Message: protocol.String(failure["message"])}
	}
	response := &protocol.Response{
		ResponseMeta: protocol.ResponseMeta{ID: protocol.String(root["responseId"]), Model: protocol.String(root["modelVersion"])},
		Finish:       protocol.FinishStop,
		Usage:        format.ParseGeminiUsage(root),
		Extras:       format.UnmodelledFields(protocol.Gemini, root, responseKeys...),
	}
	candidates := protocol.Array(root["candidates"])
	if len(candidates) > 1 {
		return nil, nil, errors.New("multiple candidates are not supported")
	}
	ids := format.NewCallIDs(peer)
	losses := []protocol.Loss{}
	for _, item := range candidates {
		candidate := protocol.Object(item)
		blocks, err := format.DecodeContent(protocol.Gemini, protocol.Object(candidate["content"])["parts"])
		if err != nil {
			return nil, nil, err
		}
		assignCallIDs(blocks, ids)
		blocks = attachCitations(blocks, candidateCitations(candidate))
		response.Blocks = blocks
		reason := protocol.String(candidate["finishReason"])
		finish, known := format.ParseGeminiFinish(reason)
		response.Finish = finish
		if !known {
			losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Detail: reason, To: protocol.Gemini})
		}
	}
	if feedback := protocol.Object(root["promptFeedback"]); protocol.String(feedback["blockReason"]) != "" {
		response.Finish = protocol.FinishContentFilter
	}
	if response.Finish == protocol.FinishStop {
		for _, block := range response.Blocks {
			if block.Kind == protocol.KindToolCall {
				response.Finish = protocol.FinishToolCalls
			}
		}
	}
	return response, losses, nil
}

func (Codec) EncodeResponse(response *protocol.Response, peer format.Peer) ([]byte, []protocol.Loss, error) {
	context := format.NewPartContext(protocol.Gemini, peer)
	context.Role = protocol.RoleAssistant
	parts, losses, err := format.EncodeBlocks(response.Blocks, context, "blocks")
	if err != nil {
		return nil, losses, err
	}
	root := map[string]any{
		"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": parts}, "finishReason": format.GeminiFinish(response.Finish)}},
	}
	if response.ID != "" {
		root["responseId"] = response.ID
	}
	if response.Model != "" {
		root["modelVersion"] = response.Model
	}
	if usage := format.EncodeGeminiUsage(response.Usage); usage != nil {
		root["usageMetadata"] = usage
	}
	if format.FinishLoss(response.Finish, protocol.Gemini) {
		losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Detail: string(response.Finish), To: protocol.Gemini})
	}
	losses = append(losses, format.ApplyExtras(root, response.Extras, protocol.Gemini, "extras")...)
	return protocol.Marshal(root), losses, nil
}

func (Codec) EncodeError(failure *protocol.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	return status, protocol.Marshal(map[string]any{"error": map[string]any{"code": status, "message": failure.Message, "status": errorStatus(failure.Kind)}})
}

func errorStatus(kind string) string {
	switch kind {
	case "invalid_request":
		return "INVALID_ARGUMENT"
	case "unauthorized":
		return "UNAUTHENTICATED"
	case "blocked":
		return "PERMISSION_DENIED"
	case "rate_limited":
		return "RESOURCE_EXHAUSTED"
	case "timeout":
		return "DEADLINE_EXCEEDED"
	}
	return "INTERNAL"
}
