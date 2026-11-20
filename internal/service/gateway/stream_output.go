package gateway

import (
	"bytes"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
)

// encode hands one event to the client encoder. Content makes the response final unless it is still
// held for a policy verdict.
func (sink *responseSink) encode(event protocol.Event, index int) *protocol.Error {
	held := sink.willHold(index)
	if event.IsContent() && !held {
		sink.recordFirstToken()
	}
	if !sink.streaming {
		return nil
	}
	if index < 0 && !sink.started {
		sink.started = true
	}
	if !sink.started && event.Kind != protocol.EventMessageStart {
		if failure := sink.encode(protocol.Event{Kind: protocol.EventMessageStart, Meta: &sink.meta}, -1); failure != nil {
			return failure
		}
	}
	frames, losses, err := sink.encoder.Encode(event)
	sink.losses = append(sink.losses, losses...)
	if err != nil {
		return internal("encode_failed", "The response could not be encoded.")
	}
	if event.IsContent() && !held {
		sink.commit()
	}
	for _, frame := range frames {
		encoded, err := frame.Encode()
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

func (sink *responseSink) writeRaw(frame format.Frame, content bool) *protocol.Error {
	if sink.state.kind == protocol.Responses {
		if root, err := protocol.DecodeObject(frame.Data); err == nil {
			if number, ok := protocol.Integer(root["sequence_number"]); ok && number > sink.rawSequence {
				sink.rawSequence = number
			}
		}
	}
	encoded, err := frame.Encode()
	if err != nil {
		return internal("encode_failed", "The response could not be encoded.")
	}
	sink.rawFrames++
	sink.started = true
	if content {
		sink.recordFirstToken()
		sink.commit()
	}
	return sink.pass(encoded)
}

func (sink *responseSink) willHold(index int) bool {
	if !sink.streaming || index < 0 {
		return false
	}
	state := sink.items[index]
	return state != nil && state.tool && !state.decided
}

// segment is a run of frames waiting behind a held item. A held segment belongs to one item; any
// other frames queue after it so the client still receives everything in order.
type segment struct {
	index   int
	held    bool
	decided bool
	counted bool
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
	counted := held || sink.heldQueued()
	if counted {
		if limit := sink.state.pipeline.settings.ToolHoldBytes; limit > 0 && sink.queued+len(encoded) > limit {
			return failure("tool_call_too_large", protocol.ClassPolicy, "tool_call_too_large", "The tool call exceeded the buffering limit before the policy verdict.", http.StatusRequestEntityTooLarge)
		}
		sink.queued += len(encoded)
		target.counted = true
	}
	target.frames.Write(encoded)
	return nil
}

// decide releases a judged item and every queued frame that no longer waits on a verdict.
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
		if head.counted {
			sink.queued = max(sink.queued-head.frames.Len(), 0)
		}
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

func (sink *responseSink) heldQueued() bool {
	for _, candidate := range sink.queue {
		if candidate.held {
			return true
		}
	}
	return false
}

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

func (sink *responseSink) commit() {
	if sink.live || !sink.streaming {
		return
	}
	sink.live = true
	if !sink.state.headersSent {
		sink.state.headersSent = true
		header := sink.state.writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("X-Accel-Buffering", "no")
		sink.state.writer.WriteHeader(http.StatusOK)
	}
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

// stageComplete encodes the terminal frames before the call is sealed, so their conversion losses
// are part of the audit record. The bytes are written after the seal. A nil payload means the
// terminal encoding failed; the caller must fail the call instead of completing it.
func (sink *responseSink) stageComplete(response *protocol.Response) ([]byte, *protocol.Error) {
	if sink.rawClosed || sink.stageClosed {
		return nil, nil
	}
	sink.stageClosed = true
	if !sink.streaming {
		if sink.prepared != nil {
			return sink.prepared, nil
		}
		payload, losses, err := sink.state.codec.EncodeResponse(response, sink.state.peer)
		if err != nil {
			return nil, sink.stageError()
		}
		sink.prepared = payload
		sink.losses = append(sink.losses, losses...)
		sink.done = true
		return payload, nil
	}
	var buffer bytes.Buffer
	events := []protocol.Event{
		{Kind: protocol.EventMessageDelta, Usage: response.Usage, Finish: &response.Finish, Failure: response.Failure},
		{Kind: protocol.EventMessageStop},
	}
	for _, event := range events {
		frames, losses, err := sink.encoder.Encode(event)
		sink.losses = append(sink.losses, losses...)
		if err != nil {
			return nil, sink.stageError()
		}
		for _, frame := range frames {
			encoded, err := frame.Encode()
			if err != nil {
				return nil, sink.stageError()
			}
			buffer.Write(encoded)
		}
	}
	trailing, losses, err := sink.encoder.Finish()
	sink.losses = append(sink.losses, losses...)
	if err != nil {
		return nil, sink.stageError()
	}
	for _, frame := range trailing {
		encoded, err := frame.Encode()
		if err != nil {
			return nil, sink.stageError()
		}
		buffer.Write(encoded)
	}
	return buffer.Bytes(), nil
}

func (sink *responseSink) stageError() *protocol.Error {
	sink.stageClosed = false
	return internal("encode_failed", "The response could not be encoded.")
}

// stageFailure encodes a protocol-native terminal error before the seal and returns its bytes.
func (sink *responseSink) stageFailure(failure *protocol.Error) []byte {
	if !sink.streaming || !sink.live || sink.stageClosed {
		return nil
	}
	sink.stageClosed = true
	frames, losses, err := sink.encoder.Fail(failure)
	sink.losses = append(sink.losses, losses...)
	if err != nil {
		return nil
	}
	var buffer bytes.Buffer
	for _, frame := range frames {
		encoded, err := frame.Encode()
		if err != nil {
			return nil
		}
		buffer.Write(encoded)
	}
	return buffer.Bytes()
}

// writeStaged releases terminal bytes after the call has been sealed.
func (sink *responseSink) writeStaged(payload []byte) {
	if len(payload) == 0 {
		return
	}
	if sink.streaming {
		if !sink.live {
			sink.commit()
		}
		_ = sink.send(payload)
		return
	}
	sink.state.writer.Header().Set("Content-Type", "application/json")
	sink.state.writer.WriteHeader(http.StatusOK)
	_, _ = sink.state.writer.Write(payload)
}

// abort stages an in-band error for a committed stream; an uncommitted attempt writes nothing.
func (sink *responseSink) abort(failure *protocol.Error) {
	if payload := sink.stageFailure(failure); len(payload) > 0 {
		sink.stagedFailure = payload
	}
}

func (sink *responseSink) writeStagedFailure() {
	if len(sink.stagedFailure) > 0 {
		_ = sink.send(sink.stagedFailure)
		sink.stagedFailure = nil
	}
}
