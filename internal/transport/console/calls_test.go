package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
)

func responseEntry(t *testing.T, payload string) auditevent.Entry {
	t.Helper()
	decoded, err := auditevent.Decode(auditevent.KindResponse, []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	return auditevent.Entry{Kind: auditevent.KindResponse, Decoded: decoded}
}

func TestTimelineViewToleratesMissingUsage(t *testing.T) {
	view := timelineView(responseEntry(t, `{"response":{"status":"incomplete","finish":{"kind":"stop"},"items":[]}}`))
	if view.Response == nil || view.Response.InputTokens != 0 || view.Response.OutputTokens != 0 {
		t.Fatalf("a response without usage must render zero tokens: %+v", view.Response)
	}
	view = timelineView(responseEntry(t, `{"response":{"status":"completed","finish":{"kind":"stop"},"items":[],"usage":{"input_tokens":12,"output_tokens":3}}}`))
	if view.Response.InputTokens != 12 || view.Response.OutputTokens != 3 {
		t.Fatalf("usage was not copied: %+v", view.Response)
	}
}
