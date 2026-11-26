package console

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	control "github.com/yv1ing/SafeAgent/internal/service/control"
)

type ProviderView struct {
	ID           string          `json:"id" required:"true"`
	Source       string          `json:"source" required:"true" enum:"builtin,custom"`
	Label        string          `json:"label" required:"true"`
	Enabled      bool            `json:"enabled" required:"true"`
	SeedVersion  int             `json:"seed_version" required:"true"`
	Version      int64           `json:"version" required:"true"`
	ChannelCount int             `json:"channel_count" required:"true"`
	Formats      []string        `json:"formats" required:"true"`
	Endpoints    json.RawMessage `json:"endpoints" required:"true" description:"Endpoint catalog keyed by wire format"`
	CreatedAt    string          `json:"created_at" required:"true"`
	UpdatedAt    string          `json:"updated_at" required:"true"`
	DeletedAt    string          `json:"deleted_at,omitempty"`
}

type ProviderInput struct {
	Label     string          `json:"label" required:"true"`
	Endpoints json.RawMessage `json:"endpoints" required:"true" description:"Endpoint catalog keyed by wire format"`
	Enabled   bool            `json:"enabled"`
	Version   int64           `json:"version" description:"Required on update, ignored on create"`
}

type providerPath struct {
	ProviderID string `path:"provider_id" json:"-" required:"true"`
}

type updateProviderInput struct {
	providerPath
	ProviderInput
}

func (input ProviderInput) service() control.ProviderInput {
	return control.ProviderInput{Label: input.Label, Endpoints: input.Endpoints, Enabled: input.Enabled, Version: input.Version}
}

func providerView(value *control.ProviderInfo) ProviderView {
	return ProviderView{
		ID: value.ID, Source: value.Source, Label: value.Label, Enabled: value.Enabled,
		SeedVersion: value.SeedVersion, Version: value.Version, ChannelCount: value.ChannelCount,
		Formats: nonNil(value.Formats), Endpoints: value.Endpoints,
		CreatedAt: stamp(value.CreatedAt), UpdatedAt: stamp(value.UpdatedAt), DeletedAt: stampOptional(value.DeletedAt),
	}
}

func (console *Console) registerProviders() {
	console.add("GET", "/providers", "listProviders", nil, []ProviderView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		includeDeleted := ctx.Query("include_deleted") == "true"
		records, err := console.Control.Providers(ctx.Request.Context(), identity.actor(), includeDeleted)
		if err != nil {
			return nil, err
		}
		return viewAll(records, providerView), nil
	})
	console.add("POST", "/providers", "createProvider", ProviderInput{}, ProviderView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ProviderInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		created, err := console.Control.CreateProvider(ctx.Request.Context(), identity.actor(), input.service())
		if err != nil {
			return nil, err
		}
		return providerView(created), nil
	})
	console.add("GET", "/providers/{provider_id}", "getProvider", providerPath{}, ProviderView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		record, err := console.Control.Provider(ctx.Request.Context(), identity.actor(), ctx.Param("provider_id"))
		if err != nil {
			return nil, err
		}
		return providerView(record), nil
	})
	console.add("PUT", "/providers/{provider_id}", "updateProvider", updateProviderInput{}, ProviderView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ProviderInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Control.UpdateProvider(ctx.Request.Context(), identity.actor(), ctx.Param("provider_id"), input.service())
		if err != nil {
			return nil, err
		}
		return providerView(updated), nil
	})
	console.add("DELETE", "/providers/{provider_id}", "deleteProvider", providerPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		return nil, console.Control.DeleteProvider(ctx.Request.Context(), identity.actor(), ctx.Param("provider_id"))
	})
	console.add("POST", "/providers/{provider_id}/reset", "resetProvider", providerPath{}, ProviderView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		reset, err := console.Control.ResetProvider(ctx.Request.Context(), identity.actor(), ctx.Param("provider_id"))
		if err != nil {
			return nil, err
		}
		return providerView(reset), nil
	})
}
