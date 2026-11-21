package messages

import (
	"errors"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

var responseKeys = []string{"id", "type", "role", "model", "content", "stop_reason", "stop_sequence", "usage"}

func (Codec) DecodeResponse(body []byte, peer codec.Peer) (*model.Response, []model.Loss, error) {
	root, err := model.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("response must be a JSON object")
	}
	if model.String(root["type"]) == "error" {
		failure := model.Object(root["error"])
		return nil, nil, &codec.StreamError{Code: model.String(failure["type"]), Type: model.String(failure["type"]), Message: model.String(failure["message"])}
	}
	items, err := codec.DecodeContentItems(model.Messages, model.RoleAssistant, root["content"], peer)
	if err != nil {
		return nil, nil, err
	}
	response := &model.Response{
		Source: model.Messages,
		Meta:   model.ResponseMeta{ID: model.String(root["id"]), Model: model.String(root["model"])},
		Items:  items,
		Finish: model.Finish{Kind: model.FinishStop, StopSequence: model.String(root["stop_sequence"])},
		Usage:  codec.ParseMessagesUsage(model.Object(root["usage"])), Raw: codec.CaptureRaw(model.Messages, root, responseKeys...),
	}
	var losses []model.Loss
	if reason := model.String(root["stop_reason"]); reason != "" {
		response.Finish.Raw = reason
		var known bool
		if response.Finish.Kind, known = codec.ParseMessagesFinish(reason); !known {
			losses = append(losses, codec.FinishMappedLoss(reason, model.Messages))
		}
	}
	return response, losses, nil
}

func (Codec) EncodeResponse(response *model.Response, peer codec.Peer) ([]byte, []model.Loss, error) {
	context := codec.PartContext{Target: model.Messages, Peer: peer, Role: model.RoleAssistant, Output: true}
	var losses []model.Loss
	content := make([]any, 0, len(response.Items))
	for index := range response.Items {
		item := &response.Items[index]
		switch item.Kind {
		case model.ItemMessage, model.ItemReasoning:
			parts, nested, err := encodeContent(item, context, "items")
			if err != nil {
				return nil, losses, err
			}
			losses = append(losses, nested...)
			content = append(content, parts...)
		case model.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == model.ToolServer {
				losses = append(losses, codec.Degraded("item", "provider-executed tool calls cannot be represented by Messages", model.Messages))
				continue
			}
			entry, nested := encodeToolUse(item, context, "items")
			losses = append(losses, nested...)
			content = append(content, entry)
		case model.ItemServerToolCall, model.ItemServerToolResult, model.ItemNative:
			if native := codec.RawObject(item.Raw, model.Messages); native != nil {
				content = append(content, native)
				continue
			}
			losses = append(losses, codec.Degraded("item", "provider-private item cannot be represented by Messages", model.Messages))
		case model.ItemToolResult:
			entry, nested, err := encodeToolResult(item, context, "items")
			if err != nil {
				return nil, losses, err
			}
			losses = append(losses, nested...)
			content = append(content, entry)
		}
	}
	stopSequence := response.Finish.StopSequence
	finishKind := response.Finish.Kind
	if finishKind == "" || finishKind == model.FinishStop {
		for index := range response.Items {
			if response.Items[index].Kind == model.ItemToolCall {
				finishKind = model.FinishToolCalls
				break
			}
		}
	}
	root := map[string]any{
		"id":          response.Meta.ID,
		"type":        "message",
		"role":        "assistant",
		"model":       response.Meta.Model,
		"content":     content,
		"stop_reason": codec.MessagesFinish(finishKind, stopSequence),
	}
	if stopSequence != "" {
		root["stop_sequence"] = stopSequence
	}
	if usage := codec.EncodeMessagesUsage(response.Usage); usage != nil {
		root["usage"] = usage
	}
	if codec.FinishLoss(response.Finish.Kind, model.Messages) {
		losses = append(losses, model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: string(response.Finish.Kind), To: model.Messages})
	}
	losses = append(losses, codec.RawFields(root, response.Raw, model.Messages, "raw")...)
	losses = append(losses, codec.UsageLosses(response.Usage, model.Messages)...)
	return model.Marshal(root), losses, nil
}

func (Codec) EncodeError(failure *model.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	return status, model.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": errorType(failure.Kind), "message": failure.Message}})
}
