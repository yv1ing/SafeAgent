package gatewayhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/pipeline"
)

// responsesSocket serves the Responses WebSocket: one create at a time and a cancel that aborts it.
// The default origin policy rejects a cross-origin browser connection; agent clients send no
// Origin and authenticate with their API key.
func (handler *Handler) responsesSocket(ctx *gin.Context) {
	conn, err := websocket.Accept(ctx.Writer, ctx.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	serveContext, cancel := context.WithCancel(ctx.Request.Context())
	defer cancel()
	writer := &socketWriter{conn: conn, ctx: serveContext}
	var mu sync.Mutex
	busy := false
	for {
		_, data, err := conn.Read(serveContext)
		if err != nil {
			return
		}
		root, err := model.DecodeObject(data)
		if err != nil {
			writer.writeError("invalid_event", "The event must be a JSON object.")
			continue
		}
		switch model.String(root["type"]) {
		case "response.create":
			mu.Lock()
			if busy {
				mu.Unlock()
				writer.writeError("response_in_progress", "A response is already running on this connection.")
				continue
			}
			busy = true
			mu.Unlock()
			payload := createPayload(root)
			go func() {
				handler.pipeline.Serve(serveContext, pipeline.Request{
					Kind: model.Responses, Header: ctx.Request.Header, QueryKey: ctx.Query("key"), Body: payload, ForceStream: true,
				}, writer)
				mu.Lock()
				busy = false
				mu.Unlock()
			}()
		case "response.cancel":
			id := model.String(root["response_id"])
			cancelled, failure := handler.pipeline.CancelResponse(ctx.Request.Context(), pipeline.BearerToken(ctx.Request.Header, ctx.Query("key")), id)
			if failure != nil {
				writer.writeError(failure.Code, failure.Message)
				continue
			}
			if !cancelled {
				writer.writeError("response_not_running", "The response is not running.")
			}
		default:
			writer.writeError("unsupported_event", "The event type is not supported.")
		}
	}
}

func createPayload(root map[string]any) []byte {
	if response := model.Object(root["response"]); response != nil {
		return model.Marshal(response)
	}
	delete(root, "type")
	return model.Marshal(root)
}

type socketWriter struct {
	conn *websocket.Conn
	ctx  context.Context
}

func (writer *socketWriter) Header() http.Header    { return http.Header{} }
func (writer *socketWriter) WriteHeader(status int) {}
func (writer *socketWriter) Flush()                 {}

func (writer *socketWriter) Write(data []byte) (int, error) {
	written := 0
	for _, block := range bytes.Split(data, []byte("\n\n")) {
		for _, line := range bytes.Split(block, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
				continue
			}
			if err := writer.conn.Write(writer.ctx, websocket.MessageText, payload); err != nil {
				return 0, err
			}
			written++
		}
	}
	if written == 0 {
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) > 0 && !bytes.HasPrefix(trimmed, []byte(":")) {
			if err := writer.conn.Write(writer.ctx, websocket.MessageText, trimmed); err != nil {
				return 0, err
			}
		}
	}
	return len(data), nil
}

func (writer *socketWriter) writeError(code, message string) {
	payload, _ := json.Marshal(map[string]any{"type": "error", "code": code, "message": message})
	_ = writer.conn.Write(writer.ctx, websocket.MessageText, payload)
}
