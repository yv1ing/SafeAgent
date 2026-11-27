package harness_test

import (
	"context"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/harness"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type writer struct{ batches []trajectory.Batch }

func (writer *writer) AppendHarnessEvents(_ context.Context, batch trajectory.Batch) error {
	writer.batches = append(writer.batches, batch)
	return nil
}

func TestRecorderEmitsStateTransfers(t *testing.T) {
	sink := &writer{}
	recorder := harness.New(sink, "trj_1", "project_1", "key_1", trajectory.Capture{Recorder: "test", Placement: "harness"})
	if err := recorder.Compact(context.Background(), trajectory.ContextCompact{Before: "a", After: "b", DroppedConstraints: []string{"c"}}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.MemoryWrite(context.Background(), trajectory.MemoryWrite{EntryID: "m", Authority: trajectory.IntegrityTrusted}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Rollback(context.Background(), trajectory.RollbackPayload{Snapshot: trajectory.Snapshot{Cache: "c"}}); err != nil {
		t.Fatal(err)
	}
	if len(sink.batches) != 3 {
		t.Fatalf("batches=%d", len(sink.batches))
	}
	if sink.batches[0].Events[0].Kind != trajectory.KindContextCompact {
		t.Fatalf("kind=%s", sink.batches[0].Events[0].Kind)
	}
}
