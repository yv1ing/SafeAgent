package pipeline

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/stream"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observe"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// run walks the candidate list until one produces a response. Each candidate is admitted by its
// circuit breaker immediately before use and every admission is answered with exactly one verdict.
func (state *call) run() {
	var last *model.Error
	for _, candidate := range state.candidates {
		if state.attempts >= state.pipeline.settings.MaxAttempts {
			break
		}
		channel := state.available.byID[candidate.Target.ChannelID]
		if channel == nil {
			continue
		}
		admission, admitted := state.pipeline.selector.Admit(candidate)
		if !admitted {
			continue
		}
		if state.ctx.Err() != nil {
			state.pipeline.selector.RecordNeutral(admission)
			state.finish(cancelled(), "cancelled")
			return
		}
		outcome := state.try(channel, admission)
		if outcome.done {
			return
		}
		last = outcome.failure
		if !failsOver(last) {
			break
		}
	}
	if last == nil {
		last = failure("upstream_failure", model.ClassNoChannel, "no_available_channel", "No channel could serve the request.", http.StatusServiceUnavailable)
	}
	if last.Class == model.ClassPolicy {
		state.finish(last, "blocked")
		return
	}
	if last.Class == model.ClassCancelled {
		state.finish(last, "cancelled")
		return
	}
	state.finish(last, "failed")
}

type outcome struct {
	done    bool
	failure *model.Error
}

// try runs every key of one candidate. It settles the breaker exactly once.
func (state *call) try(channel *entity.Channel, admission routing.Admission) outcome {
	selector, candidate := state.pipeline.selector, admission.Candidate
	var last *model.Error
	for _, key := range candidate.Keys {
		if state.attempts >= state.pipeline.settings.MaxAttempts {
			break
		}
		if state.ctx.Err() != nil {
			selector.RecordNeutral(admission)
			state.finish(cancelled(), "cancelled")
			return outcome{done: true}
		}
		state.attempts++
		state.recorder.Call().Attempts = state.attempts
		state.recorder.Event(auditevent.KindAttempt, auditevent.Attempt{
			ChannelID: channel.ID, ChannelName: channel.Name, ModelID: candidate.Target.ModelID,
			UpstreamModel: candidate.Target.UpstreamModel, Format: candidate.Target.Format, KeyID: key.ID,
		})
		result, failure := state.attempt(channel, candidate, key)
		switch result {
		case attemptCompleted:
			selector.RecordSuccess(state.stickyKey, admission)
			return outcome{done: true}
		case attemptEnded:
			state.judge(admission, failure)
			return outcome{done: true}
		}
		last = failure
		state.recorder.Event(auditevent.KindAttemptFailed, auditevent.AttemptFailure{
			Code: failure.Code, Class: string(failure.Class), ChannelID: channel.ID, KeyID: key.ID,
		})
		if failure.Class != model.ClassAuth && failure.Class != model.ClassRateLimit {
			break
		}
	}
	state.judge(admission, last)
	return outcome{failure: last}
}

// judge reports an attempt's outcome to the breaker: only upstream health problems count against it.
func (state *call) judge(admission routing.Admission, failure *model.Error) {
	selector := state.pipeline.selector
	if failure != nil && failure.Class == model.ClassTransient {
		selector.RecordFailure(admission)
		return
	}
	selector.RecordNeutral(admission)
}

type attemptResult int

const preCommitBytes = 256 << 10

const (
	attemptFailed attemptResult = iota
	attemptEnded
	attemptCompleted
)

// attempt prepares and opens one upstream response and drives it to the end.
func (state *call) attempt(channel *entity.Channel, candidate routing.Candidate, key routing.Key) (attemptResult, *model.Error) {
	ctx, cancel := attemptContext(state.ctx, channel)
	defer cancel()
	state.applyLossHeaders(nil)
	prepared, failure := state.prepare(candidate)
	if failure != nil {
		return attemptFailed, failure
	}
	upstream, failure, field := state.openUpstream(ctx, prepared, key)
	if field != "" && upstream == nil {
		if retried, retriedPrepared, retryFailure := state.retryWithoutField(ctx, candidate, key, field); retried != nil {
			upstream, prepared, failure = retried, retriedPrepared, nil
		} else if retryFailure != nil {
			failure = retryFailure
		}
	}
	if failure != nil {
		if failure.Class == model.ClassCancelled {
			state.finish(failure, "cancelled")
			return attemptEnded, failure
		}
		return attemptFailed, failure
	}
	defer upstream.close()
	record := state.recorder.Call()
	record.ChannelID, record.ModelID, record.UpstreamModel = channel.ID, candidate.Target.ModelID, candidate.Target.UpstreamModel
	record.Currency = upstream.endpoint.model.Currency
	record.EndpointFormat = string(upstream.endpoint.format)
	if state.stream {
		state.applyLossHeaders(prepared.losses)
		return state.streamResponse(ctx, upstream, candidate)
	}
	return state.collectResponse(ctx, upstream, candidate)
}

func attemptContext(parent context.Context, channel *entity.Channel) (context.Context, context.CancelFunc) {
	if channel.TimeoutSeconds > 0 {
		return context.WithTimeout(parent, time.Duration(channel.TimeoutSeconds)*time.Second)
	}
	return context.WithCancel(parent)
}

// retryWithoutField drops one request field the upstream rejected and retries the same key once.
func (state *call) retryWithoutField(ctx context.Context, candidate routing.Candidate, key routing.Key, field string) (*upstreamCall, *prepared, *model.Error) {
	endpoint, found := state.endpoints[endpointKey(candidate.Target.ChannelID, candidate.Target.ModelID, model.Format(candidate.Target.Format))]
	if !found {
		return nil, nil, nil
	}
	applyCapabilityOverride(&endpoint.endpoint, field)
	state.endpoints[endpointKey(candidate.Target.ChannelID, candidate.Target.ModelID, model.Format(candidate.Target.Format))] = endpoint
	removed := model.Loss{Path: field, Code: model.LossCapabilityRemoved, Class: model.LossDegraded, Detail: "the upstream rejected this field; it is now omitted for this channel", To: endpoint.format}
	state.failureLedger.Losses = append(state.failureLedger.Losses, removed)
	persist, cancel := persistenceContext()
	if err := state.pipeline.repository.AddCapabilityOverride(persist, endpoint.channel.ID, endpoint.model.ID, string(endpoint.format), field, "rejected by the upstream"); err != nil {
		state.pipeline.logger.Warn("capability override could not be stored", "channel", endpoint.channel.ID, "field", field, "error", err)
	}
	cancel()
	prepared, failure := state.prepare(candidate)
	if failure != nil {
		return nil, nil, failure
	}
	prepared.losses = append(prepared.losses, removed)
	upstream, failure, _ := state.openUpstream(ctx, prepared, key)
	return upstream, prepared, failure
}

func (state *call) streamResponse(ctx context.Context, upstream *upstreamCall, candidate routing.Candidate) (attemptResult, *model.Error) {
	meta := model.ResponseMeta{ID: model.ResponseID(state.kind, state.recorder.Call().ID, ""), Model: state.request.Model}
	session := stream.New(stream.Options{
		Encoder:        state.coder.NewStreamEncoder(meta, state.clientPeer()),
		Writer:         state.writer,
		Journal:        model.NewJournal(),
		Restorers:      scopeRestorers{scope: state.scope},
		Gate:           state.toolGate,
		ToolKinds:      state.toolKinds,
		Format:         state.kind,
		IncludeUsage:   state.request.StreamOptions != nil && state.request.StreamOptions.IncludeUsage,
		ResponseID:     meta.ID,
		ResponseModel:  meta.Model,
		Raw:            upstream.endpoint.format == state.kind && state.kind != model.Responses && !state.historyExpanded && state.rawResponseSafe(),
		Keepalive:      state.pipeline.settings.Keepalive,
		HoldLimit:      state.pipeline.settings.ToolHoldBytes,
		PreCommitBytes: preCommitBytes,
		OnFirstToken:   state.recordFirstToken,
	})
	if upstream.response != nil {
		state.failureLedger.Losses = append(state.failureLedger.Losses, upstream.losses...)
		for _, event := range stream.Events(upstream.response) {
			if failure := session.Handle([]model.Event{event}, nil); failure != nil {
				return state.endSessionFailure(session, upstream, failure)
			}
		}
	} else {
		if failure := state.consume(ctx, upstream, session); failure != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && state.ctx.Err() == nil {
				failure = timedOut()
			}
			return state.endSessionFailure(session, upstream, failure)
		}
	}
	return state.completeStream(session, upstream)
}

func (state *call) rawResponseSafe() bool {
	return len(state.hits) == 0 && !state.unsealed
}

func (state *call) clientPeer() codec.Peer {
	peer := state.peer
	peer.Model = state.request.Model
	peer.IncludeUsage = state.request.StreamOptions != nil && state.request.StreamOptions.IncludeUsage
	return peer
}

func (state *call) consume(ctx context.Context, upstream *upstreamCall, session *stream.Session) *model.Error {
	for {
		frame, err := upstream.reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if state.ctx.Err() != nil {
				return cancelled()
			}
			return transient("upstream_stream_failed", "The upstream stream failed.")
		}
		events, losses, err := upstream.decoder.Feed(frame)
		session.Journal().Apply(model.Event{Losses: losses})
		state.failureLedger.Losses = append(state.failureLedger.Losses, losses...)
		if err != nil {
			return upstreamDecodeFailure(err)
		}
		if failure := session.Handle(events, &frame); failure != nil {
			return failure
		}
		if ctx.Err() != nil && state.ctx.Err() == nil {
			return timedOut()
		}
	}
	trailing, losses, err := upstream.decoder.End()
	state.failureLedger.Losses = append(state.failureLedger.Losses, losses...)
	session.Journal().Apply(model.Event{Losses: losses})
	if errors.Is(err, codec.ErrTruncated) {
		return transient("upstream_stream_truncated", "The upstream stream ended before completion.")
	}
	if err != nil {
		return upstreamDecodeFailure(err)
	}
	return session.Handle(trailing, nil)
}

func (state *call) endSessionFailure(session *stream.Session, upstream *upstreamCall, failure *model.Error) (attemptResult, *model.Error) {
	if !session.Committed() {
		session.Close()
		switch {
		case failure.Class == model.ClassPolicy:
			state.recordBlocked(auditevent.Decision{Action: "block", Blocked: true, Reason: failure.Code})
			state.finish(failure, "blocked")
			return attemptEnded, failure
		case failure.Class == model.ClassCancelled:
			state.finish(failure, "cancelled")
			return attemptEnded, failure
		}
		return attemptFailed, failure
	}
	if failure.Class == model.ClassPolicy {
		state.recordBlocked(auditevent.Decision{Action: "block", Blocked: true, Reason: failure.Code})
		state.failStream(session, upstream, "blocked", failure)
		return attemptEnded, failure
	}
	if failure.Class == model.ClassCancelled {
		state.failStream(session, upstream, "cancelled", failure)
		return attemptEnded, failure
	}
	state.failStream(session, upstream, "failed", failure)
	return attemptEnded, failure
}

func (state *call) toolGate(item model.Item) *model.Error {
	trace := state.baseTrace()
	translated, ok := observe.ItemOf(item, "response.items", state.observeOptions())
	if !ok || translated.ToolCall == nil {
		return nil
	}
	observe.AddAssistantItem(trace, translated)
	tags := observation.ToolTags{}
	if spec, found := trace.ToolSpecByName(item.ToolCall.Name); found {
		tags = spec.Tags
	}
	if refusal := state.evaluateToolCall(trace, *translated.ToolCall, tags); refusal != nil {
		state.recorder.ToolCall(auditevent.ToolCall{
			ID: item.ToolCall.ID, Kind: string(item.ToolCall.Kind), Name: item.ToolCall.Name,
			Arguments: item.ToolCall.Arguments, Blocked: true,
		})
		return refusal
	}
	state.recorder.ToolCall(auditevent.ToolCall{
		ID: item.ToolCall.ID, Kind: string(item.ToolCall.Kind), Name: item.ToolCall.Name,
		Arguments: item.ToolCall.Arguments,
	})
	return nil
}

func (state *call) recordFirstToken() {
	call := state.recorder.Call()
	if call.FirstTokenMilliseconds > 0 {
		return
	}
	elapsed := max(int(time.Since(state.started).Milliseconds()), 1)
	call.FirstTokenMilliseconds = elapsed
	state.recorder.Event(auditevent.KindFirstToken, auditevent.FirstToken{Milliseconds: int64(elapsed)})
}

type scopeRestorers struct {
	scope *redact.Scope
}

func (restorers scopeRestorers) Text() stream.Restorer {
	if restorers.scope == nil {
		return passthroughRestorer{}
	}
	return restorers.scope.TextRestorer()
}

func (restorers scopeRestorers) JSON() stream.Restorer {
	if restorers.scope == nil {
		return passthroughRestorer{}
	}
	return restorers.scope.JSONRestorer()
}

type passthroughRestorer struct{}

func (passthroughRestorer) Push(text string) string { return text }
func (passthroughRestorer) Flush() string           { return "" }
