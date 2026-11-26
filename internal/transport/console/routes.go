package console

import (
	"github.com/gin-gonic/gin"
	control "github.com/yv1ing/SafeAgent/internal/service/control"
)

type RouteTargetView struct {
	ChannelID       string `json:"channel_id" required:"true"`
	ModelID         string `json:"model_id" required:"true"`
	Format          string `json:"format"`
	Priority        int    `json:"priority" required:"true" description:"Lower values are tried first"`
	Weight          int    `json:"weight" required:"true" description:"Relative share among targets of the same priority"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty" description:"Applied when the target channel requires an output limit and the client omits one"`
	Enabled         bool   `json:"enabled"`
	DisabledReason  string `json:"disabled_reason,omitempty"`
}

type RouteAliasView struct {
	Alias  string `json:"alias" required:"true"`
	Source string `json:"source" required:"true" enum:"manual,renamed"`
}

type RouteView struct {
	ID        string            `json:"id" required:"true"`
	Name      string            `json:"name" required:"true"`
	Enabled   bool              `json:"enabled"`
	Targets   []RouteTargetView `json:"targets" required:"true"`
	Aliases   []RouteAliasView  `json:"aliases" required:"true"`
	Version   int64             `json:"version" required:"true"`
	CreatedAt string            `json:"created_at" required:"true"`
	UpdatedAt string            `json:"updated_at" required:"true"`
	DeletedAt string            `json:"deleted_at,omitempty"`
}

type RouteTargetInput struct {
	ChannelID       string `json:"channel_id" required:"true"`
	ModelID         string `json:"model_id" required:"true"`
	Format          string `json:"format"`
	Priority        int    `json:"priority"`
	Weight          int    `json:"weight"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty" description:"Defaults to true"`
}

type RouteInput struct {
	Name    string             `json:"name" required:"true"`
	Enabled *bool              `json:"enabled,omitempty" description:"Defaults to true on create and to the stored value on update"`
	Targets []RouteTargetInput `json:"targets" required:"true"`
	Aliases []string           `json:"aliases"`
	Version int64              `json:"version" description:"Required on update, ignored on create"`
}

type routePath struct {
	ProjectID string `path:"project_id" json:"-" required:"true"`
	RouteID   string `path:"route_id" json:"-" required:"true"`
}

type createRouteInput struct {
	projectPath
	RouteInput
}

type updateRouteInput struct {
	routePath
	RouteInput
}

func (input RouteInput) service() control.RouteInput {
	targets := make([]control.RouteTargetInput, 0, len(input.Targets))
	for _, target := range input.Targets {
		targets = append(targets, control.RouteTargetInput{
			ChannelID: target.ChannelID, ModelID: target.ModelID, Format: target.Format,
			Priority: target.Priority, Weight: target.Weight, MaxOutputTokens: target.MaxOutputTokens,
			Enabled: target.Enabled,
		})
	}
	return control.RouteInput{Name: input.Name, Enabled: input.Enabled, Targets: targets, Aliases: input.Aliases, Version: input.Version}
}

func routeView(value *control.RouteInfo) RouteView {
	targets := make([]RouteTargetView, 0, len(value.Targets))
	for _, target := range value.Targets {
		targets = append(targets, RouteTargetView{
			ChannelID: target.Target.ChannelID, ModelID: target.Target.ModelID, Format: target.Target.Format,
			Priority: target.Target.Priority, Weight: target.Target.Weight,
			MaxOutputTokens: target.Target.MaxOutputTokens, Enabled: target.Target.Enabled,
			DisabledReason: target.Target.DisabledReason,
		})
	}
	aliases := make([]RouteAliasView, 0, len(value.Aliases))
	for _, alias := range value.Aliases {
		aliases = append(aliases, RouteAliasView{Alias: alias.Alias, Source: alias.Source})
	}
	return RouteView{
		ID: value.Route.ID, Name: value.Route.Name, Enabled: value.Route.Enabled,
		Targets: targets, Aliases: aliases, Version: value.Route.Version,
		CreatedAt: stamp(value.Route.CreatedAt), UpdatedAt: stamp(value.Route.UpdatedAt),
		DeletedAt: stampOptional(value.Route.DeletedAt),
	}
}

func (console *Console) registerModelRoutes() {
	console.add("GET", "/projects/{project_id}/routes", "listRoutes", projectPath{}, []RouteView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		includeDeleted := ctx.Query("include_deleted") == "true"
		routes, err := console.Control.Routes(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), includeDeleted)
		if err != nil {
			return nil, err
		}
		return viewAll(routes, routeView), nil
	})
	console.add("POST", "/projects/{project_id}/routes", "createRoute", createRouteInput{}, RouteView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input RouteInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		created, err := console.Control.CreateRoute(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), input.service())
		if err != nil {
			return nil, err
		}
		return routeView(created), nil
	})
	console.add("GET", "/projects/{project_id}/routes/{route_id}", "getRoute", routePath{}, RouteView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		route, err := console.Control.Route(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("route_id"))
		if err != nil {
			return nil, err
		}
		return routeView(route), nil
	})
	console.add("PUT", "/projects/{project_id}/routes/{route_id}", "updateRoute", updateRouteInput{}, RouteView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input RouteInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Control.UpdateRoute(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("route_id"), input.service())
		if err != nil {
			return nil, err
		}
		return routeView(updated), nil
	})
	console.add("DELETE", "/projects/{project_id}/routes/{route_id}", "deleteRoute", routePath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		return nil, console.Control.DeleteRoute(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("route_id"))
	})
	console.add("POST", "/projects/{project_id}/routes/{route_id}/restore", "restoreRoute", routePath{}, RouteView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		restored, err := console.Control.RestoreRoute(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("route_id"))
		if err != nil {
			return nil, err
		}
		return routeView(restored), nil
	})
}
