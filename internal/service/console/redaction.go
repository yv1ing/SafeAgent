package console

import (
	"context"
	"sort"

	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type RedactionValue struct {
	ValueID     string `json:"value_id"`
	Label       string `json:"label"`
	Action      string `json:"action"`
	Secret      bool   `json:"secret"`
	Occurrences int    `json:"occurrences"`
	FirstCallID string `json:"first_call_id"`
	LastCallID  string `json:"last_call_id"`
}

type RedactionService struct {
	store *repository.Store
}

func NewRedactionService(store *repository.Store) *RedactionService {
	return &RedactionService{store: store}
}

// Index aggregates redaction hits from observation events. The hits are event data, not a separate
// source of truth.
func (service *RedactionService) Index(ctx context.Context, actor access.Actor, projectID, trajectoryID string) ([]RedactionValue, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	events, err := service.store.TrajectoryEvents(ctx, projectID, trajectoryID)
	if err != nil {
		return nil, err
	}
	values := map[string]*RedactionValue{}
	for index := range events {
		if events[index].Kind != trajectory.KindObservation {
			continue
		}
		payload, err := trajectory.DecodePayload[trajectory.Observation](&events[index])
		if err != nil {
			continue
		}
		for _, hit := range payload.Redactions {
			if hit.ValueID == "" {
				continue
			}
			value, found := values[hit.ValueID]
			if !found {
				value = &RedactionValue{ValueID: hit.ValueID, Label: hit.Label, Action: string(hit.Action), Secret: hit.Secret, FirstCallID: hit.CallID}
				values[hit.ValueID] = value
			}
			value.Occurrences++
			value.LastCallID = hit.CallID
		}
	}
	result := make([]RedactionValue, 0, len(values))
	for _, value := range values {
		result = append(result, *value)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Occurrences != result[right].Occurrences {
			return result[left].Occurrences > result[right].Occurrences
		}
		return result[left].ValueID < result[right].ValueID
	})
	return result, nil
}
