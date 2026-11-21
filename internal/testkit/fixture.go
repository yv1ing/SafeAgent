package testkit

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// SealingEnvelope is a signature envelope with a fixed secret.
type SealingEnvelope struct{ Secret []byte }

func (envelope SealingEnvelope) Seal(signature model.Signature) (string, error) {
	return model.SealSignature(envelope.Secret, signature)
}

func (envelope SealingEnvelope) Open(encoded string) (model.Signature, bool) {
	return model.OpenSignature(envelope.Secret, encoded)
}

// DecodeFrames feeds frames through a decoder and returns every event it produced.
func DecodeFrames(t testing.TB, decoder codec.StreamDecoder, frames []codec.Frame) []model.Event {
	t.Helper()
	var events []model.Event
	for _, frame := range frames {
		produced, _, err := decoder.Feed(frame)
		if err != nil {
			t.Fatalf("decode frame: %v (%s)", err, frame.Data)
		}
		events = append(events, produced...)
	}
	trailing, _, err := decoder.End()
	if err != nil {
		t.Fatalf("decode end: %v", err)
	}
	return append(events, trailing...)
}

// EncodeEvents feeds events through an encoder and returns its frames and losses.
func EncodeEvents(t testing.TB, encoder codec.StreamEncoder, events []model.Event) ([]codec.Frame, []model.Loss) {
	t.Helper()
	var frames []codec.Frame
	var losses []model.Loss
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
func EventsOf(response *model.Response) []model.Event {
	events := []model.Event{{Kind: model.EventMessageStart, Meta: &response.Meta}}
	for index, source := range response.Items {
		item := source.Clone()
		parts := item.Parts
		if item.ToolCall != nil {
			hasArguments := false
			for _, part := range parts {
				if part.Kind == model.PartToolArguments {
					hasArguments = true
				}
			}
			if !hasArguments {
				parts = append(append([]model.Part(nil), parts...), model.Part{Kind: model.PartToolArguments, Text: item.ToolCall.Arguments})
				item.ToolCall.Arguments = ""
			}
		}
		started := stripItem(item)
		events = append(events, model.Event{Kind: model.EventItemStart, ItemIndex: index, ItemID: item.ID, Item: &started})
		for partIndex, part := range parts {
			partStarted := model.WithStreamedContent(part, "")
			events = append(events, model.Event{Kind: model.EventPartStart, ItemIndex: index, PartIndex: partIndex, Part: &partStarted})
			if content := model.StreamedContent(part); content != "" {
				events = append(events, model.Event{Kind: model.EventPartDelta, ItemIndex: index, PartIndex: partIndex, Delta: content})
			}
			stopped := part.Clone()
			events = append(events, model.Event{Kind: model.EventPartStop, ItemIndex: index, PartIndex: partIndex, Part: &stopped})
		}
		stopped := item.Clone()
		events = append(events, model.Event{Kind: model.EventItemStop, ItemIndex: index, Item: &stopped})
	}
	return append(events,
		model.Event{Kind: model.EventMessageDelta, Usage: response.Usage, Finish: &response.Finish, Failure: response.Failure},
		model.Event{Kind: model.EventMessageStop})
}

func stripItem(item model.Item) model.Item {
	clone := item.Clone()
	for index := range clone.Parts {
		clone.Parts[index] = model.WithStreamedContent(clone.Parts[index], "")
	}
	if clone.ToolCall != nil {
		clone.ToolCall.Arguments = ""
	}
	return clone
}

// Aggregate folds a stream back into the response it describes. Streamed content comes only from
// deltas; CheckEventContract verifies that stop snapshots agree with it.
func Aggregate(events []model.Event) *model.Response {
	response := &model.Response{}
	type itemState struct {
		item  model.Item
		parts map[int]*model.Part
		text  map[int]*strings.Builder
	}
	items := map[int]*itemState{}
	var order []int
	for _, event := range events {
		switch event.Kind {
		case model.EventMessageStart:
			if event.Meta != nil {
				response.Meta = *event.Meta
			}
		case model.EventItemStart:
			if event.Item == nil {
				continue
			}
			state := &itemState{item: event.Item.Clone(), parts: map[int]*model.Part{}, text: map[int]*strings.Builder{}}
			if _, seen := items[event.ItemIndex]; !seen {
				order = append(order, event.ItemIndex)
			}
			items[event.ItemIndex] = state
		case model.EventPartStart:
			state := items[event.ItemIndex]
			if state == nil || event.Part == nil {
				continue
			}
			part := event.Part.Clone()
			state.parts[event.PartIndex] = &part
			state.text[event.PartIndex] = &strings.Builder{}
		case model.EventPartDelta:
			state := items[event.ItemIndex]
			if state == nil {
				continue
			}
			if builder := state.text[event.PartIndex]; builder != nil {
				builder.WriteString(event.Delta)
			}
		case model.EventPartStop:
			state := items[event.ItemIndex]
			if state == nil || event.Part == nil {
				continue
			}
			part := event.Part.Clone()
			if builder := state.text[event.PartIndex]; builder != nil {
				part = model.WithStreamedContent(part, builder.String())
			}
			state.parts[event.PartIndex] = &part
		case model.EventItemStop:
			state := items[event.ItemIndex]
			if state == nil {
				continue
			}
			if event.Item != nil {
				model.MergeItem(&state.item, *event.Item)
			}
			state.item.Parts = nil
			for partIndex := range state.parts {
				state.item.Parts = append(state.item.Parts, state.parts[partIndex].Clone())
			}
			sort.Slice(state.item.Parts, func(left, right int) bool {
				return state.item.Parts[left].Ordinal < state.item.Parts[right].Ordinal
			})
			if state.item.ToolCall != nil {
				var arguments strings.Builder
				for _, part := range state.parts {
					if part.Kind == model.PartToolArguments {
						arguments.WriteString(part.Text)
					}
				}
				if arguments.Len() > 0 {
					state.item.ToolCall.Arguments = arguments.String()
				}
			}
		case model.EventMessageDelta:
			response.Usage, response.Failure = event.Usage, event.Failure
			if event.Finish != nil {
				response.Finish = *event.Finish
			}
		}
	}
	sort.Ints(order)
	for _, index := range order {
		response.Items = append(response.Items, items[index].item)
	}
	return response
}

// CheckEventContract fails unless every item and part starts once, receives deltas only while open
// and stops exactly once, with a snapshot whose streamed content equals its deltas. Items and parts
// may be open concurrently.
func CheckEventContract(t testing.TB, events []model.Event) {
	t.Helper()
	itemOpen, itemStarted, itemStopped := map[int]bool{}, map[int]bool{}, map[int]bool{}
	partOpen, partStarted, partStopped := map[int]map[int]bool{}, map[int]map[int]bool{}, map[int]map[int]bool{}
	streamed := map[int]map[int]string{}
	stoppedMessage := false
	for position, event := range events {
		if stoppedMessage {
			t.Fatalf("event %d: stream continues after message_stop", position)
		}
		if partOpen[event.ItemIndex] == nil {
			partOpen[event.ItemIndex] = map[int]bool{}
			partStarted[event.ItemIndex] = map[int]bool{}
			partStopped[event.ItemIndex] = map[int]bool{}
			streamed[event.ItemIndex] = map[int]string{}
		}
		switch event.Kind {
		case model.EventItemStart:
			if itemStarted[event.ItemIndex] {
				t.Fatalf("event %d: item %d started twice", position, event.ItemIndex)
			}
			itemStarted[event.ItemIndex], itemOpen[event.ItemIndex] = true, true
			if event.Item != nil {
				for _, part := range event.Item.Parts {
					if model.StreamedContent(part) != "" {
						t.Fatalf("event %d: item %d starts with streamed content", position, event.ItemIndex)
					}
				}
			}
		case model.EventPartStart:
			if !itemOpen[event.ItemIndex] {
				t.Fatalf("event %d: part for closed item %d", position, event.ItemIndex)
			}
			if partStarted[event.ItemIndex][event.PartIndex] {
				t.Fatalf("event %d: part %d/%d started twice", position, event.ItemIndex, event.PartIndex)
			}
			partStarted[event.ItemIndex][event.PartIndex], partOpen[event.ItemIndex][event.PartIndex] = true, true
			if event.Part != nil && model.StreamedContent(*event.Part) != "" {
				t.Fatalf("event %d: part %d/%d starts with streamed content", position, event.ItemIndex, event.PartIndex)
			}
		case model.EventPartDelta:
			if !partOpen[event.ItemIndex][event.PartIndex] {
				t.Fatalf("event %d: delta for closed part %d/%d", position, event.ItemIndex, event.PartIndex)
			}
			streamed[event.ItemIndex][event.PartIndex] += event.Delta
		case model.EventPartStop:
			if partStopped[event.ItemIndex][event.PartIndex] {
				t.Fatalf("event %d: part %d/%d stopped twice", position, event.ItemIndex, event.PartIndex)
			}
			if !partStarted[event.ItemIndex][event.PartIndex] {
				t.Fatalf("event %d: part %d/%d stopped before it started", position, event.ItemIndex, event.PartIndex)
			}
			partStopped[event.ItemIndex][event.PartIndex] = true
			partOpen[event.ItemIndex][event.PartIndex] = false
			if event.Part != nil && model.StreamedContent(*event.Part) != streamed[event.ItemIndex][event.PartIndex] {
				t.Fatalf("event %d: part %d/%d stops with content that differs from its deltas", position, event.ItemIndex, event.PartIndex)
			}
		case model.EventItemStop:
			if itemStopped[event.ItemIndex] {
				t.Fatalf("event %d: item %d stopped twice", position, event.ItemIndex)
			}
			if !itemStarted[event.ItemIndex] {
				t.Fatalf("event %d: item %d stopped before it started", position, event.ItemIndex)
			}
			for partIndex := range partOpen[event.ItemIndex] {
				if partOpen[event.ItemIndex][partIndex] {
					t.Fatalf("event %d: item %d stopped with part %d open", position, event.ItemIndex, partIndex)
				}
			}
			itemStopped[event.ItemIndex], itemOpen[event.ItemIndex] = true, false
		case model.EventMessageStop:
			stoppedMessage = true
		}
	}
	for index := range itemStarted {
		if !itemStopped[index] {
			t.Fatalf("item %d was never stopped", index)
		}
	}
}

// JSONValue returns the value at a dotted path such as "choices.0.message.content".
func JSONValue(t testing.TB, body []byte, path string) any {
	t.Helper()
	value, err := model.DecodeJSON(body)
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
		t.Fatalf("%s: want %s, got %s\nbody: %s", path, model.Marshal(want), model.Marshal(got), body)
	}
}

func jsonEqual(a, b any) bool {
	var left, right any
	_ = json.Unmarshal(model.Marshal(a), &left)
	_ = json.Unmarshal(model.Marshal(b), &right)
	return reflect.DeepEqual(left, right)
}

// Essence reduces a request to what must survive any conversion: the ordered text, tool calls and
// tool results. Roles collapse to system, user and assistant, and adjacent items of one role join,
// because targets merge them. Text entries are prefixed "text:", call arguments are left out unless
// withArguments is set.
func Essence(request *model.Request, withArguments bool) []string {
	var essence []string
	var run []model.Item
	var runRole model.Role
	flush := func() {
		essence = append(essence, itemEssence(run, string(runRole), withArguments)...)
		run = nil
	}
	for _, item := range request.Items {
		role := item.Role
		if item.Kind.IsToolResult() {
			role = model.RoleTool
		}
		if collapsed := collapseRole(role); collapsed != runRole {
			flush()
			runRole = collapsed
		}
		run = append(run, item)
	}
	flush()
	return essence
}

// ResponseEssence is Essence for a response.
func ResponseEssence(response *model.Response, withArguments bool) []string {
	return itemEssence(response.Items, "assistant", withArguments)
}

func collapseRole(role model.Role) model.Role {
	if role == model.RoleDeveloper {
		return model.RoleSystem
	}
	if role == model.RoleTool {
		return model.RoleUser
	}
	return role
}

func itemEssence(items []model.Item, role string, withArguments bool) []string {
	var essence []string
	text := ""
	flush := func() {
		if text != "" {
			essence = append(essence, "text:"+role+":"+text)
			text = ""
		}
	}
	for _, item := range items {
		switch item.Kind {
		case model.ItemMessage:
			text += item.Text()
		case model.ItemToolCall:
			flush()
			call := item.ToolCall
			if call == nil || call.Kind == model.ToolServer {
				continue
			}
			entry := "call " + call.ID + " " + call.Name
			if withArguments && call.Kind == model.ToolFunction {
				entry += " " + normalizedJSON(call.Arguments)
			}
			essence = append(essence, entry)
		case model.ItemToolResult:
			flush()
			var result string
			for _, part := range item.Parts {
				result += part.Text
			}
			essence = append(essence, "result "+item.CallID+" "+result)
		}
	}
	flush()
	return essence
}

// normalizedJSON makes argument strings comparable regardless of key order and whitespace.
func normalizedJSON(text string) string {
	decoded, err := model.DecodeJSON([]byte(text))
	if err != nil {
		return text
	}
	return string(model.Marshal(decoded))
}
