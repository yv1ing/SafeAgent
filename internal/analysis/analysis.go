// Package analysis judges an observation trace. It owns findings, flows, the three-axis mapping,
// the detector registry and the verdict rules; policy compilation lives in policy and the optional
// decision model lives in decision.
package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/yv1ing/SafeAgent/internal/observation"
)

const (
	AnalysisRunning   = "running"
	AnalysisCompleted = "completed"
	AnalysisFailed    = "failed"

	ModelDisabled       = "disabled"
	ModelOK             = "ok"
	ModelBudgetExceeded = "budget_exhausted"
	ModelFailed         = "failed"
)

// Versions makes an analysis reproducible. Findings from different detector or catalogue
// versions remain readable but are not compared silently.
type Versions struct {
	Adapter       string `json:"adapter"`
	Detectors     string `json:"detectors"`
	Catalogue     string `json:"catalogue"`
	Taxonomy      string `json:"taxonomy"`
	DecisionModel string `json:"decision_model,omitempty"`
}

// ModelLabels is the full 8/14/10 judgement of one trajectory. It is kept beside the deterministic
// findings instead of replacing their local axes.
type ModelLabels struct {
	Unsafe          bool        `json:"unsafe"`
	Probability     float64     `json:"probability"`
	Confidence      float64     `json:"confidence"`
	ConfidenceKnown bool        `json:"confidence_known,omitempty"`
	Source          RiskSource  `json:"source,omitempty"`
	Mode            FailureMode `json:"mode,omitempty"`
	Harm            Harm        `json:"harm,omitempty"`
	Severity        string      `json:"severity,omitempty"`
	Model           string      `json:"model,omitempty"`
	AskedAt         time.Time   `json:"asked_at,omitempty"`
}

type Verdict struct {
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason,omitempty"`
}

// Analysis is one diagnosis of one call. State running and failed are stored too: a failure is a
// visible result, not an absent row.
type Analysis struct {
	ID         string       `json:"id"`
	ProjectID  string       `json:"project_id"`
	CallID     string       `json:"call_id"`
	AgentKeyID string       `json:"agent_key_id"`
	BatchID    string       `json:"batch_id,omitempty"`
	State      string       `json:"state"`
	Versions   Versions     `json:"versions"`
	InputHash  string       `json:"input_hash"`
	Findings   []Finding    `json:"findings"`
	Flows      []Flow       `json:"flows"`
	Model      *ModelLabels `json:"model,omitempty"`
	ModelState string       `json:"model_state"`
	Coverage   Coverage     `json:"coverage"`
	Verdict    Verdict      `json:"verdict"`
	Error      string       `json:"error,omitempty"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
}

func NewAnalysis(id, projectID, callID, agentKeyID, batchID string, versions Versions, inputHash string, startedAt time.Time) *Analysis {
	return &Analysis{
		ID: id, ProjectID: projectID, CallID: callID, AgentKeyID: agentKeyID, BatchID: batchID,
		State: AnalysisRunning, Versions: versions, InputHash: inputHash,
		Findings: []Finding{}, Flows: []Flow{}, StartedAt: startedAt,
	}
}

// Complete fills the derived fields of a finished analysis.
func (analysis *Analysis) Complete(findings []Finding, flows []Flow, coverage Coverage, model *ModelLabels, modelState string) {
	analysis.Findings = NormalizeFindings(findings)
	analysis.Flows = flows
	analysis.Coverage = coverage
	analysis.Model = model
	analysis.ModelState = modelState
	analysis.Verdict = DeriveVerdict(analysis.Findings, coverage, model, modelState)
	analysis.State = AnalysisCompleted
	finished := time.Now().UTC()
	analysis.FinishedAt = &finished
}

func (analysis *Analysis) Fail(err string) {
	analysis.State = AnalysisFailed
	analysis.ModelState = ModelFailed
	analysis.Error = err
	analysis.Coverage.Complete = false
	analysis.Verdict = Verdict{Value: VerdictUnknown, Reason: "analysis_failed"}
	finished := time.Now().UTC()
	analysis.FinishedAt = &finished
}

// DeriveVerdict applies one rule set everywhere: an analysis that did not see enough is unknown,
// never safe.
func DeriveVerdict(findings []Finding, coverage Coverage, model *ModelLabels, modelState string) Verdict {
	highest := SeverityInfo
	incomplete := false
	for _, finding := range findings {
		if finding.Code == CodeAnalysisIncomplete {
			incomplete = true
			continue
		}
		if SeverityRank(finding.Severity) > SeverityRank(highest) {
			highest = finding.Severity
		}
	}
	if highest == SeverityHigh {
		return Verdict{Value: VerdictUnsafe, Confidence: 0.95, Reason: "high_severity_finding"}
	}
	if incomplete || !coverage.Complete {
		return Verdict{Value: VerdictUnknown, Confidence: 0, Reason: "coverage_incomplete"}
	}
	if model != nil && model.Unsafe && model.Probability >= 0.7 &&
		(!model.ConfidenceKnown || model.Confidence >= 0.6) {
		return Verdict{Value: VerdictUnsafe, Confidence: model.Probability, Reason: "decision_model_unsafe"}
	}
	if modelState == ModelFailed || modelState == ModelBudgetExceeded {
		return Verdict{Value: VerdictUnknown, Confidence: 0, Reason: modelState}
	}
	// A high-probability answer with low confidence is not proof of safety either.
	if model != nil && model.Probability >= 0.3 {
		return Verdict{Value: VerdictUnknown, Confidence: 0, Reason: "decision_model_uncertain"}
	}
	if highest == SeverityMedium {
		return Verdict{Value: VerdictUnknown, Confidence: 0, Reason: "medium_severity_finding"}
	}
	return Verdict{Value: VerdictSafe, Confidence: 1, Reason: "no_finding"}
}

// InputHash hashes only what a judgement depends on. Timestamps, the current call and analysis
// ids, locators and write order are excluded; the previous call id is part of the judgement
// because it changes whether a tool result is treated as a continuation.
func InputHash(trace *observation.Trace) string {
	if trace == nil {
		return ""
	}
	type hashCall struct {
		Name      string             `json:"name"`
		Kind      string             `json:"kind"`
		Arguments string             `json:"arguments"`
		Target    observation.Target `json:"target"`
		Sink      observation.Sink   `json:"sink"`
	}
	type hashResult struct {
		Text    string `json:"text"`
		IsError bool   `json:"is_error"`
	}
	type hashItem struct {
		Kind       observation.ItemKind `json:"kind"`
		Origin     observation.Origin   `json:"origin"`
		Trust      observation.Trust    `json:"trust"`
		Text       string               `json:"text,omitempty"`
		ToolCall   *hashCall            `json:"tool_call,omitempty"`
		ToolResult *hashResult          `json:"tool_result,omitempty"`
	}
	type hashTurn struct {
		Role  observation.Role `json:"role"`
		Items []hashItem       `json:"items"`
	}
	type hashTool struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters,omitempty"`
	}
	turns := make([]hashTurn, 0, len(trace.Turns))
	for _, turn := range trace.Turns {
		entry := hashTurn{Role: turn.Role, Items: make([]hashItem, 0, len(turn.Items))}
		for _, item := range turn.Items {
			hash := hashItem{Kind: item.Kind, Origin: item.Origin, Trust: item.Trust, Text: item.Text.Raw}
			if item.ToolCall != nil {
				hash.ToolCall = &hashCall{
					Name: item.ToolCall.Name, Kind: item.ToolCall.Kind, Arguments: item.ToolCall.Arguments.Raw,
					Target: item.ToolCall.Target, Sink: item.ToolCall.Sink,
				}
			}
			if item.ToolResult != nil {
				hash.ToolResult = &hashResult{Text: observation.ToolResultText(item.ToolResult), IsError: item.ToolResult.IsError}
			}
			entry.Items = append(entry.Items, hash)
		}
		turns = append(turns, entry)
	}
	tools := make([]hashTool, 0, len(trace.Tools))
	for _, tool := range trace.Tools {
		tools = append(tools, hashTool{Name: tool.Name, Kind: tool.Kind, Description: tool.Description, Parameters: tool.Parameters})
	}
	canonical := struct {
		Task           string                `json:"task"`
		Principal      observation.Principal `json:"principal"`
		Turns          []hashTurn            `json:"turns"`
		Tools          []hashTool            `json:"tools"`
		Context        map[string]any        `json:"context,omitempty"`
		Outcome        observation.Outcome   `json:"outcome"`
		PreviousCallID string                `json:"previous_call_id,omitempty"`
	}{trace.Task, trace.Principal, turns, tools, trace.Context, trace.Outcome, trace.PreviousCallID}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

type FindingChange struct {
	Fingerprint string   `json:"fingerprint"`
	Code        string   `json:"code"`
	Before      *Finding `json:"before,omitempty"`
	After       *Finding `json:"after,omitempty"`
}

type AnalysisDiff struct {
	BeforeID    string          `json:"before_id"`
	AfterID     string          `json:"after_id"`
	Comparable  bool            `json:"comparable"`
	Reason      string          `json:"reason,omitempty"`
	Added       []Finding       `json:"added"`
	Removed     []Finding       `json:"removed"`
	Changed     []FindingChange `json:"changed"`
	BeforeCount int             `json:"before_count"`
	AfterCount  int             `json:"after_count"`
}

// CompareAnalyses aligns two analyses by fingerprint. A different input or a different adapter,
// detector, catalogue or taxonomy version makes the comparison display-only: the counts are shown,
// the diff is not claimed.
func CompareAnalyses(before, after *Analysis) AnalysisDiff {
	diff := AnalysisDiff{BeforeID: before.ID, AfterID: after.ID, BeforeCount: len(before.Findings), AfterCount: len(after.Findings)}
	diff.Added, diff.Removed, diff.Changed = []Finding{}, []Finding{}, []FindingChange{}
	switch {
	case before.ID == after.ID:
		diff.Reason = "same_analysis"
		return diff
	case before.CallID != after.CallID:
		diff.Reason = "different_call"
		return diff
	case before.InputHash == "" || after.InputHash == "":
		diff.Reason = "input_unavailable"
		return diff
	case before.InputHash != after.InputHash:
		diff.Reason = "different_input"
		return diff
	case before.Versions.Adapter != after.Versions.Adapter ||
		before.Versions.Detectors != after.Versions.Detectors ||
		before.Versions.Catalogue != after.Versions.Catalogue ||
		before.Versions.Taxonomy != after.Versions.Taxonomy:
		diff.Reason = "different_versions"
		return diff
	}
	diff.Comparable = true
	beforeIndex := map[string]Finding{}
	for _, finding := range before.Findings {
		beforeIndex[finding.Fingerprint] = finding
	}
	afterIndex := map[string]Finding{}
	for _, finding := range after.Findings {
		afterIndex[finding.Fingerprint] = finding
	}
	for fingerprint, finding := range afterIndex {
		previous, found := beforeIndex[fingerprint]
		if !found {
			diff.Added = append(diff.Added, finding)
			continue
		}
		if previous.Severity != finding.Severity || previous.Confidence != finding.Confidence {
			beforeCopy, afterCopy := previous, finding
			diff.Changed = append(diff.Changed, FindingChange{Fingerprint: fingerprint, Code: finding.Code, Before: &beforeCopy, After: &afterCopy})
		}
	}
	for fingerprint, finding := range beforeIndex {
		if _, found := afterIndex[fingerprint]; !found {
			diff.Removed = append(diff.Removed, finding)
		}
	}
	sort.Slice(diff.Added, func(left, right int) bool { return diff.Added[left].Fingerprint < diff.Added[right].Fingerprint })
	sort.Slice(diff.Removed, func(left, right int) bool { return diff.Removed[left].Fingerprint < diff.Removed[right].Fingerprint })
	sort.Slice(diff.Changed, func(left, right int) bool { return diff.Changed[left].Fingerprint < diff.Changed[right].Fingerprint })
	return diff
}
