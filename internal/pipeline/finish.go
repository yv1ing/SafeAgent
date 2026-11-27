package pipeline

import (
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/threat"
)

// finish ends a call for which no response exists: nothing was attempted, or every attempt failed
// before the client saw a byte.
func (state *call) finish(failure *model.Error) {
	state.applyLossHeaders(nil)
	state.settle(failure, nil, nil)
	state.reply(failure)
}

func (state *call) reply(failure *model.Error) {
	writeError(state.writer, state.pipeline.codecs, state.kind, failure)
}

// refuse answers a call turned away before any upstream was tried and, when it was already
// recorded, settles it as rejected.
func (pipeline *Pipeline) refuse(state *call, kind model.Format, writer Writer, failure *model.Error) {
	if state != nil {
		defer state.release()
		if state.opened {
			state.settle(failure, nil, nil)
		}
	}
	writeError(writer, pipeline.codecs, kind, failure)
}

// settle records the terminal call state and the one model action execution. The trajectory stays
// open for a session; a standalone call ends its trajectory.
func (state *call) settle(failure *model.Error, usage *model.Usage, modelEntry *entity.Model) bool {
	record := state.recorder.Call()
	settled := settlementFor(failure, state.lane)
	record.Outcome = settled.outcome
	record.Attempts = state.attempts
	record.DurationMilliseconds = int(time.Since(state.started).Milliseconds())
	record.State = settled.state
	if modelEntry != nil {
		record.Currency = modelEntry.Currency
		record.CostMicros = cost(modelEntry, usage)
	}
	if usage != nil {
		record.InputTokens, record.OutputTokens = usage.Input, usage.Output
		record.CacheReadTokens, record.CacheWriteTokens, record.ReasoningTokens = usage.CacheRead, usage.CacheWrite, usage.Reasoning
		record.UsageKnown = true
	}
	if state.reserved {
		state.reserved = false
		ctx, cancel := persistenceContext()
		settleErr := state.pipeline.repository.SettleTokens(ctx, state.identity.Project.ID, state.day, state.estimate, tokens(usage))
		if settleErr != nil {
			state.pipeline.logger.Error("quota settlement failed after retries", "call", record.ID, "error", settleErr)
			state.recorder.GateDecision("release", "record", false, false, "quota_failed", "usage_settle_failed", threat.SeverityInfo, "usage settlement failed", nil)
		}
		cancel()
	}
	code := ""
	if failure != nil {
		code = failure.Code
	}
	record.ErrorCode = code
	ctx, cancel := persistenceContext()
	defer cancel()
	losses := mergeLosses(state.failureLedger.Losses, nil)
	repairs := mergeRepairs(state.failureLedger.Repairs, nil)
	err := state.recorder.Finish(ctx, settled.state, string(settled.outcome), code, usage, record.CostMicros, record.Currency,
		record.DurationMilliseconds, record.FirstTokenMilliseconds, losses, repairs, record.ToolCallIDs,
		state.streamCommitted, failure != nil && state.streamCommitted, state.streamTruncated,
		failure != nil && failure.Class == model.ClassPolicy, state.responseSegment)
	if err != nil {
		state.pipeline.logger.Error("trajectory could not be appended", "call", record.ID, "state", settled.state, "error", err)
		return false
	}
	return true
}

func writeError(writer Writer, codecs codec.Registry, kind model.Format, failure *model.Error) {
	status := failure.Status
	body := model.Marshal(map[string]any{"error": map[string]any{"message": failure.Message}})
	if coder, found := codecs.Get(kind); found {
		status, body = coder.EncodeError(failure, false)
	}
	if status == 0 {
		status = http.StatusInternalServerError
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func (pipeline *Pipeline) WriteError(writer Writer, kind model.Format, failure *model.Error) {
	writeError(writer, pipeline.codecs, kind, failure)
}
