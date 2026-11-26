package console

import (
	"context"
	"time"

	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

type MonitorService struct {
	store *repository.Store
}

func NewMonitorService(store *repository.Store) *MonitorService { return &MonitorService{store: store} }

type MonitorTotals struct {
	Calls        int64 `json:"calls"`
	Failures     int64 `json:"failures"`
	Blocked      int64 `json:"blocked"`
	Findings     int64 `json:"findings"`
	HighFindings int64 `json:"high_findings"`
	Projects     int   `json:"projects"`
}

type MonitorProjectRow struct {
	ProjectID    string    `json:"project_id"`
	Name         string    `json:"name"`
	Calls        int64     `json:"calls"`
	Failures     int64     `json:"failures"`
	Blocked      int64     `json:"blocked"`
	HighFindings int64     `json:"high_findings"`
	LastAt       time.Time `json:"last_at"`
}

type MonitorSnapshot struct {
	Window   string                     `json:"window"`
	Since    time.Time                  `json:"since"`
	Totals   MonitorTotals              `json:"totals"`
	Projects []MonitorProjectRow        `json:"projects"`
	Series   []repository.TrafficBucket `json:"series"`
	Agents   []repository.TopAgent      `json:"agents"`
	Recent   []repository.HighRiskCall  `json:"recent"`
}

var windows = map[string]struct {
	length time.Duration
	bucket string
	step   time.Duration
}{
	"24h": {24 * time.Hour, "1h", time.Hour},
	"7d":  {7 * 24 * time.Hour, "1h", 6 * time.Hour},
	"30d": {30 * 24 * time.Hour, "1d", 24 * time.Hour},
}

func (service *MonitorService) Snapshot(ctx context.Context, actor access.Actor, window string) (MonitorSnapshot, error) {
	definition, found := windows[window]
	if window != "" && !found {
		return MonitorSnapshot{}, access.Invalid("invalid_window")
	}
	if window == "" {
		window, definition = "24h", windows["24h"]
	}
	now := time.Now().UTC()
	since := now.Add(-definition.length)
	snapshot := MonitorSnapshot{Window: window, Since: since, Projects: []MonitorProjectRow{}, Series: []repository.TrafficBucket{}, Agents: []repository.TopAgent{}, Recent: []repository.HighRiskCall{}}
	projects, err := service.store.VisibleProjects(ctx, actor.UserID, actor.Administrator)
	if err != nil {
		return snapshot, err
	}
	names := make(map[string]string, len(projects))
	ids := make([]string, 0, len(projects))
	for _, project := range projects {
		names[project.ID] = project.Name
		ids = append(ids, project.ID)
	}
	snapshot.Totals.Projects = len(ids)
	if len(ids) == 0 {
		return snapshot, nil
	}
	stats, err := service.store.CallStats(ctx, ids, since)
	if err != nil {
		return snapshot, err
	}
	for _, row := range stats {
		snapshot.Totals.Calls += row.Calls
		snapshot.Totals.Failures += row.Failures
		snapshot.Totals.Blocked += row.Blocked
		snapshot.Totals.HighFindings += row.HighFindings
		snapshot.Totals.Findings += row.Findings
		snapshot.Projects = append(snapshot.Projects, MonitorProjectRow{
			ProjectID: row.ProjectID, Name: names[row.ProjectID], Calls: row.Calls, Failures: row.Failures,
			Blocked: row.Blocked, HighFindings: row.HighFindings, LastAt: row.LastAt,
		})
	}
	series, err := service.store.TrafficSeries(ctx, ids, definition.bucket, since, now, definition.step)
	if err != nil {
		return snapshot, err
	}
	snapshot.Series = series
	agents, err := service.store.TopAgents(ctx, ids, since, 10)
	if err != nil {
		return snapshot, err
	}
	snapshot.Agents = agents
	recent, err := service.store.HighRiskCalls(ctx, ids, since, 20)
	if err != nil {
		return snapshot, err
	}
	snapshot.Recent = recent
	return snapshot, nil
}
