package console

import (
	"errors"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/service/access"
)

func isKind(err error, kind access.Kind) bool {
	var failure *access.Failure
	return errors.As(err, &failure) && failure.Kind == kind
}

func TestCallFilterValidatesClassifiedAxes(t *testing.T) {
	valid := []CallFilter{
		{},
		{Lane: string(classify.LaneSide)},
		{Operation: string(classify.Embeddings)},
		{Purpose: string(classify.PurposeCompaction)},
		{Outcome: string(classify.OutcomeNotFound)},
		{State: "completed", MinSeverity: "high"},
	}
	for _, filter := range valid {
		if err := filter.validate(); err != nil {
			t.Fatalf("valid filter rejected: %+v: %v", filter, err)
		}
	}
	invalid := []struct {
		filter CallFilter
		code   string
	}{
		{CallFilter{State: "blocked"}, "invalid_state"},
		{CallFilter{State: "rejected"}, "invalid_state"},
		{CallFilter{MinSeverity: "critical"}, "invalid_severity"},
		{CallFilter{Lane: "background"}, "invalid_lane"},
		{CallFilter{Operation: "chat"}, "invalid_operation"},
		{CallFilter{Purpose: "conversation"}, "invalid_purpose"},
		{CallFilter{Outcome: "timeout"}, "invalid_outcome"},
	}
	for _, testCase := range invalid {
		err := testCase.filter.validate()
		if !isKind(err, access.KindInvalid) {
			t.Fatalf("%+v must be invalid, got %v", testCase.filter, err)
		}
		var failure *access.Failure
		if !asFailure(err, &failure) || failure.Code != testCase.code {
			t.Fatalf("%+v: code = %v", testCase.filter, err)
		}
	}
}

func asFailure(err error, target **access.Failure) bool {
	failure, ok := err.(*access.Failure)
	if !ok {
		return false
	}
	*target = failure
	return true
}

func TestCallFilterDefaultsHideAuxiliaryCalls(t *testing.T) {
	filter := CallFilter{}.repositoryFilter(Cursor{Limit: 10})
	if filter.IncludeAuxiliary || len(filter.Lanes) != 0 {
		t.Fatalf("default filter = %+v", filter)
	}
	explicit := CallFilter{Lane: string(classify.LaneAuxiliary), IncludeAuxiliary: true}.repositoryFilter(Cursor{Limit: 10})
	if len(explicit.Lanes) != 1 || explicit.Lanes[0] != classify.LaneAuxiliary {
		t.Fatalf("explicit lane filter = %+v", explicit)
	}
	if explicit.IncludeAuxiliary {
		t.Fatal("an explicit lane list overrides the auxiliary default")
	}
}

func TestCallFilterConvertsClassifiedAxes(t *testing.T) {
	filter := CallFilter{
		Lane: string(classify.LaneMain), Operation: string(classify.Generation),
		Purpose: string(classify.PurposeTitle), Outcome: string(classify.OutcomeUpstreamError),
	}.repositoryFilter(Cursor{Limit: 5, BeforeID: "c1"})
	if len(filter.Lanes) != 1 || filter.Lanes[0] != classify.LaneMain {
		t.Fatalf("lanes = %+v", filter.Lanes)
	}
	if len(filter.Operations) != 1 || filter.Operations[0] != classify.Generation {
		t.Fatalf("operations = %+v", filter.Operations)
	}
	if len(filter.Purposes) != 1 || filter.Purposes[0] != classify.PurposeTitle {
		t.Fatalf("purposes = %+v", filter.Purposes)
	}
	if len(filter.Outcomes) != 1 || filter.Outcomes[0] != classify.OutcomeUpstreamError {
		t.Fatalf("outcomes = %+v", filter.Outcomes)
	}
	if filter.Limit != 6 {
		t.Fatalf("limit = %d", filter.Limit)
	}
}
