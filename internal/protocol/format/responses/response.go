package responses

import (
	"errors"
	"time"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

var responseKeys = []string{"id", "object", "created_at", "model", "status", "output", "usage", "incomplete_details", "error", "service_tier", "metadata", "instructions", "tools"}

func (Codec) DecodeResponse(body []byte, peer format.Peer) (*protocol.Response, []protocol.Loss, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("response must be a JSON object")
	}
	if failure := protocol.Object(root["error"]); failure != nil && protocol.String(root["status"]) != "failed" {
		return nil, nil, &format.StreamError{Code: protocol.String(failure["code"]), Type: protocol.String(failure["type"]), Message: protocol.String(failure["message"]), Status: int(protocol.IntegerOrZero(failure["status"]))}
	}
	items, err := format.DecodeContentItems(protocol.Responses, protocol.RoleAssistant, root["output"], peer)
	if err != nil {
		return nil, nil, err
	}
	created, _ := protocol.Integer(root["created_at"])
	format.AssignCallIDs(items, format.NewCallIDs(peer))
	response := &protocol.Response{
		Source: protocol.Responses,
		Meta:   protocol.ResponseMeta{ID: protocol.String(root["id"]), Model: protocol.String(root["model"]), Created: created, ServiceTier: protocol.String(root["service_tier"])},
		Items:  items,
		Usage:  format.ParseResponsesUsage(root), Raw: format.CaptureRaw(protocol.Responses, root, responseKeys...),
	}
	response.Finish, response.Failure = finishOf(root)
	if response.Finish.Kind == protocol.FinishStop {
		for index := range items {
			if items[index].Kind == protocol.ItemToolCall {
				response.Finish.Kind = protocol.FinishToolCalls
				break
			}
		}
	}
	return response, nil, nil
}

func finishOf(response map[string]any) (protocol.Finish, *protocol.Error) {
	status := protocol.String(response["status"])
	finish := protocol.Finish{Kind: protocol.FinishStop, Raw: status}
	switch status {
	case "incomplete":
		reason := protocol.String(protocol.Object(response["incomplete_details"])["reason"])
		finish.IncompleteReason = reason
		if reason == "content_filter" {
			finish.Kind = protocol.FinishContentFilter
		} else {
			finish.Kind = protocol.FinishLength
		}
	case "failed":
		failure := protocol.Object(response["error"])
		finish.Kind = protocol.FinishError
		return finish, &protocol.Error{
			Kind: "upstream_failure", Class: protocol.ClassTransient,
			Code: protocol.String(failure["code"]), Message: protocol.String(failure["message"]),
		}
	}
	return finish, nil
}

func (Codec) EncodeResponse(response *protocol.Response, peer format.Peer) ([]byte, []protocol.Loss, error) {
	context := format.PartContext{Target: protocol.Responses, Peer: peer, Role: protocol.RoleAssistant, Output: true}
	var losses []protocol.Loss
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
		"status": format.ResponsesStatus(response.Finish.Kind),
		"output": output,
	}
	if root["status"] == "incomplete" {
		root["incomplete_details"] = map[string]any{"reason": incompleteReason(response.Finish)}
	}
	if response.Failure != nil {
		root["error"] = map[string]any{"code": response.Failure.Code, "message": response.Failure.Message}
	}
	if usage := format.EncodeResponsesUsage(response.Usage); usage != nil {
		root["usage"] = usage
	}
	if response.Meta.ServiceTier != "" {
		root["service_tier"] = response.Meta.ServiceTier
	}
	if format.FinishLoss(response.Finish.Kind, protocol.Responses) {
		losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: string(response.Finish.Kind), To: protocol.Responses})
	}
	losses = append(losses, format.RawFields(root, response.Raw, protocol.Responses, "raw")...)
	if _, replayed := root["created_at"]; !replayed {
		created := response.Meta.Created
		if created == 0 {
			created = time.Now().Unix()
		}
		root["created_at"] = created
	}
	losses = append(losses, format.UsageLosses(response.Usage, protocol.Responses)...)
	return protocol.Marshal(root), losses, nil
}

func incompleteReason(finish protocol.Finish) string {
	if finish.IncompleteReason != "" {
		return finish.IncompleteReason
	}
	if finish.Kind == protocol.FinishContentFilter {
		return "content_filter"
	}
	return "max_output_tokens"
}

func encodeOutputItem(item *protocol.Item, ordinal int, context format.PartContext) (map[string]any, []protocol.Loss, error) {
	switch item.Kind {
	case protocol.ItemMessage:
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
		if applyItemID(entry, item, protocol.ItemMessage, ordinal) {
			losses = append(losses, protocol.Loss{Path: "id", Code: protocol.LossItemIDMapped, Class: protocol.LossDegraded, Detail: "the source item id was replaced by a Responses id", To: protocol.Responses})
		}
		losses = append(losses, format.RawFields(entry, item.Raw, protocol.Responses, "raw")...)
		return entry, losses, nil
	case protocol.ItemReasoning:
		return encodeReasoningItem(item, context)
	case protocol.ItemToolCall:
		entry, losses, err := encodeItem(item, ordinal, context)
		if err != nil || entry == nil {
			return entry, losses, err
		}
		entry["status"] = "completed"
		return entry, losses, nil
	case protocol.ItemServerToolCall, protocol.ItemServerToolResult, protocol.ItemNative:
		return encodeItem(item, ordinal, context)
	}
	return nil, []protocol.Loss{format.Degraded("item", "the item cannot be represented in a Responses output", protocol.Responses)}, nil
}

func (Codec) EncodeError(failure *protocol.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	if streaming {
		return status, protocol.Marshal(map[string]any{"type": "error", "code": failure.Code, "message": failure.Message, "sequence_number": 0})
	}
	return status, protocol.Marshal(map[string]any{"error": map[string]any{"message": failure.Message, "type": errorType(failure.Kind), "code": failure.Code}})
}
