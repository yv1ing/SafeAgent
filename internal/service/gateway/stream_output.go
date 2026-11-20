package gateway

import (
	"bytes"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"

	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
)

// emit hands one event to the client encoder. Content makes the response final unless it is still
// held for a policy verdict.
func (sink *responseSink) emit(event protocol.Event, index int) *protocol.Error {
	held := sink.willHold(index)
	if isContent(event) && !held {
		sink.recordFirstToken()
	}
	if !sink.streaming {
		return nil
	}
	if !sink.started {
		sink.started = true
		if failure := sink.write(protocol.Event{Kind: protocol.EventMessageStart, Meta: &sink.meta}, -1); failure != nil {
			return failure
		}
	}
	if isContent(event) && !held {
		sink.commit()
	}
	return sink.write(event, index)
}

func (sink *responseSink) willHold(index int) bool {
	if !sink.streaming || index < 0 {
		return false
	}
	state := sink.blocks[index]
	if state == nil {
		return false
	}
	return state.toolCall && !state.decided
}

func (sink *responseSink) write(event protocol.Event, index int) *protocol.Error {
	frames, losses, err := sink.encoder.Encode(event)
	if err != nil {
		return internal("encode_failed", "The response could not be encoded.")
	}
	sink.losses = append(sink.losses, losses...)
	held := sink.willHold(index)
	for _, frame := range frames {
		encoded, err := format.EncodeFrame(frame.Event, string(frame.Data))
		if err != nil {
			return internal("encode_failed", "The response could not be encoded.")
		}
		if held || len(sink.queue) > 0 {
			if failure := sink.enqueue(index, held, encoded); failure != nil {
				return failure
			}
			continue
		}
		if failure := sink.pass(encoded); failure != nil {
			return failure
		}
	}
	return nil
}

// segment is a run of frames waiting behind a held block. A held segment belongs to one block; any
// other frames queue after it so the client still receives everything in order.
type segment struct {
	index   int
	held    bool
	decided bool
	frames  bytes.Buffer
}

func (sink *responseSink) enqueue(index int, held bool, encoded []byte) *protocol.Error {
	var target *segment
	for _, candidate := range sink.queue {
		if held && candidate.held && candidate.index == index {
			target = candidate
		}
	}
	if target == nil && !held && len(sink.queue) > 0 && !sink.queue[len(sink.queue)-1].held {
		target = sink.queue[len(sink.queue)-1]
	}
	if target == nil {
		target = &segment{index: index, held: held}
		sink.queue = append(sink.queue, target)
	}
	if limit := sink.state.pipeline.settings.ToolHoldBytes; limit > 0 && sink.queuedBytes+len(encoded) > limit {
		return failure("tool_call_too_large", protocol.ClassPolicy, "tool_call_too_large",
			"The tool call exceeded the buffering limit before the policy verdict.", http.StatusRequestEntityTooLarge)
	}
	sink.queuedBytes += len(encoded)
	target.frames.Write(encoded)
	return nil
}

// decide releases a judged block and every queued frame that no longer waits on a verdict.
func (sink *responseSink) decide(index int) *protocol.Error {
	for _, candidate := range sink.queue {
		if candidate.held && candidate.index == index {
			candidate.decided = true
		}
	}
	for len(sink.queue) > 0 {
		head := sink.queue[0]
		if head.held && !head.decided {
			return nil
		}
		sink.queue = sink.queue[1:]
		sink.queuedBytes = max(sink.queuedBytes-head.frames.Len(), 0)
		if head.held {
			sink.recordFirstToken()
			sink.commit()
		}
		if failure := sink.pass(head.frames.Bytes()); failure != nil {
			return failure
		}
	}
	return nil
}

// pass buffers frames until the response is committed and writes them straight through afterwards.
func (sink *responseSink) pass(encoded []byte) *protocol.Error {
	if !sink.live {
		sink.buffer.Write(encoded)
		if sink.buffer.Len() >= preCommitBytes {
			sink.commit()
		}
		return nil
	}
	return sink.send(encoded)
}

func (sink *responseSink) send(payload []byte) *protocol.Error {
	if _, err := sink.state.writer.Write(payload); err != nil {
		return cancelled()
	}
	sink.state.writer.Flush()
	return nil
}

// commit writes the response head and everything buffered so far; from here on a failure can no
// longer be hidden behind another channel.
func (sink *responseSink) commit() {
	if sink.live || !sink.streaming {
		return
	}
	sink.live = true
	header := sink.state.writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no")
	sink.state.writer.WriteHeader(http.StatusOK)
	if sink.buffer.Len() > 0 {
		_, _ = sink.state.writer.Write(sink.buffer.Bytes())
		sink.buffer.Reset()
	}
	sink.state.writer.Flush()
}

func (sink *responseSink) committed() bool { return sink.live }

func (sink *responseSink) recordFirstToken() {
	call := sink.state.recorder.Call()
	if call.FirstTokenMilliseconds > 0 {
		return
	}
	elapsed := max(int(time.Since(sink.state.started).Milliseconds()), 1)
	call.FirstTokenMilliseconds = elapsed
	sink.state.recorder.Event(auditevent.KindFirstToken, map[string]any{"milliseconds": elapsed})
}

func isContent(event protocol.Event) bool {
	switch event.Kind {
	case protocol.EventBlockDelta:
		return event.Text != ""
	case protocol.EventBlockStart:
		return event.Block != nil && event.Block.Kind == protocol.KindToolCall
	}
	return false
}

// prepare encodes a non-streaming body before the call is sealed, so conversion losses from the
// client-facing format are part of the audit record. Streaming losses are collected as frames are
// encoded.
func (sink *responseSink) prepare(response *protocol.Response) error {
	if sink.streaming || sink.prepared != nil {
		return nil
	}
	payload, losses, err := sink.state.codec.EncodeResponse(response, sink.state.peer)
	if err != nil {
		return err
	}
	sink.prepared = payload
	sink.losses = append(sink.losses, losses...)
	return nil
}

// complete tells the client the response is over. For a stream that is the closing frames; for a
// plain call it is the whole body.
func (sink *responseSink) complete(response *protocol.Response) {
	if sink.done {
		return
	}
	sink.done = true
	if !sink.streaming {
		payload := sink.prepared
		if payload == nil {
			encoded, losses, err := sink.state.codec.EncodeResponse(response, sink.state.peer)
			if err != nil {
				writeError(sink.state.writer, sink.state.pipeline.codecs, sink.state.kind, internal("encode_failed", "The response could not be encoded."))
				return
			}
			payload = encoded
			sink.losses = append(sink.losses, losses...)
		}
		if limit := sink.state.pipeline.settings.MaxResponseBytes; limit > 0 && int64(len(payload)) > limit {
			writeError(sink.state.writer, sink.state.pipeline.codecs, sink.state.kind,
				failure("upstream_failure", protocol.ClassTransient, "response_too_large", "The upstream response exceeded the size limit.", http.StatusBadGateway))
			return
		}
		sink.state.writer.Header().Set("Content-Type", "application/json")
		sink.state.writer.WriteHeader(http.StatusOK)
		_, _ = sink.state.writer.Write(payload)
		return
	}
	sink.commit()
	closing := []protocol.Event{
		{Kind: protocol.EventMessageDelta, Usage: response.Usage, Finish: response.Finish, Stop: response.StopSequence, Failure: response.Failure},
		{Kind: protocol.EventMessageStop},
	}
	for _, event := range closing {
		if failure := sink.write(event, -1); failure != nil {
			return
		}
	}
	trailing, losses, err := sink.encoder.Finish()
	if err != nil {
		return
	}
	sink.losses = append(sink.losses, losses...)
	for _, frame := range trailing {
		encoded, err := format.EncodeFrame(frame.Event, string(frame.Data))
		if err != nil || sink.send(encoded) != nil {
			return
		}
	}
}

// abort ends a committed stream with an in-band error; nothing is written for an uncommitted one,
// whose caller answers with a normal error response or tries another channel.
func (sink *responseSink) abort(failure *protocol.Error) {
	if !sink.streaming || sink.done || !sink.live {
		return
	}
	sink.done = true
	_, body := sink.state.codec.EncodeError(failure, true)
	if encoded, err := format.EncodeFrame(streamErrorEvent(sink.state.kind), string(body)); err == nil {
		_ = sink.send(encoded)
	}
}

func streamErrorEvent(kind protocol.Format) string {
	if kind == protocol.Messages || kind == protocol.Responses {
		return "error"
	}
	return ""
}
