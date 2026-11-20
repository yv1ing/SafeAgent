package console

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type OperationView struct {
	ID        string            `json:"id" required:"true"`
	Username  string            `json:"username" required:"true"`
	Action    string            `json:"action" required:"true"`
	ProjectID string            `json:"project_id" required:"true"`
	Detail    map[string]string `json:"detail" required:"true" description:"Path parameters of the request"`
	CreatedAt string            `json:"created_at" required:"true"`
}

type OperationPage struct {
	Items []OperationView `json:"items" required:"true"`
	Next  *CursorView     `json:"next,omitempty" description:"Absent on the last page"`
}

type operationsInput struct {
	pageQuery
}

type projectOperationsInput struct {
	projectPath
	pageQuery
}

func operationView(value *entity.OperationLog) OperationView {
	detail := map[string]string{}
	_ = json.Unmarshal(value.Detail, &detail)
	return OperationView{ID: value.ID, Username: value.Username, Action: value.Action, ProjectID: value.ProjectID, Detail: detail, CreatedAt: stamp(value.CreatedAt)}
}

func (console *Console) registerOperations() {
	console.add("GET", "/operation-logs", "listOperationLogs", operationsInput{}, OperationPage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		position, err := cursor(ctx)
		if err != nil {
			return nil, err
		}
		page, err := console.Operations.Operations(ctx.Request.Context(), identity.actor(), position)
		if err != nil {
			return nil, err
		}
		return OperationPage{Items: viewAll(page.Items, operationView), Next: cursorView(page.Next)}, nil
	})
	console.add("GET", "/projects/{project_id}/operations", "listProjectOperations", projectOperationsInput{}, OperationPage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		projectID := ctx.Param("project_id")
		position, err := cursor(ctx)
		if err != nil {
			return nil, err
		}
		page, err := console.Operations.ProjectOperations(ctx.Request.Context(), identity.actor(), projectID, position)
		if err != nil {
			return nil, err
		}
		return OperationPage{Items: viewAll(page.Items, operationView), Next: cursorView(page.Next)}, nil
	})
}
