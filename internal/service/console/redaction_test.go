package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/observation"
	diagnosis "github.com/yv1ing/SafeAgent/internal/service/diagnosis"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func hit(valueID, label, section string, ordinal, start, end, shownStart, shownEnd int) auditevent.Hit {
	return auditevent.Hit{
		Label: label, Action: "replace", ValueID: valueID, Section: section, Ordinal: ordinal,
		Path: "items[0].text", Start: start, End: end, ShownStart: shownStart, ShownEnd: shownEnd,
	}
}

func TestRedactionIndexDeduplicatesByValueID(t *testing.T) {
	first := projectionCall("c1", "", "completed")
	second := projectionCall("c2", "c1", "completed")
	projection := &sessionProjection{
		Conversation: &entity.Conversation{ID: "session", ProjectID: "prj"},
		Calls:        []entity.Call{first, second},
		Rebuilds: map[string]diagnosis.Rebuild{
			"c1": {Hits: []auditevent.Hit{
				hit("v1", "EMAIL", "request", 0, 0, 5, 0, 12),
				hit("v1", "EMAIL", "request", 1, 0, 5, 0, 12),
				hit("v2", "PHONE", "request", 0, 10, 21, 10, 25),
			}},
			"c2": {Hits: []auditevent.Hit{
				hit("v1", "EMAIL", "request", 0, 0, 5, 0, 12),
			}},
		},
	}
	index := BuildRedactionIndex(projection)
	if index.Total != 4 {
		t.Fatalf("total occurrences = %d", index.Total)
	}
	if len(index.Values) != 2 {
		t.Fatalf("distinct values = %d: %+v", len(index.Values), index.Values)
	}
	email := index.Values[0]
	if email.ValueID != "v1" || email.Count != 3 || email.FirstCallID != "c1" || email.LastCallID != "c2" {
		t.Fatalf("email value = %+v", email)
	}
	if len(email.Occurrences) != 3 {
		t.Fatalf("email occurrences = %+v", email.Occurrences)
	}
	firstLocation := email.Occurrences[0]
	if firstLocation.CallID != "c1" || firstLocation.Start != 0 || firstLocation.End != 5 || firstLocation.ShownEnd != 12 {
		t.Fatalf("first location = %+v", firstLocation)
	}
	if firstLocation.CallCreatedAt != first.CreatedAt {
		t.Fatalf("call time = %v", firstLocation.CallCreatedAt)
	}
	phone := index.Values[1]
	if phone.Label != "PHONE" || phone.Occurrences[0].Ordinal != 0 || phone.Occurrences[0].Start != 10 {
		t.Fatalf("phone value = %+v", phone)
	}
	if phone.Detector != "cn_mobile" {
		t.Fatalf("detector hint = %q", phone.Detector)
	}
}

func TestCallRedactionsKeepOnlyTheCallsOwnValues(t *testing.T) {
	call := projectionCall("c1", "", "completed")
	values := CallRedactions(&call, []auditevent.Hit{
		hit("v1", "EMAIL", "request", 0, 0, 5, 0, 12),
		hit("v1", "EMAIL", "request", 1, 0, 5, 0, 12),
		hit("v2", "PHONE", "request", 0, 10, 21, 10, 25),
	})
	if len(values) != 2 {
		t.Fatalf("values = %+v", values)
	}
	if values[0].ValueID != "v1" || values[0].Count != 2 || len(values[0].Occurrences) != 2 {
		t.Fatalf("first value = %+v", values[0])
	}
	if values[1].ValueID != "v2" || values[1].Count != 1 {
		t.Fatalf("second value = %+v", values[1])
	}
	if values[0].Action != "replace" || values[0].Secret {
		t.Fatalf("action/secret = %+v", values[0])
	}
}

func TestRedactionIndexSkipsCallsWithoutRebuild(t *testing.T) {
	call := projectionCall("c1", "", "running")
	projection := &sessionProjection{
		Conversation: &entity.Conversation{ID: "session", ProjectID: "prj"},
		Calls:        []entity.Call{call},
		Rebuilds:     map[string]diagnosis.Rebuild{},
	}
	index := BuildRedactionIndex(projection)
	if index.Total != 0 || len(index.Values) != 0 {
		t.Fatalf("index = %+v", index)
	}
}

func TestRedactionIndexNeverCarriesRawValues(t *testing.T) {
	call := projectionCall("c1", "", "completed")
	projection := &sessionProjection{
		Conversation: &entity.Conversation{ID: "session", ProjectID: "prj"},
		Calls:        []entity.Call{call},
		Rebuilds: map[string]diagnosis.Rebuild{
			"c1": {Hits: []auditevent.Hit{hit("v1", "EMAIL", "request", 0, 0, 5, 0, 12)}},
		},
	}
	index := BuildRedactionIndex(projection)
	value := index.Values[0]
	if value.ValueID != "v1" || value.Label != "EMAIL" {
		t.Fatalf("value = %+v", value)
	}
	if value.Occurrences[0].Section != observation.SectionRequest {
		t.Fatalf("section = %q", value.Occurrences[0].Section)
	}
}
