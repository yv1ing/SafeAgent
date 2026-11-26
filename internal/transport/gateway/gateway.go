// Package gateway adapts the proxy pipeline to HTTP. It owns route registration, body limits,
// Gemini framing and the protocol-shaped responses; the pipeline itself knows nothing about gin.
package gateway

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/pipeline"
)

type Handler struct {
	pipeline *pipeline.Pipeline
	settings pipeline.Settings
}

func NewHandler(service *pipeline.Pipeline, settings pipeline.Settings) *Handler {
	return &Handler{pipeline: service, settings: settings}
}

func (handler *Handler) Register(router *gin.Engine) {
	router.POST("/v1/chat/completions", handler.endpoint(model.Chat))
	router.POST("/v1/responses", handler.endpoint(model.Responses))
	router.POST("/v1/messages", handler.endpoint(model.Messages))
	router.POST("/v1/messages/count_tokens", handler.countTokens(model.Messages, classify.CountTokens))
	router.POST("/v1/responses/input_tokens", handler.countTokens(model.Responses, classify.CountTokens))
	router.GET("/v1/models", handler.models)
	router.GET("/v1/models/:model", handler.model)
	router.GET("/v1beta/models", handler.geminiModels)
	router.GET("/v1beta/models/:model", handler.geminiModel)
	router.POST("/v1beta/models/*action", handler.geminiAction)
	router.GET("/v1/responses", handler.responsesSocket)
	router.GET("/v1/responses/:id", handler.retrieveResponse)
	router.GET("/v1/responses/:id/input_items", handler.inputItems)
	router.POST("/v1/responses/:id/cancel", handler.cancelResponse)
	router.DELETE("/v1/responses/:id", handler.deleteResponse)
	router.POST("/v1/embeddings", handler.passthrough(provider.AuxiliaryEmbeddings, classify.Embeddings))
	router.POST("/v1/moderations", handler.passthrough(provider.AuxiliaryModerations, classify.Moderations))
}

func (handler *Handler) endpoint(kind model.Format) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		body, failure := handler.readBody(ctx)
		if failure != nil {
			handler.pipeline.WriteError(ctx.Writer, kind, failure)
			return
		}
		handler.pipeline.Serve(ctx.Request.Context(), pipeline.Request{
			Kind:      kind,
			Operation: classify.Generation,
			Header:    ctx.Request.Header,
			Body:      body,
			Model:     ctx.GetHeader("X-SafeAgent-Model"),
		}, ctx.Writer)
	}
}

func (handler *Handler) readBody(ctx *gin.Context) ([]byte, *model.Error) {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, handler.settings.MaxRequestBytes)
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		return nil, &model.Error{Kind: "invalid_request", Class: model.ClassInvalid, Code: "request_too_large", Message: "The request body exceeds the configured limit.", Status: 413}
	}
	return body, nil
}

func (handler *Handler) geminiAction(ctx *gin.Context) {
	raw := strings.TrimPrefix(ctx.Param("action"), "/")
	name, method, found := strings.Cut(raw, ":")
	if !found || name == "" {
		handler.pipeline.WriteError(ctx.Writer, model.Gemini, &model.Error{Kind: "invalid_request", Class: model.ClassInvalid, Code: "invalid_action", Message: "The Gemini action must include a method.", Status: 400})
		return
	}
	name = strings.TrimPrefix(name, "models/")
	body, failure := handler.readBody(ctx)
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, model.Gemini, failure)
		return
	}
	switch method {
	case "generateContent", "streamGenerateContent":
	case "countTokens":
		handler.writeCount(ctx, model.Gemini, name, body, false, classify.CountTokens)
		return
	default:
		handler.pipeline.WriteError(ctx.Writer, model.Gemini, &model.Error{Kind: "invalid_request", Class: model.ClassInvalid, Code: "unsupported_action", Message: "The Gemini action is not supported.", Status: 400})
		return
	}
	stream := method == "streamGenerateContent"
	writer := pipeline.Writer(ctx.Writer)
	var array *geminiArrayWriter
	if stream && !geminiSSE(ctx) {
		array = newGeminiArrayWriter(ctx.Writer)
		writer = array
	}
	handler.pipeline.Serve(ctx.Request.Context(), pipeline.Request{
		Kind:        model.Gemini,
		Operation:   classify.Generation,
		Header:      ctx.Request.Header,
		QueryKey:    ctx.Query("key"),
		Body:        body,
		Model:       name,
		ForceStream: stream,
	}, writer)
	if array != nil {
		array.Finish()
	}
}

func geminiSSE(ctx *gin.Context) bool {
	if strings.EqualFold(ctx.Query("alt"), "sse") {
		return true
	}
	return strings.Contains(ctx.GetHeader("Accept"), "text/event-stream")
}

func (handler *Handler) countTokens(kind model.Format, operation classify.Operation) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		body, failure := handler.readBody(ctx)
		if failure != nil {
			handler.pipeline.WriteError(ctx.Writer, kind, failure)
			return
		}
		handler.writeCount(ctx, kind, ctx.GetHeader("X-SafeAgent-Model"), body, true, operation)
	}
}

func (handler *Handler) writeCount(ctx *gin.Context, kind model.Format, name string, body []byte, openAI bool, operation classify.Operation) {
	count, estimated, failure := handler.pipeline.CountTokens(ctx.Request.Context(), pipeline.Request{
		Kind:      kind,
		Operation: operation,
		Header:    ctx.Request.Header,
		QueryKey:  ctx.Query("key"),
		Body:      body,
		Model:     name,
	})
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, kind, failure)
		return
	}
	if estimated {
		ctx.Header("X-SafeAgent-Token-Estimate", "true")
	}
	if kind == model.Gemini {
		ctx.JSON(http.StatusOK, map[string]any{"totalTokens": count})
		return
	}
	if openAI && kind == model.Responses {
		ctx.JSON(http.StatusOK, map[string]any{"object": "response.input_tokens", "input_tokens": count})
		return
	}
	ctx.JSON(http.StatusOK, map[string]any{"input_tokens": count})
}

func (handler *Handler) models(ctx *gin.Context) {
	names, failure := handler.pipeline.Models(ctx.Request.Context(), pipeline.BearerToken(ctx.Request.Header, ""))
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, model.Chat, failure)
		return
	}
	if ctx.GetHeader("anthropic-version") != "" {
		list := make([]any, 0, len(names))
		for _, name := range names {
			list = append(list, anthropicModel(name))
		}
		ctx.JSON(200, map[string]any{"data": list, "has_more": false, "first_id": "", "last_id": ""})
		return
	}
	list := make([]any, 0, len(names))
	for _, name := range names {
		list = append(list, openAIModel(name))
	}
	ctx.JSON(200, map[string]any{"object": "list", "data": list})
}

func (handler *Handler) model(ctx *gin.Context) {
	route, failure := handler.pipeline.Model(ctx.Request.Context(), pipeline.BearerToken(ctx.Request.Header, ""), ctx.Param("model"))
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, model.Chat, failure)
		return
	}
	if ctx.GetHeader("anthropic-version") != "" {
		ctx.JSON(200, anthropicModel(route.Name))
		return
	}
	ctx.JSON(200, openAIModel(route.Name))
}

func openAIModel(name string) map[string]any {
	return map[string]any{"id": name, "object": "model", "owned_by": "safeagent"}
}

func anthropicModel(name string) map[string]any {
	return map[string]any{"id": name, "type": "model", "display_name": name, "created_at": "1970-01-01T00:00:00Z"}
}

func (handler *Handler) geminiModels(ctx *gin.Context) {
	names, failure := handler.pipeline.Models(ctx.Request.Context(), pipeline.BearerToken(ctx.Request.Header, ctx.Query("key")))
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, model.Gemini, failure)
		return
	}
	list := make([]any, 0, len(names))
	for _, name := range names {
		list = append(list, geminiModel(name))
	}
	ctx.JSON(200, map[string]any{"models": list})
}

func (handler *Handler) geminiModel(ctx *gin.Context) {
	name := strings.TrimPrefix(ctx.Param("model"), "models/")
	route, failure := handler.pipeline.Model(ctx.Request.Context(), pipeline.BearerToken(ctx.Request.Header, ctx.Query("key")), name)
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, model.Gemini, failure)
		return
	}
	ctx.JSON(200, geminiModel(route.Name))
}

func geminiModel(name string) map[string]any {
	return map[string]any{"name": "models/" + name, "displayName": name, "supportedGenerationMethods": []string{"generateContent", "streamGenerateContent", "countTokens"}}
}

func (handler *Handler) retrieveResponse(ctx *gin.Context) {
	payload, failure := handler.pipeline.RetrieveResponse(ctx.Request.Context(), pipeline.BearerToken(ctx.Request.Header, ""), ctx.Param("id"))
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, model.Responses, failure)
		return
	}
	ctx.Data(http.StatusOK, "application/json", payload)
}

func (handler *Handler) inputItems(ctx *gin.Context) {
	encoded, losses, failure := handler.pipeline.InputItems(ctx.Request.Context(), pipeline.BearerToken(ctx.Request.Header, ""), ctx.Param("id"))
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, model.Responses, failure)
		return
	}
	if len(losses) > 0 {
		codes := make([]string, 0, len(losses))
		for _, loss := range losses {
			codes = append(codes, string(loss.Code))
		}
		ctx.Header("X-SafeAgent-Losses", strings.Join(codes, ","))
		ctx.Header("X-SafeAgent-Loss-Count", strconv.Itoa(len(losses)))
	}
	first, last := "", ""
	if len(encoded) > 0 {
		first = stringID(encoded[0])
		last = stringID(encoded[len(encoded)-1])
	}
	ctx.JSON(http.StatusOK, map[string]any{"object": "list", "data": encoded, "first_id": first, "last_id": last, "has_more": false})
}

func stringID(entry any) string {
	if object, ok := entry.(map[string]any); ok {
		if value, ok := object["id"].(string); ok {
			return value
		}
	}
	return ""
}

func (handler *Handler) cancelResponse(ctx *gin.Context) {
	cancelled, failure := handler.pipeline.CancelResponse(ctx.Request.Context(), pipeline.BearerToken(ctx.Request.Header, ""), ctx.Param("id"))
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, model.Responses, failure)
		return
	}
	if !cancelled {
		ctx.JSON(http.StatusConflict, map[string]any{"error": map[string]any{"code": "response_not_running", "message": "The response is not running."}})
		return
	}
	ctx.JSON(http.StatusOK, map[string]any{"id": ctx.Param("id"), "status": "cancelled"})
}

func (handler *Handler) deleteResponse(ctx *gin.Context) {
	if failure := handler.pipeline.DeleteResponse(ctx.Request.Context(), pipeline.BearerToken(ctx.Request.Header, ""), ctx.Param("id")); failure != nil {
		handler.pipeline.WriteError(ctx.Writer, model.Responses, failure)
		return
	}
	ctx.JSON(http.StatusOK, map[string]any{"id": ctx.Param("id"), "deleted": true})
}

func (handler *Handler) passthrough(kind provider.AuxiliaryKind, operation classify.Operation) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		body, failure := handler.readBody(ctx)
		if failure != nil {
			handler.pipeline.WriteError(ctx.Writer, model.Chat, failure)
			return
		}
		payload, failure := handler.pipeline.Passthrough(ctx.Request.Context(), pipeline.Request{
			Kind: model.Chat, Operation: operation, Header: ctx.Request.Header, QueryKey: ctx.Query("key"), Body: body,
		}, kind)
		if failure != nil {
			handler.pipeline.WriteError(ctx.Writer, model.Chat, failure)
			return
		}
		ctx.Data(http.StatusOK, "application/json", payload)
	}
}

// geminiArrayWriter turns the SSE frames the streaming session produces into the JSON array a
// Gemini client without alt=sse expects.
type geminiArrayWriter struct {
	underlying pipeline.Writer
	started    bool
	sse        bool
}

func newGeminiArrayWriter(writer pipeline.Writer) *geminiArrayWriter {
	return &geminiArrayWriter{underlying: writer}
}

func (writer *geminiArrayWriter) Header() http.Header { return writer.underlying.Header() }

func (writer *geminiArrayWriter) WriteHeader(status int) {
	writer.underlying.Header().Set("Content-Type", "application/json")
	writer.underlying.WriteHeader(status)
}

func (writer *geminiArrayWriter) Write(data []byte) (int, error) {
	if !writer.sse && !bytes.Contains(data, []byte("data:")) && !bytes.Contains(data, []byte("event:")) {
		return writer.underlying.Write(data)
	}
	writer.sse = true
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		if !writer.started {
			if _, err := writer.underlying.Write([]byte("[")); err != nil {
				return 0, err
			}
			writer.started = true
		} else {
			if _, err := writer.underlying.Write([]byte(",")); err != nil {
				return 0, err
			}
		}
		if _, err := writer.underlying.Write(payload); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}

func (writer *geminiArrayWriter) Flush() { writer.underlying.Flush() }

func (writer *geminiArrayWriter) Finish() {
	if writer.started {
		_, _ = writer.underlying.Write([]byte("]"))
		writer.underlying.Flush()
	}
}
