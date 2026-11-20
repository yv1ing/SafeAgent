package responses

import (
	"time"

	"errors"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

var responseKeys = []string{"id", "object", "model", "status", "output", "usage", "incomplete_details", "error"}

func (Codec) DecodeResponse(body []byte, peer format.Peer) (*protocol.Response, []protocol.Loss, error) {
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, nil, errors.New("response must be a JSON object")
	}
	if failure := protocol.Object(root["error"]); failure != nil && protocol.String(root["status"]) != "failed" {
		return nil, nil, &format.StreamError{Code: protocol.String(failure["code"]), Message: protocol.String(failure["message"])}
	}
	messages, err := decodeItems(protocol.Array(root["output"]))
	if err != nil {
		return nil, nil, err
	}
	response := &protocol.Response{ResponseMeta: protocol.ResponseMeta{ID: protocol.String(root["id"]), Model: protocol.String(root["model"])}, Usage: format.ParseResponsesUsage(root), Extras: format.UnmodelledFields(protocol.Responses, root, responseKeys...)}
	for _, message := range messages {
		response.Blocks = append(response.Blocks, message.Blocks...)
	}
	response.Finish, response.Failure = finishOf(root, response.Blocks)
	return response, nil, nil
}

// finishOf maps a response object's status; a completed response that called a function reports
// tool_calls, which Responses itself has no word for.
func finishOf(response map[string]any, blocks []protocol.Block) (protocol.FinishReason, *protocol.Error) {
	switch protocol.String(response["status"]) {
	case "incomplete":
		if protocol.String(protocol.Object(response["incomplete_details"])["reason"]) == "content_filter" {
			return protocol.FinishContentFilter, nil
		}
		return protocol.FinishLength, nil
	case "failed":
		failure := protocol.Object(response["error"])
		return protocol.FinishError, &protocol.Error{Kind: "upstream_failure", Code: protocol.String(failure["code"]), Message: protocol.String(failure["message"])}
	}
	for _, block := range blocks {
		if block.Kind == protocol.KindToolCall && block.ToolCall != nil && block.ToolCall.Kind != protocol.ToolServer {
			return protocol.FinishToolCalls, nil
		}
	}
	return protocol.FinishStop, nil
}

func (Codec) EncodeResponse(response *protocol.Response, peer format.Peer) ([]byte, []protocol.Loss, error) {
	context := format.NewPartContext(protocol.Responses, peer)
	context.Output = true
	items, losses, err := encodeItems(protocol.Message{Role: protocol.RoleAssistant, Blocks: format.DropBlank(response.Blocks)}, context, "blocks")
	if err != nil {
		return nil, losses, err
	}
	for index, item := range items {
		object := item.(map[string]any)
		assignItemID(object, index)
		if object["type"] == "message" {
			object["status"] = "completed"
		}
	}
	root := map[string]any{
		"id":     response.ID,
		"object": "response",
		"model":  response.Model,
		"status": format.ResponsesStatus(response.Finish),
		"output": items,
	}
	if status := root["status"]; status == "incomplete" {
		root["incomplete_details"] = map[string]any{"reason": format.ResponsesIncompleteReason(response.Finish)}
	}
	if response.Failure != nil {
		root["error"] = map[string]any{"code": response.Failure.Code, "message": response.Failure.Message}
	}
	if usage := format.EncodeResponsesUsage(response.Usage); usage != nil {
		root["usage"] = usage
	}
	if format.FinishLoss(response.Finish, protocol.Responses) {
		losses = append(losses, protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Detail: string(response.Finish), To: protocol.Responses})
	}
	losses = append(losses, format.ApplyExtras(root, response.Extras, protocol.Responses, "extras")...)
	if _, replayed := root["created_at"]; !replayed {
		root["created_at"] = time.Now().Unix()
	}
	return protocol.Marshal(root), losses, nil
}

// assignItemID gives an output item an identifier unless it carries its own; every item of a
// response needs a distinct one.
func assignItemID(item map[string]any, index int) {
	if protocol.String(item["id"]) != "" {
		return
	}
	prefix := map[string]string{"message": "msg_", "reasoning": "rs_", "function_call": "fc_", "custom_tool_call": "ctc_"}[protocol.String(item["type"])]
	if prefix != "" {
		item["id"] = prefix + strconv.Itoa(index)
	}
}

func (Codec) EncodeError(failure *protocol.Error, streaming bool) (int, []byte) {
	status := failure.Status
	if status == 0 {
		status = 400
	}
	if streaming {
		return status, protocol.Marshal(map[string]any{"type": "error", "code": failure.Code, "message": failure.Message, "param": nil, "sequence_number": 0})
	}
	return status, protocol.Marshal(map[string]any{"error": map[string]any{"message": failure.Message, "type": errorType(failure.Kind), "code": failure.Code}})
}

func errorType(kind string) string {
	switch kind {
	case "invalid_request", "unauthorized":
		return "invalid_request_error"
	case "rate_limited":
		return "rate_limit_error"
	}
	return "server_error"
}
