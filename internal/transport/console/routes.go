package console

import (
	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type RouteView struct {
	ID        string        `json:"id" required:"true"`
	Name      string        `json:"name" required:"true"`
	Targets   []RouteTarget `json:"targets" required:"true"`
	Version   int64         `json:"version" required:"true"`
	CreatedAt string        `json:"created_at" required:"true"`
}

type RouteTarget struct {
	ChannelID       string `json:"channel_id" required:"true"`
	ModelID         string `json:"model_id" required:"true"`
	Priority        int    `json:"priority" required:"true" description:"Lower values are tried first"`
	Weight          int    `json:"weight" required:"true" description:"Relative share among targets of the same priority"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty" description:"Applied when the target channel requires an output limit and the client omits one"`
}

type RouteInput struct {
	Name    string        `json:"name" required:"true"`
	Targets []RouteTarget `json:"targets" required:"true"`
	Version int64         `json:"version" description:"Required on update, ignored on create"`
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

func routeView(value *entity.Route) RouteView {
	targets := make([]RouteTarget, 0, len(value.Targets))
	for _, target := range value.Targets {
		targets = append(targets, RouteTarget{ChannelID: target.ChannelID, ModelID: target.ModelID, Priority: target.Priority, Weight: target.Weight, MaxOutputTokens: target.MaxOutputTokens})
	}
	return RouteView{ID: value.ID, Name: value.Name, Targets: targets, Version: value.Version, CreatedAt: stamp(value.CreatedAt)}
}

func (input RouteInput) service() service.RouteInput {
	targets := make([]entity.Target, 0, len(input.Targets))
	for _, target := range input.Targets {
		targets = append(targets, entity.Target{ChannelID: target.ChannelID, ModelID: target.ModelID, Priority: target.Priority, Weight: target.Weight, MaxOutputTokens: target.MaxOutputTokens})
	}
	return service.RouteInput{Name: input.Name, Targets: targets, Version: input.Version}
}

func (console *Console) registerModelRoutes() {
	console.add("GET", "/projects/{project_id}/routes", "listRoutes", projectPath{}, []RouteView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		routes, err := console.Route.List(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
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
		created, err := console.Route.Create(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), input.service())
		if err != nil {
			return nil, err
		}
		return routeView(created), nil
	})
	console.add("PUT", "/projects/{project_id}/routes/{route_id}", "updateRoute", updateRouteInput{}, RouteView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input RouteInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Route.Update(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("route_id"), input.service())
		if err != nil {
			return nil, err
		}
		return routeView(updated), nil
	})
	console.add("DELETE", "/projects/{project_id}/routes/{route_id}", "deleteRoute", routePath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		_, err := console.Route.Delete(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("route_id"))
		return nil, err
	})
}
