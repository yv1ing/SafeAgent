package console

import (
	"context"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/service/access"
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

// ChannelHealth is the live breaker state of one channel model format.
type ChannelHealth struct {
	ChannelID   string
	ChannelName string
	ModelID     string
	ModelName   string
	Format      string
	routing.Health
}

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
		return "", 0, access.Invalid("invalid_window")
	}
	if bucket == "" {
		bucket = defaultBuckets[window]
	}
	length, bucketKnown := bucketLengths[bucket]
	if !bucketKnown {
		return "", 0, access.Invalid("invalid_bucket")
	}
	count := int(duration / length)
	if count < 2 || count > MaximumBuckets {
		return "", 0, access.Invalid("invalid_bucket")
	}
	return bucket, length, nil
}

func (service *StatsService) Project(ctx context.Context, actor access.Actor, projectID, window, group string) (entity.Statistics, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return entity.Statistics{}, err
	}
	if window == "" {
		window = DefaultWindow
	}
	if _, found := repository.Windows[window]; !found {
		return entity.Statistics{}, access.Invalid("invalid_window")
	}
	if group == "" {
		group = DefaultGroup
	}
	if group != DefaultGroup && group != groupAgentKey {
		return entity.Statistics{}, access.Invalid("invalid_group")
	}
	statistics, err := service.store.Statistics(ctx, projectID, window, group)
	return statistics, access.Translate(err)
}

// Series returns bucketed traffic plus the live breaker state of every channel model the project
// has routed to in the window.
func (service *StatsService) Series(ctx context.Context, actor access.Actor, projectID, window, bucket string) (*StatsSeries, error) {
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
		return nil, access.Translate(err)
	}
	health, err := service.health(ctx, projectID)
	if err != nil {
		return nil, access.Translate(err)
	}
	return &StatsSeries{Window: window, Bucket: bucket, Series: stored, Health: health}, nil
}

// health walks the project's route targets. A route target names a channel, a catalog model and
// optionally one format; the formats a target may serve are derived from the provider endpoints and
// the model kind, exactly like the runtime resolver does.
func (service *StatsService) health(ctx context.Context, projectID string) ([]ChannelHealth, error) {
	routes, err := service.store.Routes(ctx, projectID, false)
	if err != nil {
		return nil, err
	}
	seen := map[[3]string]bool{}
	health := []ChannelHealth{}
	for _, route := range routes {
		if !route.Route.Enabled {
			continue
		}
		for _, target := range route.Targets {
			if !target.Target.Enabled || !target.Channel.Enabled || !target.Model.Enabled {
				continue
			}
			endpoints, err := provider.UnmarshalEndpoints(target.Provider.Endpoints)
			if err != nil {
				continue
			}
			formats := make([]string, 0, 4)
			if target.Target.Format != "" {
				formats = append(formats, target.Target.Format)
			} else {
				for _, format := range provider.FormatsForKind(endpoints, string(target.Model.Kind)) {
					formats = append(formats, string(format))
				}
			}
			for _, format := range formats {
				key := [3]string{target.Channel.ID, target.Model.ID, format}
				if seen[key] {
					continue
				}
				seen[key] = true
				health = append(health, ChannelHealth{
					ChannelID: target.Channel.ID, ChannelName: target.Channel.Name,
					ModelID: target.Model.ID, ModelName: target.Model.UpstreamName, Format: format,
					Health: service.selector.Health(target.Channel.ID, target.Model.ID, format),
				})
			}
		}
	}
	return health, nil
}

func (service *StatsService) ProjectSummaries(ctx context.Context, projectIDs []string) (map[string]entity.ProjectSummary, error) {
	return service.store.ProjectSummaries(ctx, projectIDs)
}
