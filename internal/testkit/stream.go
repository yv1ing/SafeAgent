package testkit

import (
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// Peers returns the client and upstream peers of one format. Messages clients carry a sealing
// envelope; other protocols have no signatures to seal.
func Peers(format model.Format) (codec.Peer, codec.Peer) {
	return PeersWithEnvelope(format, nil)
}

func PeersWithEnvelope(format model.Format, envelope codec.Envelope) (codec.Peer, codec.Peer) {
	vendor := format.DefaultVendor()
	return codec.Peer{Side: codec.Client, Vendor: vendor, Model: "m", Envelope: envelope},
		codec.Peer{Side: codec.Upstream, Vendor: vendor, Model: "m"}
}

func Frames(payloads ...string) []codec.Frame {
	frames := make([]codec.Frame, len(payloads))
	for index, payload := range payloads {
		frames[index] = codec.Frame{Data: []byte(payload)}
	}
	return frames
}

func Payloads(frames []codec.Frame) []string {
	payloads := make([]string, len(frames))
	for index, frame := range frames {
		payloads[index] = string(frame.Data)
	}
	return payloads
}

func JoinPayloads(frames []codec.Frame) string {
	var text strings.Builder
	for _, frame := range frames {
		text.Write(frame.Data)
		text.WriteByte('\n')
	}
	return text.String()
}

// ToolCallEvents is the canonical event sequence of one complete streamed tool call.
func ToolCallEvents(meta model.ResponseMeta, arguments string) []model.Event {
	started := model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "read"}}
	stopped := model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{Kind: model.ToolFunction, ID: "call_1", Name: "read", Arguments: arguments}}
	return []model.Event{
		{Kind: model.EventMessageStart, Meta: &meta},
		{Kind: model.EventItemStart, ItemIndex: 0, Item: &started},
		{Kind: model.EventPartStart, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartToolArguments}},
		{Kind: model.EventPartDelta, ItemIndex: 0, PartIndex: 0, Delta: arguments},
		{Kind: model.EventPartStop, ItemIndex: 0, PartIndex: 0, Part: &model.Part{Kind: model.PartToolArguments, Text: arguments}},
		{Kind: model.EventItemStop, ItemIndex: 0, Item: &stopped},
		{Kind: model.EventMessageDelta, Finish: &model.Finish{Kind: model.FinishToolCalls}},
		{Kind: model.EventMessageStop},
	}
}
