// Package builtin contains the deterministic detectors shipped with SafeAgent. It is imported for
// its registration side effect; the engine discovers detectors through the registry.
package builtin

import (
	"context"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/observation"
)

const version = "2.0.0"

type evaluateFunc func(ctx context.Context, trace *observation.Trace) []analysis.Finding

type detector struct {
	id       string
	evaluate evaluateFunc
}

func (d *detector) ID() string      { return d.id }
func (d *detector) Version() string { return version }
func (d *detector) Evaluate(ctx context.Context, trace *observation.Trace) []analysis.Finding {
	return d.evaluate(ctx, trace)
}

func init() {
	analysis.MustRegister(&detector{id: "schema", evaluate: evaluateSchema})
	analysis.MustRegister(&detector{id: "content", evaluate: evaluateContent})
	analysis.MustRegister(&detector{id: "flow", evaluate: evaluateFlow})
}

func makeFinding(detectorID, code string, ref observation.ItemRef, origin observation.Origin, sink observation.Sink, section, path string, value observation.Value, start, end, ordinal int) analysis.Finding {
	finding := analysis.NewFinding(
		analysis.DetectorRef{ID: detectorID, Version: version}, code, origin, sink, "", confidenceFor(code),
		locationKey(ref, path), ordinal, analysis.EvidenceAt(ref, section, path, value, start, end),
	)
	finding.Attribution = []analysis.Attribution{{
		Turn: ref.Turn, Item: ref.Position, ToolCallID: ref.Item.Locator.CallID, Source: section,
	}}
	return finding
}

func makeWholeFinding(detectorID, code string, ref observation.ItemRef, origin observation.Origin, sink observation.Sink, section, path string, value observation.Value) analysis.Finding {
	start, end := wholeValue(value)
	return makeFinding(detectorID, code, ref, origin, sink, section, path, value, start, end, 0)
}

// hasSecretPlaceholder reports whether the shown view contains a placeholder issued from a secret
// detector. A placeholder for an e-mail address is still sensitive data, but it is not a credential
// and must not turn every redacted value into a secret-flow finding.
func hasSecretPlaceholder(value observation.Value) bool {
	if value.Shown == value.Raw {
		return false
	}
	for _, span := range value.Redactions {
		if span.Secret && span.Label != "" && strings.Contains(value.Shown, "["+span.Label+"_") {
			return true
		}
	}
	return false
}

func confidenceFor(code string) float64 {
	definition, found := analysis.CodeDefinitionFor(code)
	if !found {
		return 0.5
	}
	switch definition.Severity {
	case analysis.SeverityHigh:
		return 0.95
	case analysis.SeverityMedium:
		return 0.8
	}
	return 0.6
}

func locationKey(ref observation.ItemRef, path string) string {
	return strconv.Itoa(ref.Turn) + "/" + strconv.Itoa(ref.Position) + "/" + ref.Item.Locator.Path + "/" + path + "/" + ref.Item.Locator.CallID
}

// wholeValue locates a finding over the entire value, used when the check has no narrower match.
func wholeValue(value observation.Value) (int, int) {
	return 0, len(value.Raw)
}

func matchOffsets(text, match string) (int, int) {
	if match == "" {
		return 0, len(text)
	}
	if start := strings.Index(text, match); start >= 0 {
		return start, start + len(match)
	}
	return 0, len(text)
}
