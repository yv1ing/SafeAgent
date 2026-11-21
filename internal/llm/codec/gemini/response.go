package gemini

import (
	"errors"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

var responseKeys = []string{"candidates", "usageMetadata", "responseId", "modelVersion", "promptFeedback"}

func (Codec) DecodeResponse(body []byte, peer codec.Peer) (*model.Response, []model.Loss, error) {
	root, err := model.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("response must be a JSON object")
	}
	if failure := model.Object(root["error"]); failure != nil {
		return nil, nil, &codec.StreamError{Code: model.String(failure["status"]), Type: model.String(failure["status"]), Message: model.String(failure["message"]), Status: int(model.IntegerOrZero(failure["code"]))}
	}
	response := &model.Response{
		Source: model.Gemini,
		Meta:   model.ResponseMeta{ID: model.String(root["responseId"]), Model: model.String(root["modelVersion"])},
		Finish: model.Finish{Kind: model.FinishStop},
		Usage:  codec.ParseGeminiUsage(root), Raw: codec.CaptureRaw(model.Gemini, root, responseKeys...),
	}
	candidates := model.Array(root["candidates"])
	ids := codec.NewCallIDs(peer)
	losses := []model.Loss{}
	if len(candidates) > 1 {
		losses = append(losses, model.Loss{Path: "candidates", Code: model.LossMultiChoicePassthrough, Class: model.LossDegraded, Detail: "only the first candidate is convertible; multiple candidates require same-format pass-through", From: model.Gemini})
	}
	for _, value := range candidates {
		if index, _ := model.Integer(model.Object(value)["index"]); index != 0 {
			continue
		}
		candidate := model.Object(value)
		items, err := decodeParts(model.RoleAssistant, model.Array(model.Object(candidate["content"])["parts"]), ids, nil)
		if err != nil {
			return nil, nil, err
		}
		response.Items = attachCitations(items, candidateCitations(candidate))
		reason := model.String(candidate["finishReason"])
		response.Finish = model.Finish{Kind: model.FinishStop, Raw: reason}
		if reason != "" {
			finish, known := codec.ParseGeminiFinish(reason)
			response.Finish.Kind = finish
			if !known {
				losses = append(losses, model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: reason, To: model.Gemini})
			}
		}
	}
	if feedback := model.Object(root["promptFeedback"]); model.String(feedback["blockReason"]) != "" {
		response.Finish.Kind = model.FinishContentFilter
	}
	if response.Finish.Kind == model.FinishStop {
		for _, item := range response.Items {
			if item.Kind == model.ItemToolCall {
				response.Finish.Kind = model.FinishToolCalls
			}
		}
	}
	return response, losses, nil
}

func (Codec) EncodeResponse(response *model.Response, peer codec.Peer) ([]byte, []model.Loss, error) {
	var losses []model.Loss
	parts := make([]any, 0, len(response.Items))
	var citations []model.Citation
	for index := range response.Items {
		item := &response.Items[index]
		switch item.Kind {
		case model.ItemMessage, model.ItemReasoning:
			nested, itemLosses, err := encodeItemParts(item, "items", peer, true)
			if err != nil {
				return nil, losses, err
			}
			losses = append(losses, itemLosses...)
			parts = append(parts, nested...)
			for _, part := range item.Parts {
				citations = append(citations, part.Citations...)
			}
		case model.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == model.ToolServer {
				losses = append(losses, codec.Degraded("item", "provider-executed tool calls cannot be represented by Gemini", model.Gemini))
				continue
			}
			entry, itemLosses := encodeFunctionCall(item, peer, "items")
			losses = append(losses, itemLosses...)
			parts = append(parts, entry)
		case model.ItemServerToolCall, model.ItemServerToolResult, model.ItemNative:
			if native := codec.RawObject(item.Raw, model.Gemini); native != nil {
				parts = append(parts, native)
				continue
			}
			losses = append(losses, codec.Degraded("item", "provider-private item cannot be represented by Gemini", model.Gemini))
		}
	}
	candidate := map[string]any{
		"index": 0, "content": map[string]any{"role": "model", "parts": parts},
		"finishReason": codec.GeminiFinish(response.Finish.Kind),
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
	if usage := codec.EncodeGeminiUsage(response.Usage); usage != nil {
		root["usageMetadata"] = usage
	}
	if codec.FinishLoss(response.Finish.Kind, model.Gemini) {
		losses = append(losses, model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: string(response.Finish.Kind), To: model.Gemini})
	}
	losses = append(losses, codec.RawFields(root, response.Raw, model.Gemini, "raw")...)
	losses = append(losses, codec.UsageLosses(response.Usage, model.Gemini)...)
	return model.Marshal(root), losses, nil
}

func (Codec) EncodeError(failure *model.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	return status, model.Marshal(map[string]any{"error": map[string]any{"code": status, "message": failure.Message, "status": errorStatus(failure.Kind)}})
}
