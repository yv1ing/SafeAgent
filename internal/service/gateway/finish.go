package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// complete inspects a finished response, settles quota and seals the call. Closing frames are encoded before the seal so their losses are recorded, then written.
func (state *call) complete(sink *responseSink, upstream *upstreamCall, response *protocol.Response) bool {
	record := state.recorder.Call()
	upstreamID := sink.upstreamID
	if upstreamID == "" {
		upstreamID = response.Meta.ID
	}
	response.Meta.ID = protocol.ResponseID(state.kind, record.ID, upstreamID)
	response.Meta.Model = state.request.Model
	payload, stageFailure := sink.stageComplete(response)
	if stageFailure != nil {
		sink.abort(stageFailure)
		state.finishWith(sink, upstream, "failed", stageFailure)
		return false
	}
	if !sink.streaming {
		if limit := state.pipeline.settings.MaxResponseBytes; limit > 0 && int64(len(payload)) > limit {
			state.finishWith(sink, upstream, "failed", failure("upstream_failure", protocol.ClassTransient, "response_too_large", "The upstream response exceeded the size limit.", http.StatusBadGateway))
			return false
		}
	}
	ledger := sink.ledger(upstream)
	ledger.Losses = mergeLosses(state.failureLedger.Losses, ledger.Losses)
	ledger.Repairs = mergeRepairs(state.failureLedger.Repairs, ledger.Repairs)
	record.Losses, record.Repairs = convertLosses(ledger.Losses), convertRepairs(ledger.Repairs)
	state.recordTools(response)
	state.recorder.Event(auditevent.KindResponse, auditevent.Response{Response: *response, Ledger: ledger})
	record.ResponseID = response.Meta.ID
	record.ConversationHash = protocol.ConversationFingerprint(state.pipeline.secret, state.client, response)
	if !state.settle("completed", nil, response.Usage, upstream.channel) {
		failure := internal("audit_unavailable", "The response could not be recorded.")
		if sink.committed() {
			sink.writeStaged(payload)
		} else {
			state.reply(sink, failure)
		}
		return false
	}
	sink.writeStaged(payload)
	return true
}

// recordTools writes one event per tool call the model issued and per tool result this request
// added; calls of earlier turns were recorded by the turns that issued them.
func (state *call) recordTools(response *protocol.Response) {
	for index := range response.Items {
		item := &response.Items[index]
		if item.Kind == protocol.ItemToolCall && item.ToolCall != nil {
			state.recorder.ToolCall(auditevent.ToolCall{
				ID: item.ToolCall.ID, Kind: string(item.ToolCall.Kind), Name: item.ToolCall.Name,
				Arguments: item.ToolCall.Arguments,
			})
		}
	}
	for index := min(state.offset, len(state.request.Items)); index < len(state.request.Items); index++ {
		item := &state.request.Items[index]
		if item.Kind == protocol.ItemToolResult {
			state.recorder.Event(auditevent.KindToolResult, auditevent.ToolResult{CallID: item.CallID, Name: item.Name, IsError: item.IsError})
		}
	}
}

func (sink *responseSink) ledger(upstream *upstreamCall) protocol.Ledger {
	ledger := protocol.Ledger{Losses: append(append([]protocol.Loss{}, upstream.losses...), sink.losses...), Repairs: append([]protocol.Repair{}, upstream.repairs...)}
	return ledger
}

// finishWith ends a call that was attempted but did not complete.
func (state *call) finishWith(sink *responseSink, upstream *upstreamCall, stateName string, failure *protocol.Error) {
	record := state.recorder.Call()
	response := sink.response()
	ledger := sink.ledger(upstream)
	ledger.Losses = mergeLosses(state.failureLedger.Losses, ledger.Losses)
	ledger.Repairs = mergeRepairs(state.failureLedger.Repairs, ledger.Repairs)
	if len(response.Items) > 0 {
		state.recorder.Event(auditevent.KindResponse, auditevent.Response{Response: *response, Ledger: ledger})
	}
	record.Losses, record.Repairs = convertLosses(ledger.Losses), convertRepairs(ledger.Repairs)
	state.settle(stateName, failure, sink.usage, upstream.channel)
	if sink.committed() {
		sink.writeStagedFailure()
		return
	}
	state.reply(sink, failure)
}

// reply tells a client whose call ended abnormally. A committed stream was already told in-band;
// everything else gets a normal error response.
func (state *call) reply(sink *responseSink, failure *protocol.Error) {
	if sink != nil && sink.committed() {
		return
	}
	if state.headersSent {
		writeStreamFailure(state.writer, state.pipeline.codecs, state.kind, failure)
		return
	}
	writeError(state.writer, state.pipeline.codecs, state.kind, failure)
}

// finish ends a call for which no response exists: nothing was attempted, or every attempt failed.
func (state *call) finish(failure *protocol.Error, stateName string) {
	record := state.recorder.Call()
	record.Losses, record.Repairs = convertLosses(state.failureLedger.Losses), convertRepairs(state.failureLedger.Repairs)
	state.settle(stateName, failure, nil, nil)
	state.reply(nil, failure)
}

// refuse answers a call turned away before any upstream was tried and, when it was already
// recorded, seals it as rejected.
func (pipeline *Pipeline) refuse(state *call, kind protocol.Format, writer Writer, failure *protocol.Error) {
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
func (state *call) settle(stateName string, failure *protocol.Error, usage *protocol.Usage, channel *entity.Channel) bool {
	record := state.recorder.Call()
	record.Attempts = state.attempts
	record.DurationMilliseconds = int(time.Since(state.started).Milliseconds())
	if channel != nil {
		record.Currency = channel.Currency
		record.CostMicros = cost(channel, record.UpstreamModel, usage)
	}
	if usage != nil {
		record.InputTokens, record.OutputTokens = usage.Input, usage.Output
		record.CacheReadTokens, record.CacheWriteTokens, record.ReasoningTokens = usage.CacheRead, usage.CacheWrite, usage.Reasoning
		record.UsageKnown = true
	}
	if state.reserved {
		state.reserved = false
		ctx, cancel := persistenceContext()
		var settleErr error
		for attempt := 0; attempt < 3; attempt++ {
			settleErr = state.pipeline.repository.SettleTokens(ctx, state.identity.Project.ID, state.day, state.estimate, tokens(usage))
			if settleErr == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if settleErr != nil {
			state.pipeline.logger.Error("quota settlement failed after retries", "call", record.ID, "error", settleErr)
			state.recorder.Event(auditevent.KindQuotaFailed, map[string]any{"error": "usage_settle_failed"})
		}
		cancel()
	}
	state.attachAnalysis()
	code := ""
	if failure != nil {
		code = failure.Code
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
