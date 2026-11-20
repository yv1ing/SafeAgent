package testkit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
)

func fixtureRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "protocol", "format", "testdata", "axonhub")
}

// FixtureBytes reads a protocol fixture by its path below testdata/axonhub.
func FixtureBytes(t testing.TB, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(fixtureRoot(), name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return content
}

// FixtureNames lists the fixtures of one directory whose names end with suffix.
func FixtureNames(t testing.TB, directory, suffix string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fixtureRoot(), directory))
	if err != nil {
		t.Fatalf("list fixtures %s: %v", directory, err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), suffix) {
			names = append(names, directory+"/"+entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

// FixtureFrames reads a recorded stream: one JSON envelope per line holding the SSE event name and
// data of a frame.
func FixtureFrames(t testing.TB, name string) []format.Frame {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(FixtureBytes(t, name)))
	scanner.Buffer(make([]byte, 1<<20), 8<<20)
	var frames []format.Frame
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		envelope, err := protocol.DecodeObject([]byte(line))
		if err != nil {
			t.Fatalf("fixture %s: %v", name, err)
		}
		data := protocol.String(envelope["Data"])
		if data == "" {
			continue
		}
		frames = append(frames, format.Frame{Event: protocol.String(envelope["Type"]), Data: []byte(data)})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return frames
}

// SealingEnvelope is a signature envelope with a fixed secret.
type SealingEnvelope struct{ Secret []byte }

func (envelope SealingEnvelope) Seal(signature protocol.Signature) (string, error) {
	return protocol.SealSignature(envelope.Secret, signature)
}

func (envelope SealingEnvelope) Open(encoded string) (protocol.Signature, bool) {
	return protocol.OpenSignature(envelope.Secret, encoded)
}

// DecodeFrames feeds frames through a decoder and returns every event it produced.
func DecodeFrames(t testing.TB, decoder format.StreamDecoder, frames []format.Frame) []protocol.Event {
	t.Helper()
	var events []protocol.Event
	for _, frame := range frames {
		produced, err := decoder.Feed(frame)
		if err != nil {
			t.Fatalf("decode frame: %v (%s)", err, frame.Data)
		}
		events = append(events, produced...)
	}
	trailing, err := decoder.End()
	if err != nil {
		t.Fatalf("decode end: %v", err)
	}
	return append(events, trailing...)
}

// EncodeEvents feeds events through an encoder and returns its frames and losses.
func EncodeEvents(t testing.TB, encoder format.StreamEncoder, events []protocol.Event) ([]format.Frame, []protocol.Loss) {
	t.Helper()
	var frames []format.Frame
	var losses []protocol.Loss
	for _, event := range events {
		produced, nested, err := encoder.Encode(event)
		if err != nil {
			t.Fatalf("encode %s: %v", event.Kind, err)
		}
		frames, losses = append(frames, produced...), append(losses, nested...)
	}
	trailing, nested, err := encoder.Finish()
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	return append(frames, trailing...), append(losses, nested...)
}

// EventsOf replays a response as the event sequence a decoder would have produced.
func EventsOf(response *protocol.Response) []protocol.Event {
	events := []protocol.Event{{Kind: protocol.EventMessageStart, Meta: &protocol.ResponseMeta{ID: response.ID, Model: response.Model}}}
	for index, block := range response.Blocks {
		started := protocol.WithStreamedContent(block, "")
		stopped := protocol.CloneBlock(block)
		delta := protocol.StreamedContent(block)
		events = append(events, protocol.Event{Kind: protocol.EventBlockStart, Index: index, Block: &started})
		if delta != "" {
			events = append(events, protocol.Event{Kind: protocol.EventBlockDelta, Index: index, Text: delta})
		}
		events = append(events, protocol.Event{Kind: protocol.EventBlockStop, Index: index, Block: &stopped})
	}
	return append(events,
		protocol.Event{Kind: protocol.EventMessageDelta, Usage: response.Usage, Finish: response.Finish, Stop: response.StopSequence, Failure: response.Failure},
		protocol.Event{Kind: protocol.EventMessageStop})
}

// Aggregate folds a stream back into the response it describes. Streamed content comes only from
// deltas; CheckEventContract verifies that stop snapshots agree with it.
func Aggregate(events []protocol.Event) *protocol.Response {
	response := &protocol.Response{}
	blocks := map[int]*protocol.Block{}
	for _, event := range events {
		switch event.Kind {
		case protocol.EventMessageStart:
			if event.Meta != nil {
				response.ResponseMeta = *event.Meta
			}
		case protocol.EventBlockStart:
			if event.Block != nil {
				block := protocol.CloneBlock(*event.Block)
				blocks[event.Index] = &block
			}
		case protocol.EventBlockDelta:
			block := blocks[event.Index]
			if block == nil {
				continue
			}
			if block.Kind == protocol.KindToolCall && block.ToolCall != nil {
				block.ToolCall.Arguments += event.Text
			} else {
				block.Text += event.Text
			}
		case protocol.EventBlockStop:
			if block := blocks[event.Index]; block != nil && event.Block != nil {
				content := protocol.StreamedContent(*block)
				*block = protocol.WithStreamedContent(*event.Block, content)
			}
		case protocol.EventMessageDelta:
			response.Usage, response.Finish, response.Failure = event.Usage, event.Finish, event.Failure
			if event.Stop != "" {
				response.StopSequence = event.Stop
			}
		}
	}
	indexes := make([]int, 0, len(blocks))
	for index := range blocks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		response.Blocks = append(response.Blocks, *blocks[index])
	}
	return response
}

// CheckEventContract fails unless every block is started once without streamed content, receives
// deltas only while open and is stopped exactly once, in order, before the next block starts, with
// a snapshot whose streamed content equals its deltas.
func CheckEventContract(t testing.TB, events []protocol.Event) {
	t.Helper()
	open, started, stopped := -1, map[int]bool{}, map[int]bool{}
	streamed := map[int]string{}
	for position, event := range events {
		switch event.Kind {
		case protocol.EventBlockStart:
			if open >= 0 {
				t.Fatalf("event %d: block %d started while block %d is open", position, event.Index, open)
			}
			if started[event.Index] {
				t.Fatalf("event %d: block %d started twice", position, event.Index)
			}
			started[event.Index], open = true, event.Index
			if event.Block != nil && protocol.StreamedContent(*event.Block) != "" {
				t.Fatalf("event %d: block %d starts with streamed content", position, event.Index)
			}
		case protocol.EventBlockDelta:
			if open != event.Index {
				t.Fatalf("event %d: delta for block %d while %d is open", position, event.Index, open)
			}
			streamed[event.Index] += event.Text
		case protocol.EventBlockStop:
			if stopped[event.Index] {
				t.Fatalf("event %d: block %d stopped twice", position, event.Index)
			}
			if !started[event.Index] {
				t.Fatalf("event %d: block %d stopped before it started", position, event.Index)
			}
			stopped[event.Index] = true
			if open == event.Index {
				open = -1
			}
			if event.Block != nil && protocol.StreamedContent(*event.Block) != streamed[event.Index] {
				t.Fatalf("event %d: block %d stops with content that differs from its deltas", position, event.Index)
			}
		}
	}
	for index := range started {
		if !stopped[index] {
			t.Fatalf("block %d was never stopped", index)
		}
	}
}

// JSONValue returns the value at a dotted path such as "choices.0.message.content".
func JSONValue(t testing.TB, body []byte, path string) any {
	t.Helper()
	value, err := protocol.DecodeJSON(body)
	if err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, body)
	}
	for _, step := range strings.Split(path, ".") {
		switch typed := value.(type) {
		case map[string]any:
			value = typed[step]
		case []any:
			index, err := strconv.Atoi(step)
			if err != nil || index < 0 || index >= len(typed) {
				return nil
			}
			value = typed[index]
		default:
			return nil
		}
	}
	return value
}

// AssertJSON fails unless the value at path equals want after JSON normalization.
func AssertJSON(t testing.TB, body []byte, path string, want any) {
	t.Helper()
	got := JSONValue(t, body, path)
	if !jsonEqual(got, want) {
		t.Fatalf("%s: want %s, got %s\nbody: %s", path, protocol.Marshal(want), protocol.Marshal(got), body)
	}
}

func jsonEqual(a, b any) bool {
	var left, right any
	_ = json.Unmarshal(protocol.Marshal(a), &left)
	_ = json.Unmarshal(protocol.Marshal(b), &right)
	return reflect.DeepEqual(left, right)
}

// AssertNoNull fails if any array of the document holds a null element, the shape providers reject.
func AssertNoNull(t testing.TB, body []byte) {
	t.Helper()
	value, err := protocol.DecodeJSON(body)
	if err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, body)
	}
	var walk func(path string, value any)
	walk = func(path string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				walk(path+"."+key, child)
			}
		case []any:
			for index, child := range typed {
				if child == nil {
					t.Fatalf("%s[%d] is null\nbody: %s", path, index, body)
				}
				walk(path+"["+strconv.Itoa(index)+"]", child)
			}
		}
	}
	walk("$", value)
}

// Essence reduces a request to what must survive any conversion: the ordered text, tool calls and
// tool results. Roles collapse to system, user and assistant, and adjacent messages of one role
// join, because targets merge them. Text entries are prefixed "text:", call arguments are left out
// unless withArguments is set.
func Essence(request *protocol.Request, withArguments bool) []string {
	var essence []string
	var run []protocol.Block
	var runRole protocol.Role
	flush := func() {
		essence = append(essence, blockEssence(run, string(runRole), withArguments)...)
		run = nil
	}
	for _, message := range request.Messages {
		if role := collapseRole(message.Role); role != runRole {
			flush()
			runRole = role
		}
		run = append(run, message.Blocks...)
	}
	flush()
	return essence
}

// ResponseEssence is Essence for a response.
func ResponseEssence(response *protocol.Response, withArguments bool) []string {
	return blockEssence(response.Blocks, "assistant", withArguments)
}

func collapseRole(role protocol.Role) protocol.Role {
	if role == protocol.RoleDeveloper {
		return protocol.RoleSystem
	}
	if role == protocol.RoleTool {
		return protocol.RoleUser
	}
	return role
}

func blockEssence(blocks []protocol.Block, role string, withArguments bool) []string {
	var essence []string
	text := ""
	flush := func() {
		if text != "" {
			essence = append(essence, "text:"+role+":"+text)
			text = ""
		}
	}
	for _, block := range blocks {
		switch block.Kind {
		case protocol.KindText:
			text += block.Text
			continue
		case protocol.KindToolCall:
			flush()
			call := block.ToolCall
			if call == nil || call.Kind == protocol.ToolServer {
				continue
			}
			entry := "call " + call.ID + " " + call.Name
			if withArguments && call.Kind == protocol.ToolFunction {
				entry += " " + normalizedJSON(call.Arguments)
			}
			essence = append(essence, entry)
		case protocol.KindToolResult:
			flush()
			if block.ToolResult != nil {
				var result string
				for _, nested := range block.ToolResult.Content {
					result += nested.Text
				}
				essence = append(essence, "result "+block.ToolResult.CallID+" "+result)
			}
		}
	}
	flush()
	return essence
}

// normalizedJSON makes argument strings comparable regardless of key order and whitespace.
func normalizedJSON(text string) string {
	decoded, err := protocol.DecodeJSON([]byte(text))
	if err != nil {
		return text
	}
	return string(protocol.Marshal(decoded))
}

// Subsequence reports whether every element of want appears in got in the same order. A text entry
// is satisfied by any later text of the same role that contains it, since targets may fold
// reasoning or neighbouring blocks into it.
func Subsequence(want, got []string) (missing string, ok bool) {
	position := 0
	for _, entry := range want {
		for position < len(got) && !matches(entry, got[position]) {
			position++
		}
		if position == len(got) {
			return entry, false
		}
		position++
	}
	return "", true
}

func matches(want, got string) bool {
	if strings.HasPrefix(want, "text:") {
		role, text, _ := strings.Cut(strings.TrimPrefix(want, "text:"), ":")
		return strings.HasPrefix(got, "text:"+role+":") && strings.Contains(got, text)
	}
	return want == got
}

// JSONMissing lists the leaves of want that got does not reproduce. Objects in got may carry more
// keys; keys named in ignore are skipped wherever they occur, and a string in want matches a
// single-element text array in got, the two forms Messages accepts for the same content.
func JSONMissing(want, got []byte, ignore ...string) []string {
	var left, right any
	if json.Unmarshal(want, &left) != nil || json.Unmarshal(got, &right) != nil {
		return []string{"$: not JSON"}
	}
	skipped := make(map[string]bool, len(ignore))
	for _, key := range ignore {
		skipped[key] = true
	}
	var missing []string
	var walk func(path string, want, got any)
	walk = func(path string, want, got any) {
		switch expected := want.(type) {
		case map[string]any:
			actual, ok := got.(map[string]any)
			if !ok {
				missing = append(missing, path+": not an object")
				return
			}
			for key, value := range expected {
				if skipped[key] || value == nil {
					continue
				}
				child, present := actual[key]
				if !present {
					missing = append(missing, path+"."+key+": absent")
					continue
				}
				walk(path+"."+key, value, child)
			}
		case []any:
			actual, ok := got.([]any)
			if !ok || len(actual) != len(expected) {
				missing = append(missing, path+": array length differs")
				return
			}
			for index := range expected {
				walk(path+"["+strconv.Itoa(index)+"]", expected[index], actual[index])
			}
		case string:
			if text, isText := got.(string); isText && text == expected {
				return
			}
			if parts, isArray := got.([]any); isArray && len(parts) == 1 {
				if part, isObject := parts[0].(map[string]any); isObject && part["text"] == expected {
					return
				}
			}
			missing = append(missing, path+": text differs")
		default:
			if !reflect.DeepEqual(want, got) {
				missing = append(missing, path+": value differs")
			}
		}
	}
	walk("$", left, right)
	return missing
}
