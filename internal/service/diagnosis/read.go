package diagnosis

import (
	"context"
	"time"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// RiskReport is the project risk view: distributions over the three axes, the top codes, the agents
// with most findings and the recent high-severity calls.
type RiskReport struct {
	Since         time.Time                 `json:"since"`
	Sources       []repository.FindingCount `json:"sources"`
	Modes         []repository.FindingCount `json:"modes"`
	Harms         []repository.FindingCount `json:"harms"`
	Severities    []repository.FindingCount `json:"severities"`
	Codes         []repository.FindingCode  `json:"codes"`
	Agents        []repository.AgentRisk    `json:"agents"`
	HighRecent    []repository.HighRiskCall `json:"high_recent"`
	TotalFindings int64                     `json:"total_findings"`
}

func (service *Service) Risk(ctx context.Context, projectID string, since time.Time) (RiskReport, error) {
	report := RiskReport{
		Since: since, Sources: []repository.FindingCount{}, Modes: []repository.FindingCount{},
		Harms: []repository.FindingCount{}, Severities: []repository.FindingCount{},
		Codes: []repository.FindingCode{}, Agents: []repository.AgentRisk{}, HighRecent: []repository.HighRiskCall{},
	}
	var err error
	if report.Sources, err = service.store.FindingDistribution(ctx, projectID, since, "source"); err != nil {
		return report, err
	}
	if report.Modes, err = service.store.FindingDistribution(ctx, projectID, since, "mode"); err != nil {
		return report, err
	}
	if report.Harms, err = service.store.FindingDistribution(ctx, projectID, since, "harm"); err != nil {
		return report, err
	}
	if report.Severities, err = service.store.FindingDistribution(ctx, projectID, since, "severity"); err != nil {
		return report, err
	}
	if report.Codes, err = service.store.FindingCodes(ctx, projectID, since); err != nil {
		return report, err
	}
	for _, severity := range report.Severities {
		report.TotalFindings += severity.Count
	}
	if report.Agents, err = service.store.AgentRisk(ctx, projectID, since); err != nil {
		return report, err
	}
	if report.HighRecent, err = service.store.HighRiskCalls(ctx, []string{projectID}, since, 20); err != nil {
		return report, err
	}
	return report, nil
}

// FlowsOfAgent returns the flow edges the risk page draws for one agent key.
func (service *Service) FlowsOfAgent(ctx context.Context, projectID, agentKeyID string, since time.Time) ([]entity.AnalysisFlow, error) {
	return service.store.AgentFlows(ctx, projectID, agentKeyID, since, 500)
}

func (service *Service) BatchesOfProject(ctx context.Context, projectID string, limit int) ([]entity.AnalysisBatch, error) {
	return service.store.BatchesOfProject(ctx, projectID, limit)
}
