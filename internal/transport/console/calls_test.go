package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
)

func TestTimelineViewToleratesMissingUsage(t *testing.T) {
	entry := auditevent.Entry{
		Kind: auditevent.KindResponse,
		Decoded: auditevent.Decoded{Response: &auditevent.Response{
			Response: model.Response{Status: "incomplete", Finish: model.Finish{Kind: model.FinishStop}},
		}},
	}
	view := timelineView(entry)
	if view.Response == nil || view.Response.InputTokens != 0 || view.Response.OutputTokens != 0 {
		t.Fatalf("a response without usage must render zero tokens: %+v", view.Response)
	}
	entry.Decoded.Response.Response.Usage = &model.Usage{Input: 12, Output: 3}
	view = timelineView(entry)
	if view.Response.InputTokens != 12 || view.Response.OutputTokens != 3 {
		t.Fatalf("usage was not copied: %+v", view.Response)
	}
}
