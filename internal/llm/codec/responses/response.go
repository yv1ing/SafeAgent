package responses

import (
	"errors"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

var responseKeys = []string{"id", "object", "created_at", "model", "status", "output", "usage", "incomplete_details", "error", "service_tier", "metadata", "instructions", "tools"}

func (Codec) DecodeResponse(body []byte, peer codec.Peer) (*model.Response, []model.Loss, error) {
	root, err := model.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("response must be a JSON object")
	}
	if failure := model.Object(root["error"]); failure != nil && model.String(root["status"]) != "failed" {
		return nil, nil, &codec.StreamError{Code: model.String(failure["code"]), Type: model.String(failure["type"]), Message: model.String(failure["message"]), Status: int(model.IntegerOrZero(failure["status"]))}
	}
	items, err := codec.DecodeContentItems(model.Responses, model.RoleAssistant, root["output"], peer)
	if err != nil {
		return nil, nil, err
	}
	created, _ := model.Integer(root["created_at"])
	codec.AssignCallIDs(items, codec.NewCallIDs(peer))
	response := &model.Response{
		Source: model.Responses,
		Meta:   model.ResponseMeta{ID: model.String(root["id"]), Model: model.String(root["model"]), Created: created, ServiceTier: model.String(root["service_tier"])},
		Items:  items,
		Usage:  codec.ParseResponsesUsage(root), Raw: codec.CaptureRaw(model.Responses, root, responseKeys...),
	}
	response.Finish, response.Failure = finishOf(root)
	if response.Finish.Kind == model.FinishStop {
		for index := range items {
			if items[index].Kind == model.ItemToolCall {
				response.Finish.Kind = model.FinishToolCalls
				break
			}
		}
	}
	return response, nil, nil
}

func finishOf(response map[string]any) (model.Finish, *model.Error) {
	status := model.String(response["status"])
	finish := model.Finish{Kind: model.FinishStop, Raw: status}
	switch status {
	case "incomplete":
		reason := model.String(model.Object(response["incomplete_details"])["reason"])
		finish.IncompleteReason = reason
		if reason == "content_filter" {
			finish.Kind = model.FinishContentFilter
		} else {
			finish.Kind = model.FinishLength
		}
	case "failed":
		failure := model.Object(response["error"])
		finish.Kind = model.FinishError
		return finish, &model.Error{
			Kind: "upstream_failure", Class: model.ClassTransient,
			Code: model.String(failure["code"]), Message: model.String(failure["message"]),
		}
	}
	return finish, nil
}

func (Codec) EncodeResponse(response *model.Response, peer codec.Peer) ([]byte, []model.Loss, error) {
	context := codec.PartContext{Target: model.Responses, Peer: peer, Role: model.RoleAssistant, Output: true}
	var losses []model.Loss
	output := make([]any, 0, len(response.Items))
	for index := range response.Items {
		item := &response.Items[index]
		encoded, nested, err := encodeOutputItem(item, index, context)
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		if encoded != nil {
			output = append(output, encoded)
		}
	}
	root := map[string]any{
		"id":     response.Meta.ID,
		"object": "response",
		"model":  response.Meta.Model,
		"status": codec.ResponsesStatus(response.Finish.Kind),
		"output": output,
	}
	if root["status"] == "incomplete" {
		root["incomplete_details"] = map[string]any{"reason": incompleteReason(response.Finish)}
	}
	if response.Failure != nil {
		root["error"] = map[string]any{"code": response.Failure.Code, "message": response.Failure.Message}
	}
	if usage := codec.EncodeResponsesUsage(response.Usage); usage != nil {
		root["usage"] = usage
	}
	if response.Meta.ServiceTier != "" {
		root["service_tier"] = response.Meta.ServiceTier
	}
	if codec.FinishLoss(response.Finish.Kind, model.Responses) {
		losses = append(losses, model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: string(response.Finish.Kind), To: model.Responses})
	}
	losses = append(losses, codec.RawFields(root, response.Raw, model.Responses, "raw")...)
	if _, replayed := root["created_at"]; !replayed {
		created := response.Meta.Created
		if created == 0 {
			created = time.Now().Unix()
		}
		root["created_at"] = created
	}
	losses = append(losses, codec.UsageLosses(response.Usage, model.Responses)...)
	return model.Marshal(root), losses, nil
}

func incompleteReason(finish model.Finish) string {
	if finish.IncompleteReason != "" {
		return finish.IncompleteReason
	}
	if finish.Kind == model.FinishContentFilter {
		return "content_filter"
	}
	return "max_output_tokens"
}

func encodeOutputItem(item *model.Item, ordinal int, context codec.PartContext) (map[string]any, []model.Loss, error) {
	switch item.Kind {
	case model.ItemMessage:
		parts, losses, err := encodeMessageParts(item, false, context)
		if err != nil {
			return nil, losses, err
		}
		if len(parts) == 0 {
			return nil, losses, nil
		}
		role := string(item.Role)
		if role == "" {
			role = "assistant"
		}
		entry := map[string]any{"type": "message", "role": role, "content": parts, "status": "completed"}
		if applyItemID(entry, item, model.ItemMessage, ordinal) {
			losses = append(losses, model.Loss{Path: "id", Code: model.LossItemIDMapped, Class: model.LossDegraded, Detail: "the source item id was replaced by a Responses id", To: model.Responses})
		}
		losses = append(losses, codec.RawFields(entry, item.Raw, model.Responses, "raw")...)
		return entry, losses, nil
	case model.ItemReasoning:
		return encodeReasoningItem(item, context)
	case model.ItemToolCall:
		entry, losses, err := encodeItem(item, ordinal, context)
		if err != nil || entry == nil {
			return entry, losses, err
		}
		entry["status"] = "completed"
		return entry, losses, nil
	case model.ItemServerToolCall, model.ItemServerToolResult, model.ItemNative:
		return encodeItem(item, ordinal, context)
	}
	return nil, []model.Loss{codec.Degraded("item", "the item cannot be represented in a Responses output", model.Responses)}, nil
}

func (Codec) EncodeError(failure *model.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	if streaming {
		return status, model.Marshal(map[string]any{"type": "error", "code": failure.Code, "message": failure.Message, "sequence_number": 0})
	}
	return status, model.Marshal(map[string]any{"error": map[string]any{"message": failure.Message, "type": errorType(failure.Kind), "code": failure.Code}})
}
