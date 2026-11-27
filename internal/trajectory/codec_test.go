package trajectory_test

import (
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

const secret = "0123456789abcdef0123456789abcdef"

func TestChainDetectsTampering(t *testing.T) {
	events := chain(t)
	report := trajectory.VerifyChain(events, []byte(secret))
	if report.Status != trajectory.StatusVerified || report.Events != 3 {
		t.Fatalf("valid chain: %+v", report)
	}
	tampered := append([]trajectory.Event(nil), events...)
	tampered[1].Payload = []byte(`{"changed":true}`)
	report = trajectory.VerifyChain(tampered, []byte(secret))
	if report.Status != trajectory.StatusBroken || report.BrokenAt != 2 || report.Error != "hash_mismatch" {
		t.Fatalf("payload tamper: %+v", report)
	}
	tampered = append([]trajectory.Event(nil), events...)
	tampered[2].PrevHash = "00"
	report = trajectory.VerifyChain(tampered, []byte(secret))
	if report.Status != trajectory.StatusBroken || report.Error != "previous_hash_mismatch" {
		t.Fatalf("previous hash tamper: %+v", report)
	}
}

func TestCanonicalPayloadIsStable(t *testing.T) {
	canonical, err := trajectory.Canonicalize([]byte(`{"b":1.0,"a":1e2}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != `{"a":100,"b":1}` {
		t.Fatalf("canonical=%s", canonical)
	}
}

func TestPayloadNullEscapeIsReplaced(t *testing.T) {
	payload := trajectory.SanitizePayload([]byte(`{"text":"a\u0000b"}`))
	if string(payload) != `{"text":"a\ufffdb"}` {
		t.Fatalf("sanitized=%s", payload)
	}
	event := trajectory.Event{
		ID: trajectory.NewID("evt"), TrajectoryID: "trj_1", ProjectID: "project_1", Seq: 1,
		Kind: trajectory.KindObservation, Boundary: trajectory.BoundaryIngress, Actor: trajectory.ActorGateway,
		At: time.Unix(1, 0).UTC(), Payload: []byte(`{"text":"a\u0000b"}`),
		Capture: trajectory.Capture{Recorder: "gateway", Placement: "gateway"},
	}
	if err := trajectory.Sign(&event, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	if string(event.Payload) != `{"text":"a�b"}` {
		t.Fatalf("signed payload=%s", event.Payload)
	}
	if report := trajectory.VerifyChain([]trajectory.Event{event}, []byte(secret)); report.Status != trajectory.StatusVerified {
		t.Fatalf("verified=%+v", report)
	}
}

func chain(t *testing.T) []trajectory.Event {
	t.Helper()
	events := make([]trajectory.Event, 0, 3)
	previous := ""
	for index := 0; index < 3; index++ {
		event := trajectory.Event{
			ID: trajectory.NewID("evt"), TrajectoryID: "trj_1", ProjectID: "project_1", Seq: int64(index + 1),
			Kind: trajectory.KindObservation, Boundary: trajectory.BoundaryIngress, Actor: trajectory.ActorGateway,
			At: time.Unix(1000+int64(index), 0).UTC(), Payload: trajectory.Marshal(map[string]int{"n": index}), PrevHash: previous,
			Capture: trajectory.Capture{Recorder: "gateway", Placement: "gateway"},
		}
		if err := trajectory.Sign(&event, []byte(secret)); err != nil {
			t.Fatal(err)
		}
		previous = event.Hash
		events = append(events, event)
	}
	return events
}
