package codec

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func TestDeepSeekUsageFieldsAreMapped(t *testing.T) {
	chat := ParseChatUsage(map[string]any{"usage": map[string]any{
		"prompt_tokens": float64(300), "completion_tokens": float64(61),
		"prompt_cache_hit_tokens":   float64(128),
		"completion_tokens_details": map[string]any{"reasoning_tokens": float64(22)},
	}})
	if chat.CacheRead != 128 || chat.Reasoning != 22 {
		t.Fatalf("chat usage = %+v", chat)
	}

	messages := ParseMessagesUsage(map[string]any{
		"input_tokens": float64(100), "output_tokens": float64(40),
		"cache_read_input_tokens": float64(32),
		"output_tokens_details":   map[string]any{"thinking_tokens": float64(17)},
	})
	if messages.CacheRead != 32 || messages.Reasoning != 17 {
		t.Fatalf("messages usage = %+v", messages)
	}
	if messages.Input != 132 {
		t.Fatalf("messages input must include cache reads: %+v", messages)
	}
}

func TestUsageDetailsAreReportedOnlyWhereTheyAreEncoded(t *testing.T) {
	usage := &model.Usage{Details: map[string]int64{"prompt.audio": 5}}
	if losses := UsageLosses(usage, model.Chat); len(losses) != 0 {
		t.Fatalf("Chat carries audio details: %+v", losses)
	}
	for _, target := range []model.Format{model.Responses, model.Messages, model.Gemini} {
		if losses := UsageLosses(usage, target); len(losses) == 0 {
			t.Fatalf("%s does not encode usage details", target)
		}
	}
}
