package console

import (
	"context"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	DefaultWindow  = "24h"
	DefaultGroup   = "model"
	MaximumBuckets = 200
	groupAgentKey  = "agent_key"
	bucketQuarter  = "15m"
	bucketHour     = "1h"
	bucketSixHours = "6h"
	bucketDay      = "1d"
)

var bucketLengths = map[string]time.Duration{
	bucketQuarter:  15 * time.Minute,
	bucketHour:     time.Hour,
	bucketSixHours: 6 * time.Hour,
	bucketDay:      24 * time.Hour,
}

// defaultBuckets is the bucket length a window uses when the caller names none.
var defaultBuckets = map[string]string{"24h": bucketQuarter, "7d": bucketSixHours, "30d": bucketDay}

type StatsService struct {
	store    *repository.Store
	selector *routing.Selector
}

func NewStatsService(store *repository.Store, selector *routing.Selector) *StatsService {
	return &StatsService{store: store, selector: selector}
}

type StatsSeries struct {
	Window string
	Bucket string
	Series entity.Series
	Health []ChannelHealth
}

// resolveBucket picks the bucket for a window and rejects combinations that would draw too few or
// too many points.
func resolveBucket(window, bucket string) (string, time.Duration, error) {
	duration, windowKnown := repository.Windows[window]
	if !windowKnown {
		return "", 0, Invalid("invalid_window")
	}
	if bucket == "" {
		bucket = defaultBuckets[window]
	}
	length, bucketKnown := bucketLengths[bucket]
	if !bucketKnown {
		return "", 0, Invalid("invalid_bucket")
	}
	count := int(duration / length)
	if count < 2 || count > MaximumBuckets {
		return "", 0, Invalid("invalid_bucket")
	}
	return bucket, length, nil
}

func (service *StatsService) Project(ctx context.Context, actor Actor, projectID, window, group string) (entity.Statistics, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return entity.Statistics{}, err
	}
	if window == "" {
		window = DefaultWindow
	}
	if _, found := repository.Windows[window]; !found {
		return entity.Statistics{}, Invalid("invalid_window")
	}
	if group == "" {
		group = DefaultGroup
	}
	if group != DefaultGroup && group != groupAgentKey {
		return entity.Statistics{}, Invalid("invalid_group")
	}
	statistics, err := service.store.Statistics(ctx, projectID, window, group)
	return statistics, Translate(err)
}

// StatsSeries returns bucketed traffic plus the live breaker state of every channel model the project
// has routed to in the window.
func (service *StatsService) Series(ctx context.Context, actor Actor, projectID, window, bucket string) (*StatsSeries, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if window == "" {
		window = DefaultWindow
	}
	bucket, length, err := resolveBucket(window, bucket)
	if err != nil {
		return nil, err
	}
	stored, err := service.store.Series(ctx, projectID, repository.Windows[window], length)
	if err != nil {
		return nil, Translate(err)
	}
	health, err := service.health(ctx, projectID)
	if err != nil {
		return nil, Translate(err)
	}
	return &StatsSeries{Window: window, Bucket: bucket, Series: stored, Health: health}, nil
}

func (service *StatsService) health(ctx context.Context, projectID string) ([]ChannelHealth, error) {
	routes, err := service.store.Routes(ctx, projectID)
	if err != nil {
		return nil, err
	}
	channels, err := service.store.Channels(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(channels))
	for _, entry := range channels {
		names[entry.ID] = entry.Name
	}
	seen := map[[2]string]bool{}
	health := []ChannelHealth{}
	for _, route := range routes {
		for _, target := range route.Targets {
			key := [2]string{target.ChannelID, target.UpstreamModel}
			name, known := names[target.ChannelID]
			if seen[key] || !known {
				continue
			}
			seen[key] = true
			health = append(health, ChannelHealth{ChannelID: target.ChannelID, ChannelName: name, UpstreamModel: target.UpstreamModel, Health: service.selector.Health(target.ChannelID, target.UpstreamModel)})
		}
	}
	return health, nil
}

func (service *StatsService) ProjectSummaries(ctx context.Context, projectIDs []string) (map[string]entity.ProjectSummary, error) {
	return service.store.ProjectSummaries(ctx, projectIDs)
}
