package evidence_test

import (
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/evidence"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

const secret = "0123456789abcdef0123456789abcdef"

func TestBundleRejectsMissingSegment(t *testing.T) {
	event := trajectory.Event{
		ID: trajectory.NewID("evt"), TrajectoryID: "trj_1", ProjectID: "project_1", Seq: 1,
		Kind: trajectory.KindObservation, Boundary: trajectory.BoundaryIngress, Actor: trajectory.ActorGateway,
		At: time.Unix(1, 0).UTC(), Inputs: []trajectory.Ref{{Segment: "seg_missing", Role: "observation"}},
		Payload: trajectory.Marshal(map[string]int{"n": 1}),
	}
	if err := trajectory.Sign(&event, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	events := []trajectory.Event{event}
	checkpoint := evidence.NewCheckpoint("trj_1", events, []byte(secret), nil)
	bundle := evidence.Build("trj_1", events, nil, nil, nil, checkpoint, nil, evidence.Policy{})
	report := evidence.Verify(bundle, []byte(secret))
	if report.Valid || len(report.Failures) == 0 || report.Failures[0] != "segment_missing:seg_missing" {
		t.Fatalf("missing segment was not detected: %+v", report)
	}
}

func TestBundleVerifiesAndDetectsTampering(t *testing.T) {
	events := []trajectory.Event{}
	previous := ""
	for index := 0; index < 2; index++ {
		event := trajectory.Event{
			ID: trajectory.NewID("evt"), TrajectoryID: "trj_1", ProjectID: "project_1", Seq: int64(index + 1),
			Kind: trajectory.KindObservation, Boundary: trajectory.BoundaryIngress, Actor: trajectory.ActorGateway,
			At: time.Unix(1, 0).UTC(), Payload: trajectory.Marshal(map[string]int{"n": index}), PrevHash: previous,
		}
		if err := trajectory.Sign(&event, []byte(secret)); err != nil {
			t.Fatal(err)
		}
		previous = event.Hash
		events = append(events, event)
	}
	checkpoint := evidence.NewCheckpoint("trj_1", events, []byte(secret), nil)
	content := []byte("hello")
	bundle := evidence.Build("trj_1", events, []evidence.Segment{{ID: trajectory.ContentHash(content), Content: content}}, nil, nil, checkpoint, nil, evidence.Policy{})
	if report := evidence.Verify(bundle, []byte(secret)); !report.Valid {
		t.Fatalf("valid bundle failed: %+v", report)
	}
	bundle.Events[0].Payload = []byte(`{"n":99}`)
	if report := evidence.Verify(bundle, []byte(secret)); report.Valid || report.Failures[0] != "chain:hash_mismatch" {
		t.Fatalf("tamper was not detected: %+v", report)
	}
}
