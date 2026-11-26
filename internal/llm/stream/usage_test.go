package stream

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func TestUsageOnlyRecognisesProtocolShapes(t *testing.T) {
	cases := []struct {
		name   string
		format model.Format
		frame  codec.Frame
		want   bool
	}{
		{"chat usage chunk", model.Chat, codec.Frame{Data: []byte(`{"choices":[],"usage":{"total_tokens":5}}`)}, true},
		{"chat empty choice", model.Chat, codec.Frame{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":null}],"usage":{"total_tokens":5}}`)}, true},
		{"chat content chunk", model.Chat, codec.Frame{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"total_tokens":5}}`)}, false},
		{"chat finish chunk", model.Chat, codec.Frame{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"total_tokens":5}}`)}, false},
		{"chat without choices", model.Chat, codec.Frame{Data: []byte(`{"usage":{"total_tokens":5}}`)}, false},
		{"messages usage delta", model.Messages, codec.Frame{Event: "message_delta", Data: []byte(`{"type":"message_delta","usage":{"output_tokens":2}}`)}, true},
		{"messages empty delta", model.Messages, codec.Frame{Data: []byte(`{"type":"message_delta","delta":{},"usage":{"output_tokens":2}}`)}, true},
		{"messages finish delta", model.Messages, codec.Frame{Event: "message_delta", Data: []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`)}, false},
		{"messages stop sequence", model.Messages, codec.Frame{Data: []byte(`{"type":"message_delta","delta":{"stop_sequence":"END"},"usage":{"output_tokens":2}}`)}, false},
		{"messages message start", model.Messages, codec.Frame{Event: "message_start", Data: []byte(`{"type":"message_start","usage":{"output_tokens":2}}`)}, false},
		{"gemini usage metadata", model.Gemini, codec.Frame{Data: []byte(`{"usageMetadata":{"totalTokenCount":5}}`)}, false},
		{"invalid json", model.Chat, codec.Frame{Data: []byte(`{`)}, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := usageOnly(test.format, test.frame); got != test.want {
				t.Fatalf("usageOnly(%s, %s) = %v, want %v", test.format, test.frame.Data, got, test.want)
			}
		})
	}
}
