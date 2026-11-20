package chat

import (
	"errors"
	"time"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

var responseKeys = []string{"id", "object", "created", "model", "choices", "usage", "system_fingerprint", "service_tier"}

func (Codec) DecodeResponse(body []byte, peer format.Peer) (*protocol.Response, []protocol.Loss, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("response must be a JSON object")
	}
	if failure := protocol.Object(root["error"]); failure != nil {
		return nil, nil, &format.StreamError{Code: protocol.String(failure["code"]), Type: protocol.String(failure["type"]), Message: protocol.String(failure["message"]), Status: int(protocol.IntegerOrZero(failure["status"]))}
	}
	created, _ := protocol.Integer(root["created"])
	response := &protocol.Response{
		Source: protocol.Chat,
		Meta: protocol.ResponseMeta{
			ID: protocol.String(root["id"]), Model: protocol.String(root["model"]), Created: created,
			SystemFingerprint: protocol.String(root["system_fingerprint"]), ServiceTier: protocol.String(root["service_tier"]),
		},
		Usage: format.ParseChatUsage(root), Raw: format.CaptureRaw(protocol.Chat, root, responseKeys...),
	}
	choices := protocol.Array(root["choices"])
	if len(choices) == 0 {
		return response, nil, nil
	}
	choice := protocol.Object(choices[0])
	if index, _ := protocol.Integer(choice["index"]); index != 0 || len(choices) > 1 {
		return nil, nil, protocol.UnsupportedFeature("unsupported_multi_choice", "Multiple choices are not supported.")
	}
	items, err := decodeMessage(protocol.Object(choice["message"]), peer)
	if err != nil {
		return nil, nil, err
	}
	if choice["message"] == nil {
		items, err = decodeMessage(protocol.Object(choice["delta"]), peer)
		if err != nil {
			return nil, nil, err
		}
	}
	response.Items = items
	format.AssignCallIDs(response.Items, format.NewCallIDs(peer))
	finish := protocol.String(choice["finish_reason"])
	reason, known := format.ParseChatFinish(finish)
	response.Finish = protocol.Finish{Kind: reason, Raw: finish}
	var losses []protocol.Loss
	if !known {
		losses = append(losses, format.FinishMappedLoss(finish, protocol.Chat))
	}
	return response, losses, nil
}

func (Codec) EncodeResponse(response *protocol.Response, peer format.Peer) ([]byte, []protocol.Loss, error) {
	context := format.PartContext{Target: protocol.Chat, Peer: peer, Role: protocol.RoleAssistant, Output: true}
	encoded, losses, err := encodeMessages(response.Items, context)
	if err != nil {
		return nil, losses, err
	}
	message := map[string]any{"role": "assistant", "content": nil}
	if len(encoded) > 0 {
		message = encoded[0].(map[string]any)
	}
	finishKind := response.Finish.Kind
	if finishKind == "" || finishKind == protocol.FinishStop {
		for index := range response.Items {
			if response.Items[index].Kind == protocol.ItemToolCall {
				finishKind = protocol.FinishToolCalls
				break
			}
		}
	}
	finish := format.ChatFinish(finishKind)
	if finish == "" {
		finish = "stop"
	}
	created := response.Meta.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	model := response.Meta.Model
	if peer.Model != "" {
		model = peer.Model
	}
	root := map[string]any{
		"id":      response.Meta.ID,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
	}
	if response.Meta.SystemFingerprint != "" {
		root["system_fingerprint"] = response.Meta.SystemFingerprint
	}
	if response.Meta.ServiceTier != "" {
		root["service_tier"] = response.Meta.ServiceTier
	}
	if usage := format.EncodeChatUsage(response.Usage); usage != nil {
		root["usage"] = usage
	}
	if format.FinishLoss(response.Finish.Kind, protocol.Chat) {
		losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: string(response.Finish.Kind), To: protocol.Chat})
	}
	losses = append(losses, format.RawFields(root, response.Raw, protocol.Chat, "raw")...)
	losses = append(losses, format.UsageLosses(response.Usage, protocol.Chat)...)
	return protocol.Marshal(root), losses, nil
}

func (Codec) EncodeError(failure *protocol.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	return status, protocol.Marshal(map[string]any{"error": map[string]any{"message": failure.Message, "type": errorType(failure.Kind), "code": failure.Code}})
}
