package messages

import (
	"errors"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

var responseKeys = []string{"id", "type", "role", "model", "content", "stop_reason", "stop_sequence", "usage"}

func (Codec) DecodeResponse(body []byte, peer format.Peer) (*protocol.Response, []protocol.Loss, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("response must be a JSON object")
	}
	if protocol.String(root["type"]) == "error" {
		failure := protocol.Object(root["error"])
		return nil, nil, &format.StreamError{Code: protocol.String(failure["type"]), Message: protocol.String(failure["message"])}
	}
	blocks, err := format.DecodeContent(protocol.Messages, root["content"])
	if err != nil {
		return nil, nil, err
	}
	response := &protocol.Response{
		ResponseMeta: protocol.ResponseMeta{ID: protocol.String(root["id"]), Model: protocol.String(root["model"])},
		Blocks:       blocks,
		Finish:       protocol.FinishStop,
		StopSequence: protocol.String(root["stop_sequence"]),
		Usage:        format.ParseMessagesUsage(protocol.Object(root["usage"])),
		Extras:       format.UnmodelledFields(protocol.Messages, root, responseKeys...),
	}
	var losses []protocol.Loss
	if reason := protocol.String(root["stop_reason"]); reason != "" {
		var known bool
		if response.Finish, known = format.ParseMessagesFinish(reason); !known {
			losses = append(losses, format.FinishMappedLoss(reason, protocol.Messages))
		}
	}
	return response, losses, nil
}

func (Codec) EncodeResponse(response *protocol.Response, peer format.Peer) ([]byte, []protocol.Loss, error) {
	context := format.NewPartContext(protocol.Messages, peer)
	context.Role = protocol.RoleAssistant
	content, losses, err := format.EncodeBlocks(response.Blocks, context, "blocks")
	if err != nil {
		return nil, losses, err
	}
	root := map[string]any{
		"id":          response.ID,
		"type":        "message",
		"role":        "assistant",
		"model":       response.Model,
		"content":     content,
		"stop_reason": format.MessagesFinish(response.Finish, response.StopSequence),
	}
	if response.StopSequence != "" {
		root["stop_sequence"] = response.StopSequence
	}
	if usage := format.EncodeMessagesUsage(response.Usage); usage != nil {
		root["usage"] = usage
	}
	if format.FinishLoss(response.Finish, protocol.Messages) {
		losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Detail: string(response.Finish), To: protocol.Messages})
	}
	losses = append(losses, format.ApplyExtras(root, response.Extras, protocol.Messages, "extras")...)
	return protocol.Marshal(root), losses, nil
}

func (Codec) EncodeError(failure *protocol.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	return status, protocol.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": errorType(failure.Kind), "message": failure.Message}})
}

func errorType(kind string) string {
	switch kind {
	case "invalid_request":
		return "invalid_request_error"
	case "unauthorized":
		return "authentication_error"
	case "rate_limited":
		return "rate_limit_error"
	case "timeout":
		return "timeout_error"
	case "blocked":
		return "permission_error"
	}
	return "api_error"
}
