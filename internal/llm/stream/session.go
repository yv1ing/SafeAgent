// Package stream drives one canonical response stream from an upstream decoder to a client
// encoder. It owns the commit boundary, tool holds, redaction restoration, keepalive and the raw
// same-format fast path; policy and persistence enter through callbacks.
package stream

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type Writer interface {
	Header() http.Header
	WriteHeader(status int)
	Write(data []byte) (int, error)
	Flush()
}

type Restorer interface {
	Push(text string) string
	Flush() string
}

type Restorers interface {
	Text() Restorer
	JSON() Restorer
}

// Gate judges one complete tool call. The call has not been released to the client yet; a non-nil
// failure drops the call and terminates the stream.
type Gate func(item model.Item) *model.Error

type Options struct {
	Encoder        codec.StreamEncoder
	Writer         Writer
	Journal        *model.Journal
	Restorers      Restorers
	Gate           Gate
	ToolKinds      map[string]model.ToolKind
	Format         model.Format
	IncludeUsage   bool
	ResponseID     string
	ResponseModel  string
	Raw            bool
	Keepalive      time.Duration
	HoldLimit      int
	PreCommitBytes int
	OnFirstToken   func()
}

type passthroughRestorers struct{}

func (passthroughRestorers) Text() Restorer { return passthroughRestorer{} }
func (passthroughRestorers) JSON() Restorer { return passthroughRestorer{} }

type passthroughRestorer struct{}

func (passthroughRestorer) Push(text string) string { return text }
func (passthroughRestorer) Flush() string           { return "" }

type partState struct {
	part     model.Part
	restorer Restorer
	build    bytes.Buffer
}

type itemState struct {
	item     model.Item
	wireKind model.ToolKind
	parts    map[int]*partState
	order    []int
	tool     bool
	judged   bool
}

type segment struct {
	index  int
	held   bool
	frames bytes.Buffer
}

type Session struct {
	encoder        codec.StreamEncoder
	writer         Writer
	journal        *model.Journal
	restorers      Restorers
	gate           Gate
	toolKinds      map[string]model.ToolKind
	format         model.Format
	includeUsage   bool
	responseID     string
	responseModel  string
	raw            bool
	rawItems       int
	rawParts       int
	rawClosed      bool
	items          map[int]*itemState
	order          []int
	queue          []*segment
	queued         int
	holdLimit      int
	preCommit      int
	keepalive      time.Duration
	onFirstToken   func()
	buffer         bytes.Buffer
	committed      bool
	terminalStaged bool
	staged         []byte
	stagedFailure  []byte
	stopped        atomic.Bool
	mu             sync.Mutex
	stopKeepalive  chan struct{}
	keepaliveOnce  sync.Once
}

func New(options Options) *Session {
	if options.Encoder == nil || options.Writer == nil {
		panic("stream: encoder and writer are required")
	}
	if options.Journal == nil {
		options.Journal = model.NewJournal()
	}
	if options.Restorers == nil {
		options.Restorers = passthroughRestorers{}
	}
	session := &Session{
		encoder: options.Encoder, writer: options.Writer, journal: options.Journal,
		restorers: options.Restorers, gate: options.Gate, toolKinds: options.ToolKinds, format: options.Format, includeUsage: options.IncludeUsage,
		responseID: options.ResponseID, responseModel: options.ResponseModel, raw: options.Raw,
		items: map[int]*itemState{}, holdLimit: options.HoldLimit, preCommit: options.PreCommitBytes,
		keepalive: options.Keepalive, onFirstToken: options.OnFirstToken,
		stopKeepalive: make(chan struct{}),
	}
	if session.preCommit <= 0 {
		session.preCommit = 256 << 10
	}
	if session.holdLimit <= 0 {
		session.holdLimit = 1 << 20
	}
	go session.keepaliveLoop()
	return session
}

func (session *Session) Committed() bool { return session.committed }

func (session *Session) Journal() *model.Journal { return session.journal }

// Handle applies one upstream frame. frame is the raw SSE frame when raw pass-through may apply;
// events are the decoded shadow events for the same frame.
func (session *Session) Handle(events []model.Event, frame *codec.Frame) *model.Error {
	if session.stopped.Load() {
		return nil
	}
	if session.raw {
		if len(events) == 0 {
			if frame != nil && usageOnly(*frame) {
				if !session.includeUsage {
					return nil
				}
				encoded, err := frame.Encode()
				if err != nil {
					return internalFailure()
				}
				return session.pass(encoded, false)
			}
			session.switchCanonical()
			return nil
		}
		if session.rawSafe(events) {
			return session.forwardRaw(events, frame)
		}
		session.switchCanonical()
	}
	for index := range events {
		session.adoptMeta(&events[index])
	}
	for _, event := range events {
		if failure := session.emit(event); failure != nil {
			return failure
		}
	}
	return nil
}

func (session *Session) adoptMeta(event *model.Event) {
	if event.Meta == nil {
		return
	}
	meta := *event.Meta
	meta.ID = session.responseID
	meta.Model = session.responseModel
	event.Meta = &meta
}

func (session *Session) rawSafe(events []model.Event) bool {
	for _, event := range events {
		switch event.Kind {
		case model.EventMessageStart:
			if event.Meta != nil && event.Meta.ID != "" && !model.CompatibleResponseID(session.format, event.Meta.ID) {
				return false
			}
		case model.EventItemStart:
			if event.Item == nil || event.Item.Kind != model.ItemMessage {
				return false
			}
		case model.EventPartStart, model.EventPartStop:
			if event.Part == nil || !rawPart(event.Part.Kind) {
				return false
			}
		case model.EventPartDelta, model.EventItemStop, model.EventMessageDelta, model.EventMessageStop, model.EventPing:
		default:
			return false
		}
	}
	return true
}

// usageOnly reports a frame that carries nothing but a usage total, which a client that did not ask
// for stream usage must not receive.
func usageOnly(frame codec.Frame) bool {
	root, err := model.DecodeObject(frame.Data)
	if err != nil || root["usage"] == nil {
		return false
	}
	for _, choice := range model.Array(root["choices"]) {
		entry := model.Object(choice)
		if delta := model.Object(entry["delta"]); len(delta) > 0 {
			return false
		}
		if model.String(entry["finish_reason"]) != "" {
			return false
		}
	}
	return true
}

func rawPart(kind model.PartKind) bool {
	switch kind {
	case model.PartText, model.PartRefusal, model.PartReasoningText, model.PartReasoningSummary:
		return true
	}
	return false
}

func (session *Session) forwardRaw(events []model.Event, frame *codec.Frame) *model.Error {
	if frame == nil {
		session.switchCanonical()
		for _, event := range events {
			if failure := session.emit(event); failure != nil {
				return failure
			}
		}
		return nil
	}
	for _, event := range events {
		session.journal.Apply(event)
		switch event.Kind {
		case model.EventItemStart:
			session.rawItems++
		case model.EventPartStart:
			session.rawParts++
		}
	}
	terminal := false
	content := false
	for _, event := range events {
		if event.Kind == model.EventMessageStop {
			terminal = true
		}
		if event.Kind == model.EventPartDelta && event.Delta != "" {
			content = true
		}
	}
	if terminal && !session.journal.Content() {
		return nil
	}
	encoded, err := frame.Encode()
	if err != nil {
		return internalFailure()
	}
	if content {
		session.firstToken()
	}
	if failure := session.pass(encoded, content); failure != nil {
		return failure
	}
	if content {
		session.commit()
	}
	if terminal {
		session.rawClosed = true
		session.stopped.Store(true)
	}
	return nil
}

func (session *Session) switchCanonical() {
	if !session.raw {
		return
	}
	session.raw = false
	if encoder, ok := session.encoder.(codec.Continuable); ok && session.rawItems > 0 {
		encoder.Resume(session.rawItems, session.rawParts)
	}
}

func (session *Session) emit(event model.Event) *model.Error {
	switch event.Kind {
	case model.EventMessageStart:
		if event.Meta != nil {
			session.journal.SetMeta(*event.Meta)
		}
		session.journal.Apply(event)
		return session.encode(event, -1)
	case model.EventItemStart:
		if event.Item == nil {
			return nil
		}
		state := &itemState{item: stripItemContent(*event.Item), parts: map[int]*partState{}}
		state.tool = state.item.Kind == model.ItemToolCall || state.item.Kind == model.ItemServerToolCall
		if state.item.ToolCall != nil {
			state.wireKind = state.item.ToolCall.Kind
			if session.toolKinds[state.item.ToolCall.Name] == model.ToolCustom && (session.format == model.Chat || session.format == model.Responses) {
				state.item.ToolCall.Kind = model.ToolCustom
			}
		}
		session.items[event.ItemIndex] = state
		session.order = append(session.order, event.ItemIndex)
		event.Item = &state.item
		session.journal.Apply(event)
		return session.encode(event, event.ItemIndex)
	case model.EventPartStart:
		state := session.items[event.ItemIndex]
		if state == nil || event.Part == nil {
			return nil
		}
		part := &partState{part: model.WithStreamedContent(*event.Part, "")}
		switch part.part.Kind {
		case model.PartToolArguments:
			if state.wireKind == model.ToolCustom {
				part.restorer = session.restorers.Text()
			} else {
				part.restorer = session.restorers.JSON()
			}
		default:
			if rawPart(part.part.Kind) || part.part.Kind == model.PartRefusal {
				part.restorer = session.restorers.Text()
			}
		}
		state.parts[event.PartIndex] = part
		state.order = append(state.order, event.PartIndex)
		event.Part = &part.part
		session.journal.Apply(event)
		return session.encode(event, event.ItemIndex)
	case model.EventPartDelta:
		state := session.items[event.ItemIndex]
		if state == nil || event.Delta == "" {
			return nil
		}
		part := state.parts[event.PartIndex]
		if part == nil {
			return nil
		}
		restored := event.Delta
		if part.restorer != nil {
			restored = part.restorer.Push(event.Delta)
			if restored == "" {
				return nil
			}
		}
		part.build.WriteString(restored)
		event.Delta = restored
		session.journal.Apply(event)
		return session.encode(event, event.ItemIndex)
	case model.EventPartStop:
		state := session.items[event.ItemIndex]
		if state == nil {
			return nil
		}
		part := state.parts[event.PartIndex]
		if part == nil {
			return nil
		}
		if part.restorer != nil {
			if flushed := part.restorer.Flush(); flushed != "" {
				part.build.WriteString(flushed)
				delta := model.Event{Kind: model.EventPartDelta, ItemIndex: event.ItemIndex, ItemID: event.ItemID, PartIndex: event.PartIndex, PartID: event.PartID, Delta: flushed}
				session.journal.Apply(delta)
				if failure := session.encode(delta, event.ItemIndex); failure != nil {
					return failure
				}
			}
			part.restorer = nil
		}
		if event.Part != nil {
			snapshot := event.Part.Clone()
			session.restorePart(&snapshot)
			model.MergePart(&part.part, snapshot)
		}
		if part.build.Len() > 0 {
			part.part = model.WithStreamedContent(part.part, part.build.String())
		}
		event.Part = &part.part
		session.journal.Apply(event)
		return session.encode(event, event.ItemIndex)
	case model.EventItemStop:
		state := session.items[event.ItemIndex]
		if state == nil {
			return nil
		}
		if event.Item != nil {
			snapshot := event.Item.Clone()
			session.restoreItem(&snapshot)
			model.MergeItem(&state.item, snapshot)
		}
		session.materialize(state)
		session.normalizeTool(&state.item)
		event.Item = &state.item
		if state.tool && !state.judged {
			state.judged = true
			if session.gate != nil {
				if failure := session.judge(event.ItemIndex, state); failure != nil {
					return failure
				}
			}
		}
		session.journal.Apply(event)
		if failure := session.encode(event, event.ItemIndex); failure != nil {
			return failure
		}
		if state.tool {
			return session.release(event.ItemIndex)
		}
		return nil
	case model.EventMessageDelta:
		session.journal.Apply(event)
		return nil
	case model.EventMessageStop:
		session.journal.Apply(event)
		session.stopped.Store(true)
		return nil
	case model.EventError:
		session.journal.Apply(event)
		return nil
	case model.EventPing:
		session.journal.Apply(event)
		return session.encode(event, -1)
	}
	return nil
}

// normalizeTool restores a client custom tool that a target without custom tools received as a
// function taking one string input.
func (session *Session) normalizeTool(item *model.Item) {
	if item.Kind != model.ItemToolCall || item.ToolCall == nil || item.ToolCall.Kind != model.ToolFunction {
		return
	}
	if session.toolKinds[item.ToolCall.Name] != model.ToolCustom {
		return
	}
	if session.format != model.Chat && session.format != model.Responses {
		return
	}
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(item.ToolCall.Arguments), &payload); err != nil || len(payload) != 1 {
		return
	}
	input, ok := payload["input"].(string)
	if !ok {
		return
	}
	item.ToolCall.Kind, item.ToolCall.Arguments = model.ToolCustom, input
}

func (session *Session) materialize(state *itemState) {
	order := append([]int(nil), state.order...)
	sort.Ints(order)
	parts := make([]model.Part, 0, len(order))
	arguments := ""
	for _, index := range order {
		part := state.parts[index]
		if part == nil {
			continue
		}
		if part.part.Kind == model.PartToolArguments {
			arguments = part.build.String()
			continue
		}
		parts = append(parts, part.part)
	}
	state.item.Parts = parts
	if state.tool && state.item.ToolCall != nil {
		if arguments == "" {
			arguments = session.restoreArguments(state.item.ToolCall, state.wireKind)
		}
		state.item.ToolCall.Arguments = arguments
	}
}

func (session *Session) judge(index int, state *itemState) *model.Error {
	if failure := session.gate(state.item); failure != nil {
		session.journal.Remove(index)
		session.discard(index)
		return failure
	}
	return nil
}

func (session *Session) restoreArguments(call *model.ToolCall, wireKind model.ToolKind) string {
	if call == nil {
		return ""
	}
	if wireKind == model.ToolCustom {
		restorer := session.restorers.Text()
		return restorer.Push(call.Arguments) + restorer.Flush()
	}
	restorer := session.restorers.JSON()
	return restorer.Push(call.Arguments) + restorer.Flush()
}

func (session *Session) restorePart(part *model.Part) {
	part.Citations = append([]model.Citation(nil), part.Citations...)
	for index := range part.Citations {
		part.Citations[index].CitedText = session.restoreText(part.Citations[index].CitedText)
		part.Citations[index].Title = session.restoreText(part.Citations[index].Title)
	}
}

func (session *Session) restoreItem(item *model.Item) {
	for index := range item.Parts {
		item.Parts[index].Text = session.restoreText(item.Parts[index].Text)
		session.restorePart(&item.Parts[index])
	}
}

func (session *Session) restoreText(text string) string {
	restorer := session.restorers.Text()
	return restorer.Push(text) + restorer.Flush()
}

func (session *Session) encode(event model.Event, index int) *model.Error {
	held := session.willHold(index)
	if event.IsContent() && !held {
		session.firstToken()
	}
	frames, losses, err := session.encoder.Encode(event)
	session.journal.Apply(model.Event{Losses: losses})
	if err != nil {
		return internalFailure()
	}
	if event.IsContent() && !held {
		if failure := session.passFrames(frames, index, false, true); failure != nil {
			return failure
		}
		session.commit()
		return nil
	}
	return session.passFrames(frames, index, held, false)
}

func (session *Session) willHold(index int) bool {
	if index < 0 {
		return false
	}
	state := session.items[index]
	return state != nil && state.tool && !state.judged
}

func (session *Session) passFrames(frames []codec.Frame, index int, held, content bool) *model.Error {
	for _, frame := range frames {
		encoded, err := frame.Encode()
		if err != nil {
			return internalFailure()
		}
		if held || len(session.queue) > 0 {
			if failure := session.enqueue(index, held, encoded); failure != nil {
				return failure
			}
			continue
		}
		if failure := session.pass(encoded, content); failure != nil {
			return failure
		}
	}
	return nil
}

func (session *Session) enqueue(index int, held bool, encoded []byte) *model.Error {
	var target *segment
	if held {
		for _, segment := range session.queue {
			if segment.held && segment.index == index {
				target = segment
				break
			}
		}
	} else if len(session.queue) > 0 && !session.queue[len(session.queue)-1].held {
		target = session.queue[len(session.queue)-1]
	}
	if target == nil {
		target = &segment{index: index, held: held}
		session.queue = append(session.queue, target)
	}
	if held || session.heldQueued() {
		if session.holdLimit > 0 && session.queued+len(encoded) > session.holdLimit {
			return failure("upstream_failure", model.ClassRejected, "tool_call_too_large", "The tool call exceeded the buffering limit before the policy verdict.", http.StatusRequestEntityTooLarge)
		}
		session.queued += len(encoded)
	}
	target.frames.Write(encoded)
	return nil
}

func (session *Session) heldQueued() bool {
	for _, segment := range session.queue {
		if segment.held {
			return true
		}
	}
	return false
}

// release drops every queued frame of a judged item and flushes what no longer waits on a verdict.
func (session *Session) release(index int) *model.Error {
	for _, segment := range session.queue {
		if segment.held && segment.index == index {
			segment.held = false
		}
	}
	return session.drain()
}

// discard removes every queued frame of a rejected item and keeps the following frames.
func (session *Session) discard(index int) {
	kept := make([]*segment, 0, len(session.queue))
	for _, segment := range session.queue {
		if segment.held && segment.index == index {
			if session.queued >= segment.frames.Len() {
				session.queued -= segment.frames.Len()
			}
			continue
		}
		kept = append(kept, segment)
	}
	session.queue = kept
}

func (session *Session) drain() *model.Error {
	for len(session.queue) > 0 {
		head := session.queue[0]
		if head.held {
			return nil
		}
		session.queue = session.queue[1:]
		if session.queued >= head.frames.Len() {
			session.queued -= head.frames.Len()
		}
		if failure := session.pass(head.frames.Bytes(), true); failure != nil {
			return failure
		}
		session.commit()
	}
	return nil
}

func (session *Session) pass(encoded []byte, mayCommit bool) *model.Error {
	if !session.committed {
		session.buffer.Write(encoded)
		if mayCommit && session.buffer.Len() >= session.preCommit {
			session.commit()
		}
		return nil
	}
	return session.send(encoded)
}

func (session *Session) send(encoded []byte) *model.Error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if _, err := session.writer.Write(encoded); err != nil {
		return cancelledFailure()
	}
	session.writer.Flush()
	return nil
}

func (session *Session) commit() {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.committed {
		return
	}
	session.committed = true
	header := session.writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no")
	session.writer.WriteHeader(http.StatusOK)
	if session.buffer.Len() > 0 {
		_, _ = session.writer.Write(session.buffer.Bytes())
		session.buffer.Reset()
	}
	session.writer.Flush()
}

func (session *Session) firstToken() {
	if session.onFirstToken != nil {
		session.onFirstToken()
	}
}

func (session *Session) keepaliveLoop() {
	if session.keepalive <= 0 || session.format == model.Gemini {
		return
	}
	ticker := time.NewTicker(session.keepalive)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			session.mu.Lock()
			if !session.committed || session.stopped.Load() {
				session.mu.Unlock()
				continue
			}
			if session.format == model.Messages {
				_, _ = session.writer.Write([]byte("event: ping\ndata: {\"type\": \"ping\"}\n\n"))
			} else {
				_, _ = session.writer.Write([]byte(": keepalive\n\n"))
			}
			session.writer.Flush()
			session.mu.Unlock()
		case <-session.stopKeepalive:
			return
		}
	}
}

// StageComplete encodes the terminal frames before the call is sealed, so their conversion losses
// are part of the audit record. The bytes are written after the seal.
func (session *Session) StageComplete() error {
	if session.terminalStaged || session.rawClosed {
		return nil
	}
	session.terminalStaged = true
	var buffer bytes.Buffer
	events := []model.Event{{Kind: model.EventMessageDelta, Usage: session.journal.Usage(), Finish: finishOf(session.journal), Failure: session.journal.Failure()}, {Kind: model.EventMessageStop}}
	for _, event := range events {
		frames, losses, err := session.encoder.Encode(event)
		session.journal.Apply(model.Event{Losses: losses})
		if err != nil {
			return err
		}
		for _, frame := range frames {
			encoded, err := frame.Encode()
			if err != nil {
				return err
			}
			buffer.Write(encoded)
		}
	}
	trailing, losses, err := session.encoder.Finish()
	session.journal.Apply(model.Event{Losses: losses})
	if err != nil {
		return err
	}
	for _, frame := range trailing {
		encoded, err := frame.Encode()
		if err != nil {
			return err
		}
		buffer.Write(encoded)
	}
	session.staged = buffer.Bytes()
	return nil
}

func finishOf(journal *model.Journal) *model.Finish {
	finish := journal.Finish()
	return &finish
}

// StageFailure encodes a protocol-native error frame for an already committed stream.
func (session *Session) StageFailure(failure *model.Error) {
	if session.terminalStaged || session.rawClosed || !session.committed {
		return
	}
	session.terminalStaged = true
	frames, losses, err := session.encoder.Fail(failure)
	session.journal.Apply(model.Event{Losses: losses})
	if err != nil {
		return
	}
	var buffer bytes.Buffer
	for _, frame := range frames {
		encoded, err := frame.Encode()
		if err != nil {
			return
		}
		buffer.Write(encoded)
	}
	session.stagedFailure = buffer.Bytes()
}

func (session *Session) WriteStaged() {
	defer session.Close()
	if len(session.staged) > 0 {
		if !session.committed {
			session.commit()
		}
		_ = session.send(session.staged)
		session.staged = nil
	}
	if len(session.stagedFailure) > 0 {
		if !session.committed {
			session.commit()
		}
		_ = session.send(session.stagedFailure)
		session.stagedFailure = nil
	}
}

func (session *Session) Close() {
	session.keepaliveOnce.Do(func() { close(session.stopKeepalive) })
}

func stripItemContent(item model.Item) model.Item {
	clone := item.Clone()
	for index := range clone.Parts {
		clone.Parts[index] = model.WithStreamedContent(clone.Parts[index], "")
	}
	if clone.ToolCall != nil {
		clone.ToolCall.Arguments = ""
	}
	return clone
}

func internalFailure() *model.Error {
	return &model.Error{Kind: "internal", Class: model.ClassInternal, Code: "encode_failed", Message: "The response could not be encoded.", Status: http.StatusInternalServerError}
}

func cancelledFailure() *model.Error {
	return &model.Error{Kind: "timeout", Class: model.ClassCancelled, Code: "client_cancelled", Message: "The client canceled the request.", Status: 499}
}

func failure(kind string, class model.FailureClass, code, message string, status int) *model.Error {
	return &model.Error{Kind: kind, Class: class, Code: code, Message: message, Status: status}
}
