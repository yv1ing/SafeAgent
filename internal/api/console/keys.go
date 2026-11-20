package console

import (
	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type AgentKeyView struct {
	ID                string `json:"id" required:"true"`
	Label             string `json:"label"`
	Prefix            string `json:"prefix"`
	RequestsPerMinute int    `json:"requests_per_minute"`
	Concurrency       int    `json:"concurrency"`
	TrustLevel        int    `json:"trust_level"`
	Role              string `json:"role"`
	Enabled           bool   `json:"enabled"`
	Revoked           bool   `json:"revoked"`
	CreatedAt         string `json:"created_at"`
	LastUsedAt        string `json:"last_used_at,omitempty"`
	Secret            string `json:"secret,omitempty" description:"Returned once, when the key is created"`
}

type AgentKeyInput struct {
	Label             string `json:"label" required:"true"`
	RequestsPerMinute int    `json:"requests_per_minute"`
	Concurrency       int    `json:"concurrency"`
	TrustLevel        int    `json:"trust_level"`
	Role              string `json:"role" enum:"agent,user,service"`
	Enabled           bool   `json:"enabled"`
}

type createKeyInput struct {
	projectPath
	AgentKeyInput
}

type agentKeyPath struct {
	ProjectID string `path:"project_id" json:"-" required:"true"`
	KeyID     string `path:"key_id" json:"-" required:"true"`
}

type updateKeyInput struct {
	agentKeyPath
	AgentKeyInput
}

func agentKeyView(value *entity.AgentKey) AgentKeyView {
	return AgentKeyView{ID: value.ID, Label: value.Label, Prefix: value.Prefix, RequestsPerMinute: value.RequestsPerMinute, Concurrency: value.Concurrency, TrustLevel: value.TrustLevel, Role: value.Role, Enabled: value.Enabled, Revoked: value.RevokedAt != nil, CreatedAt: stamp(value.CreatedAt), LastUsedAt: stampOptional(value.LastUsedAt)}
}

func (input AgentKeyInput) service() service.KeyInput {
	return service.KeyInput{Label: input.Label, RequestsPerMinute: input.RequestsPerMinute, Concurrency: input.Concurrency, TrustLevel: input.TrustLevel, Role: input.Role, Enabled: input.Enabled}
}

func (console *Console) registerKeys() {
	console.add("GET", "/projects/{project_id}/keys", "listKeys", projectPath{}, []AgentKeyView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		keys, err := console.Key.Keys(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
		if err != nil {
			return nil, err
		}
		return viewAll(keys, agentKeyView), nil
	})
	console.add("POST", "/projects/{project_id}/keys", "createKey", createKeyInput{}, AgentKeyView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input AgentKeyInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		result, err := console.Key.CreateKey(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), input.service())
		if err != nil {
			return nil, err
		}
		view := agentKeyView(result.Key)
		view.Secret = result.Secret
		return view, nil
	})
	console.add("PUT", "/projects/{project_id}/keys/{key_id}", "updateKey", updateKeyInput{}, AgentKeyView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input AgentKeyInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Key.UpdateKey(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("key_id"), input.service())
		if err != nil {
			return nil, err
		}
		return agentKeyView(updated), nil
	})
	console.add("DELETE", "/projects/{project_id}/keys/{key_id}", "revokeKey", agentKeyPath{}, AgentKeyView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		revoked, err := console.Key.RevokeKey(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("key_id"))
		if err != nil {
			return nil, err
		}
		return agentKeyView(revoked), nil
	})
}
