package repository

import (
	"context"
	"time"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

const (
	seriesOrigin       = `TIMESTAMPTZ '2001-01-01 00:00:00+00'`
	redactionCallLimit = 5000
	channelRowLimit    = 50
	codeRowLimit       = 50
)

// Series aggregates one project's traffic over [now-window, now] into fixed buckets. Empty buckets are
// present with zero counts so the caller can chart them without gap handling.
func (store *Store) Series(ctx context.Context, projectID string, window, bucket time.Duration) (entity.Series, error) {
	now := time.Now().UTC()
	since := now.Add(-window)
	step := bucket.Seconds()
	result := entity.Series{
		Buckets: []entity.SeriesBucket{}, Costs: []entity.Cost{}, Redactions: []entity.Redaction{},
		Findings: []entity.SeverityCount{}, FindingCodes: []entity.CodeCount{}, Channels: []entity.ChannelTraffic{},
	}
	if err := store.DB.NewRaw(`WITH grid AS (
			SELECT generate_series(date_bin(make_interval(secs => ?), ?::timestamptz, `+seriesOrigin+`),
				date_bin(make_interval(secs => ?), ?::timestamptz, `+seriesOrigin+`), make_interval(secs => ?)) AS start
		), traffic AS (
			SELECT date_bin(make_interval(secs => ?), created_at, `+seriesOrigin+`) AS start,
				count(*) AS requests,
				count(*) FILTER (WHERE state = 'failed') AS errors,
				count(*) FILTER (WHERE state = 'blocked') AS blocked,
				coalesce(sum(input_tokens), 0) AS input_tokens,
				coalesce(sum(output_tokens), 0) AS output_tokens,
				coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_milliseconds) FILTER (WHERE duration_milliseconds > 0), 0)::bigint AS duration_p95,
				coalesce(sum(redaction_hits), 0) AS redaction_hits,
				count(*) FILTER (WHERE max_severity = 'high') AS findings_high
			FROM calls WHERE project_id = ? AND created_at >= ? GROUP BY 1
		)
		SELECT grid.start, coalesce(traffic.requests, 0) AS requests, coalesce(traffic.errors, 0) AS errors,
			coalesce(traffic.blocked, 0) AS blocked, coalesce(traffic.input_tokens, 0) AS input_tokens,
			coalesce(traffic.output_tokens, 0) AS output_tokens, coalesce(traffic.duration_p95, 0) AS duration_p95,
			coalesce(traffic.redaction_hits, 0) AS redaction_hits, coalesce(traffic.findings_high, 0) AS findings_high
		FROM grid LEFT JOIN traffic USING (start) ORDER BY grid.start`,
		step, since, step, now, step, step, projectID, since).Scan(ctx, &result.Buckets); err != nil {
		return result, err
	}
	if err := store.DB.NewRaw(`SELECT currency, sum(cost_micros) AS cost_micros FROM calls
		WHERE project_id = ? AND created_at >= ? AND cost_micros IS NOT NULL AND currency <> ''
		GROUP BY 1 ORDER BY 1`, projectID, since).Scan(ctx, &result.Costs); err != nil {
		return result, err
	}
	if err := store.DB.NewRaw(`SELECT hit->>'label' AS label, count(*) AS hits
		FROM (SELECT id FROM calls WHERE project_id = ? AND created_at >= ? AND redaction_hits > 0
				ORDER BY created_at DESC, id DESC LIMIT ?) recent
		JOIN events e ON e.call_id = recent.id AND e.kind = 'request'
		CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(e.payload->'hits') = 'array' THEN e.payload->'hits' ELSE '[]'::jsonb END) hit
		WHERE e.project_id = ? GROUP BY 1 ORDER BY hits DESC, label`,
		projectID, since, redactionCallLimit, projectID).Scan(ctx, &result.Redactions); err != nil {
		return result, err
	}
	if err := store.DB.NewRaw(`SELECT severity, count(*) AS count
		FROM analysis_findings WHERE project_id = ? AND created_at >= ?
		GROUP BY 1 ORDER BY 1`, projectID, since).Scan(ctx, &result.Findings); err != nil {
		return result, err
	}
	if err := store.DB.NewRaw(`SELECT code, count(*) AS count
		FROM analysis_findings WHERE project_id = ? AND created_at >= ?
		GROUP BY 1 ORDER BY count DESC, code LIMIT ?`, projectID, since, codeRowLimit).Scan(ctx, &result.FindingCodes); err != nil {
		return result, err
	}
	if err := store.DB.NewRaw(`SELECT c.channel_id, coalesce(ch.name, c.channel_id) AS channel_name,
			count(*) AS requests, count(*) FILTER (WHERE c.state = 'failed') AS errors,
			coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY c.duration_milliseconds) FILTER (WHERE c.duration_milliseconds > 0), 0)::bigint AS duration_p95
		FROM calls c LEFT JOIN channels ch ON ch.id = c.channel_id
		WHERE c.project_id = ? AND c.created_at >= ? AND c.channel_id <> ''
		GROUP BY 1, 2 ORDER BY requests DESC, channel_name LIMIT ?`, projectID, since, channelRowLimit).Scan(ctx, &result.Channels); err != nil {
		return result, err
	}
	return result, nil
}
