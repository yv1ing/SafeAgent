package console

import (
	"github.com/gin-gonic/gin"
	control "github.com/yv1ing/SafeAgent/internal/service/control"
)

type CatalogModelView struct {
	ID              string   `json:"id" required:"true"`
	ProviderID      string   `json:"provider_id" required:"true"`
	UpstreamName    string   `json:"upstream_name" required:"true"`
	DisplayName     string   `json:"display_name,omitempty"`
	Kind            string   `json:"kind" required:"true" enum:"generation,embedding,moderation"`
	Formats         []string `json:"formats" required:"true"`
	InputPrice      int64    `json:"input_price"`
	OutputPrice     int64    `json:"output_price"`
	CacheReadPrice  int64    `json:"cache_read_price"`
	CacheWritePrice int64    `json:"cache_write_price"`
	Currency        string   `json:"currency" required:"true"`
	Enabled         bool     `json:"enabled"`
	Version         int64    `json:"version" required:"true"`
	CreatedAt       string   `json:"created_at" required:"true"`
	UpdatedAt       string   `json:"updated_at" required:"true"`
	DeletedAt       string   `json:"deleted_at,omitempty"`
}

type CatalogModelInput struct {
	ProviderID      string `json:"provider_id" required:"true"`
	UpstreamName    string `json:"upstream_name" required:"true"`
	DisplayName     string `json:"display_name,omitempty"`
	Kind            string `json:"kind" required:"true" enum:"generation,embedding,moderation"`
	InputPrice      int64  `json:"input_price"`
	OutputPrice     int64  `json:"output_price"`
	CacheReadPrice  int64  `json:"cache_read_price"`
	CacheWritePrice int64  `json:"cache_write_price"`
	Currency        string `json:"currency"`
	Enabled         bool   `json:"enabled"`
	Version         int64  `json:"version" description:"Required on update, ignored on create"`
}

type CatalogModelBatchInput struct {
	Models []CatalogModelInput `json:"models" required:"true"`
}

type modelPath struct {
	ModelID string `path:"model_id" json:"-" required:"true"`
}

type updateCatalogModelInput struct {
	modelPath
	CatalogModelInput
}

func (input CatalogModelInput) service() control.ModelInput {
	return control.ModelInput{
		ProviderID: input.ProviderID, UpstreamName: input.UpstreamName, DisplayName: input.DisplayName,
		Kind: input.Kind, InputPrice: input.InputPrice, OutputPrice: input.OutputPrice,
		CacheReadPrice: input.CacheReadPrice, CacheWritePrice: input.CacheWritePrice,
		Currency: input.Currency, Enabled: input.Enabled, Version: input.Version,
	}
}

func catalogModelView(value *control.ModelView) CatalogModelView {
	model := value.Model
	return CatalogModelView{
		ID: model.ID, ProviderID: model.ProviderID, UpstreamName: model.UpstreamName,
		DisplayName: model.DisplayName, Kind: string(model.Kind), Formats: nonNil(value.Formats),
		InputPrice: model.InputPrice, OutputPrice: model.OutputPrice,
		CacheReadPrice: model.CacheReadPrice, CacheWritePrice: model.CacheWritePrice,
		Currency: model.Currency, Enabled: model.Enabled, Version: model.Version,
		CreatedAt: stamp(model.CreatedAt), UpdatedAt: stamp(model.UpdatedAt), DeletedAt: stampOptional(model.DeletedAt),
	}
}

func (console *Console) registerModels() {
	console.add("GET", "/models", "listModels", nil, []CatalogModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		views, err := console.Control.Models(ctx.Request.Context(), identity.actor(), control.ModelFilter{
			ProviderID:     ctx.Query("provider_id"),
			Unassociated:   ctx.Query("unassociated") == "true",
			IncludeDeleted: ctx.Query("include_deleted") == "true",
		})
		if err != nil {
			return nil, err
		}
		return viewAll(views, catalogModelView), nil
	})
	console.add("POST", "/models", "createModel", CatalogModelInput{}, CatalogModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input CatalogModelInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		created, err := console.Control.CreateModel(ctx.Request.Context(), identity.actor(), input.service())
		if err != nil {
			return nil, err
		}
		return catalogModelView(created), nil
	})
	console.add("POST", "/models/batch", "createModelBatch", CatalogModelBatchInput{}, []CatalogModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input CatalogModelBatchInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		inputs := make([]control.ModelInput, 0, len(input.Models))
		for _, model := range input.Models {
			inputs = append(inputs, model.service())
		}
		created, err := console.Control.CreateModels(ctx.Request.Context(), identity.actor(), inputs)
		if err != nil {
			return nil, err
		}
		return viewAll(created, catalogModelView), nil
	})
	console.add("GET", "/models/{model_id}", "getModel", modelPath{}, CatalogModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		view, err := console.Control.Model(ctx.Request.Context(), identity.actor(), ctx.Param("model_id"))
		if err != nil {
			return nil, err
		}
		return catalogModelView(view), nil
	})
	console.add("PUT", "/models/{model_id}", "updateModel", updateCatalogModelInput{}, CatalogModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input CatalogModelInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Control.UpdateModel(ctx.Request.Context(), identity.actor(), ctx.Param("model_id"), input.service())
		if err != nil {
			return nil, err
		}
		return catalogModelView(updated), nil
	})
	console.add("DELETE", "/models/{model_id}", "deleteModel", modelPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		acknowledge := ctx.Query("acknowledge") == "true"
		detachTargets := ctx.Query("detach_targets") == "true"
		return nil, console.Control.DeleteModel(ctx.Request.Context(), identity.actor(), ctx.Param("model_id"), acknowledge, detachTargets)
	})
	console.add("POST", "/models/{model_id}/restore", "restoreModel", modelPath{}, CatalogModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		restored, err := console.Control.RestoreModel(ctx.Request.Context(), identity.actor(), ctx.Param("model_id"))
		if err != nil {
			return nil, err
		}
		return catalogModelView(restored), nil
	})
	console.add("DELETE", "/models/{model_id}/targets", "detachModelTargets", modelPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		_, err := console.Control.DetachModelTargets(ctx.Request.Context(), identity.actor(), ctx.Param("model_id"))
		return nil, err
	})
}
