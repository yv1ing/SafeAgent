package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// EventWriter is the persistence a recorder needs.
type EventWriter interface {
	AppendEvent(event *entity.Event) error
	CreateCall(ctx context.Context, call *entity.Call) error
	SealCall(ctx context.Context, call *entity.Call, state, code string, toolCallIDs []string, analysis entity.SealAnalysis) error
	AbandonCall(callID string)
	FailCall(ctx context.Context, callID, code string) error
}

const CodeAuditFailed = "audit_failed"

type Recorder struct {
	writer       EventWriter
	logger       *slog.Logger
	call         *entity.Call
	toolCallIDs  []string
	toolCallSet  map[string]bool
	sequence     int64
	analysis     entity.SealAnalysis
	writeFailure error
}

func NewRecorder(writer EventWriter, logger *slog.Logger, call *entity.Call) *Recorder {
	return &Recorder{writer: writer, logger: logger, call: call, toolCallSet: map[string]bool{}}
}

func (recorder *Recorder) Call() *entity.Call { return recorder.call }

func (recorder *Recorder) Open(ctx context.Context) error {
	return recorder.writer.CreateCall(ctx, recorder.call)
}

func (recorder *Recorder) Event(kind string, payload any) {
	recorder.sequence++
	event := &entity.Event{
		CallID: recorder.call.ID, Sequence: recorder.sequence, ProjectID: recorder.call.ProjectID,
		Kind: kind, SchemaVersion: auditevent.SchemaVersion, Payload: protocol.Marshal(payload), CreatedAt: time.Now().UTC(),
	}
	if err := recorder.writer.AppendEvent(event); err != nil {
		recorder.logger.Error("audit event could not be queued", "call", recorder.call.ID, "kind", kind, "error", err)
		if recorder.writeFailure == nil {
			recorder.writeFailure = err
		}
	}
}

func (recorder *Recorder) ToolCall(payload auditevent.ToolCall) {
	if payload.ID == "" || recorder.toolCallSet[payload.ID] {
		return
	}
	recorder.toolCallSet[payload.ID] = true
	recorder.toolCallIDs = append(recorder.toolCallIDs, payload.ID)
	recorder.Event(auditevent.KindToolCall, payload)
}

// SetAnalysis attaches the decisions, snapshot and outbox job written with the seal.
func (recorder *Recorder) SetAnalysis(analysis entity.SealAnalysis) { recorder.analysis = analysis }

func (recorder *Recorder) Seal(ctx context.Context, state, code string) error {
	if recorder.writeFailure != nil {
		recorder.writer.AbandonCall(recorder.call.ID)
		marking, cancel := context.WithTimeout(context.WithoutCancel(ctx), failMarkTimeout)
		defer cancel()
		_ = recorder.writer.FailCall(marking, recorder.call.ID, CodeAuditFailed)
		return recorder.writeFailure
	}
	recorder.Event(auditevent.KindTerminal, auditevent.Terminal{State: state, Code: code})
	if recorder.writeFailure != nil {
		recorder.writer.AbandonCall(recorder.call.ID)
		marking, cancel := context.WithTimeout(context.WithoutCancel(ctx), failMarkTimeout)
		defer cancel()
		_ = recorder.writer.FailCall(marking, recorder.call.ID, CodeAuditFailed)
		return recorder.writeFailure
	}
	err := recorder.writer.SealCall(ctx, recorder.call, state, code, recorder.toolCallIDs, recorder.analysis)
	if err == nil {
		return nil
	}
	recorder.writer.AbandonCall(recorder.call.ID)
	marking, cancel := context.WithTimeout(context.WithoutCancel(ctx), failMarkTimeout)
	defer cancel()
	if markErr := recorder.writer.FailCall(marking, recorder.call.ID, CodeAuditFailed); markErr != nil {
		recorder.logger.Error("unsealed call could not be marked failed", "call", recorder.call.ID, "error", markErr)
	}
	return err
}

const failMarkTimeout = 5 * time.Second
