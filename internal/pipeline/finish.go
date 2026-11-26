package pipeline

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// finish ends a call for which no response exists: nothing was attempted, or every attempt failed
// before the client saw a byte.
func (state *call) finish(failure *model.Error) {
	record := state.recorder.Call()
	record.Losses = convertLosses(mergeLosses(state.failureLedger.Losses, nil))
	record.Repairs = convertRepairs(mergeRepairs(state.failureLedger.Repairs, nil))
	state.applyLossHeaders(nil)
	state.settle(failure, nil, nil)
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
			state.settle(failure, nil, nil)
		}
	}
	writeError(writer, pipeline.codecs, kind, failure)
}

// settle records the final accounting, stores the online run and asynchronous outbox entry in the
// seal transaction, and moves the call to its terminal state. The state and outcome pair comes from
// the one mapping function, so every exit path writes the same semantics.
func (state *call) settle(failure *model.Error, usage *model.Usage, modelEntry *entity.Model) bool {
	record := state.recorder.Call()
	settled := settlementFor(failure, state.lane)
	record.Outcome = settled.outcome
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
	if settled.state == "cancelled" {
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
	if err := state.recorder.Seal(ctx, settled.state, code); err != nil {
		state.pipeline.logger.Error("call could not be sealed", "call", record.ID, "state", settled.state, "error", err)
		return false
	}
	return true
}

// attachAnalysis records the decisions and the redaction snapshot in the seal transaction. Only a
// main call queues the asynchronous diagnosis job; a side or auxiliary call keeps the snapshot so a
// replay shows exactly how it was redacted, but is never analyzed.
func (state *call) attachAnalysis() {
	record := state.recorder.Call()
	seal := sealAnalysis(record, state.lane, state.identity.Project, state.redactionRules, state.decisions)
	state.recorder.SetAnalysis(seal)
}

// sealAnalysis is the shared seal payload of every lane. The snapshot is written for auxiliary calls
// too: without it, a replay would re-redact a probe with today's rules instead of the ones that
// produced the recorded call.
func sealAnalysis(record *entity.Call, lane classify.Lane, project *entity.Project, rules []redact.Rule, decisions []entity.Decision) entity.SealAnalysis {
	payload, _ := json.Marshal(nonNil(rules))
	now := time.Now().UTC()
	seal := entity.SealAnalysis{
		Decisions: decisions,
		Snapshot: &entity.RedactionSnapshot{
			ID: "snap_" + record.ID, CallID: record.ID, Rules: payload, CreatedAt: now,
		},
		RulesVersion: project.RulesVersion,
		RulesHash:    secure.Digest(string(payload)),
	}
	if !lane.Analyzed() {
		record.AnalysisState = "skipped"
		return seal
	}
	record.AnalysisState = "pending"
	scope, _ := json.Marshal(map[string]any{"call_id": record.ID})
	seal.Job = &entity.AnalysisJob{
		ID: "job_" + secure.Random(12), ProjectID: record.ProjectID, Kind: "analyze_call",
		Scope: scope, State: "pending", MaxAttempts: 5, Priority: 10,
		RunAfter: now, DedupeKey: "call:" + record.ID + ":async", CreatedAt: now, UpdatedAt: now,
	}
	return seal
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
