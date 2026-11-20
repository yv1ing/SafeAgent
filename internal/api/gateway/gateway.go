// Package gateway adapts the proxy pipeline to HTTP. It owns routing, body limits and the
// protocol-shaped error responses; the pipeline itself knows nothing about gin.
package gateway

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/service/gateway"
)

type Handler struct {
	pipeline *gateway.Pipeline
	settings gateway.Settings
}

func NewHandler(pipeline *gateway.Pipeline, settings gateway.Settings) *Handler {
	return &Handler{pipeline: pipeline, settings: settings}
}

func (handler *Handler) Register(router *gin.Engine) {
	router.POST("/v1/chat/completions", handler.endpoint(protocol.Chat))
	router.POST("/v1/responses", handler.endpoint(protocol.Responses))
	router.POST("/v1/messages", handler.endpoint(protocol.Messages))
	router.POST("/v1/messages/count_tokens", handler.countTokens)
	router.GET("/v1/models", handler.models)
	router.GET("/v1beta/models", handler.geminiModels)
	router.POST("/v1beta/models/*action", handler.geminiAction)
}

func (handler *Handler) endpoint(kind protocol.Format) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		body, failure := handler.readBody(ctx)
		if failure != nil {
			handler.pipeline.WriteError(ctx.Writer, kind, failure)
			return
		}
		handler.pipeline.Serve(ctx.Request.Context(), gateway.Request{
			Kind:   kind,
			Header: ctx.Request.Header,
			Body:   body,
			Model:  ctx.GetHeader("X-SafeAgent-Model"),
		}, ctx.Writer)
	}
}

func (handler *Handler) readBody(ctx *gin.Context) ([]byte, *protocol.Error) {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, handler.settings.MaxRequestBytes)
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		return nil, &protocol.Error{Kind: "invalid_request", Class: protocol.ClassInvalid, Code: "request_too_large", Message: "The request body exceeds the configured limit.", Status: 413}
	}
	return body, nil
}

func (handler *Handler) geminiAction(ctx *gin.Context) {
	raw := strings.TrimPrefix(ctx.Param("action"), "/")
	name, method, found := strings.Cut(raw, ":")
	if !found || name == "" {
		handler.pipeline.WriteError(ctx.Writer, protocol.Gemini, &protocol.Error{Kind: "invalid_request", Class: protocol.ClassInvalid, Code: "invalid_action", Message: "The Gemini action must include a method.", Status: 400})
		return
	}
	var stream bool
	switch method {
	case "generateContent":
	case "streamGenerateContent":
		stream = true
	default:
		handler.pipeline.WriteError(ctx.Writer, protocol.Gemini, &protocol.Error{Kind: "invalid_request", Class: protocol.ClassInvalid, Code: "unsupported_action", Message: "The Gemini action is not supported.", Status: 400})
		return
	}
	body, failure := handler.readBody(ctx)
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, protocol.Gemini, failure)
		return
	}
	handler.pipeline.Serve(ctx.Request.Context(), gateway.Request{
		Kind:        protocol.Gemini,
		Header:      ctx.Request.Header,
		QueryKey:    ctx.Query("key"),
		Body:        body,
		Model:       name,
		ForceStream: stream,
	}, ctx.Writer)
}

func (handler *Handler) countTokens(ctx *gin.Context) {
	body, failure := handler.readBody(ctx)
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, protocol.Messages, failure)
		return
	}
	count, estimated, failure := handler.pipeline.CountTokens(ctx.Request.Context(), gateway.Request{
		Kind:   protocol.Messages,
		Header: ctx.Request.Header,
		Body:   body,
		Model:  ctx.GetHeader("X-SafeAgent-Model"),
	})
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, protocol.Messages, failure)
		return
	}
	if estimated {
		ctx.Header("X-SafeAgent-Token-Estimate", "true")
	}
	ctx.JSON(http.StatusOK, map[string]any{"input_tokens": count})
}

func (handler *Handler) models(ctx *gin.Context) {
	routes, failure := handler.pipeline.Models(ctx.Request.Context(), gateway.BearerToken(ctx.Request.Header, ""))
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, protocol.Chat, failure)
		return
	}
	if ctx.GetHeader("anthropic-version") != "" {
		list := make([]any, 0, len(routes))
		for _, item := range routes {
			list = append(list, map[string]any{"id": item.Name, "type": "model", "display_name": item.Name, "created_at": "1970-01-01T00:00:00Z"})
		}
		ctx.JSON(200, map[string]any{"data": list, "has_more": false, "first_id": "", "last_id": ""})
		return
	}
	list := make([]any, 0, len(routes))
	for _, item := range routes {
		list = append(list, map[string]any{"id": item.Name, "object": "model", "owned_by": "safeagent"})
	}
	ctx.JSON(200, map[string]any{"object": "list", "data": list})
}

func (handler *Handler) geminiModels(ctx *gin.Context) {
	routes, failure := handler.pipeline.Models(ctx.Request.Context(), gateway.BearerToken(ctx.Request.Header, ctx.Query("key")))
	if failure != nil {
		handler.pipeline.WriteError(ctx.Writer, protocol.Gemini, failure)
		return
	}
	list := make([]any, 0, len(routes))
	for _, item := range routes {
		list = append(list, map[string]any{"name": "models/" + item.Name, "displayName": item.Name, "supportedGenerationMethods": []string{"generateContent", "streamGenerateContent"}})
	}
	ctx.JSON(200, map[string]any{"models": list})
}
