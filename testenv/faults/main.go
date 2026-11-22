// Command faults serves deterministic upstream failures for the acceptance lab.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func main() {
	address := flag.String("listen", "127.0.0.1:18080", "listen address")
	flag.Parse()
	http.HandleFunc("/chat/text", chatText)
	http.HandleFunc("/chat/review", chatReview)
	http.HandleFunc("/chat/fail", chatFail)
	http.HandleFunc("/chat/partial", chatPartial)
	http.HandleFunc("/chat/tool", chatTool)
	http.HandleFunc("/chat/tool-sibling", chatToolSibling)
	http.HandleFunc("/chat/multi", chatMulti)
	http.HandleFunc("/chat/echo", chatEcho)
	http.HandleFunc("/messages/echo", messagesEcho)
	http.HandleFunc("/v1/embeddings", embeddings)
	if err := http.ListenAndServe(*address, nil); err != nil {
		panic(err)
	}
}

func chatText(writer http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	if streaming, _ := payload["stream"].(bool); !streaming {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"id": "fault", "model": "fault", "object": "chat.completion",
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "FAULT_OK"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
		return
	}
	stream(writer, []string{
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{"content":"FAULT_OK"}}]}`,
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
		`[DONE]`,
	})
}

func chatReview(writer http.ResponseWriter, request *http.Request) {
	stream(writer, []string{
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{"content":"REVIEW_BAD"}}]}`,
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
		`[DONE]`,
	})
}

func chatFail(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]any{"message": "fault", "type": "server_error"}})
}

func chatPartial(writer http.ResponseWriter, request *http.Request) {
	stream(writer, []string{
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{"content":"PARTIAL"}}]}`,
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_partial","type":"function","function":{"name":"lookup","arguments":"{\"city\":"}}]}}]}`,
	})
}

func chatTool(writer http.ResponseWriter, request *http.Request) {
	stream(writer, []string{
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_fault","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}}]}`,
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
		`[DONE]`,
	})
}

func chatToolSibling(writer http.ResponseWriter, request *http.Request) {
	stream(writer, []string{
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{"content":"delivered"}}]}`,
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"one","arguments":"{}"}},{"index":1,"id":"call_2","type":"function","function":{"name":"two","arguments":"{}"}}]}}]}`,
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`,
		`[DONE]`,
	})
}

func chatMulti(writer http.ResponseWriter, request *http.Request) {
	stream(writer, []string{
		`{"id":"chatcmpl-fault","model":"fault","choices":[{"index":0,"delta":{"content":"MULTI_0"}},{"index":1,"delta":{"content":"MULTI_1"}}]}`,
		`{"id":"chatcmpl-fault","model":"fault","choices":[{"index":0,"delta":{},"finish_reason":"stop"},{"index":1,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`,
		`[DONE]`,
	})
}

func chatEcho(writer http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	text := fmt.Sprintf("body=%s organization=%s beta=%s static=%s cookie=%s", string(body),
		request.Header.Get("OpenAI-Organization"), request.Header.Get("OpenAI-Beta"),
		request.Header.Get("X-Lab-Static"), request.Header.Get("Cookie"))
	encoded, _ := json.Marshal(text)
	stream(writer, []string{
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{"content":` + string(encoded) + `}}]}`,
		`{"id":"fault","model":"fault","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
		`[DONE]`,
	})
}

func messagesEcho(writer http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	text := fmt.Sprintf("max_tokens=%v", payload["max_tokens"])
	message(writer, text)
}

func embeddings(writer http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"object": "list",
		"model":  payload["model"],
		"data":   []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{0.1, 0.2}}},
	})
}

func message(writer http.ResponseWriter, text string) {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.WriteHeader(http.StatusOK)
	flusher, _ := writer.(http.Flusher)
	write := func(event, data string) {
		_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, data)
		if flusher != nil {
			flusher.Flush()
		}
	}
	write("message_start", `{"type":"message_start","message":{"id":"msg_fault","type":"message","role":"assistant","model":"fault","content":[],"usage":{"input_tokens":1,"output_tokens":1}}}`)
	encoded, _ := json.Marshal(text)
	write("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	write("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":`+string(encoded)+`}}`)
	write("content_block_stop", `{"type":"content_block_stop","index":0}`)
	write("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`)
	write("message_stop", `{"type":"message_stop"}`)
}

func stream(writer http.ResponseWriter, frames []string) {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.WriteHeader(http.StatusOK)
	flusher, _ := writer.(http.Flusher)
	for _, frame := range frames {
		if strings.HasPrefix(frame, "event:") {
			_, _ = io.WriteString(writer, frame+"\n\n")
		} else if frame == "[DONE]" {
			_, _ = io.WriteString(writer, "data: [DONE]\n\n")
		} else {
			_, _ = io.WriteString(writer, "data: "+frame+"\n\n")
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}
