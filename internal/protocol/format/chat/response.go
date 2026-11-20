package chat

import (
	"errors"
	"time"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

var responseKeys = []string{"id", "object", "model", "choices", "usage"}

func (Codec) DecodeResponse(body []byte, peer format.Peer) (*protocol.Response, []protocol.Loss, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("response must be a JSON object")
	}
	if failure := protocol.Object(root["error"]); failure != nil {
		return nil, nil, &format.StreamError{Code: protocol.String(failure["code"]), Message: protocol.String(failure["message"])}
	}
	created, _ := protocol.Integer(root["created"])
	response := &protocol.Response{
		ResponseMeta: protocol.ResponseMeta{
			ID: protocol.String(root["id"]), Model: protocol.String(root["model"]), Created: created,
			SystemFingerprint: protocol.String(root["system_fingerprint"]), ServiceTier: protocol.String(root["service_tier"]),
		},
		Usage: format.ParseChatUsage(root), Extras: format.UnmodelledFields(protocol.Chat, root, responseKeys...),
	}
	choices := protocol.Array(root["choices"])
	if len(choices) == 0 {
		return response, nil, nil
	}
	choice := protocol.Object(choices[0])
	if index, _ := protocol.Integer(choice["index"]); index != 0 || len(choices) > 1 {
		return nil, nil, errors.New("multiple choices are not supported")
	}
	message, err := decodeMessage(protocol.Object(choice["message"]))
	if err != nil {
		return nil, nil, err
	}
	response.Blocks = message.Blocks
	var losses []protocol.Loss
	finish := protocol.String(choice["finish_reason"])
	var known bool
	if response.Finish, known = format.ParseChatFinish(finish); !known {
		losses = append(losses, format.FinishMappedLoss(finish, protocol.Chat))
	}
	return response, losses, nil
}

func (Codec) EncodeResponse(response *protocol.Response, peer format.Peer) ([]byte, []protocol.Loss, error) {
	context := format.NewPartContext(protocol.Chat, peer)
	context.Role = protocol.RoleAssistant
	encoded, losses, err := encodeMessage(protocol.Message{Role: protocol.RoleAssistant, Blocks: response.Blocks}, context, "blocks")
	if err != nil {
		return nil, losses, err
	}
	message := map[string]any{"role": "assistant", "content": nil}
	if len(encoded) > 0 {
		message = encoded[len(encoded)-1].(map[string]any)
	}
	finish := format.ChatFinish(response.Finish)
	if finish == "" {
		finish = "stop"
	}
	created := response.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	root := map[string]any{
		"id":      response.ID,
		"object":  "chat.completion",
		"created": created,
		"model":   response.Model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
	}
	if response.SystemFingerprint != "" {
		root["system_fingerprint"] = response.SystemFingerprint
	}
	if response.ServiceTier != "" {
		root["service_tier"] = response.ServiceTier
	}
	if usage := format.EncodeChatUsage(response.Usage); usage != nil {
		root["usage"] = usage
	}
	if format.FinishLoss(response.Finish, protocol.Chat) {
		losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Detail: string(response.Finish), To: protocol.Chat})
	}
	losses = append(losses, format.ApplyExtras(root, response.Extras, protocol.Chat, "extras")...)
	return protocol.Marshal(root), losses, nil
}

func (Codec) EncodeError(failure *protocol.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	return status, protocol.Marshal(map[string]any{"error": map[string]any{"message": failure.Message, "type": errorType(failure.Kind), "code": failure.Code}})
}

func errorType(kind string) string {
	switch kind {
	case "invalid_request", "unauthorized":
		return "invalid_request_error"
	case "rate_limited":
		return "rate_limit_error"
	case "upstream_failure":
		return "api_error"
	case "timeout":
		return "timeout_error"
	case "blocked":
		return "permission_error"
	}
	return "server_error"
}
