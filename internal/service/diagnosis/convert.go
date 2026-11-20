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

func findingRows(value *analysis.Analysis) []entity.AnalysisFinding {
	rows := make([]entity.AnalysisFinding, 0, len(value.Findings))
	for _, finding := range value.Findings {
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
			row.FlowSourceTurn = finding.Flow.SourceTurn
			row.FlowSourceItem = finding.Flow.SourceItem
			row.FlowSourceOrigin = string(finding.Flow.SourceOrigin)
			row.FlowRelation = finding.Flow.Relation
			row.FlowSinkToolCallID = finding.Flow.SinkToolCallID
		}
		rows = append(rows, row)
	}
	return rows
}

func flowRows(value *analysis.Analysis) []entity.AnalysisFlow {
	flows := flowsOf(value.Findings)
	rows := make([]entity.AnalysisFlow, 0, len(flows))
	for _, flow := range flows {
		rows = append(rows, entity.AnalysisFlow{
			ID:         "afl_" + secure.Digest(value.ID + "\x00" + flowLocationKey(flow))[:24],
			AnalysisID: value.ID, ProjectID: value.ProjectID, CallID: value.CallID, AgentKeyID: value.AgentKeyID,
			SourceTurn: flow.SourceTurn, SourceItem: flow.SourceItem, SourceOrigin: string(flow.SourceOrigin),
			SourcePath: flow.SourcePath, SinkToolCallID: flow.SinkToolCallID, Sink: string(flow.Sink),
			Relation: flow.Relation, ContentHash: flow.ContentHash, Confidence: flow.Confidence,
			CreatedAt: value.StartedAt,
		})
	}
	return rows
}

func analysisFindingID(analysisID, fingerprint string) string {
	return "af_" + secure.Digest(analysisID + "\x00" + fingerprint)[:24]
}

func analysisFromEntity(row *entity.Analysis, findings []entity.AnalysisFinding, flows []entity.AnalysisFlow) *analysis.Analysis {
	value := &analysis.Analysis{
		ID: row.ID, ProjectID: row.ProjectID, CallID: row.CallID, AgentKeyID: row.AgentKeyID,
		BatchID: row.BatchID, State: row.State, InputHash: row.InputHash, ModelState: row.ModelState,
		Error: row.Error, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		Versions: analysis.Versions{
			Adapter: row.AdapterVersion, Detectors: row.DetectorVersion, Catalogue: row.CatalogueVersion,
			Taxonomy: row.TaxonomyVersion, DecisionModel: row.DecisionModel,
		},
		Findings: analysisOfFindings(findings), Flows: analysisOfFlows(flows),
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
				SourceTurn: row.FlowSourceTurn, SourceItem: row.FlowSourceItem,
				SourceOrigin: observation.Origin(row.FlowSourceOrigin), SinkToolCallID: row.FlowSinkToolCallID,
				Sink: finding.Sink, Relation: row.FlowRelation,
			}
		}
		findings = append(findings, finding)
	}
	return findings
}

func analysisOfFlows(rows []entity.AnalysisFlow) []analysis.Flow {
	flows := make([]analysis.Flow, 0, len(rows))
	for _, row := range rows {
		flows = append(flows, analysis.Flow{
			SourceTurn: row.SourceTurn, SourceItem: row.SourceItem, SourceOrigin: observation.Origin(row.SourceOrigin),
			SourcePath: row.SourcePath, SinkToolCallID: row.SinkToolCallID, Sink: observation.Sink(row.Sink),
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
