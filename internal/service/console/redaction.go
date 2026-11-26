package console

import (
	"context"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// RedactionIndex is the deduplicated redaction view of one conversation. Each value appears once,
// with every occurrence anchored to the call, section and ordinal that stored it; raw values are
// never part of the index.
type RedactionIndex struct {
	SessionID string           `json:"session_id"`
	Total     int              `json:"total"`
	Values    []RedactionValue `json:"values"`
}

type RedactionValue struct {
	ValueID     string                `json:"value_id"`
	Label       string                `json:"label"`
	Action      redact.Action         `json:"action"`
	Detector    string                `json:"detector,omitempty"`
	Secret      bool                  `json:"secret"`
	Count       int                   `json:"count"`
	FirstCallID string                `json:"first_call_id"`
	LastCallID  string                `json:"last_call_id"`
	Occurrences []RedactionOccurrence `json:"occurrences"`
}

type RedactionOccurrence struct {
	CallID        string              `json:"call_id"`
	Section       observation.Section `json:"section"`
	Ordinal       int                 `json:"ordinal"`
	Path          string              `json:"path"`
	CallCreatedAt time.Time           `json:"call_created_at"`
	Start         int                 `json:"start"`
	End           int                 `json:"end"`
	ShownStart    int                 `json:"shown_start"`
	ShownEnd      int                 `json:"shown_end"`
}

// RedactionService reads the session redaction index. It shares the trajectory service's loader and
// cache, so the conversation graph and the index are always built from the same snapshot.
type RedactionService struct {
	trajectory *TrajectoryService
}

func NewRedactionService(trajectory *TrajectoryService) *RedactionService {
	return &RedactionService{trajectory: trajectory}
}

func (service *RedactionService) Index(ctx context.Context, actor access.Actor, projectID, sessionID string) (*RedactionIndex, error) {
	if err := actor.RequireProject(ctx, service.trajectory.store, projectID); err != nil {
		return nil, err
	}
	projection, err := service.trajectory.load(ctx, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	return BuildRedactionIndex(projection), nil
}

// BuildRedactionIndex groups the session's persisted hits by value id. It is pure so fixtures can
// verify deduplication and occurrence ranges without a database.
func BuildRedactionIndex(projection *sessionProjection) *RedactionIndex {
	index := &RedactionIndex{SessionID: projection.Conversation.ID, Values: []RedactionValue{}}
	position := map[string]int{}
	for _, call := range projection.Calls {
		rebuild, rebuilt := projection.Rebuilds[call.ID]
		if !rebuilt {
			continue
		}
		for _, hit := range rebuild.Hits {
			if hit.ValueID == "" {
				continue
			}
			occurrence := redactionOccurrence(call, hit)
			index.Total++
			valueIndex, found := position[hit.ValueID]
			if !found {
				position[hit.ValueID] = len(index.Values)
				index.Values = append(index.Values, RedactionValue{
					ValueID: hit.ValueID, Label: hit.Label, Action: hit.Action, Secret: hit.Secret,
					Detector: detectorHint(hit.Label), FirstCallID: call.ID,
					Occurrences: []RedactionOccurrence{},
				})
				valueIndex = len(index.Values) - 1
			}
			value := &index.Values[valueIndex]
			value.Count++
			value.LastCallID = call.ID
			value.Occurrences = append(value.Occurrences, occurrence)
		}
	}
	return index
}

// CallRedactions keeps only the values one call introduced, deduplicated by value id. A call detail
// must not accumulate the values of its ancestors.
func CallRedactions(call *entity.Call, hits []auditevent.Hit) []RedactionValue {
	values := []RedactionValue{}
	position := map[string]int{}
	for _, hit := range hits {
		if hit.ValueID == "" {
			continue
		}
		index, found := position[hit.ValueID]
		if !found {
			position[hit.ValueID] = len(values)
			values = append(values, RedactionValue{
				ValueID: hit.ValueID, Label: hit.Label, Action: hit.Action, Secret: hit.Secret,
				Detector: detectorHint(hit.Label), FirstCallID: call.ID, LastCallID: call.ID,
				Occurrences: []RedactionOccurrence{},
			})
			index = len(values) - 1
		}
		value := &values[index]
		value.Count++
		value.Occurrences = append(value.Occurrences, redactionOccurrence(*call, hit))
	}
	return values
}

func redactionOccurrence(call entity.Call, hit auditevent.Hit) RedactionOccurrence {
	return RedactionOccurrence{
		CallID: call.ID, Section: observation.Section(hit.Section), Ordinal: hit.Ordinal, Path: hit.Path,
		CallCreatedAt: call.CreatedAt, Start: hit.Start, End: hit.End, ShownStart: hit.ShownStart, ShownEnd: hit.ShownEnd,
	}
}

var detectorHintOnce sync.Once
var detectorHints map[string]string

// detectorHint derives the built-in detector for a label when the label names exactly one detector.
// Stored hits carry the label and the action, not the detector, so this is a hint rather than a
// persisted fact.
func detectorHint(label string) string {
	detectorHintOnce.Do(func() {
		counts := map[string]int{}
		hints := map[string]string{}
		for _, detector := range redact.Detectors {
			counts[detector.Label]++
			hints[detector.Label] = detector.ID
		}
		derived := make(map[string]string, len(hints))
		for labelName, detectorID := range hints {
			if counts[labelName] == 1 {
				derived[labelName] = detectorID
			}
		}
		detectorHints = derived
	})
	return detectorHints[label]
}
