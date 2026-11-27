package console

import (
	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
)

type monitorInput struct {
	Window string `query:"window" json:"-" enum:"24h,7d,30d"`
}

type MonitorTotalsView struct {
	Calls        int64 `json:"calls" required:"true" description:"Main-lane calls only"`
	Failures     int64 `json:"failures" required:"true"`
	Blocked      int64 `json:"blocked" required:"true"`
	Findings     int64 `json:"findings" required:"true"`
	HighFindings int64 `json:"high_findings" required:"true"`
	Projects     int   `json:"projects" required:"true"`
}

type MonitorProjectView struct {
	ProjectID    string `json:"project_id" required:"true"`
	Name         string `json:"name" required:"true"`
	Calls        int64  `json:"calls" required:"true"`
	Failures     int64  `json:"failures" required:"true"`
	Blocked      int64  `json:"blocked" required:"true"`
	HighFindings int64  `json:"high_findings" required:"true"`
	LastAt       string `json:"last_at" required:"true"`
}

type MonitorBucketView struct {
	Start          string `json:"start" required:"true"`
	Requests       int64  `json:"requests" required:"true"`
	Errors         int64  `json:"errors" required:"true"`
	Blocked        int64  `json:"blocked" required:"true"`
	InputTokens    int64  `json:"input_tokens" required:"true"`
	OutputTokens   int64  `json:"output_tokens" required:"true"`
	DurationP95Max int64  `json:"duration_p95_max" required:"true"`
}

type MonitorAgentView struct {
	ProjectID    string `json:"project_id" required:"true"`
	AgentKeyID   string `json:"agent_key_id" required:"true"`
	Label        string `json:"label" required:"true"`
	Calls        int64  `json:"calls" required:"true"`
	Blocked      int64  `json:"blocked" required:"true"`
	HighFindings int64  `json:"high_findings" required:"true"`
	LastAt       string `json:"last_at" required:"true"`
}

type MonitorRecentView struct {
	CallID     string `json:"call_id" required:"true"`
	NodeID     string `json:"node_id" required:"true" description:"Trajectory call node of this finding"`
	ProjectID  string `json:"project_id" required:"true"`
	AgentKeyID string `json:"agent_key_id" required:"true"`
	Model      string `json:"model" required:"true"`
	Severity   string `json:"severity" required:"true"`
	State      string `json:"state" required:"true"`
	CreatedAt  string `json:"created_at" required:"true"`
}

type MonitorView struct {
	Window   string               `json:"window" required:"true"`
	Since    string               `json:"since" required:"true"`
	Totals   MonitorTotalsView    `json:"totals" required:"true"`
	Projects []MonitorProjectView `json:"projects" required:"true"`
	Series   []MonitorBucketView  `json:"series" required:"true"`
	Agents   []MonitorAgentView   `json:"agents" required:"true"`
	Recent   []MonitorRecentView  `json:"recent" required:"true"`
}

func monitorView(snapshot service.MonitorSnapshot) MonitorView {
	view := MonitorView{
		Window: snapshot.Window, Since: stamp(snapshot.Since),
		Totals:   MonitorTotalsView{Calls: snapshot.Totals.Calls, Failures: snapshot.Totals.Failures, Blocked: snapshot.Totals.Blocked, Findings: snapshot.Totals.Findings, HighFindings: snapshot.Totals.HighFindings, Projects: snapshot.Totals.Projects},
		Projects: make([]MonitorProjectView, 0, len(snapshot.Projects)),
		Series:   make([]MonitorBucketView, 0, len(snapshot.Series)),
		Agents:   make([]MonitorAgentView, 0, len(snapshot.Agents)),
		Recent:   make([]MonitorRecentView, 0, len(snapshot.Recent)),
	}
	for _, project := range snapshot.Projects {
		view.Projects = append(view.Projects, MonitorProjectView{ProjectID: project.ProjectID, Name: project.Name, Calls: project.Calls, Failures: project.Failures, Blocked: project.Blocked, HighFindings: project.HighFindings, LastAt: stamp(project.LastAt)})
	}
	for _, bucket := range snapshot.Series {
		view.Series = append(view.Series, MonitorBucketView{Start: stamp(bucket.Start), Requests: bucket.Requests, Errors: bucket.Errors, Blocked: bucket.Blocked, InputTokens: bucket.InputTokens, OutputTokens: bucket.OutputTokens, DurationP95Max: bucket.DurationP95Max})
	}
	for _, agent := range snapshot.Agents {
		view.Agents = append(view.Agents, MonitorAgentView{ProjectID: agent.ProjectID, AgentKeyID: agent.AgentKeyID, Label: agent.Label, Calls: agent.Calls, Blocked: agent.Blocked, HighFindings: agent.HighFindings, LastAt: stamp(agent.LastAt)})
	}
	for _, recent := range snapshot.Recent {
		view.Recent = append(view.Recent, MonitorRecentView{CallID: recent.ID, NodeID: "call:" + recent.ID, ProjectID: recent.ProjectID, AgentKeyID: recent.AgentKeyID, Model: recent.PublicModel, Severity: recent.MaxSeverity, State: recent.State, CreatedAt: stamp(recent.CreatedAt)})
	}
	return view
}

func (console *Console) registerMonitor() {
	console.add("GET", "/monitor", "getMonitor", monitorInput{}, MonitorView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		snapshot, err := console.Monitor.Snapshot(ctx.Request.Context(), identity.actor(), ctx.Query("window"))
		if err != nil {
			return nil, err
		}
		return monitorView(snapshot), nil
	})
}
