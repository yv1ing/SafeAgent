package console

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	diagnosis "github.com/yv1ing/SafeAgent/internal/service/diagnosis"
)

type riskInput struct {
	projectPath
	Window string `query:"window" json:"-" enum:"24h,7d,30d"`
}

type RiskCountView struct {
	Name  string `json:"name" required:"true"`
	Count int64  `json:"count" required:"true"`
}

type RiskCodeView struct {
	Code     string `json:"code" required:"true"`
	Source   string `json:"source" required:"true"`
	Mode     string `json:"mode" required:"true"`
	Harm     string `json:"harm" required:"true"`
	Severity string `json:"severity" required:"true"`
	Count    int64  `json:"count" required:"true"`
}

type RiskAgentView struct {
	AgentKeyID string `json:"agent_key_id" required:"true"`
	Label      string `json:"label" required:"true"`
	Findings   int64  `json:"findings" required:"true"`
	High       int64  `json:"high" required:"true"`
	Flows      int64  `json:"flows" required:"true"`
	LastAt     string `json:"last_at" required:"true"`
}

type RiskCallView struct {
	CallID     string `json:"call_id" required:"true"`
	AgentKeyID string `json:"agent_key_id" required:"true"`
	Model      string `json:"model" required:"true"`
	Severity   string `json:"severity" required:"true"`
	State      string `json:"state" required:"true"`
	CreatedAt  string `json:"created_at" required:"true"`
}

type RiskView struct {
	Since         string          `json:"since" required:"true"`
	TotalFindings int64           `json:"total_findings" required:"true"`
	Sources       []RiskCountView `json:"sources" required:"true"`
	Modes         []RiskCountView `json:"modes" required:"true"`
	Harms         []RiskCountView `json:"harms" required:"true"`
	Severities    []RiskCountView `json:"severities" required:"true"`
	Codes         []RiskCodeView  `json:"codes" required:"true"`
	Agents        []RiskAgentView `json:"agents" required:"true"`
	HighRecent    []RiskCallView  `json:"high_recent" required:"true"`
}

type flowsInput struct {
	projectPath
	AgentKeyID string `query:"agent_key_id" json:"-" required:"true"`
	Window     string `query:"window" json:"-" enum:"24h,7d,30d"`
}

type FlowPage struct {
	Items []FlowView `json:"items" required:"true"`
}

func riskView(report diagnosis.RiskReport) RiskView {
	view := RiskView{
		Since: stamp(report.Since), TotalFindings: report.TotalFindings,
		Sources: []RiskCountView{}, Modes: []RiskCountView{}, Harms: []RiskCountView{},
		Severities: []RiskCountView{}, Codes: []RiskCodeView{}, Agents: []RiskAgentView{},
		HighRecent: []RiskCallView{},
	}
	for _, source := range report.Sources {
		view.Sources = append(view.Sources, RiskCountView{Name: source.Name, Count: source.Count})
	}
	for _, mode := range report.Modes {
		view.Modes = append(view.Modes, RiskCountView{Name: mode.Name, Count: mode.Count})
	}
	for _, harm := range report.Harms {
		view.Harms = append(view.Harms, RiskCountView{Name: harm.Name, Count: harm.Count})
	}
	for _, code := range report.Codes {
		view.Codes = append(view.Codes, RiskCodeView{Code: code.Code, Source: code.Source, Mode: code.Mode, Harm: code.Harm, Severity: code.Severity, Count: code.Count})
	}
	for _, severity := range report.Severities {
		view.Severities = append(view.Severities, RiskCountView{Name: severity.Name, Count: severity.Count})
	}
	for _, agent := range report.Agents {
		view.Agents = append(view.Agents, RiskAgentView{AgentKeyID: agent.AgentKeyID, Label: agent.Label, Findings: agent.Findings, High: agent.High, Flows: agent.Flows, LastAt: stamp(agent.LastAt)})
	}
	for _, call := range report.HighRecent {
		view.HighRecent = append(view.HighRecent, RiskCallView{CallID: call.CallID, AgentKeyID: call.AgentKeyID, Model: call.Model, Severity: call.Severity, State: call.State, CreatedAt: stamp(call.CreatedAt)})
	}
	return view
}

func windowSince(window string) (time.Time, error) {
	duration := 24 * time.Hour
	switch window {
	case "", "24h":
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	default:
		return time.Time{}, service.Invalid("invalid_window")
	}
	return time.Now().UTC().Add(-duration), nil
}

func (console *Console) registerRisk() {
	console.add("GET", "/projects/{project_id}/risk", "getProjectRisk", riskInput{}, RiskView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		if err := console.Project.Require(ctx.Request.Context(), identity.actor(), ctx.Param("project_id")); err != nil {
			return nil, err
		}
		since, err := windowSince(ctx.Query("window"))
		if err != nil {
			return nil, err
		}
		report, err := console.Analysis.Risk(ctx.Request.Context(), ctx.Param("project_id"), since)
		if err != nil {
			return nil, analysisError(err)
		}
		return riskView(report), nil
	})
	console.add("GET", "/projects/{project_id}/flows", "listProjectFlows", flowsInput{}, FlowPage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		if err := console.Project.Require(ctx.Request.Context(), identity.actor(), ctx.Param("project_id")); err != nil {
			return nil, err
		}
		if ctx.Query("agent_key_id") == "" {
			return nil, errInvalidRequest
		}
		since, err := windowSince(ctx.Query("window"))
		if err != nil {
			return nil, err
		}
		flows, err := console.Analysis.FlowsOfAgent(ctx.Request.Context(), ctx.Param("project_id"), ctx.Query("agent_key_id"), since)
		if err != nil {
			return nil, analysisError(err)
		}
		items := make([]FlowView, 0, len(flows))
		for index := range flows {
			items = append(items, FlowView{
				SourceTurn: flows[index].SourceTurn, SourceItem: flows[index].SourceItem,
				SourceOrigin: flows[index].SourceOrigin, SourcePath: flows[index].SourcePath,
				SinkToolCallID: flows[index].SinkToolCallID, Sink: flows[index].Sink,
				Relation: flows[index].Relation, ContentHash: flows[index].ContentHash,
				Confidence: flows[index].Confidence,
			})
		}
		return FlowPage{Items: items}, nil
	})
}

func analysisError(err error) error {
	switch {
	case errors.Is(err, diagnosis.ErrScopeTooLarge):
		return service.Invalid("scope_too_large")
	case errors.Is(err, diagnosis.ErrCallRunning):
		return service.Conflict("call_running")
	}
	return service.Translate(err)
}
