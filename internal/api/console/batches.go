package console

import (
	"time"

	"github.com/gin-gonic/gin"
	diagnosis "github.com/yv1ing/SafeAgent/internal/service/diagnosis"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type AnalysisBatchView struct {
	ID        string `json:"id" required:"true"`
	Kind      string `json:"kind" required:"true"`
	State     string `json:"state" required:"true" enum:"running,completed,failed"`
	Total     int    `json:"total" required:"true"`
	Completed int    `json:"completed" required:"true"`
	Failed    int    `json:"failed" required:"true"`
	CreatedAt string `json:"created_at" required:"true"`
	Finished  string `json:"finished_at" required:"true"`
}

type AnalysisBatchPage struct {
	Items []AnalysisBatchView `json:"items" required:"true"`
}

type analysisBatchInput struct {
	projectPath
	Kind       string `json:"kind" required:"true" enum:"call,session,window,agent_key"`
	CallID     string `json:"call_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	AgentKeyID string `json:"agent_key_id,omitempty"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

func analysisBatchView(value *entity.AnalysisBatch) AnalysisBatchView {
	return AnalysisBatchView{
		ID: value.ID, Kind: value.Kind, State: value.State, Total: value.Total,
		Completed: value.Completed, Failed: value.Failed,
		CreatedAt: stamp(value.CreatedAt), Finished: stampOptional(value.FinishedAt),
	}
}

func (console *Console) registerBatches() {
	console.add("GET", "/projects/{project_id}/analysis-batches", "listAnalysisBatches", projectPath{}, AnalysisBatchPage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		if err := console.Project.Require(ctx.Request.Context(), identity.actor(), ctx.Param("project_id")); err != nil {
			return nil, err
		}
		batches, err := console.Analysis.BatchesOfProject(ctx.Request.Context(), ctx.Param("project_id"), 20)
		if err != nil {
			return nil, analysisError(err)
		}
		items := make([]AnalysisBatchView, 0, len(batches))
		for index := range batches {
			items = append(items, analysisBatchView(&batches[index]))
		}
		return AnalysisBatchPage{Items: items}, nil
	})
	console.add("POST", "/projects/{project_id}/analysis-batches", "createAnalysisBatch", analysisBatchInput{}, AnalysisBatchView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		if err := console.Project.Require(ctx.Request.Context(), identity.actor(), ctx.Param("project_id")); err != nil {
			return nil, err
		}
		var input analysisBatchInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		switch input.Kind {
		case "call", "session", "window", "agent_key":
		default:
			return nil, errInvalidRequest
		}
		scope := diagnosis.ReplayScope{Kind: input.Kind, CallID: input.CallID, SessionID: input.SessionID, AgentKeyID: input.AgentKeyID, Limit: input.Limit}
		if input.From != "" {
			parsed, err := time.Parse(time.RFC3339, input.From)
			if err != nil {
				return nil, errInvalidRequest
			}
			scope.From = parsed
		}
		if input.To != "" {
			parsed, err := time.Parse(time.RFC3339, input.To)
			if err != nil {
				return nil, errInvalidRequest
			}
			scope.To = parsed
		}
		batch, err := console.Analysis.Replay(ctx.Request.Context(), ctx.Param("project_id"), scope)
		if err != nil {
			return nil, analysisError(err)
		}
		return analysisBatchView(batch), nil
	})
}
