package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// run walks the candidate list until one produces a response. Each candidate is admitted by its
// circuit breaker immediately before use and every admission is answered with exactly one verdict.
func (state *call) run() {
	var last *protocol.Error
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
		last = failure("upstream_failure", protocol.ClassNoChannel, "no_available_channel", "No channel could serve the request.", http.StatusServiceUnavailable)
	}
	state.finish(last, "failed")
}

type outcome struct {
	done    bool
	failure *protocol.Error
}

// try runs every key of one candidate. It settles the breaker exactly once.
func (state *call) try(channel *entity.Channel, admission routing.Admission) outcome {
	selector, candidate := state.pipeline.selector, admission.Candidate
	var last *protocol.Error
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
		state.recorder.Event(auditevent.KindAttempt, map[string]any{"channel_id": channel.ID, "channel_name": channel.Name, "upstream_model": candidate.Target.UpstreamModel, "key_id": key.ID})
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
		state.recorder.Event(auditevent.KindAttemptFailed, map[string]any{"code": failure.Code, "class": failure.Class, "channel_id": channel.ID, "key_id": key.ID})
		if failure.Class != protocol.ClassAuth && failure.Class != protocol.ClassRateLimit {
			break
		}
	}
	state.judge(admission, last)
	return outcome{failure: last}
}

// judge reports an attempt's outcome to the breaker: only upstream health problems count against it.
func (state *call) judge(admission routing.Admission, failure *protocol.Error) {
	selector := state.pipeline.selector
	if failure != nil && failure.Class == protocol.ClassTransient {
		selector.RecordFailure(admission)
		return
	}
	selector.RecordNeutral(admission)
}

type attemptResult int

const (
	attemptFailed attemptResult = iota
	attemptEnded
	attemptCompleted
)

// attempt opens one upstream response and drives it to the end.
func (state *call) attempt(channel *entity.Channel, candidate routing.Candidate, key routing.Key) (attemptResult, *protocol.Error) {
	ctx, cancel := attemptContext(state.ctx, channel)
	defer cancel()
	upstream, failure := state.pipeline.openUpstream(ctx, state, channel, candidate, key)
	if failure != nil {
		if failure.Class == protocol.ClassCancelled {
			state.finish(failure, "cancelled")
			return attemptEnded, failure
		}
		return attemptFailed, failure
	}
	defer upstream.close()
	record := state.recorder.Call()
	record.ChannelID, record.UpstreamModel, record.Currency = channel.ID, candidate.Target.UpstreamModel, channel.Currency

	state.targetFormat = protocol.Format(channel.Format)
	sink := newResponseSink(state)
	sink.losses = append(sink.losses, upstream.losses...)
	if sink.streaming && !state.headersSent {
		state.headersSent = true
		header := state.writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("X-Accel-Buffering", "no")
		state.writer.WriteHeader(http.StatusOK)
		if state.kind != protocol.Gemini {
			_, _ = state.writer.Write([]byte(": keepalive\n\n"))
		}
		state.writer.Flush()
	}
	if failure := state.consume(upstream, sink); failure != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && state.ctx.Err() == nil {
			failure = timedOut()
		}
		return state.afterFailure(sink, upstream, failure)
	}
	response := sink.response()
	switch {
	case len(response.Items) == 0 && response.Failure == nil && response.Finish.Kind != protocol.FinishContentFilter:
		return attemptFailed, transient("empty_response", "The upstream returned no content.")
	case response.Failure != nil && !sink.committed():
		return attemptFailed, transient("upstream_reported_failure", "The upstream reported a failed response.")
	case response.Failure != nil:
		state.sealFailed(sink, upstream, failureOf(response.Failure))
		return attemptEnded, response.Failure
	}
	if state.complete(sink, upstream, response) {
		return attemptCompleted, nil
	}
	return attemptEnded, nil
}

func attemptContext(parent context.Context, channel *entity.Channel) (context.Context, context.CancelFunc) {
	if channel.TimeoutSeconds > 0 {
		return context.WithTimeout(parent, time.Duration(channel.TimeoutSeconds)*time.Second)
	}
	return context.WithCancel(parent)
}

func failureOf(failure *protocol.Error) *protocol.Error {
	if failure.Class == "" {
		failure.Class = protocol.ClassTransient
	}
	return failure
}

// afterFailure decides what a failed attempt means. Before the first byte reaches the client it is
// retryable; afterwards the call ends here with a protocol-native error frame.
func (state *call) afterFailure(sink *responseSink, upstream *upstreamCall, failure *protocol.Error) (attemptResult, *protocol.Error) {
	switch {
	case failure.Class == protocol.ClassPolicy:
		sink.abort(failure)
		state.recordBlocked(auditevent.Decision{Action: "block", Blocked: true, Reason: failure.Code})
		state.finishWith(sink, upstream, "blocked", failure)
		return attemptEnded, failure
	case failure.Class == protocol.ClassCancelled:
		state.finishWith(sink, upstream, "cancelled", failure)
		return attemptEnded, failure
	case sink.committed():
		sink.abort(failure)
		state.finishWith(sink, upstream, "failed", failure)
		return attemptEnded, failure
	}
	return attemptFailed, failure
}

func (state *call) sealFailed(sink *responseSink, upstream *upstreamCall, failure *protocol.Error) {
	sink.abort(failure)
	state.finishWith(sink, upstream, "failed", failure)
}

// consume feeds every upstream frame through the sink.
func (state *call) consume(upstream *upstreamCall, sink *responseSink) *protocol.Error {
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
		sink.losses = append(sink.losses, losses...)
		if err != nil {
			return upstreamDecodeFailure(err)
		}
		if failure := sink.forward(events, &frame); failure != nil {
			return failure
		}
	}
	trailing, losses, err := upstream.decoder.End()
	sink.losses = append(sink.losses, losses...)
	if errors.Is(err, format.ErrTruncated) {
		return transient("upstream_stream_truncated", "The upstream stream ended before completion.")
	}
	if err != nil {
		return upstreamDecodeFailure(err)
	}
	return sink.forward(trailing, nil)
}
