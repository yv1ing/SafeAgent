package console

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type ProjectView struct {
	ID                     string `json:"id" required:"true"`
	Name                   string `json:"name" required:"true"`
	RetentionDays          int    `json:"retention_days" required:"true"`
	RequestsPerMinute      int    `json:"requests_per_minute" required:"true" description:"0 means unlimited"`
	Concurrency            int    `json:"concurrency" required:"true" description:"0 means unlimited"`
	DailyTokenQuota        int64  `json:"daily_token_quota" required:"true" description:"0 means unlimited"`
	DefaultMaxOutputTokens int    `json:"default_max_output_tokens" description:"Applied when an upstream requires an output limit and the client omits one"`
	StreamReviewBytes      int    `json:"stream_review_bytes" required:"true" description:"Leading streaming text bytes reviewed before the first byte; 0 disables review"`
	StreamReviewTimeoutMS  int    `json:"stream_review_timeout_ms" required:"true" description:"Delay before a partially filled review window is checked on the next frame"`
	ActivePolicyVersion    int64  `json:"active_policy_version" required:"true"`
	Version                int64  `json:"version" required:"true"`
	CreatedAt              string `json:"created_at" required:"true"`
	// Summary fields for project card display (24h window)
	Calls24h          int64  `json:"calls_24h"`
	Blocked24h        int64  `json:"blocked_24h"`
	FailureRate       string `json:"failure_rate"` // e.g., "0.4%"
	QuotaUsagePercent int    `json:"quota_usage_percent"`
	LastActivityAt    string `json:"last_activity_at"`
}

// ProjectCreate leaves a limit out to take its default; an explicit zero means unlimited.
type ProjectCreate struct {
	Name                   string `json:"name" required:"true"`
	RetentionDays          *int   `json:"retention_days,omitempty"`
	RequestsPerMinute      *int   `json:"requests_per_minute,omitempty"`
	Concurrency            *int   `json:"concurrency,omitempty"`
	DailyTokenQuota        *int64 `json:"daily_token_quota,omitempty"`
	DefaultMaxOutputTokens *int   `json:"default_max_output_tokens,omitempty" description:"Applied when an upstream requires an output limit and the client omits one"`
	StreamReviewBytes      *int   `json:"stream_review_bytes,omitempty" description:"Leading streaming text bytes reviewed before the first byte; 0 disables review"`
	StreamReviewTimeoutMS  *int   `json:"stream_review_timeout_ms,omitempty" description:"Delay before a partially filled review window is checked on the next frame"`
}

type ProjectUpdate struct {
	Name                   string `json:"name" required:"true"`
	RetentionDays          int    `json:"retention_days" required:"true"`
	RequestsPerMinute      int    `json:"requests_per_minute" required:"true"`
	Concurrency            int    `json:"concurrency" required:"true"`
	DailyTokenQuota        int64  `json:"daily_token_quota" required:"true"`
	DefaultMaxOutputTokens int    `json:"default_max_output_tokens" description:"Applied when an upstream requires an output limit and the client omits one"`
	StreamReviewBytes      int    `json:"stream_review_bytes" description:"Leading streaming text bytes reviewed before the first byte; 0 disables review"`
	StreamReviewTimeoutMS  int    `json:"stream_review_timeout_ms" description:"Delay before a partially filled review window is checked on the next frame"`
	Version                int64  `json:"version" required:"true"`
}

type updateProjectInput struct {
	projectPath
	ProjectUpdate
}

func projectView(value *entity.Project) ProjectView {
	return ProjectView{
		ID:                     value.ID,
		Name:                   value.Name,
		RetentionDays:          value.RetentionDays,
		RequestsPerMinute:      value.RequestsPerMinute,
		Concurrency:            value.Concurrency,
		DailyTokenQuota:        value.DailyTokenQuota,
		DefaultMaxOutputTokens: value.DefaultMaxOutputTokens,
		StreamReviewBytes:      value.StreamReviewBytes,
		StreamReviewTimeoutMS:  value.StreamReviewTimeoutMS,
		ActivePolicyVersion:    value.ActivePolicyVersion,
		Version:                value.Version,
		CreatedAt:              stamp(value.CreatedAt),
	}
}

func projectViewWithSummary(value *entity.Project, summary *entity.ProjectSummary) ProjectView {
	view := projectView(value)
	if summary != nil {
		view.Calls24h = summary.Calls24h
		view.Blocked24h = summary.Blocked24h
		failureRate := 0.0
		if summary.Calls24h > 0 {
			failureRate = float64(summary.Failed24h) / float64(summary.Calls24h) * 100
		}
		view.FailureRate = fmt.Sprintf("%.1f%%", failureRate)

		quotaPercent := 0
		if value.DailyTokenQuota > 0 {
			quotaPercent = int((summary.InputTokens24h + summary.OutputTokens24h) * 100 / value.DailyTokenQuota)
			if quotaPercent > 100 {
				quotaPercent = 100
			}
		}
		view.QuotaUsagePercent = quotaPercent

		if summary.LastActivityAt > 0 {
			view.LastActivityAt = stamp(time.UnixMilli(summary.LastActivityAt).UTC())
		}
	}
	return view
}

// projectSummaryView adds the 24-hour summary to a single project, so a read after an update
// returns the same counters as the project list.
func (console *Console) projectSummaryView(ctx context.Context, value *entity.Project) ProjectView {
	summaries, err := console.Stats.ProjectSummaries(ctx, []string{value.ID})
	if err != nil {
		slog.Warn("project summary unavailable", "project", value.ID, "error", err)
		return projectView(value)
	}
	if summary, found := summaries[value.ID]; found {
		return projectViewWithSummary(value, &summary)
	}
	return projectView(value)
}

func (console *Console) registerProjects() {
	console.add("GET", "/projects", "listProjects", nil, []ProjectView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		projects, err := console.Project.List(ctx.Request.Context(), identity.actor())
		if err != nil {
			return nil, err
		}

		ids := make([]string, 0, len(projects))
		for _, project := range projects {
			ids = append(ids, project.ID)
		}
		summaries, err := console.Stats.ProjectSummaries(ctx.Request.Context(), ids)
		if err != nil {
			slog.Warn("project summaries unavailable", "error", err)
			summaries = nil
		}
		views := make([]ProjectView, 0, len(projects))
		for _, project := range projects {
			var summary *entity.ProjectSummary
			if value, found := summaries[project.ID]; found {
				summary = &value
			}
			views = append(views, projectViewWithSummary(&project, summary))
		}
		return views, nil
	})
	console.add("POST", "/projects", "createProject", ProjectCreate{}, ProjectView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ProjectCreate
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		created, err := console.Project.Create(ctx.Request.Context(), identity.actor(), service.ProjectCreateInput{
			Name: input.Name, RetentionDays: input.RetentionDays, RequestsPerMinute: input.RequestsPerMinute, Concurrency: input.Concurrency,
			DailyTokenQuota: input.DailyTokenQuota, DefaultMaxOutputTokens: input.DefaultMaxOutputTokens,
			StreamReviewBytes: input.StreamReviewBytes, StreamReviewTimeoutMS: input.StreamReviewTimeoutMS,
		})
		if err != nil {
			return nil, err
		}
		return projectViewWithSummary(created, &entity.ProjectSummary{}), nil
	})
	console.add("GET", "/projects/{project_id}", "getProject", projectPath{}, ProjectView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		found, err := console.Project.Get(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
		if err != nil {
			return nil, err
		}
		return console.projectSummaryView(ctx.Request.Context(), found), nil
	})
	console.add("PUT", "/projects/{project_id}", "updateProject", updateProjectInput{}, ProjectView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ProjectUpdate
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Project.Update(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), service.ProjectInput{
			Name: input.Name, RetentionDays: input.RetentionDays, RequestsPerMinute: input.RequestsPerMinute, Concurrency: input.Concurrency,
			DailyTokenQuota: input.DailyTokenQuota, DefaultMaxOutputTokens: input.DefaultMaxOutputTokens,
			StreamReviewBytes: input.StreamReviewBytes, StreamReviewTimeoutMS: input.StreamReviewTimeoutMS, Version: input.Version,
		})
		if err != nil {
			return nil, err
		}
		return console.projectSummaryView(ctx.Request.Context(), updated), nil
	})
	console.add("DELETE", "/projects/{project_id}", "deleteProject", projectPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		_, err := console.Project.Delete(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
		return nil, err
	})
}
