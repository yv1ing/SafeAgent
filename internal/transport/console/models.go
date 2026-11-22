package console

import (
	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
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
}

type CatalogModelInput struct {
	ProviderID      string   `json:"provider_id" required:"true"`
	UpstreamName    string   `json:"upstream_name" required:"true"`
	DisplayName     string   `json:"display_name,omitempty"`
	Kind            string   `json:"kind" required:"true" enum:"generation,embedding,moderation"`
	Formats         []string `json:"formats" required:"true"`
	InputPrice      int64    `json:"input_price"`
	OutputPrice     int64    `json:"output_price"`
	CacheReadPrice  int64    `json:"cache_read_price"`
	CacheWritePrice int64    `json:"cache_write_price"`
	Currency        string   `json:"currency"`
	Enabled         bool     `json:"enabled"`
	Version         int64    `json:"version" description:"Required on update, ignored on create"`
}

type modelPath struct {
	ModelID string `path:"model_id" json:"-" required:"true"`
}

type updateCatalogModelInput struct {
	modelPath
	CatalogModelInput
}

func catalogModelView(value *entity.Model) CatalogModelView {
	return CatalogModelView{
		ID: value.ID, ProviderID: value.ProviderID, UpstreamName: value.UpstreamName,
		DisplayName: value.DisplayName, Kind: string(value.Kind), Formats: nonNil(value.Formats),
		InputPrice: value.InputPrice, OutputPrice: value.OutputPrice,
		CacheReadPrice: value.CacheReadPrice, CacheWritePrice: value.CacheWritePrice,
		Currency: value.Currency, Enabled: value.Enabled, Version: value.Version, CreatedAt: stamp(value.CreatedAt),
	}
}

func (input CatalogModelInput) service() service.ModelInput {
	return service.ModelInput{
		ProviderID: input.ProviderID, UpstreamName: input.UpstreamName, DisplayName: input.DisplayName,
		Kind: input.Kind, Formats: input.Formats, InputPrice: input.InputPrice, OutputPrice: input.OutputPrice,
		CacheReadPrice: input.CacheReadPrice, CacheWritePrice: input.CacheWritePrice,
		Currency: input.Currency, Enabled: input.Enabled, Version: input.Version,
	}
}

func (console *Console) registerModels() {
	console.add("GET", "/models", "listModels", nil, []CatalogModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		models, err := console.Model.List(ctx.Request.Context(), identity.actor())
		if err != nil {
			return nil, err
		}
		return viewAll(models, func(model *entity.Model) CatalogModelView {
			return catalogModelView(model)
		}), nil
	})
	console.add("POST", "/models", "createModel", CatalogModelInput{}, CatalogModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input CatalogModelInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		created, err := console.Model.Create(ctx.Request.Context(), identity.actor(), input.service())
		if err != nil {
			return nil, err
		}
		return catalogModelView(created), nil
	})
	console.add("GET", "/models/{model_id}", "getModel", modelPath{}, CatalogModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		found, err := console.Model.Get(ctx.Request.Context(), identity.actor(), ctx.Param("model_id"))
		if err != nil {
			return nil, err
		}
		return catalogModelView(found), nil
	})
	console.add("PUT", "/models/{model_id}", "updateModel", updateCatalogModelInput{}, CatalogModelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input CatalogModelInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Model.Update(ctx.Request.Context(), identity.actor(), ctx.Param("model_id"), input.service())
		if err != nil {
			return nil, err
		}
		return catalogModelView(updated), nil
	})
	console.add("DELETE", "/models/{model_id}", "deleteModel", modelPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		_, err := console.Model.Delete(ctx.Request.Context(), identity.actor(), ctx.Param("model_id"))
		return nil, err
	})
}
