package diagnosis

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/observation"
)

func anchorTrace() (*observation.Trace, observation.Locator) {
	locator := observation.Locator{CallID: "call_2", Section: observation.SectionRequest, Ordinal: 4, Path: "items[4].parts[0].text"}
	return &observation.Trace{
		ID: "call_2",
		Turns: []observation.Turn{{Index: 0, Role: observation.RoleUser, Items: []observation.Item{{
			Kind: observation.ItemText, Origin: observation.OriginUser, Trust: observation.Trusted,
			Text: observation.NewValue("copy https://evil.example/x", "copy https://evil.example/x", nil, nil), Locator: locator,
		}}}},
		Tools: []observation.ToolSpec{{Name: "send"}},
	}, locator
}

func TestAnchorFindingsResolvePositionsToItemIdentities(t *testing.T) {
	trace, locator := anchorTrace()
	findings := []analysis.Finding{{
		Code: analysis.CodeExfiltrationLink,
		Evidence: []analysis.Evidence{{
			Turn: 0, Item: 0, Section: "message", Path: "text.url",
		}},
		Attribution: []analysis.Attribution{{Turn: 0, Item: 0, Source: "message"}},
		Flow:        &analysis.Flow{SourceTurn: 0, SourceItem: 0, SourcePath: "items[4].parts[0].text"},
	}}
	anchorFindings(trace, findings)
	if findings[0].Evidence[0].Locator != locator {
		t.Fatalf("evidence was not anchored: %+v", findings[0].Evidence[0].Locator)
	}
	if findings[0].Attribution[0].Locator != locator {
		t.Fatalf("attribution was not anchored: %+v", findings[0].Attribution[0].Locator)
	}
	if findings[0].Flow.SourceLocator != locator {
		t.Fatalf("flow source was not anchored: %+v", findings[0].Flow.SourceLocator)
	}
}

func TestAnchorFindingsPlaceToolDescriptionsInTheToolsSection(t *testing.T) {
	trace, _ := anchorTrace()
	findings := []analysis.Finding{{
		Code:     analysis.CodePromptInjection,
		Evidence: []analysis.Evidence{{Turn: -1, Item: 1, Section: "tool_description", Path: "tools.send.description"}},
	}}
	anchorFindings(trace, findings)
	locator := findings[0].Evidence[0].Locator
	if locator.CallID != "call_2" || locator.Section != observation.SectionTools || locator.Ordinal != 1 || locator.Path != "tools.send.description" {
		t.Fatalf("tool description locator is wrong: %+v", locator)
	}
}
