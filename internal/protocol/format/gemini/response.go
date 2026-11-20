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
		return nil, nil, &format.StreamError{Code: protocol.String(failure["status"]), Type: protocol.String(failure["status"]), Message: protocol.String(failure["message"]), Status: int(protocol.IntegerOrZero(failure["code"]))}
	}
	response := &protocol.Response{
		Source: protocol.Gemini,
		Meta:   protocol.ResponseMeta{ID: protocol.String(root["responseId"]), Model: protocol.String(root["modelVersion"])},
		Finish: protocol.Finish{Kind: protocol.FinishStop},
		Usage:  format.ParseGeminiUsage(root), Raw: format.CaptureRaw(protocol.Gemini, root, responseKeys...),
	}
	candidates := protocol.Array(root["candidates"])
	if len(candidates) > 1 {
		return nil, nil, protocol.UnsupportedFeature("unsupported_multi_choice", "Multiple candidates are not supported.")
	}
	ids := format.NewCallIDs(peer)
	losses := []protocol.Loss{}
	for _, value := range candidates {
		candidate := protocol.Object(value)
		items, err := decodeParts(protocol.RoleAssistant, protocol.Array(protocol.Object(candidate["content"])["parts"]), ids, nil)
		if err != nil {
			return nil, nil, err
		}
		response.Items = attachCitations(items, candidateCitations(candidate))
		reason := protocol.String(candidate["finishReason"])
		response.Finish = protocol.Finish{Kind: protocol.FinishStop, Raw: reason}
		if reason != "" {
			finish, known := format.ParseGeminiFinish(reason)
			response.Finish.Kind = finish
			if !known {
				losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: reason, To: protocol.Gemini})
			}
		}
	}
	if feedback := protocol.Object(root["promptFeedback"]); protocol.String(feedback["blockReason"]) != "" {
		response.Finish.Kind = protocol.FinishContentFilter
	}
	if response.Finish.Kind == protocol.FinishStop {
		for _, item := range response.Items {
			if item.Kind == protocol.ItemToolCall {
				response.Finish.Kind = protocol.FinishToolCalls
			}
		}
	}
	return response, losses, nil
}

func (Codec) EncodeResponse(response *protocol.Response, peer format.Peer) ([]byte, []protocol.Loss, error) {
	var losses []protocol.Loss
	parts := make([]any, 0, len(response.Items))
	var citations []protocol.Citation
	for index := range response.Items {
		item := &response.Items[index]
		switch item.Kind {
		case protocol.ItemMessage, protocol.ItemReasoning:
			nested, itemLosses, err := encodeItemParts(item, "items", peer, true)
			if err != nil {
				return nil, losses, err
			}
			losses = append(losses, itemLosses...)
			parts = append(parts, nested...)
			for _, part := range item.Parts {
				citations = append(citations, part.Citations...)
			}
		case protocol.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == protocol.ToolServer {
				losses = append(losses, format.Degraded("item", "provider-executed tool calls cannot be represented by Gemini", protocol.Gemini))
				continue
			}
			entry, itemLosses := encodeFunctionCall(item, peer, "items")
			losses = append(losses, itemLosses...)
			parts = append(parts, entry)
		case protocol.ItemServerToolCall, protocol.ItemServerToolResult, protocol.ItemNative:
			if native := format.RawObject(item.Raw, protocol.Gemini); native != nil {
				parts = append(parts, native)
				continue
			}
			losses = append(losses, format.Degraded("item", "provider-private item cannot be represented by Gemini", protocol.Gemini))
		}
	}
	candidate := map[string]any{
		"index": 0, "content": map[string]any{"role": "model", "parts": parts},
		"finishReason": format.GeminiFinish(response.Finish.Kind),
	}
	if grounding, nested := encodeCitations(citations); grounding != nil {
		for key, value := range grounding {
			candidate[key] = value
		}
		losses = append(losses, nested...)
	} else {
		losses = append(losses, nested...)
	}
	root := map[string]any{"candidates": []any{candidate}}
	if response.Meta.ID != "" {
		root["responseId"] = response.Meta.ID
	}
	if response.Meta.Model != "" {
		root["modelVersion"] = response.Meta.Model
	}
	if usage := format.EncodeGeminiUsage(response.Usage); usage != nil {
		root["usageMetadata"] = usage
	}
	if format.FinishLoss(response.Finish.Kind, protocol.Gemini) {
		losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: string(response.Finish.Kind), To: protocol.Gemini})
	}
	losses = append(losses, format.RawFields(root, response.Raw, protocol.Gemini, "raw")...)
	losses = append(losses, format.UsageLosses(response.Usage, protocol.Gemini)...)
	return protocol.Marshal(root), losses, nil
}

func (Codec) EncodeError(failure *protocol.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	return status, protocol.Marshal(map[string]any{"error": map[string]any{"code": status, "message": failure.Message, "status": errorStatus(failure.Kind)}})
}
