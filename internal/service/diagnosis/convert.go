package diagnosis

import (
	"encoding/json"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func analysisRow(value *analysis.Analysis) *entity.Analysis {
	verdict, _ := json.Marshal(value.Verdict)
	coverage, _ := json.Marshal(value.Coverage)
	var model []byte
	if value.Model != nil {
		model, _ = json.Marshal(value.Model)
	}
	return &entity.Analysis{
		ID: value.ID, ProjectID: value.ProjectID, CallID: value.CallID, AgentKeyID: value.AgentKeyID,
		BatchID: value.BatchID, State: value.State,
		AdapterVersion: value.Versions.Adapter, DetectorVersion: value.Versions.Detectors,
		CatalogueVersion: value.Versions.Catalogue, TaxonomyVersion: value.Versions.Taxonomy,
		DecisionModel: value.Versions.DecisionModel, InputHash: value.InputHash,
		Verdict: verdict, Coverage: coverage, Model: model,
		ModelState: value.ModelState, Error: value.Error, StartedAt: value.StartedAt, FinishedAt: value.FinishedAt,
	}
}

// findingRows maps the anchored findings of one analysis onto the row shape. The item identity is
// written both inside the evidence/attribution payloads and in the flow columns the console queries.
func findingRows(value *analysis.Analysis, trace *observation.Trace) []entity.AnalysisFinding {
	rows := make([]entity.AnalysisFinding, 0, len(value.Findings))
	for index := range value.Findings {
		finding := value.Findings[index]
		evidence, _ := json.Marshal(finding.Evidence)
		attribution, _ := json.Marshal(finding.Attribution)
		row := entity.AnalysisFinding{
			ID: analysisFindingID(value.ID, finding.Fingerprint), AnalysisID: value.ID,
			ProjectID: value.ProjectID, CallID: value.CallID, Fingerprint: finding.Fingerprint,
			Code: finding.Code, Origin: string(finding.Origin), Trust: string(finding.Trust), Sink: string(finding.Sink),
			Source: string(finding.Axes.Source), Mode: string(finding.Axes.Mode), Harm: string(finding.Axes.Harm),
			Severity: finding.Severity, Confidence: finding.Confidence,
			DetectorID: finding.Detector.ID, DetectorVersion: finding.Detector.Version,
			Evidence: evidence, Attribution: attribution, CreatedAt: value.StartedAt,
		}
		if finding.Flow != nil {
			locator := finding.Flow.SourceLocator
			if locator.CallID == "" {
				locator = anchorLocator(trace, finding.Flow.SourceTurn, finding.Flow.SourceItem, finding.Flow.SourcePath, locator)
			}
			row.FlowSourceCallID = locator.CallID
			row.FlowSourceSection = string(locator.Section)
			row.FlowSourceOrdinal = locator.Ordinal
			row.FlowSourcePath = firstPath(locator.Path, finding.Flow.SourcePath)
			row.FlowSourceOrigin = string(finding.Flow.SourceOrigin)
			row.FlowRelation = finding.Flow.Relation
			row.FlowSinkToolCallID = finding.Flow.SinkToolCallID
		}
		rows = append(rows, row)
	}
	return rows
}

func flowRows(value *analysis.Analysis, trace *observation.Trace) []entity.AnalysisFlow {
	flows := flowsOf(value.Findings)
	rows := make([]entity.AnalysisFlow, 0, len(flows))
	for _, flow := range flows {
		locator := flow.SourceLocator
		if locator.CallID == "" {
			locator = anchorLocator(trace, flow.SourceTurn, flow.SourceItem, flow.SourcePath, locator)
		}
		rows = append(rows, entity.AnalysisFlow{
			ID:         "afl_" + secure.Digest(value.ID + "\x00" + flowLocationKey(flow))[:24],
			AnalysisID: value.ID, ProjectID: value.ProjectID, CallID: value.CallID, AgentKeyID: value.AgentKeyID,
			SourceCallID: locator.CallID, SourceSection: string(locator.Section), SourceOrdinal: locator.Ordinal,
			SourceOrigin: string(flow.SourceOrigin), SourcePath: firstPath(locator.Path, flow.SourcePath),
			SinkToolCallID: flow.SinkToolCallID, Sink: string(flow.Sink),
			Relation: flow.Relation, ContentHash: flow.ContentHash, Confidence: flow.Confidence,
			CreatedAt: value.StartedAt,
		})
	}
	return rows
}

// firstPath prefers the stored item path over the detector's leaf path.
func firstPath(locatorPath, detectorPath string) string {
	if locatorPath != "" {
		return locatorPath
	}
	return detectorPath
}

func analysisFindingID(analysisID, fingerprint string) string {
	return "af_" + secure.Digest(analysisID + "\x00" + fingerprint)[:24]
}

func analysisFromEntity(row *entity.Analysis, findings []entity.AnalysisFinding, flows []entity.AnalysisFlow) *analysis.Analysis {
	parsedFindings := analysisOfFindings(findings)
	parsedFlows := analysisOfFlows(flows)
	hydrateFindingFlows(parsedFindings, parsedFlows)
	value := &analysis.Analysis{
		ID: row.ID, ProjectID: row.ProjectID, CallID: row.CallID, AgentKeyID: row.AgentKeyID,
		BatchID: row.BatchID, State: row.State, InputHash: row.InputHash, ModelState: row.ModelState,
		Error: row.Error, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		Versions: analysis.Versions{
			Adapter: row.AdapterVersion, Detectors: row.DetectorVersion, Catalogue: row.CatalogueVersion,
			Taxonomy: row.TaxonomyVersion, DecisionModel: row.DecisionModel,
		},
		Findings: parsedFindings, Flows: parsedFlows,
	}
	_ = decode(row.Verdict, &value.Verdict)
	_ = decode(row.Coverage, &value.Coverage)
	if len(row.Model) > 0 && string(row.Model) != "null" {
		labels := new(analysis.ModelLabels)
		if decode(row.Model, labels) == nil {
			value.Model = labels
		}
	}
	return value
}

func analysisOfFindings(rows []entity.AnalysisFinding) []analysis.Finding {
	findings := make([]analysis.Finding, 0, len(rows))
	for _, row := range rows {
		finding := analysis.Finding{
			Fingerprint: row.Fingerprint, Code: row.Code,
			Origin: observation.Origin(row.Origin), Trust: observation.Trust(row.Trust), Sink: observation.Sink(row.Sink),
			Axes:     analysis.Axes{Source: analysis.RiskSource(row.Source), Mode: analysis.FailureMode(row.Mode), Harm: analysis.Harm(row.Harm)},
			Severity: row.Severity, Confidence: row.Confidence,
			Detector: analysis.DetectorRef{ID: row.DetectorID, Version: row.DetectorVersion},
		}
		_ = decode(row.Evidence, &finding.Evidence)
		_ = decode(row.Attribution, &finding.Attribution)
		if row.FlowRelation != "" {
			finding.Flow = &analysis.Flow{
				SourceLocator: observation.Locator{
					CallID: row.FlowSourceCallID, Section: observation.Section(row.FlowSourceSection),
					Ordinal: row.FlowSourceOrdinal, Path: row.FlowSourcePath,
				},
				SourceOrigin: observation.Origin(row.FlowSourceOrigin), SourcePath: row.FlowSourcePath,
				SinkToolCallID: row.FlowSinkToolCallID, Sink: finding.Sink, Relation: row.FlowRelation,
			}
		}
		findings = append(findings, finding)
	}
	return findings
}

// hydrateFindingFlows restores the fields that analysis_findings does not store. The complete edge
// is kept in analysis_flows; the embedded copy exists only for a finding's own view.
func hydrateFindingFlows(findings []analysis.Finding, flows []analysis.Flow) {
	for index := range findings {
		embedded := findings[index].Flow
		if embedded == nil {
			continue
		}
		for _, flow := range flows {
			if !sameFlowSource(flow, *embedded) || flow.SinkToolCallID != embedded.SinkToolCallID || flow.Relation != embedded.Relation {
				continue
			}
			copied := flow
			findings[index].Flow = &copied
			break
		}
	}
}

func sameFlowSource(left, right analysis.Flow) bool {
	if left.SourceLocator.CallID != "" || right.SourceLocator.CallID != "" {
		return left.SourceLocator == right.SourceLocator
	}
	return left.SourceTurn == right.SourceTurn && left.SourceItem == right.SourceItem
}

func analysisOfFlows(rows []entity.AnalysisFlow) []analysis.Flow {
	flows := make([]analysis.Flow, 0, len(rows))
	for _, row := range rows {
		locator := observation.Locator{
			CallID: row.SourceCallID, Section: observation.Section(row.SourceSection),
			Ordinal: row.SourceOrdinal, Path: row.SourcePath,
		}
		flows = append(flows, analysis.Flow{
			SourceLocator: locator, SourcePath: row.SourcePath, SourceOrigin: observation.Origin(row.SourceOrigin),
			SinkToolCallID: row.SinkToolCallID, Sink: observation.Sink(row.Sink),
			Relation: row.Relation, ContentHash: row.ContentHash, Confidence: row.Confidence,
		})
	}
	return flows
}

func decode(raw []byte, target any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, target)
}
