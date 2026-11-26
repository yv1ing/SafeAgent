package repository

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

const rollupOrigin = `TIMESTAMPTZ '2001-01-01 00:00:00+00'`

// Rollup recomputes the recent buckets of every granularity. Aggregation is idempotent: the bucket
// rows are replaced from the calls table, so a retry after a crash produces the same result.
func (store *Store) Rollup(ctx context.Context, now time.Time) error {
	windows := []struct {
		size     string
		interval string
		width    time.Duration
		lookback time.Duration
	}{
		{"1h", "1 hour", time.Hour, 90 * 24 * time.Hour},
		{"1d", "1 day", 24 * time.Hour, 31 * 24 * time.Hour},
	}
	for _, window := range windows {
		since := now.Add(-window.lookback).Truncate(window.width)
		if err := store.rollupTraffic(ctx, window.size, window.interval, since); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) rollupTraffic(ctx context.Context, size, interval string, since time.Time) error {
	_, err := store.DB.NewRaw(`INSERT INTO traffic_rollups
			(bucket_start, bucket_size, project_id, channel_id, model, calls, errors, blocked, input_tokens, output_tokens, duration_p95, redaction_values)
		SELECT date_bin(?::interval, created_at, `+rollupOrigin+`) AS bucket_start, ?, project_id,
			channel_id, public_model,
			count(*), count(*) FILTER (WHERE state = 'failed'), count(*) FILTER (WHERE outcome = 'blocked'),
			COALESCE(sum(input_tokens), 0), COALESCE(sum(output_tokens), 0),
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_milliseconds) FILTER (WHERE duration_milliseconds > 0), 0)::bigint,
			COALESCE(sum(redaction_values), 0)
		FROM calls WHERE lane = 'main' AND created_at >= ? GROUP BY 1, 3, 4, 5
		ON CONFLICT (bucket_start, bucket_size, project_id, channel_id, model) DO UPDATE
		SET calls = EXCLUDED.calls, errors = EXCLUDED.errors, blocked = EXCLUDED.blocked,
			input_tokens = EXCLUDED.input_tokens, output_tokens = EXCLUDED.output_tokens,
			duration_p95 = EXCLUDED.duration_p95, redaction_values = EXCLUDED.redaction_values`,
		interval, size, since).Exec(ctx)
	return err
}

// TrafficBucket is the monitor read model. DurationP95Max is the maximum of the pre-aggregated
// per-(project, channel, model) p95 values that fall in the bucket; a percentile cannot be merged
// from percentiles, so it is not the p95 of the whole bucket.
type TrafficBucket struct {
	Start           time.Time `bun:"start"`
	Requests        int64     `bun:"requests"`
	Errors          int64     `bun:"errors"`
	Blocked         int64     `bun:"blocked"`
	InputTokens     int64     `bun:"input_tokens"`
	OutputTokens    int64     `bun:"output_tokens"`
	DurationP95Max  int64     `bun:"duration_p95_max"`
	RedactionValues int64     `bun:"redaction_values"`
}

// TrafficSeries reads pre-aggregated buckets. Only the current bucket is completed from calls, so a
// monitor poll never scans history.
func (store *Store) TrafficSeries(ctx context.Context, projectIDs []string, bucketSize string, since, until time.Time, step time.Duration) ([]TrafficBucket, error) {
	if len(projectIDs) == 0 {
		return []TrafficBucket{}, nil
	}
	rows := []TrafficBucket{}
	err := store.DB.NewRaw(`WITH grid AS (
			SELECT generate_series(date_bin(make_interval(secs => ?), ?::timestamptz, `+rollupOrigin+`),
				date_bin(make_interval(secs => ?), ?::timestamptz, `+rollupOrigin+`), make_interval(secs => ?)) AS start
		), rolled AS (
			SELECT date_bin(make_interval(secs => ?), bucket_start, `+rollupOrigin+`) AS start,
				sum(calls) AS requests, sum(errors) AS errors, sum(blocked) AS blocked,
				sum(input_tokens) AS input_tokens, sum(output_tokens) AS output_tokens,
				max(duration_p95) AS duration_p95_max, sum(redaction_values) AS redaction_values
			FROM traffic_rollups
			WHERE project_id IN (?) AND bucket_size = ?
				AND bucket_start >= date_bin(make_interval(secs => ?), ?::timestamptz, `+rollupOrigin+`)
				AND bucket_start < date_bin(make_interval(secs => ?), ?::timestamptz, `+rollupOrigin+`)
			GROUP BY 1
		), tail AS (
			SELECT date_bin(make_interval(secs => ?), created_at, `+rollupOrigin+`) AS start,
				count(*) AS requests, count(*) FILTER (WHERE state = 'failed') AS errors,
				count(*) FILTER (WHERE outcome = 'blocked') AS blocked,
				COALESCE(sum(input_tokens), 0) AS input_tokens, COALESCE(sum(output_tokens), 0) AS output_tokens,
				COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_milliseconds) FILTER (WHERE duration_milliseconds > 0), 0)::bigint AS duration_p95_max,
				COALESCE(sum(redaction_values), 0) AS redaction_values
			FROM calls WHERE project_id IN (?) AND lane = 'main'
				AND created_at >= date_bin(make_interval(secs => ?), ?::timestamptz, `+rollupOrigin+`)
				AND created_at < ?
			GROUP BY 1
		), merged AS (
			SELECT start, requests, errors, blocked, input_tokens, output_tokens, duration_p95_max, redaction_values FROM rolled
			UNION ALL SELECT start, requests, errors, blocked, input_tokens, output_tokens, duration_p95_max, redaction_values FROM tail
		), summed AS (
			SELECT start, sum(requests) AS requests, sum(errors) AS errors, sum(blocked) AS blocked,
				sum(input_tokens) AS input_tokens, sum(output_tokens) AS output_tokens,
				max(duration_p95_max) AS duration_p95_max, sum(redaction_values) AS redaction_values
			FROM merged GROUP BY 1
		)
		SELECT grid.start, COALESCE(summed.requests, 0)::bigint AS requests, COALESCE(summed.errors, 0)::bigint AS errors,
			COALESCE(summed.blocked, 0)::bigint AS blocked, COALESCE(summed.input_tokens, 0)::bigint AS input_tokens,
			COALESCE(summed.output_tokens, 0)::bigint AS output_tokens, COALESCE(summed.duration_p95_max, 0)::bigint AS duration_p95_max,
			COALESCE(summed.redaction_values, 0)::bigint AS redaction_values
		FROM grid LEFT JOIN summed USING (start) ORDER BY grid.start`,
		step.Seconds(), since, step.Seconds(), until, step.Seconds(),
		step.Seconds(), bun.In(projectIDs), bucketSize, step.Seconds(), since, step.Seconds(), until,
		step.Seconds(), bun.In(projectIDs), step.Seconds(), until, until).Scan(ctx, &rows)
	return rows, err
}
