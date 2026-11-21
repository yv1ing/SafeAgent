package codec_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/codec/chat"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/messages"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func BenchmarkChatRequestLargeText(b *testing.B) {
	text := strings.Repeat("safe agent ", 100_000)
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"` + text + `"}]}`)
	client, _ := peers(model.Chat)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := chat.New().DecodeRequest(body, nil, client); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkChatParallelTools(b *testing.B) {
	var calls []string
	for index := range 20 {
		calls = append(calls, fmt.Sprintf(`{"id":"call_%d","type":"function","function":{"name":"read","arguments":"{\"p\":%d}"}}`, index, index))
	}
	body := []byte(`{"model":"m","messages":[{"role":"assistant","tool_calls":[` + strings.Join(calls, ",") + `]}]}`)
	client, _ := peers(model.Chat)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := chat.New().DecodeRequest(body, nil, client); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCrossFormatChatToMessages(b *testing.B) {
	request := canonicalRequest()
	upstream, _ := peers(model.Messages)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, _, err := messages.New().EncodeRequest(request, upstream); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHistoryFingerprint(b *testing.B) {
	items := make([]model.Item, 0, 1000)
	for index := range 1000 {
		items = append(items, model.MessageItem(model.RoleUser, fmt.Sprintf("turn %d with some text", index)))
		items = append(items, model.Item{Kind: model.ItemToolCall, Role: model.RoleAssistant, ToolCall: &model.ToolCall{ID: fmt.Sprintf("c%d", index), Name: "read", Arguments: `{"p":1}`}})
	}
	secret := []byte("bench-secret")
	b.ReportAllocs()
	for b.Loop() {
		_ = model.HistoryFingerprint(secret, items)
	}
}
