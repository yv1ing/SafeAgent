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

const preCommitBytes = 256 << 10

type partState struct {
	part     protocol.Part
	restorer *redact.Restorer
	restored strings.Builder
}

type itemState struct {
	item    protocol.Item
	parts   map[int]*partState
	order   []int
	tool    bool
	final   bool
	decided bool
}

// responseSink restores, analyzes and encodes the upstream events of one attempt. Frames stay
// buffered until the first content delta so that an attempt that fails early can still fail over;
// tool calls are held until the online policy has judged them.
type responseSink struct {
	state         *call
	encoder       format.StreamEncoder
	meta          protocol.ResponseMeta
	upstreamID    string
	items         map[int]*itemState
	order         []int
	usage         *protocol.Usage
	finish        protocol.Finish
	failure       *protocol.Error
	losses        []protocol.Loss
	buffer        bytes.Buffer
	queue         []*segment
	queued        int
	streaming     bool
	started       bool
	live          bool
	done          bool
	prepared      []byte
	rawMode       bool
	rawClosed     bool
	rawItems      int
	rawParts      int
	rawSequence   int64
	rawFrames     int
	stagedFailure []byte
	stageClosed   bool
}

func newResponseSink(state *call) *responseSink {
	sink := &responseSink{
		state: state, items: map[int]*itemState{},
		streaming: state.request.Stream,
		meta:      protocol.ResponseMeta{ID: protocol.ResponseID(state.kind, state.recorder.Call().ID, ""), Model: state.request.Model},
	}
	sink.rawMode = sink.streaming && state.kind == state.targetFormat && state.kind != protocol.Responses && len(state.hits) == 0 && !state.unsealed && !state.historyExpanded
	if sink.streaming {
		sink.encoder = state.codec.NewStreamEncoder(sink.meta, state.peer)
	}
	return sink
}

func (sink *responseSink) forward(events []protocol.Event, frame *format.Frame) *protocol.Error {
	if sink.done {
		return nil
	}
	if sink.rawMode {
		if frame != nil && sink.rawSafe(events) {
			terminal := rawTerminal(events)
			sink.applyEvents(events)
			if terminal {
				if !sink.hasContent() {
					return nil
				}
				sink.rawClosed, sink.done = true, true
				sink.commit()
			}
			return sink.writeRaw(*frame, hasRawContent(events))
		}
		sink.switchCanonical()
	}
	for _, event := range events {
		if failure := sink.emit(event); failure != nil {
			return failure
		}
	}
	return nil
}

func (sink *responseSink) rawSafe(events []protocol.Event) bool {
	for _, event := range events {
		switch event.Kind {
		case protocol.EventMessageStart:
			if event.Meta != nil && event.Meta.ID != "" && !protocol.CompatibleResponseID(sink.state.kind, event.Meta.ID) {
				return false
			}
		case protocol.EventItemStart:
			if event.Item == nil || event.Item.Kind != protocol.ItemMessage {
				return false
			}
			if sink.state.kind == protocol.Responses && event.Item.ID != "" && !protocol.CompatibleItemID(protocol.Responses, event.Item.Kind, event.Item.ID) {
				return false
			}
		case protocol.EventPartStart, protocol.EventPartStop:
			if event.Part == nil || !rawPart(event.Part.Kind) || event.Part.Kind == protocol.PartReasoningText || event.Part.Kind == protocol.PartReasoningSummary {
				return false
			}
		case protocol.EventPartDelta:
		case protocol.EventItemStop, protocol.EventMessageDelta, protocol.EventMessageStop, protocol.EventPing:
		default:
			return false
		}
	}
	return true
}

func rawPart(kind protocol.PartKind) bool {
	switch kind {
	case protocol.PartText, protocol.PartRefusal, protocol.PartReasoningText, protocol.PartReasoningSummary:
		return true
	}
	return false
}

func hasRawContent(events []protocol.Event) bool {
	for _, event := range events {
		if event.Kind == protocol.EventPartDelta && event.Delta != "" {
			return true
		}
	}
	return false
}

func (sink *responseSink) switchCanonical() {
	if !sink.rawMode {
		return
	}
	sink.rawMode = false
	if encoder, ok := sink.encoder.(format.Continuable); ok && sink.rawFrames > 0 {
		encoder.Resume(sink.rawItems, sink.rawParts, sink.rawSequence+1)
	}
}

// applyEvents updates the audit state when the raw frames go straight to the client.
func (sink *responseSink) applyEvents(events []protocol.Event) {
	for _, event := range events {
		switch event.Kind {
		case protocol.EventMessageStart:
			if event.Meta != nil {
				sink.upstreamID = event.Meta.ID
				sink.meta = protocol.ResponseMeta{
					ID:    protocol.ResponseID(sink.state.kind, sink.state.recorder.Call().ID, event.Meta.ID),
					Model: event.Meta.Model, Created: event.Meta.Created,
					SystemFingerprint: event.Meta.SystemFingerprint, ServiceTier: event.Meta.ServiceTier,
				}
			}
		case protocol.EventItemStart:
			if event.Item == nil {
				continue
			}
			sink.rawItems++
			state := &itemState{item: event.Item.Clone(), parts: map[int]*partState{}}
			sink.items[event.ItemIndex] = state
			sink.order = append(sink.order, event.ItemIndex)
		case protocol.EventPartStart:
			state := sink.items[event.ItemIndex]
			if state == nil || event.Part == nil {
				continue
			}
			sink.rawParts++
			state.parts[event.PartIndex] = &partState{part: event.Part.Clone()}
			state.order = append(state.order, event.PartIndex)
		case protocol.EventPartDelta:
			state := sink.items[event.ItemIndex]
			if state == nil {
				continue
			}
			if part := state.parts[event.PartIndex]; part != nil {
				part.part.Text += event.Delta
			}
		case protocol.EventPartStop:
			state := sink.items[event.ItemIndex]
			if state == nil {
				continue
			}
			if part := state.parts[event.PartIndex]; part != nil && event.Part != nil {
				protocol.MergePart(&part.part, *event.Part)
				if content := protocol.StreamedContent(*event.Part); content != "" && part.part.Text == content {
					part.part.Text = content
				}
			}
		case protocol.EventItemStop:
			state := sink.items[event.ItemIndex]
			if state == nil {
				continue
			}
			if event.Item != nil {
				protocol.MergeItem(&state.item, *event.Item)
			}
			order := append([]int(nil), state.order...)
			sort.Ints(order)
			parts := make([]protocol.Part, 0, len(order))
			for _, index := range order {
				if part := state.parts[index]; part != nil {
					parts = append(parts, part.part)
				}
			}
			state.item.Parts = parts
			state.final = true
		case protocol.EventMessageDelta:
			sink.usage, sink.failure = event.Usage, event.Failure
			if event.Finish != nil {
				sink.finish = *event.Finish
			}
		}
	}
}

func rawTerminal(events []protocol.Event) bool {
	for _, event := range events {
		if event.Kind == protocol.EventMessageStop {
			return true
		}
	}
	return false
}

func (sink *responseSink) hasContent() bool {
	for _, state := range sink.items {
		if state.item.HasText() {
			return true
		}
		if state.item.Reasoning != nil && state.item.Reasoning.Signature != nil {
			return true
		}
		if state.item.ToolCall != nil {
			return true
		}
		for _, part := range state.parts {
			if part.part.Text != "" {
				return true
			}
		}
	}
	return false
}

func (sink *responseSink) emit(event protocol.Event) *protocol.Error {
	switch event.Kind {
	case protocol.EventMessageStart:
		if event.Meta != nil {
			sink.upstreamID = event.Meta.ID
			sink.meta = protocol.ResponseMeta{
				ID:    protocol.ResponseID(sink.state.kind, sink.state.recorder.Call().ID, event.Meta.ID),
				Model: event.Meta.Model, Created: event.Meta.Created,
				SystemFingerprint: event.Meta.SystemFingerprint, ServiceTier: event.Meta.ServiceTier,
			}
		}
		sink.started = true
		meta := sink.meta
		event.Meta = &meta
		return sink.encode(event, -1)
	case protocol.EventItemStart:
		if event.Item == nil {
			return nil
		}
		state := &itemState{item: event.Item.Clone(), parts: map[int]*partState{}}
		state.item = stripItemContent(state.item)
		state.tool = state.item.Kind == protocol.ItemToolCall || state.item.Kind == protocol.ItemServerToolCall
		sink.items[event.ItemIndex] = state
		sink.order = append(sink.order, event.ItemIndex)
		return sink.encode(event, event.ItemIndex)
	case protocol.EventPartStart:
		state := sink.items[event.ItemIndex]
		if state == nil || event.Part == nil {
			return nil
		}
		part := &partState{part: event.Part.Clone()}
		switch part.part.Kind {
		case protocol.PartToolArguments:
			if state.item.ToolCall != nil && state.item.ToolCall.Kind == protocol.ToolCustom {
				part.restorer = sink.state.scope.TextRestorer()
			} else {
				part.restorer = sink.state.scope.JSONRestorer()
			}
		default:
			if rawPart(part.part.Kind) {
				part.restorer = sink.state.scope.TextRestorer()
			}
		}
		part.part = protocol.WithStreamedContent(part.part, "")
		state.parts[event.PartIndex] = part
		state.order = append(state.order, event.PartIndex)
		return sink.encode(event, event.ItemIndex)
	case protocol.EventPartDelta:
		state := sink.items[event.ItemIndex]
		if state == nil || event.Delta == "" {
			return nil
		}
		part := state.parts[event.PartIndex]
		if part == nil {
			return nil
		}
		if part.restorer == nil {
			return sink.encode(event, event.ItemIndex)
		}
		restored := part.restorer.Push(event.Delta)
		if restored == "" {
			return nil
		}
		if limit := sink.state.pipeline.settings.MaxStreamBytes; limit > 0 && int64(part.restored.Len())+int64(len(restored)) > limit {
			return failure("upstream_failure", protocol.ClassTransient, "response_too_large", "The restored response exceeded the stream size limit.", http.StatusBadGateway)
		}
		part.restored.WriteString(restored)
		event.Delta = restored
		return sink.encode(event, event.ItemIndex)
	case protocol.EventPartStop:
		state := sink.items[event.ItemIndex]
		if state == nil {
			return nil
		}
		part := state.parts[event.PartIndex]
		if part == nil {
			return nil
		}
		if part.restorer != nil {
			if flushed := part.restorer.Flush(); flushed != "" {
				part.restored.WriteString(flushed)
				if failure := sink.encode(protocol.Event{Kind: protocol.EventPartDelta, ItemIndex: event.ItemIndex, PartIndex: event.PartIndex, Delta: flushed}, event.ItemIndex); failure != nil {
					return failure
				}
			}
			part.restorer = nil
		}
		if event.Part != nil {
			snapshot := event.Part.Clone()
			sink.restorePart(&snapshot)
			protocol.MergePart(&part.part, snapshot)
		}
		if part.restored.Len() > 0 {
			part.part = protocol.WithStreamedContent(part.part, part.restored.String())
		}
		stopped := part.part.Clone()
		event.Part = &stopped
		return sink.encode(event, event.ItemIndex)
	case protocol.EventItemStop:
		state := sink.items[event.ItemIndex]
		if state == nil {
			return nil
		}
		if event.Item != nil {
			snapshot := event.Item.Clone()
			sink.restoreItem(&snapshot)
			protocol.MergeItem(&state.item, snapshot)
		}
		order := append([]int(nil), state.order...)
		sort.Ints(order)
		parts := make([]protocol.Part, 0, len(order))
		for _, index := range order {
			if part := state.parts[index]; part != nil {
				parts = append(parts, part.part)
			}
		}
		state.item.Parts = parts
		if state.tool && state.item.ToolCall != nil {
			arguments := ""
			for _, part := range state.parts {
				if part.part.Kind == protocol.PartToolArguments && part.restored.Len() > 0 {
					arguments = part.restored.String()
				}
			}
			if arguments == "" {
				arguments = sink.restoreArguments(state.item.ToolCall)
			}
			state.item.ToolCall.Arguments = arguments
		}
		state.final = true
		sink.state.normalizeToolKind(&state.item)
		stopped := state.item.Clone()
		event.Item = &stopped
		if failure := sink.encode(event, event.ItemIndex); failure != nil {
			return failure
		}
		if state.tool {
			return sink.judgeToolCall(event.ItemIndex, state, stopped)
		}
		return nil
	case protocol.EventMessageDelta:
		sink.usage, sink.failure = event.Usage, event.Failure
		if event.Finish != nil {
			sink.finish = *event.Finish
		}
		return nil
	case protocol.EventMessageStop:
		sink.done = true
		return nil
	case protocol.EventError:
		sink.failure = event.Failure
		return nil
	case protocol.EventPing:
		return sink.encode(event, -1)
	}
	return nil
}

// normalizeToolKind restores a client custom tool that a target without custom tools received
// as a function taking one string input.
func (state *call) normalizeToolKind(item *protocol.Item) {
	if item.Kind != protocol.ItemToolCall || item.ToolCall == nil || item.ToolCall.Kind != protocol.ToolFunction {
		return
	}
	if state.toolKinds[item.ToolCall.Name] != protocol.ToolCustom {
		return
	}
	if state.kind != protocol.Chat && state.kind != protocol.Responses {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(item.ToolCall.Arguments), &payload); err != nil || len(payload) != 1 {
		return
	}
	input, ok := payload["input"].(string)
	if !ok {
		return
	}
	item.ToolCall.Kind, item.ToolCall.Arguments = protocol.ToolCustom, input
}

func stripItemContent(item protocol.Item) protocol.Item {
	item = item.Clone()
	for index := range item.Parts {
		item.Parts[index] = protocol.WithStreamedContent(item.Parts[index], "")
	}
	if item.ToolCall != nil {
		item.ToolCall.Arguments = ""
	}
	return item
}

func (sink *responseSink) judgeToolCall(index int, state *itemState, item protocol.Item) *protocol.Error {
	state.decided = true
	trace := sink.state.baseTrace()
	translated, ok := observe.ItemOf(item, "response.items["+strconv.Itoa(index)+"]", sink.state.observeOptions())
	if !ok || translated.ToolCall == nil {
		return sink.decide(index)
	}
	observe.AddAssistantItem(trace, translated)
	tags := observation.ToolTags{}
	if spec, found := trace.ToolSpecByName(item.ToolCall.Name); found {
		tags = spec.Tags
	}
	if refusal := sink.state.evaluateToolCall(trace, *translated.ToolCall, tags); refusal != nil {
		sink.state.recorder.ToolCall(auditevent.ToolCall{
			ID: item.ToolCall.ID, Kind: string(item.ToolCall.Kind), Name: item.ToolCall.Name,
			Arguments: item.ToolCall.Arguments, Blocked: true,
		})
		return refusal
	}
	if sink.streaming {
		sink.state.recorder.ToolCall(auditevent.ToolCall{
			ID: item.ToolCall.ID, Kind: string(item.ToolCall.Kind), Name: item.ToolCall.Name,
			Arguments: item.ToolCall.Arguments,
		})
	}
	return sink.decide(index)
}

func (sink *responseSink) restoreArguments(call *protocol.ToolCall) string {
	if call == nil {
		return ""
	}
	if call.Kind == protocol.ToolCustom {
		return sink.state.scope.Restore(call.Arguments)
	}
	restorer := sink.state.scope.JSONRestorer()
	return restorer.Push(call.Arguments) + restorer.Flush()
}

func (sink *responseSink) restorePart(part *protocol.Part) {
	scope := sink.state.scope
	part.Citations = append([]protocol.Citation(nil), part.Citations...)
	for index := range part.Citations {
		part.Citations[index].CitedText = scope.Restore(part.Citations[index].CitedText)
		part.Citations[index].Title = scope.Restore(part.Citations[index].Title)
		part.Citations[index].Raw = sink.restoreRaw(part.Citations[index].Raw)
	}
	part.Raw = sink.restoreRaw(part.Raw)
}

func (sink *responseSink) restoreItem(item *protocol.Item) {
	scope := sink.state.scope
	for index := range item.Parts {
		item.Parts[index].Text = scope.Restore(item.Parts[index].Text)
		sink.restorePart(&item.Parts[index])
	}
	item.Raw = sink.restoreRaw(item.Raw)
}

func (sink *responseSink) restoreRaw(raw *protocol.Raw) *protocol.Raw {
	if raw == nil || len(raw.Fields) == 0 {
		return raw
	}
	clone := raw.Clone()
	for name, value := range clone.Fields {
		restorer := sink.state.scope.JSONRestorer()
		restored := restorer.Push(string(value)) + restorer.Flush()
		if json.Valid([]byte(restored)) {
			clone.Fields[name] = json.RawMessage(restored)
		}
	}
	return clone
}

func (sink *responseSink) response() *protocol.Response {
	response := &protocol.Response{Source: sink.state.kind, Meta: sink.meta, Finish: sink.finish, Failure: sink.failure, Usage: sink.usage}
	indexes := append([]int(nil), sink.order...)
	sort.Ints(indexes)
	for _, index := range indexes {
		state := sink.items[index]
		if state == nil {
			continue
		}
		item := state.item.Clone()
		order := append([]int(nil), state.order...)
		sort.Ints(order)
		parts := make([]protocol.Part, 0, len(order))
		for _, partIndex := range order {
			if part := state.parts[partIndex]; part != nil {
				clone := part.part.Clone()
				if part.restored.Len() > 0 {
					clone = protocol.WithStreamedContent(clone, part.restored.String())
				}
				parts = append(parts, clone)
			}
		}
		item.Parts = parts
		response.Items = append(response.Items, item)
	}
	return response
}
