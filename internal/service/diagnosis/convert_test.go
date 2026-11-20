package diagnosis

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func TestFindingFlowIsHydratedFromTheFlowTable(t *testing.T) {
	findings := []entity.AnalysisFinding{{
		Fingerprint: "f1", Code: "untrusted_value_reuse", Origin: "tool_result", Trust: "untrusted", Sink: "external_send",
		Severity: "high", DetectorID: "flow", DetectorVersion: "1",
		FlowSourceTurn: 1, FlowSourceItem: 2, FlowSourceOrigin: "tool_result",
		FlowRelation: "value_reuse", FlowSinkToolCallID: "call_1",
	}}
	flows := []entity.AnalysisFlow{{
		SourceTurn: 1, SourceItem: 2, SourceOrigin: "tool_result", SourcePath: "items[1].parts[0].text",
		SinkToolCallID: "call_1", Sink: "external_send", Relation: "value_reuse", ContentHash: "hash", Confidence: 0.9,
	}}
	value := analysisFromEntity(&entity.Analysis{}, findings, flows)
	if len(value.Findings) != 1 || value.Findings[0].Flow == nil {
		t.Fatalf("finding flow missing: %+v", value.Findings)
	}
	flow := value.Findings[0].Flow
	if flow.SourcePath != "items[1].parts[0].text" || flow.ContentHash != "hash" || flow.Confidence != 0.9 {
		t.Fatalf("the finding view must carry the complete flow: %+v", flow)
	}
}
