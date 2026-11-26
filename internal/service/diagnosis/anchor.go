package diagnosis

import (
	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/observation"
)

// anchorFindings resolves each detector position onto the item identity of the rebuilt trace. The
// detectors keep emitting turn/item positions; converting them here means a finding, a flow and a
// transcript node name the same item even when the trace was expanded with ancestor history.
func anchorFindings(trace *observation.Trace, findings []analysis.Finding) {
	for index := range findings {
		finding := &findings[index]
		for position := range finding.Evidence {
			evidence := &finding.Evidence[position]
			evidence.Locator = anchorLocator(trace, evidence.Turn, evidence.Item, evidence.Path, evidence.Locator)
		}
		for position := range finding.Attribution {
			attribution := &finding.Attribution[position]
			attribution.Locator = anchorLocator(trace, attribution.Turn, attribution.Item, "", attribution.Locator)
		}
		if finding.Flow == nil {
			continue
		}
		flow := finding.Flow
		flow.SourceLocator = anchorLocator(trace, flow.SourceTurn, flow.SourceItem, flow.SourcePath, flow.SourceLocator)
		if flow.SourcePath == "" {
			flow.SourcePath = flow.SourceLocator.Path
		}
	}
}

// anchorLocator keeps a locator the detector already built, otherwise resolves the position. A tool
// description has no turn (turn is -1) and lives in the tools section by index.
func anchorLocator(trace *observation.Trace, turn, item int, path string, existing observation.Locator) observation.Locator {
	if existing.CallID != "" {
		if existing.Path == "" {
			existing.Path = path
		}
		return existing
	}
	if trace == nil {
		return existing
	}
	if turn < 0 {
		return observation.Locator{CallID: trace.ID, Section: observation.SectionTools, Ordinal: item, Path: path}
	}
	if turn < len(trace.Turns) && item >= 0 && item < len(trace.Turns[turn].Items) {
		locator := trace.Turns[turn].Items[item].Locator
		if locator.Path == "" {
			locator.Path = path
		}
		return locator
	}
	return existing
}
