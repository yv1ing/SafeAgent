package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/yv1ing/SafeAgent/internal/observation"
)

const (
	SeverityInfo   = "info"
	SeverityMedium = "medium"
	SeverityHigh   = "high"

	VerdictSafe    = "safe"
	VerdictUnsafe  = "unsafe"
	VerdictUnknown = "unknown"
)

func SeverityRank(severity string) int {
	switch severity {
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityInfo:
		return 1
	}
	return 0
}

func StrongerSeverity(left, right string) string {
	if SeverityRank(right) > SeverityRank(left) {
		return right
	}
	return left
}

// Evidence locates a finding inside one text value. Start and End are byte offsets in the view
// named by View; Quote is always the redacted excerpt, so a stored finding cannot leak a secret.
type Evidence struct {
	Turn    int    `json:"turn"`
	Item    int    `json:"item"`
	Section string `json:"section"`
	Path    string `json:"path"`
	View    string `json:"view"`
	Start   int    `json:"start,omitempty"`
	End     int    `json:"end,omitempty"`
	Quote   string `json:"quote,omitempty"`
}

type Attribution struct {
	Turn       int    `json:"turn,omitempty"`
	Item       int    `json:"item,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Source     string `json:"source,omitempty"`
}

type DetectorRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// Finding is one deterministic observation. Its Fingerprint is stable across analyses of the same
// call: it hashes the detector, code, origin, sink, semantic location and match ordinal, never a
// character offset, a timestamp or an analysis id.
type Finding struct {
	Fingerprint string             `json:"fingerprint"`
	Code        string             `json:"code"`
	Origin      observation.Origin `json:"origin"`
	Trust       observation.Trust  `json:"trust"`
	Sink        observation.Sink   `json:"sink"`
	Axes        Axes               `json:"axes"`
	Severity    string             `json:"severity"`
	Confidence  float64            `json:"confidence"`
	Detector    DetectorRef        `json:"detector"`
	Evidence    []Evidence         `json:"evidence,omitempty"`
	Attribution []Attribution      `json:"attribution,omitempty"`
	Flow        *Flow              `json:"flow,omitempty"`
}

// Flow is one edge from external content to a tool call that can act on it.
type Flow struct {
	SourceTurn     int                `json:"source_turn"`
	SourceItem     int                `json:"source_item"`
	SourceOrigin   observation.Origin `json:"source_origin"`
	SourcePath     string             `json:"source_path"`
	SinkToolCallID string             `json:"sink_tool_call_id"`
	Sink           observation.Sink   `json:"sink"`
	Relation       string             `json:"relation"`
	ContentHash    string             `json:"content_hash"`
	Confidence     float64            `json:"confidence"`
	// Matched is the source snippet used for evidence formatting. It is not stored: a persisted
	// flow carries only its hash.
	Matched string `json:"-"`
}

const (
	RelationValueReuse          = "value_reuse"
	RelationInstructionFollowed = "instruction_follow"
)

// NewFinding builds a finding from the catalogue. Unknown codes and origins that the code does not
// permit are programming errors and yield an empty fingerprint, which the engine reports as a
// coverage error instead of silently publishing a finding nobody can interpret.
func NewFinding(detector DetectorRef, code string, origin observation.Origin, sink observation.Sink, severity string, confidence float64, location string, ordinal int, evidence ...Evidence) Finding {
	definition, found := CodeCatalogue[code]
	if !found {
		return Finding{}
	}
	if severity == "" {
		severity = definition.Severity
	}
	if !originAllowed(definition, origin) {
		return Finding{}
	}
	if sink == "" {
		sink = observation.SinkNone
	}
	finding := Finding{
		Code: code, Origin: origin, Trust: trustOf(origin), Sink: sink,
		Axes: AxisFor(code, origin, sink), Severity: severity, Confidence: confidence,
		Detector: detector, Evidence: evidence,
	}
	finding.Fingerprint = fingerprint(detector.ID, code, origin, sink, location, ordinal)
	return finding
}

func originAllowed(definition CodeDefinition, origin observation.Origin) bool {
	if len(definition.Origins) == 0 {
		return true
	}
	for _, candidate := range definition.Origins {
		if candidate == origin {
			return true
		}
	}
	return false
}

func trustOf(origin observation.Origin) observation.Trust {
	switch origin {
	case observation.OriginToolResult, observation.OriginToolDescription:
		return observation.Untrusted
	case observation.OriginEnvironment:
		return observation.Unknown
	}
	return observation.Trusted
}

func fingerprint(detector, code string, origin observation.Origin, sink observation.Sink, location string, ordinal int) string {
	payload, _ := json.Marshal(struct {
		Detector string             `json:"detector"`
		Code     string             `json:"code"`
		Origin   observation.Origin `json:"origin"`
		Sink     observation.Sink   `json:"sink"`
		Location string             `json:"location"`
		Ordinal  int                `json:"ordinal"`
	}{detector, code, origin, sink, location, ordinal})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:16])
}

// EvidenceAt locates a match inside a value. When the match overlaps a redacted span the quote is
// replaced with the label, so the evidence remains useful without revealing what was hidden.
func EvidenceAt(ref observation.ItemRef, section, path string, value observation.Value, start, end int) Evidence {
	if start < 0 {
		start = 0
	}
	if start > len(value.Raw) {
		start = len(value.Raw)
	}
	if end > len(value.Raw) {
		end = len(value.Raw)
	}
	if end < start {
		end = start
	}
	quote := ""
	switch {
	case start == end:
		quote = truncateQuote(value.Shown)
	case overlapsRedaction(value.Redactions, start, end):
		quote = "[redacted]"
	default:
		quote = truncateQuote(value.Raw[start:end])
	}
	return Evidence{
		Turn: ref.Turn, Item: ref.Position, Section: section, Path: path,
		View: string(observation.ViewRaw), Start: start, End: end, Quote: quote,
	}
}

func overlapsRedaction(spans []observation.Span, start, end int) bool {
	for _, span := range spans {
		if start < span.End && end > span.Start {
			return true
		}
	}
	return false
}

// TruncateQuote bounds a quote without splitting a UTF-8 sequence.
func truncateQuote(value string) string {
	runes := []rune(value)
	if len(runes) <= 200 {
		return value
	}
	return string(runes[:200]) + "…"
}

// NormalizeFindings drops malformed findings, fills empty fields and deduplicates by fingerprint.
// A zero confidence is a valid calibrated answer and is preserved.
func NormalizeFindings(findings []Finding) []Finding {
	result := make([]Finding, 0, len(findings))
	seen := map[string]bool{}
	for _, finding := range findings {
		if finding.Fingerprint == "" || finding.Code == "" {
			continue
		}
		if finding.Severity == "" {
			finding.Severity = SeverityInfo
		}
		if seen[finding.Fingerprint] {
			continue
		}
		seen[finding.Fingerprint] = true
		result = append(result, finding)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Severity != result[right].Severity {
			return SeverityRank(result[left].Severity) > SeverityRank(result[right].Severity)
		}
		return result[left].Fingerprint < result[right].Fingerprint
	})
	return result
}

// Summary is the projection shown by call listings and risk pages.
type Summary struct {
	Count       int            `json:"count"`
	MaxSeverity string         `json:"max_severity,omitempty"`
	Codes       map[string]int `json:"codes,omitempty"`
	Sources     map[string]int `json:"sources,omitempty"`
	Modes       map[string]int `json:"modes,omitempty"`
	Harms       map[string]int `json:"harms,omitempty"`
}

func Summarize(findings []Finding) Summary {
	summary := Summary{Codes: map[string]int{}, Sources: map[string]int{}, Modes: map[string]int{}, Harms: map[string]int{}}
	for _, finding := range findings {
		summary.Count++
		summary.MaxSeverity = StrongerSeverity(summary.MaxSeverity, finding.Severity)
		summary.Codes[finding.Code]++
		if finding.Axes.Source != "" {
			summary.Sources[string(finding.Axes.Source)]++
		}
		if finding.Axes.Mode != "" {
			summary.Modes[string(finding.Axes.Mode)]++
		}
		if finding.Axes.Harm != "" {
			summary.Harms[string(finding.Axes.Harm)]++
		}
	}
	return summary
}
