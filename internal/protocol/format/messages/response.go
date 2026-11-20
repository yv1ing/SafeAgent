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
		return nil, nil, &format.StreamError{Code: protocol.String(failure["type"]), Type: protocol.String(failure["type"]), Message: protocol.String(failure["message"])}
	}
	items, err := format.DecodeContentItems(protocol.Messages, protocol.RoleAssistant, root["content"], peer)
	if err != nil {
		return nil, nil, err
	}
	response := &protocol.Response{
		Source: protocol.Messages,
		Meta:   protocol.ResponseMeta{ID: protocol.String(root["id"]), Model: protocol.String(root["model"])},
		Items:  items,
		Finish: protocol.Finish{Kind: protocol.FinishStop, StopSequence: protocol.String(root["stop_sequence"])},
		Usage:  format.ParseMessagesUsage(protocol.Object(root["usage"])), Raw: format.CaptureRaw(protocol.Messages, root, responseKeys...),
	}
	var losses []protocol.Loss
	if reason := protocol.String(root["stop_reason"]); reason != "" {
		response.Finish.Raw = reason
		var known bool
		if response.Finish.Kind, known = format.ParseMessagesFinish(reason); !known {
			losses = append(losses, format.FinishMappedLoss(reason, protocol.Messages))
		}
	}
	return response, losses, nil
}

func (Codec) EncodeResponse(response *protocol.Response, peer format.Peer) ([]byte, []protocol.Loss, error) {
	context := format.PartContext{Target: protocol.Messages, Peer: peer, Role: protocol.RoleAssistant, Output: true}
	var losses []protocol.Loss
	content := make([]any, 0, len(response.Items))
	for index := range response.Items {
		item := &response.Items[index]
		switch item.Kind {
		case protocol.ItemMessage, protocol.ItemReasoning:
			parts, nested, err := encodeContent(item, context, "items")
			if err != nil {
				return nil, losses, err
			}
			losses = append(losses, nested...)
			content = append(content, parts...)
		case protocol.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == protocol.ToolServer {
				losses = append(losses, format.Degraded("item", "provider-executed tool calls cannot be represented by Messages", protocol.Messages))
				continue
			}
			entry, nested := encodeToolUse(item, context, "items")
			losses = append(losses, nested...)
			content = append(content, entry)
		case protocol.ItemServerToolCall, protocol.ItemServerToolResult, protocol.ItemNative:
			if native := format.RawObject(item.Raw, protocol.Messages); native != nil {
				content = append(content, native)
				continue
			}
			losses = append(losses, format.Degraded("item", "provider-private item cannot be represented by Messages", protocol.Messages))
		case protocol.ItemToolResult:
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
	if finishKind == "" || finishKind == protocol.FinishStop {
		for index := range response.Items {
			if response.Items[index].Kind == protocol.ItemToolCall {
				finishKind = protocol.FinishToolCalls
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
		"stop_reason": format.MessagesFinish(finishKind, stopSequence),
	}
	if stopSequence != "" {
		root["stop_sequence"] = stopSequence
	}
	if usage := format.EncodeMessagesUsage(response.Usage); usage != nil {
		root["usage"] = usage
	}
	if format.FinishLoss(response.Finish.Kind, protocol.Messages) {
		losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: string(response.Finish.Kind), To: protocol.Messages})
	}
	losses = append(losses, format.RawFields(root, response.Raw, protocol.Messages, "raw")...)
	losses = append(losses, format.UsageLosses(response.Usage, protocol.Messages)...)
	return protocol.Marshal(root), losses, nil
}

func (Codec) EncodeError(failure *protocol.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	return status, protocol.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": errorType(failure.Kind), "message": failure.Message}})
}
