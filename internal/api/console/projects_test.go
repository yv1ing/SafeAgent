package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func TestProjectViewWithSummary(t *testing.T) {
	project := &entity.Project{Name: "ops", DailyTokenQuota: 1000}
	view := projectViewWithSummary(project, &entity.ProjectSummary{
		Calls24h: 10, Failed24h: 1, Blocked24h: 2,
		InputTokens24h: 400, OutputTokens24h: 100,
		LastActivityAt: 1_700_000_000_000,
	})
	if view.FailureRate != "10.0%" || view.QuotaUsagePercent != 50 {
		t.Fatalf("unexpected summary: %+v", view)
	}
	if view.Calls24h != 10 || view.Blocked24h != 2 || view.LastActivityAt == "" {
		t.Fatalf("summary fields were not copied: %+v", view)
	}
}

func TestProjectViewWithSummaryCapsQuotaAtFull(t *testing.T) {
	project := &entity.Project{DailyTokenQuota: 100}
	view := projectViewWithSummary(project, &entity.ProjectSummary{
		InputTokens24h: 80, OutputTokens24h: 70,
	})
	if view.QuotaUsagePercent != 100 {
		t.Fatalf("quota usage must cap at 100: %d", view.QuotaUsagePercent)
	}
}
