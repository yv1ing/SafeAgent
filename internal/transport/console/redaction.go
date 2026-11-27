package console

import (
	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
)

type RedactionIndexView struct {
	Items []service.RedactionValue `json:"items" required:"true"`
}

func (console *Console) registerRedactions() {
	console.add("GET", "/projects/{project_id}/trajectories/{trajectory_id}/redactions", "getTrajectoryRedactions", trajectoryPath{}, RedactionIndexView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		items, err := console.Redaction.Index(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id"))
		if err != nil {
			return nil, err
		}
		return RedactionIndexView{Items: nonNil(items)}, nil
	})
}
