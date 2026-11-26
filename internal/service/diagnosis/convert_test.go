package diagnosis

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func TestFindingFlowIsHydratedFromTheFlowTable(t *testing.T) {
	findings := []entity.AnalysisFinding{{
		Fingerprint: "f1", Code: "untrusted_value_reuse", Origin: "tool_result", Trust: "untrusted", Sink: "external_send",
		Severity: "high", DetectorID: "flow", DetectorVersion: "1",
		FlowSourceCallID: "call_0", FlowSourceSection: "request", FlowSourceOrdinal: 3,
		FlowSourcePath: "items[3].parts[0].text", FlowSourceOrigin: "tool_result",
		FlowRelation: "value_reuse", FlowSinkToolCallID: "call_1",
	}}
	flows := []entity.AnalysisFlow{{
		SourceCallID: "call_0", SourceSection: "request", SourceOrdinal: 3,
		SourceOrigin: "tool_result", SourcePath: "items[3].parts[0].text",
		SinkToolCallID: "call_1", Sink: "external_send", Relation: "value_reuse", ContentHash: "hash", Confidence: 0.9,
	}}
	value := analysisFromEntity(&entity.Analysis{}, findings, flows)
	if len(value.Findings) != 1 || value.Findings[0].Flow == nil {
		t.Fatalf("finding flow missing: %+v", value.Findings)
	}
	flow := value.Findings[0].Flow
	if flow.SourcePath != "items[3].parts[0].text" || flow.ContentHash != "hash" || flow.Confidence != 0.9 {
		t.Fatalf("the finding view must carry the complete flow: %+v", flow)
	}
}

func TestFindingRowsPersistTheAnchoredFlowIdentity(t *testing.T) {
	locator := observation.Locator{CallID: "call_1", Section: observation.SectionRequest, Ordinal: 5, Path: "items[5].parts[0].text"}
	value := &analysis.Analysis{
		ID: "an_1", ProjectID: "project_1", CallID: "call_1",
		Findings: []analysis.Finding{{
			Fingerprint: "f1", Code: analysis.CodeUntrustedValueReuse, Origin: observation.OriginToolResult,
			Trust: observation.Untrusted, Sink: observation.SinkExternalSend, Severity: analysis.SeverityHigh,
			Evidence:    []analysis.Evidence{{Locator: locator}},
			Attribution: []analysis.Attribution{{Locator: locator}},
			Flow: &analysis.Flow{
				SourceLocator: locator, SourceOrigin: observation.OriginToolResult,
				SinkToolCallID: "tool_1", Relation: analysis.RelationValueReuse, ContentHash: "hash",
			},
		}},
	}
	rows := findingRows(value, nil)
	if len(rows) != 1 {
		t.Fatalf("one finding expected: %+v", rows)
	}
	row := rows[0]
	if row.FlowSourceCallID != "call_1" || row.FlowSourceSection != "request" || row.FlowSourceOrdinal != 5 || row.FlowSourcePath != "items[5].parts[0].text" {
		t.Fatalf("the flow identity was not persisted: %+v", row)
	}
	if row.FlowSinkToolCallID != "tool_1" || row.FlowRelation != analysis.RelationValueReuse {
		t.Fatalf("the flow edge was not persisted: %+v", row)
	}
	var evidence []analysis.Evidence
	if err := decode(row.Evidence, &evidence); err != nil || len(evidence) != 1 || evidence[0].Locator != locator {
		t.Fatalf("the evidence locator must be stored in the payload: %+v %v", evidence, err)
	}
}

func TestEntityRoundTripKeepsFlowSourceLocator(t *testing.T) {
	row := &entity.Analysis{ID: "an_1", ProjectID: "project_1", CallID: "call_1"}
	findings := []entity.AnalysisFinding{{
		Fingerprint: "f1", Code: analysis.CodeUntrustedValueReuse, Origin: "tool_result", Trust: "untrusted",
		Sink: "external_send", Severity: analysis.SeverityHigh, DetectorID: "flow", DetectorVersion: "2",
		FlowSourceCallID: "call_0", FlowSourceSection: "response", FlowSourceOrdinal: 2,
		FlowSourcePath: "items[2].parts[0].text", FlowSourceOrigin: "tool_result",
		FlowRelation: analysis.RelationValueReuse, FlowSinkToolCallID: "tool_1",
	}}
	flows := []entity.AnalysisFlow{{
		AnalysisID: "an_1", SourceCallID: "call_0", SourceSection: "response", SourceOrdinal: 2,
		SourcePath: "items[2].parts[0].text", SourceOrigin: "tool_result", SinkToolCallID: "tool_1",
		Sink: "external_send", Relation: analysis.RelationValueReuse, ContentHash: "hash", Confidence: 0.8,
	}}
	value := analysisFromEntity(row, findings, flows)
	if len(value.Findings) != 1 || value.Findings[0].Flow == nil {
		t.Fatalf("the finding flow is missing: %+v", value.Findings)
	}
	flow := value.Findings[0].Flow
	if flow.SourceLocator.CallID != "call_0" || flow.SourceLocator.Section != "response" || flow.SourceLocator.Ordinal != 2 {
		t.Fatalf("the flow source identity is wrong: %+v", flow.SourceLocator)
	}
	if flow.ContentHash != "hash" || flow.Confidence != 0.8 || flow.SourcePath != "items[2].parts[0].text" {
		t.Fatalf("the complete flow must come from the flow table: %+v", flow)
	}
}
