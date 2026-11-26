package console

import (
	"errors"

	"github.com/gin-gonic/gin"
	access "github.com/yv1ing/SafeAgent/internal/service/access"
	diagnosis "github.com/yv1ing/SafeAgent/internal/service/diagnosis"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type DecisionModelView struct {
	Enabled          bool    `json:"enabled" required:"true"`
	Kind             string  `json:"kind" required:"true" enum:"typesafe,openai"`
	Endpoint         string  `json:"endpoint" required:"true"`
	Model            string  `json:"model" required:"true"`
	APIKeySet        bool    `json:"api_key_set" required:"true"`
	TimeoutSeconds   int     `json:"timeout_seconds" required:"true"`
	AllowPrivate     bool    `json:"allow_private" required:"true"`
	UseRaw           bool    `json:"use_raw" required:"true"`
	DailyTokenBudget int64   `json:"daily_token_budget" required:"true"`
	AuthorizeBelow   float64 `json:"authorize_below" required:"true"`
	UpdatedAt        string  `json:"updated_at" required:"true"`
}

type DecisionModelInput struct {
	Enabled          bool    `json:"enabled"`
	Kind             string  `json:"kind" required:"true" enum:"typesafe,openai"`
	Endpoint         string  `json:"endpoint"`
	Model            string  `json:"model" required:"true"`
	APIKey           string  `json:"api_key"`
	TimeoutSeconds   int     `json:"timeout_seconds"`
	AllowPrivate     bool    `json:"allow_private"`
	UseRaw           bool    `json:"use_raw"`
	DailyTokenBudget int64   `json:"daily_token_budget"`
	AuthorizeBelow   float64 `json:"authorize_below" minimum:"0" maximum:"1"`
}

type updateDecisionModelInput struct {
	projectPath
	DecisionModelInput
}

func decisionModelView(value *entity.DecisionModelConfig) DecisionModelView {
	return DecisionModelView{
		Enabled: value.Enabled, Kind: value.Kind, Endpoint: value.Endpoint, Model: value.Model,
		APIKeySet: value.APIKey != "", TimeoutSeconds: value.TimeoutSeconds,
		AllowPrivate: value.AllowPrivate, UseRaw: value.UseRaw,
		DailyTokenBudget: value.DailyTokenBudget, AuthorizeBelow: value.AuthorizeBelow,
		UpdatedAt: stamp(value.UpdatedAt),
	}
}

func (console *Console) registerDecisionModel() {
	console.add("GET", "/projects/{project_id}/decision-model", "getDecisionModel", projectPath{}, DecisionModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		stored, err := console.Analysis.StoredDecisionConfig(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
		if err != nil {
			return nil, err
		}
		return decisionModelView(stored), nil
	})
	console.add("PUT", "/projects/{project_id}/decision-model", "updateDecisionModel", updateDecisionModelInput{}, DecisionModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input DecisionModelInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		config := entity.DecisionModelConfig{
			Enabled: input.Enabled, Kind: input.Kind, Endpoint: input.Endpoint, Model: input.Model,
			TimeoutSeconds: input.TimeoutSeconds, AllowPrivate: input.AllowPrivate, UseRaw: input.UseRaw,
			DailyTokenBudget: input.DailyTokenBudget, AuthorizeBelow: input.AuthorizeBelow,
		}
		saved, err := console.Analysis.SaveDecisionConfig(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), config, input.APIKey)
		if err != nil {
			if errors.Is(err, diagnosis.ErrInvalidDecisionConfig) {
				return nil, access.Invalid("invalid_decision_model")
			}
			return nil, err
		}
		return decisionModelView(saved), nil
	})
}
