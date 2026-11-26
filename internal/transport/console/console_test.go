package console

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	control "github.com/yv1ing/SafeAgent/internal/service/control"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
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

func TestChannelViewMasksCredentialsAndCarriesReferences(t *testing.T) {
	deleted := time.Now().UTC()
	view := channelView(&control.ChannelInfo{
		Channel: entity.Channel{
			ID: "channel_1", Name: "c", ProviderID: "openai",
			BaseURL: "https://example.com", Headers: map[string]string{"Authorization": "secret", "X-Lab": "one"},
			ForwardHeaders: []string{"x-provider-feature"}, DefaultMaxOutputTokens: 1234,
			TimeoutSeconds: 600, Enabled: true, Version: 2, DeletedAt: &deleted,
		},
		ProviderLabel: "OpenAI",
		RouteCount:    3,
		CapabilityOverrides: []entity.CapabilityOverride{{
			ChannelID: "channel_1", ModelID: "model_1", Format: "openai_chat", Field: "thinking", Reason: "rejected",
		}},
	})
	if view.Headers["Authorization"] != "****" || view.Headers["X-Lab"] != "one" {
		t.Fatalf("the view must mask credential headers: %+v", view.Headers)
	}
	if len(view.ForwardHeaders) != 1 || view.ForwardHeaders[0] != "x-provider-feature" {
		t.Fatalf("forward headers missing from the view: %+v", view)
	}
	if view.DefaultMaxOutputTokens != 1234 || view.RouteCount != 3 || view.ProviderLabel != "OpenAI" {
		t.Fatalf("channel defaults missing from the view: %+v", view)
	}
	if len(view.CapabilityOverrides) != 1 || view.CapabilityOverrides[0].Field != "thinking" {
		t.Fatalf("capability overrides missing from the view: %+v", view)
	}
	if view.DeletedAt == "" {
		t.Fatalf("the deletion stamp must be visible: %+v", view)
	}
}

func TestProviderViewCarriesFormatsAndChannelCount(t *testing.T) {
	view := providerView(&control.ProviderInfo{
		Provider: entity.Provider{
			ID: "openai", Source: entity.ProviderBuiltin, Label: "OpenAI", Enabled: true, Version: 1,
			Endpoints: []byte(`{"openai_chat":{"Format":"openai_chat","BaseURL":"https://api.openai.com"},"openai_responses":{"Format":"openai_responses","BaseURL":"https://api.openai.com"}}`),
		},
		Formats:      []string{"openai_chat", "openai_responses"},
		ChannelCount: 2,
	})
	if len(view.Formats) != 2 || view.Formats[0] != "openai_chat" || view.Formats[1] != "openai_responses" {
		t.Fatalf("provider formats = %+v", view.Formats)
	}
	if view.ChannelCount != 2 {
		t.Fatalf("provider channel count = %d", view.ChannelCount)
	}
}

func TestEveryErrorCodeHasAConsoleLabel(t *testing.T) {
	catalog, err := os.ReadFile("../../../web/src/i18n/messages.ts")
	if err != nil {
		t.Fatal(err)
	}
	chinese, err := os.ReadFile("../../../web/src/i18n/zh-CN.ts")
	if err != nil {
		t.Fatal(err)
	}
	for code := range errorMessages {
		for name, content := range map[string]string{"English": string(catalog), "Chinese": string(chinese)} {
			if !strings.Contains(content, code+":") {
				t.Errorf("%s catalog has no label for error code %s", name, code)
			}
		}
	}
}

func TestCallViewOmitsOutcomeAndResolutionUntilKnown(t *testing.T) {
	payload, err := json.Marshal(callView(&entity.Call{ID: "call_1", State: "running"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"outcome"`, `"resolution"`} {
		if strings.Contains(string(payload), field) {
			t.Fatalf("a running call must omit %s: %s", field, payload)
		}
	}
	payload, err = json.Marshal(callView(&entity.Call{ID: "call_2", State: "failed", Outcome: classify.OutcomeRejected, Resolution: "exact"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"outcome":"rejected"`) || !strings.Contains(string(payload), `"resolution":"exact"`) {
		t.Fatalf("a settled call must carry its outcome and resolution: %s", payload)
	}
}

func TestProjectViewWithSummary(t *testing.T) {
	project := &entity.Project{Name: "ops", DailyTokenQuota: 1000}
	view := projectViewWithSummary(project, &entity.ProjectSummary{
		Calls24h: 10, Failed24h: 1, Blocked24h: 2,
		InputTokens24h: 400, OutputTokens24h: 100,
		LastActivityAt: 1_700_000_000_000,
	})
	if view.FailureRate != "10.0%" || view.QuotaUsagePercent != 50 {
		t.Fatalf("unexpected summary: %+v", view)
	}
	if view.Calls24h != 10 || view.Blocked24h != 2 || view.LastActivityAt == "" {
		t.Fatalf("summary fields were not copied: %+v", view)
	}
}

func TestProjectViewWithSummaryCapsQuotaAtFull(t *testing.T) {
	project := &entity.Project{DailyTokenQuota: 100}
	view := projectViewWithSummary(project, &entity.ProjectSummary{
		InputTokens24h: 80, OutputTokens24h: 70,
	})
	if view.QuotaUsagePercent != 100 {
		t.Fatalf("quota usage must cap at 100: %d", view.QuotaUsagePercent)
	}
}
