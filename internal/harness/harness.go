// Package harness is the state-transfer SDK used by an agent harness. It submits typed events to a
// writer; it never signs a checkpoint and never writes projections.
package harness

import (
	"context"
	"time"

	"github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type Writer interface {
	AppendHarnessEvents(ctx context.Context, batch trajectory.Batch) error
}

type Recorder struct {
	writer       Writer
	trajectoryID string
	projectID    string
	principal    string
	capture      trajectory.Capture
}

func New(writer Writer, trajectoryID, projectID, principal string, capture trajectory.Capture) *Recorder {
	if capture.Recorder == "" {
		capture.Recorder = "harness"
	}
	if capture.Placement == "" {
		capture.Placement = "harness"
	}
	return &Recorder{writer: writer, trajectoryID: trajectoryID, projectID: projectID, principal: principal, capture: capture}
}

func (recorder *Recorder) Compact(ctx context.Context, payload trajectory.ContextCompact) error {
	payload.RetainedConstraints = nonNil(payload.RetainedConstraints)
	payload.DroppedConstraints = nonNil(payload.DroppedConstraints)
	return recorder.event(ctx, trajectory.KindContextCompact, trajectory.BoundaryContext, payload)
}

func (recorder *Recorder) MemoryWrite(ctx context.Context, payload trajectory.MemoryWrite) error {
	return recorder.event(ctx, trajectory.KindMemoryWrite, trajectory.BoundaryMemory, payload)
}

func (recorder *Recorder) MemoryRead(ctx context.Context, payload trajectory.MemoryRead) error {
	return recorder.event(ctx, trajectory.KindMemoryRead, trajectory.BoundaryMemory, payload)
}

func (recorder *Recorder) MemoryConsolidate(ctx context.Context, payload trajectory.MemoryConsolidate) error {
	return recorder.event(ctx, trajectory.KindMemoryConsolidate, trajectory.BoundaryMemory, payload)
}

func (recorder *Recorder) Rollback(ctx context.Context, payload trajectory.RollbackPayload) error {
	return recorder.event(ctx, trajectory.KindRollback, trajectory.BoundaryRecovery, payload)
}

func (recorder *Recorder) DelegationSpawn(ctx context.Context, payload trajectory.DelegationSpawn) error {
	return recorder.event(ctx, trajectory.KindDelegationSpawn, trajectory.BoundaryAction, payload)
}

func (recorder *Recorder) DelegationReturn(ctx context.Context, payload trajectory.DelegationReturn) error {
	return recorder.event(ctx, trajectory.KindDelegationReturn, trajectory.BoundaryRecovery, payload)
}

func (recorder *Recorder) ToolRegistry(ctx context.Context, payload trajectory.ToolRegistryChange) error {
	return recorder.event(ctx, trajectory.KindToolRegistryChange, trajectory.BoundaryControl, payload)
}

func (recorder *Recorder) Heartbeat(ctx context.Context, payload trajectory.CaptureHeartbeat) error {
	if payload.Recorder == "" {
		payload.Recorder = recorder.capture.Recorder
	}
	recorder.capture.Sequence = payload.Sequence
	return recorder.event(ctx, trajectory.KindCaptureHeartbeat, trajectory.BoundaryControl, payload)
}

func (recorder *Recorder) Event(ctx context.Context, kind trajectory.Kind, boundary trajectory.Boundary, payload any) error {
	return recorder.event(ctx, kind, boundary, payload)
}

func (recorder *Recorder) event(ctx context.Context, kind trajectory.Kind, boundary trajectory.Boundary, payload any) error {
	return recorder.append(ctx, trajectory.Draft{
		Kind: kind, Boundary: boundary, Actor: trajectory.ActorHarness,
		Principal: recorder.principal, At: time.Now().UTC(), Capture: recorder.capture, Payload: payload,
	})
}

func (recorder *Recorder) append(ctx context.Context, draft trajectory.Draft) error {
	return recorder.writer.AppendHarnessEvents(ctx, trajectory.Batch{
		TrajectoryID: recorder.trajectoryID, ProjectID: recorder.projectID,
		Events: []trajectory.Draft{draft},
	})
}

func LabelsFor(state *state.State, segments []string) []trajectory.LabelRef {
	labels := make([]trajectory.LabelRef, 0, len(segments))
	for _, id := range segments {
		label := state.Label(id)
		labels = append(labels, trajectory.LabelRef{Segment: id, Integrity: label.Integrity, Confidentiality: label.Confidentiality})
	}
	return labels
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
