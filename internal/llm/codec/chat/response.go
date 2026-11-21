package chat

import (
	"errors"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

var responseKeys = []string{"id", "object", "created", "model", "choices", "usage", "system_fingerprint", "service_tier"}

func (Codec) DecodeResponse(body []byte, peer codec.Peer) (*model.Response, []model.Loss, error) {
	root, err := model.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("response must be a JSON object")
	}
	if failure := model.Object(root["error"]); failure != nil {
		return nil, nil, &codec.StreamError{Code: model.String(failure["code"]), Type: model.String(failure["type"]), Message: model.String(failure["message"]), Status: int(model.IntegerOrZero(failure["status"]))}
	}
	created, _ := model.Integer(root["created"])
	response := &model.Response{
		Source: model.Chat,
		Meta: model.ResponseMeta{
			ID: model.String(root["id"]), Model: model.String(root["model"]), Created: created,
			SystemFingerprint: model.String(root["system_fingerprint"]), ServiceTier: model.String(root["service_tier"]),
		},
		Usage: codec.ParseChatUsage(root), Raw: codec.CaptureRaw(model.Chat, root, responseKeys...),
	}
	choices := model.Array(root["choices"])
	if len(choices) == 0 {
		return response, nil, nil
	}
	choice := model.Object(choices[0])
	var losses []model.Loss
	if len(choices) > 1 {
		losses = append(losses, model.Loss{Path: "choices", Code: model.LossMultiChoicePassthrough, Class: model.LossDegraded, Detail: "only the first choice is convertible; multiple choices require same-format pass-through", From: model.Chat})
	}
	items, err := decodeMessage(model.Object(choice["message"]), peer)
	if err != nil {
		return nil, nil, err
	}
	if choice["message"] == nil {
		items, err = decodeMessage(model.Object(choice["delta"]), peer)
		if err != nil {
			return nil, nil, err
		}
	}
	response.Items = items
	codec.AssignCallIDs(response.Items, codec.NewCallIDs(peer))
	finish := model.String(choice["finish_reason"])
	reason, known := codec.ParseChatFinish(finish)
	response.Finish = model.Finish{Kind: reason, Raw: finish}
	if !known {
		losses = append(losses, codec.FinishMappedLoss(finish, model.Chat))
	}
	response.Status = "completed"
	return response, losses, nil
}

func (Codec) EncodeResponse(response *model.Response, peer codec.Peer) ([]byte, []model.Loss, error) {
	context := codec.PartContext{Target: model.Chat, Peer: peer, Role: model.RoleAssistant, Output: true}
	encoded, losses, err := encodeMessages(response.Items, context)
	if err != nil {
		return nil, losses, err
	}
	message := map[string]any{"role": "assistant", "content": nil}
	if len(encoded) > 0 {
		message = encoded[0].(map[string]any)
	}
	finishKind := response.Finish.Kind
	if finishKind == "" || finishKind == model.FinishStop {
		for index := range response.Items {
			if response.Items[index].Kind == model.ItemToolCall {
				finishKind = model.FinishToolCalls
				break
			}
		}
	}
	finish := codec.ChatFinish(finishKind)
	if finish == "" {
		finish = "stop"
	}
	created := response.Meta.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	modelName := response.Meta.Model
	if peer.Model != "" {
		modelName = peer.Model
	}
	root := map[string]any{
		"id":      response.Meta.ID,
		"object":  "chat.completion",
		"created": created,
		"model":   modelName,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
	}
	if response.Meta.SystemFingerprint != "" {
		root["system_fingerprint"] = response.Meta.SystemFingerprint
	}
	if response.Meta.ServiceTier != "" {
		root["service_tier"] = response.Meta.ServiceTier
	}
	if usage := codec.EncodeChatUsage(response.Usage); usage != nil {
		root["usage"] = usage
	}
	if codec.FinishLoss(response.Finish.Kind, model.Chat) {
		losses = append(losses, model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: string(response.Finish.Kind), To: model.Chat})
	}
	losses = append(losses, codec.RawFields(root, response.Raw, model.Chat, "raw")...)
	losses = append(losses, codec.UsageLosses(response.Usage, model.Chat)...)
	return model.Marshal(root), losses, nil
}

func (Codec) EncodeError(failure *model.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	return status, model.Marshal(map[string]any{"error": map[string]any{"message": failure.Message, "type": errorType(failure.Kind), "code": failure.Code}})
}
