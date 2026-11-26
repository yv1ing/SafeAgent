package console

import (
	"github.com/gin-gonic/gin"
	control "github.com/yv1ing/SafeAgent/internal/service/control"
)

type ConfigurationWarningView struct {
	Code    string `json:"code" required:"true"`
	Subject string `json:"subject" required:"true"`
	Message string `json:"message" required:"true"`
}

type ConfigurationHealthView struct {
	Providers int                        `json:"providers" required:"true"`
	Channels  int                        `json:"channels" required:"true"`
	Models    int                        `json:"models" required:"true"`
	Routes    int                        `json:"routes" required:"true"`
	Warnings  []ConfigurationWarningView `json:"warnings" required:"true"`
}

type ImpactRouteView struct {
	RouteID     string `json:"route_id" required:"true"`
	RouteName   string `json:"route_name" required:"true"`
	ProjectID   string `json:"project_id" required:"true"`
	TargetCount int    `json:"target_count" required:"true"`
}

type ImpactView struct {
	Routes   []ImpactRouteView `json:"routes" required:"true"`
	Projects []string          `json:"projects" required:"true"`
}

type ConnectInput struct {
	Provider *ProviderInput      `json:"provider,omitempty"`
	Channel  ChannelInput        `json:"channel" required:"true"`
	Keys     []string            `json:"keys" required:"true"`
	Models   []CatalogModelInput `json:"models"`
}

type ConnectView struct {
	Provider *ProviderView      `json:"provider,omitempty"`
	Channel  ChannelView        `json:"channel" required:"true"`
	Models   []CatalogModelView `json:"models" required:"true"`
}

func impactView(value *control.Impact) ImpactView {
	routes := make([]ImpactRouteView, 0, len(value.Routes))
	for _, reference := range value.Routes {
		routes = append(routes, ImpactRouteView{
			RouteID: reference.RouteID, RouteName: reference.RouteName,
			ProjectID: reference.ProjectID, TargetCount: reference.TargetCount,
		})
	}
	return ImpactView{Routes: routes, Projects: nonNil(value.Projects)}
}

func (input ConnectInput) service() control.ConnectInput {
	serviceInput := control.ConnectInput{
		Channel: input.Channel.service(), Keys: input.Keys, Models: []control.ModelInput{},
	}
	if input.Provider != nil {
		provider := input.Provider.service()
		serviceInput.Provider = &provider
	}
	for _, model := range input.Models {
		serviceInput.Models = append(serviceInput.Models, model.service())
	}
	return serviceInput
}

func (console *Console) registerConfiguration() {
	console.add("GET", "/configuration/health", "getConfigurationHealth", nil, ConfigurationHealthView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		health, err := console.Control.ConfigurationHealth(ctx.Request.Context(), identity.actor())
		if err != nil {
			return nil, err
		}
		warnings := make([]ConfigurationWarningView, 0, len(health.Warnings))
		for _, warning := range health.Warnings {
			warnings = append(warnings, ConfigurationWarningView{Code: warning.Code, Subject: warning.Subject, Message: warning.Message})
		}
		return ConfigurationHealthView{
			Providers: health.Providers, Channels: health.Channels, Models: health.Models,
			Routes: health.Routes, Warnings: warnings,
		}, nil
	})
	console.add("GET", "/configuration/impact", "getConfigurationImpact", nil, ImpactView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		if channelID := ctx.Query("channel_id"); channelID != "" {
			impact, err := console.Control.ChannelImpact(ctx.Request.Context(), identity.actor(), channelID)
			if err != nil {
				return nil, err
			}
			return impactView(impact), nil
		}
		if modelID := ctx.Query("model_id"); modelID != "" {
			impact, err := console.Control.ModelImpact(ctx.Request.Context(), identity.actor(), modelID)
			if err != nil {
				return nil, err
			}
			return impactView(impact), nil
		}
		return nil, errInvalidRequest
	})
	console.add("POST", "/configuration/connect", "connectProvider", ConnectInput{}, ConnectView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ConnectInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		result, err := console.Control.Connect(ctx.Request.Context(), identity.actor(), input.service())
		if err != nil {
			return nil, err
		}
		view := ConnectView{Channel: channelView(result.Channel), Models: []CatalogModelView{}}
		if result.Provider != nil {
			provider := providerView(result.Provider)
			view.Provider = &provider
		}
		for index := range result.Models {
			view.Models = append(view.Models, catalogModelView(&result.Models[index]))
		}
		return view, nil
	})
}
