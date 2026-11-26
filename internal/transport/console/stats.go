package console

import (
	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type StatsView struct {
	Window string      `json:"window" required:"true" enum:"24h,7d,30d"`
	Totals TotalsView  `json:"totals" required:"true"`
	Groups []GroupView `json:"groups" required:"true"`
	Costs  []CostView  `json:"costs" required:"true" description:"Spend per currency; amounts in different currencies are never added"`
}

type TotalsView struct {
	Calls           int64 `json:"calls" required:"true" description:"Main-lane calls only"`
	Failures        int64 `json:"failures" required:"true" description:"Main-lane calls whose state is failed"`
	Blocked         int64 `json:"blocked" required:"true" description:"Main-lane calls whose outcome is blocked"`
	InputTokens     int64 `json:"input_tokens" required:"true"`
	OutputTokens    int64 `json:"output_tokens" required:"true"`
	RedactionValues int64 `json:"redaction_values" required:"true" description:"Distinct redacted values, not occurrences"`
	HighSeverity    int64 `json:"high_severity" required:"true"`
	FirstTokenP50   int64 `json:"first_token_p50" required:"true"`
	FirstTokenP95   int64 `json:"first_token_p95" required:"true"`
	DurationP50     int64 `json:"duration_p50" required:"true"`
	DurationP95     int64 `json:"duration_p95" required:"true"`
}

type GroupView struct {
	Name         string     `json:"name" required:"true"`
	Label        string     `json:"label" required:"true"`
	Calls        int64      `json:"calls" required:"true"`
	Failures     int64      `json:"failures" required:"true"`
	InputTokens  int64      `json:"input_tokens" required:"true"`
	OutputTokens int64      `json:"output_tokens" required:"true"`
	Costs        []CostView `json:"costs" required:"true"`
}

type CostView struct {
	Currency   string `json:"currency" required:"true"`
	CostMicros int64  `json:"cost_micros" required:"true"`
}

type SeriesView struct {
	Window       string              `json:"window" required:"true" enum:"24h,7d,30d"`
	Bucket       string              `json:"bucket" required:"true" enum:"15m,1h,6h,1d"`
	Buckets      []BucketView        `json:"buckets" required:"true"`
	Costs        []CostView          `json:"costs" required:"true"`
	Redactions   []RedactionView     `json:"redactions" required:"true"`
	Severities   []SeverityCountView `json:"severities" required:"true"`
	FindingCodes []CodeCountView     `json:"finding_codes" required:"true"`
	Channels     []ChannelTraffic    `json:"channels" required:"true"`
	Health       []SeriesHealthView  `json:"health" required:"true"`
}

// SeriesHealthView is the live breaker state of one channel model format used by the project routes.
type SeriesHealthView struct {
	ChannelID           string `json:"channel_id" required:"true"`
	ChannelName         string `json:"channel_name" required:"true"`
	ModelID             string `json:"model_id" required:"true"`
	ModelName           string `json:"model_name" required:"true"`
	Format              string `json:"format" required:"true"`
	State               string `json:"state" required:"true"`
	ConsecutiveFailures int    `json:"consecutive_failures" required:"true"`
	CooldownSeconds     int64  `json:"cooldown_seconds" required:"true"`
}

type BucketView struct {
	Start           string `json:"start" required:"true"`
	Requests        int64  `json:"requests" required:"true"`
	Errors          int64  `json:"errors" required:"true"`
	Blocked         int64  `json:"blocked" required:"true"`
	InputTokens     int64  `json:"input_tokens" required:"true"`
	OutputTokens    int64  `json:"output_tokens" required:"true"`
	DurationP95     int64  `json:"duration_p95" required:"true"`
	RedactionValues int64  `json:"redaction_values" required:"true" description:"Distinct redacted values, not occurrences"`
	FindingsHigh    int64  `json:"findings_high" required:"true"`
}

type RedactionView struct {
	Label  string `json:"label" required:"true"`
	Values int64  `json:"values" required:"true" description:"Distinct redacted values carrying this label"`
}

type SeverityCountView struct {
	Severity string `json:"severity" required:"true" enum:"info,medium,high"`
	Count    int64  `json:"count" required:"true"`
}

type CodeCountView struct {
	Code  string `json:"code" required:"true"`
	Count int64  `json:"count" required:"true"`
}

type ChannelTraffic struct {
	ChannelID   string `json:"channel_id" required:"true"`
	ChannelName string `json:"channel_name" required:"true"`
	Requests    int64  `json:"requests" required:"true"`
	Errors      int64  `json:"errors" required:"true"`
	DurationP95 int64  `json:"duration_p95" required:"true"`
}

type statsInput struct {
	projectPath
	Window string `query:"window" json:"-" enum:"24h,7d,30d"`
	Group  string `query:"group" json:"-" enum:"model,agent_key"`
}

type seriesInput struct {
	projectPath
	Window string `query:"window" json:"-" enum:"24h,7d,30d"`
	Bucket string `query:"bucket" json:"-" enum:"15m,1h,6h,1d"`
}

func costViews(costs []entity.Cost) []CostView {
	return viewAll(costs, func(cost *entity.Cost) CostView {
		return CostView{Currency: cost.Currency, CostMicros: cost.CostMicros}
	})
}

func statsView(value *entity.Statistics) StatsView {
	totals := value.Totals
	return StatsView{
		Window: value.Window,
		Totals: TotalsView{Calls: totals.Calls, Failures: totals.Failures, Blocked: totals.Blocked, InputTokens: totals.InputTokens, OutputTokens: totals.OutputTokens, RedactionValues: totals.RedactionValues, HighSeverity: totals.HighSeverity, FirstTokenP50: totals.FirstTokenP50, FirstTokenP95: totals.FirstTokenP95, DurationP50: totals.DurationP50, DurationP95: totals.DurationP95},
		Groups: viewAll(value.Groups, func(group *entity.Group) GroupView {
			return GroupView{Name: group.Name, Label: group.Label, Calls: group.Calls, Failures: group.Failures, InputTokens: group.InputTokens, OutputTokens: group.OutputTokens, Costs: costViews(group.Costs)}
		}),
		Costs: costViews(value.Costs),
	}
}

func seriesView(window, bucket string, series *entity.Series) SeriesView {
	return SeriesView{
		Window: window,
		Bucket: bucket,
		Buckets: viewAll(series.Buckets, func(entry *entity.SeriesBucket) BucketView {
			return BucketView{Start: stamp(entry.Start), Requests: entry.Requests, Errors: entry.Errors, Blocked: entry.Blocked, InputTokens: entry.InputTokens, OutputTokens: entry.OutputTokens, DurationP95: entry.DurationP95, RedactionValues: entry.RedactionValues, FindingsHigh: entry.FindingsHigh}
		}),
		Costs: costViews(series.Costs),
		Redactions: viewAll(series.Redactions, func(entry *entity.Redaction) RedactionView {
			return RedactionView{Label: entry.Label, Values: entry.Hits}
		}),
		Severities: viewAll(series.Findings, func(entry *entity.SeverityCount) SeverityCountView {
			return SeverityCountView{Severity: entry.Severity, Count: entry.Count}
		}),
		FindingCodes: viewAll(series.FindingCodes, func(entry *entity.CodeCount) CodeCountView {
			return CodeCountView{Code: entry.Code, Count: entry.Count}
		}),
		Channels: viewAll(series.Channels, func(entry *entity.ChannelTraffic) ChannelTraffic {
			return ChannelTraffic{ChannelID: entry.ChannelID, ChannelName: entry.ChannelName, Requests: entry.Requests, Errors: entry.Errors, DurationP95: entry.DurationP95}
		}),
	}
}

func (console *Console) registerStats() {
	console.add("GET", "/projects/{project_id}/stats", "getProjectStats", statsInput{}, StatsView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		statistics, err := console.Stats.Project(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Query("window"), ctx.Query("group"))
		if err != nil {
			return nil, err
		}
		return statsView(&statistics), nil
	})
	console.add("GET", "/projects/{project_id}/series", "getProjectSeries", seriesInput{}, SeriesView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		series, err := console.Stats.Series(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Query("window"), ctx.Query("bucket"))
		if err != nil {
			return nil, err
		}
		view := seriesView(series.Window, series.Bucket, &series.Series)
		view.Health = viewAll(series.Health, seriesHealthView)
		return view, nil
	})
}

func seriesHealthView(value *service.ChannelHealth) SeriesHealthView {
	return SeriesHealthView{
		ChannelID: value.ChannelID, ChannelName: value.ChannelName, ModelID: value.ModelID,
		ModelName: value.ModelName, Format: value.Format, State: string(value.State),
		ConsecutiveFailures: value.Failures, CooldownSeconds: value.Cooldown,
	}
}
