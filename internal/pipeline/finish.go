package pipeline

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// finish ends a call for which no response exists: nothing was attempted, or every attempt failed
// before the client saw a byte.
func (state *call) finish(failure *model.Error, stateName string) {
	record := state.recorder.Call()
	record.Losses = convertLosses(mergeLosses(state.failureLedger.Losses, nil))
	record.Repairs = convertRepairs(mergeRepairs(state.failureLedger.Repairs, nil))
	state.applyLossHeaders(nil)
	state.settle(stateName, failure, nil, nil)
	state.reply(failure)
}

// reply tells a client whose call ended abnormally. A committed stream was already told in-band;
// everything else gets a normal error response.
func (state *call) reply(failure *model.Error) {
	writeError(state.writer, state.pipeline.codecs, state.kind, failure)
}

// refuse answers a call turned away before any upstream was tried and, when it was already
// recorded, seals it as rejected.
func (pipeline *Pipeline) refuse(state *call, kind model.Format, writer Writer, failure *model.Error) {
	if state != nil {
		defer state.release()
		if state.opened {
			state.settle("rejected", failure, nil, nil)
		}
	}
	writeError(writer, pipeline.codecs, kind, failure)
}

// settle records the final accounting, stores the online run and asynchronous outbox entry in the
// seal transaction, and moves the call to its terminal state.
func (state *call) settle(stateName string, failure *model.Error, usage *model.Usage, modelEntry *entity.Model) bool {
	record := state.recorder.Call()
	record.Attempts = state.attempts
	record.DurationMilliseconds = int(time.Since(state.started).Milliseconds())
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
			state.recorder.Event(auditevent.KindQuotaFailed, auditevent.QuotaFailure{Error: "usage_settle_failed"})
		}
		cancel()
	}
	state.attachAnalysis()
	code := ""
	if failure != nil {
		code = failure.Code
	}
	if stateName == "cancelled" {
		reason := ""
		if failure != nil {
			reason = failure.Message
		}
		state.recorder.Event(auditevent.KindCancellation, auditevent.Cancellation{
			Code: code, Reason: reason, Committed: state.streamCommitted, Truncated: state.streamTruncated,
		})
	}
	ctx, cancel := persistenceContext()
	defer cancel()
	if err := state.recorder.Seal(ctx, stateName, code); err != nil {
		state.pipeline.logger.Error("call could not be sealed", "call", record.ID, "state", stateName, "error", err)
		return false
	}
	return true
}

// attachAnalysis records the online decisions and queues the asynchronous job in the seal
// transaction. Online diagnosis results do not exist.
func (state *call) attachAnalysis() {
	record := state.recorder.Call()
	rules, _ := json.Marshal(state.redactionRules)
	now := time.Now().UTC()
	record.AnalysisState = "pending"
	jobScope, _ := json.Marshal(map[string]any{"call_id": record.ID})
	job := &entity.AnalysisJob{
		ID: "job_" + secure.Random(12), ProjectID: record.ProjectID, Kind: "analyze_call",
		Scope: jobScope, State: "pending", MaxAttempts: 5, Priority: 10,
		RunAfter: now, DedupeKey: "call:" + record.ID + ":async", CreatedAt: now, UpdatedAt: now,
	}
	state.recorder.SetAnalysis(entity.SealAnalysis{
		Decisions: state.decisions,
		Job:       job,
		Snapshot: &entity.RedactionSnapshot{
			ID: "snap_" + record.ID, CallID: record.ID, Rules: rules, CreatedAt: now,
		},
	})
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

// WriteError renders one protocol-shaped failure; the HTTP layer uses it for errors that occur
// before the pipeline is entered.
func (pipeline *Pipeline) WriteError(writer Writer, kind model.Format, failure *model.Error) {
	writeError(writer, pipeline.codecs, kind, failure)
}
