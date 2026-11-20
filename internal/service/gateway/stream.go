package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observe"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
)

// preCommitBytes bounds how much of a response is held back while a failing attempt could still
// fail over to another channel.
const preCommitBytes = 256 << 10

type blockState struct {
	block     protocol.Block
	restorer  *redact.Restorer
	restored  strings.Builder
	toolCall  bool
	finalized bool
	decided   bool
}

// responseSink restores, analyzes and encodes the upstream events of one attempt. Frames stay
// buffered until the first content delta so that an attempt that fails early can still fail over;
// text and tool calls are held for a bounded window while a blocking policy can still veto them.
type responseSink struct {
	state       *call
	encoder     format.StreamEncoder
	meta        protocol.ResponseMeta
	upstreamID  string
	blocks      map[int]*blockState
	usage       *protocol.Usage
	finish      protocol.FinishReason
	stop        string
	failure     *protocol.Error
	losses      []protocol.Loss
	buffer      bytes.Buffer
	queue       []*segment
	queuedBytes int
	streaming   bool
	started     bool
	live        bool
	done        bool
	prepared    []byte
}

func newResponseSink(state *call) *responseSink {
	sink := &responseSink{
		state: state, blocks: map[int]*blockState{},
		streaming: state.request.Stream,
		meta:      protocol.ResponseMeta{ID: responseID(state.kind, state.recorder.Call().ID), Model: state.request.Model},
	}
	if sink.streaming {
		sink.encoder = state.codec.NewStreamEncoder(sink.meta, state.peer)
	}
	return sink
}

func (sink *responseSink) forward(event protocol.Event) *protocol.Error {
	switch event.Kind {
	case protocol.EventMessageStart:
		if event.Meta != nil {
			sink.upstreamID = event.Meta.ID
			sink.meta.Created = event.Meta.Created
			sink.meta.SystemFingerprint = event.Meta.SystemFingerprint
			sink.meta.ServiceTier = event.Meta.ServiceTier
		}
	case protocol.EventBlockStart:
		if event.Block == nil {
			return nil
		}
		return sink.startBlock(event.Index, *event.Block)
	case protocol.EventBlockDelta:
		return sink.delta(event.Index, event.Text)
	case protocol.EventBlockStop:
		return sink.stopBlock(event.Index, event.Block)
	case protocol.EventMessageDelta:
		sink.usage, sink.finish, sink.stop, sink.failure = event.Usage, event.Finish, event.Stop, event.Failure
		sink.losses = append(sink.losses, event.Losses...)
	}
	return nil
}

func (sink *responseSink) startBlock(index int, block protocol.Block) *protocol.Error {
	state := &blockState{block: protocol.CloneBlock(block)}
	scope := sink.state.scope
	switch state.block.Kind {
	case protocol.KindToolCall:
		state.toolCall = true
		if state.block.ToolCall != nil && state.block.ToolCall.Kind == protocol.ToolCustom {
			state.restorer = scope.TextRestorer()
		} else {
			state.restorer = scope.JSONRestorer()
		}
		if state.block.ToolCall != nil {
			state.block.ToolCall.Arguments = ""
		}
	case protocol.KindText, protocol.KindReasoning, protocol.KindRefusal:
		state.restorer = scope.TextRestorer()
		state.block.Text = ""
	}
	sink.restoreMeta(&state.block)
	sink.blocks[index] = state
	return sink.emit(protocol.Event{Kind: protocol.EventBlockStart, Index: index, Block: blockPointer(state.block)}, index)
}

func (sink *responseSink) delta(index int, text string) *protocol.Error {
	state := sink.blocks[index]
	if state == nil || text == "" {
		return nil
	}
	if state.restorer == nil {
		state.block.Text += text
		if failure := sink.emit(protocol.Event{Kind: protocol.EventBlockDelta, Index: index, Text: text}, index); failure != nil {
			return failure
		}
		return nil
	}
	restored := state.restorer.Push(text)
	if restored == "" {
		return nil
	}
	if limit := sink.state.pipeline.settings.MaxStreamBytes; limit > 0 && int64(state.restored.Len())+int64(len(restored)) > limit {
		return failure("upstream_failure", protocol.ClassTransient, "response_too_large",
			"The restored response exceeded the stream size limit.", http.StatusBadGateway)
	}
	state.restored.WriteString(restored)
	return sink.emit(protocol.Event{Kind: protocol.EventBlockDelta, Index: index, Text: restored}, index)
}

func (sink *responseSink) stopBlock(index int, final *protocol.Block) *protocol.Error {
	state := sink.blocks[index]
	if state == nil || state.finalized {
		return nil
	}
	state.finalized = true
	if state.restorer != nil {
		if flushed := state.restorer.Flush(); flushed != "" {
			state.restored.WriteString(flushed)
			if failure := sink.emit(protocol.Event{Kind: protocol.EventBlockDelta, Index: index, Text: flushed}, index); failure != nil {
				return failure
			}
		}
	}
	if final != nil {
		snapshot := protocol.CloneBlock(*final)
		sink.restoreMeta(&snapshot)
		protocol.MergeBlock(&state.block, snapshot)
		state.block.Citations, state.block.Extras = snapshot.Citations, snapshot.Extras
	}
	// A provider may put the complete content only in the final snapshot, without deltas. The
	// restorer is authoritative when it saw content; otherwise the merged snapshot is.
	switch {
	case state.toolCall && state.block.ToolCall != nil && state.restored.Len() > 0:
		state.block.ToolCall.Arguments = state.restored.String()
	case state.restorer != nil && state.restored.Len() > 0:
		state.block.Text = state.restored.String()
	}
	stopped := protocol.CloneBlock(state.block)
	if failure := sink.emit(protocol.Event{Kind: protocol.EventBlockStop, Index: index, Block: &stopped}, index); failure != nil {
		return failure
	}
	if state.toolCall && stopped.ToolCall != nil {
		return sink.judgeToolCall(index, state, stopped)
	}
	return nil
}

// judgeToolCall evaluates one complete tool call before its frames are released. The call was never
// executed upstream, so a blocking decision prevents the side effect rather than reporting it.
func (sink *responseSink) judgeToolCall(index int, state *blockState, block protocol.Block) *protocol.Error {
	state.decided = true
	trace := sink.state.baseTrace()
	item, ok := observe.BlockItem(block, "response.blocks["+strconv.Itoa(index)+"]", sink.state.observeOptions())
	if !ok {
		return sink.decide(index)
	}
	observe.AddAssistantItem(trace, item)
	tags := observation.ToolTags{}
	if spec, found := trace.ToolSpecByName(block.ToolCall.Name); found {
		tags = spec.Tags
	}
	if refusal := sink.state.evaluateToolCall(trace, *item.ToolCall, tags); refusal != nil {
		sink.state.recorder.ToolCall(auditevent.ToolCall{
			ID: block.ToolCall.ID, Kind: string(block.ToolCall.Kind), Name: block.ToolCall.Name,
			Arguments: block.ToolCall.Arguments, Blocked: true,
		})
		return refusal
	}
	// A streaming client receives the released call before the attempt ends. Record it now so a
	// later stream failure cannot erase the index that links it to the next tool-result turn. A
	// non-streaming call is recorded by complete, where the attempt is known to have succeeded.
	if sink.streaming {
		sink.state.recorder.ToolCall(auditevent.ToolCall{
			ID: block.ToolCall.ID, Kind: string(block.ToolCall.Kind), Name: block.ToolCall.Name,
			Arguments: block.ToolCall.Arguments,
		})
	}
	return sink.decide(index)
}

// restoreMeta restores placeholders in the parts of a block that arrive whole: citations, provider
// extras and nested tool results.
func (sink *responseSink) restoreMeta(block *protocol.Block) {
	scope := sink.state.scope
	block.Citations = append([]protocol.Citation(nil), block.Citations...)
	for index := range block.Citations {
		block.Citations[index].CitedText = scope.Restore(block.Citations[index].CitedText)
		block.Citations[index].Title = scope.Restore(block.Citations[index].Title)
		block.Citations[index].Extras = sink.restoreExtras(block.Citations[index].Extras)
	}
	block.Extras = sink.restoreExtras(block.Extras)
	if block.ToolResult != nil {
		result := *block.ToolResult
		result.Content = append([]protocol.Block(nil), result.Content...)
		for index := range result.Content {
			nested := &result.Content[index]
			nested.Text = scope.Restore(nested.Text)
			sink.restoreMeta(nested)
		}
		block.ToolResult = &result
	}
}

func (sink *responseSink) restoreExtras(extras protocol.Extras) protocol.Extras {
	if len(extras.Fields) == 0 {
		return extras
	}
	fields := make(map[string]json.RawMessage, len(extras.Fields))
	for name, raw := range extras.Fields {
		restorer := sink.state.scope.JSONRestorer()
		restored := restorer.Push(string(raw)) + restorer.Flush()
		if json.Valid([]byte(restored)) {
			fields[name] = json.RawMessage(restored)
		} else {
			fields[name] = raw
		}
	}
	return protocol.Extras{Format: extras.Format, Fields: fields}
}

func blockPointer(block protocol.Block) *protocol.Block {
	clone := protocol.CloneBlock(block)
	return &clone
}

// response is what the client has been, or is about to be, shown: restored and in block order.
func (sink *responseSink) response() *protocol.Response {
	indexes := make([]int, 0, len(sink.blocks))
	for index := range sink.blocks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	response := &protocol.Response{ResponseMeta: sink.meta, Finish: sink.finish, StopSequence: sink.stop, Failure: sink.failure, Usage: sink.usage}
	for _, index := range indexes {
		state := sink.blocks[index]
		block := protocol.CloneBlock(state.block)
		switch {
		case state.toolCall && block.ToolCall != nil && state.restored.Len() > 0:
			block.ToolCall.Arguments = state.restored.String()
		case state.restorer != nil && state.restored.Len() > 0:
			block.Text = state.restored.String()
		}
		response.Blocks = append(response.Blocks, block)
	}
	return response
}
