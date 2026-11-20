package repository

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// Windows maps a statistics window to its length; it is the single list of accepted windows.
var Windows = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

type costRow struct {
	Name       string
	Currency   string
	CostMicros int64
}

func (store *Store) Statistics(ctx context.Context, projectID, window, group string) (entity.Statistics, error) {
	since := time.Now().UTC().Add(-Windows[window])
	result := entity.Statistics{Window: window, Groups: []entity.Group{}, Costs: []entity.Cost{}}
	err := store.DB.NewRaw(`SELECT
			count(*) AS calls,
			count(*) FILTER (WHERE state = 'failed') AS failures,
			count(*) FILTER (WHERE state = 'blocked') AS blocked,
			coalesce(sum(input_tokens), 0) AS input_tokens,
			coalesce(sum(output_tokens), 0) AS output_tokens,
			coalesce(sum(redaction_hits), 0) AS redaction_hits,
			count(*) FILTER (WHERE max_severity = 'high') AS high_severity,
			coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY first_token_milliseconds) FILTER (WHERE first_token_milliseconds > 0), 0)::bigint AS first_token_p50,
			coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY first_token_milliseconds) FILTER (WHERE first_token_milliseconds > 0), 0)::bigint AS first_token_p95,
			coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_milliseconds) FILTER (WHERE duration_milliseconds > 0), 0)::bigint AS duration_p50,
			coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_milliseconds) FILTER (WHERE duration_milliseconds > 0), 0)::bigint AS duration_p95
		FROM calls WHERE project_id = ? AND created_at >= ?`, projectID, since).
		Scan(ctx, &result.Totals)
	if err != nil {
		return result, err
	}
	column, label := "public_model", "public_model"
	if group == "agent_key" {
		column, label = "agent_key_id", "coalesce((SELECT k.label FROM agent_keys k WHERE k.id = calls.agent_key_id), '')"
	}
	if err := store.DB.NewRaw(`SELECT `+column+` AS name, `+label+` AS label,
			count(*) AS calls,
			count(*) FILTER (WHERE state = 'failed') AS failures,
			coalesce(sum(input_tokens), 0) AS input_tokens,
			coalesce(sum(output_tokens), 0) AS output_tokens
		FROM calls WHERE project_id = ? AND created_at >= ?
		GROUP BY 1, 2 ORDER BY calls DESC, name LIMIT 50`, projectID, since).Scan(ctx, &result.Groups); err != nil {
		return result, err
	}
	rows := []costRow{}
	if err := store.DB.NewRaw(`SELECT `+column+` AS name, currency, sum(cost_micros) AS cost_micros
		FROM calls WHERE project_id = ? AND created_at >= ? AND cost_micros IS NOT NULL AND currency <> ''
		GROUP BY 1, 2 ORDER BY 1, 2`, projectID, since).Scan(ctx, &rows); err != nil {
		return result, err
	}
	index := make(map[string]int, len(result.Groups))
	for position, entry := range result.Groups {
		index[entry.Name] = position
		result.Groups[position].Costs = []entity.Cost{}
	}
	for _, row := range rows {
		if position, found := index[row.Name]; found {
			result.Groups[position].Costs = append(result.Groups[position].Costs, entity.Cost{Currency: row.Currency, CostMicros: row.CostMicros})
		}
	}
	if err := store.DB.NewRaw(`SELECT currency, sum(cost_micros) AS cost_micros
		FROM calls WHERE project_id = ? AND created_at >= ? AND cost_micros IS NOT NULL AND currency <> ''
		GROUP BY 1 ORDER BY 1`, projectID, since).Scan(ctx, &result.Costs); err != nil {
		return result, err
	}
	return result, nil
}

// ProjectSummaries returns the 24h traffic aggregate of every named project in one query.
func (store *Store) ProjectSummaries(ctx context.Context, projectIDs []string) (map[string]entity.ProjectSummary, error) {
	summaries := map[string]entity.ProjectSummary{}
	if len(projectIDs) == 0 {
		return summaries, nil
	}
	since := time.Now().UTC().Add(-24 * time.Hour)
	rows := []entity.ProjectSummary{}
	if err := store.DB.NewRaw(`
		SELECT
			project_id,
			count(*) AS calls_24h,
			count(*) FILTER (WHERE state = 'blocked') AS blocked_24h,
			count(*) FILTER (WHERE state = 'failed') AS failed_24h,
			COALESCE(EXTRACT(EPOCH FROM max(created_at)) * 1000, 0)::bigint AS last_activity_at,
			COALESCE(sum(input_tokens), 0) AS input_tokens_24h,
			COALESCE(sum(output_tokens), 0) AS output_tokens_24h
		FROM calls
		WHERE project_id IN (?) AND created_at >= ?
		GROUP BY project_id
	`, bun.In(projectIDs), since).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		summaries[row.ProjectID] = row
	}
	return summaries, nil
}
