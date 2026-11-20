package analysis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yv1ing/SafeAgent/internal/observation"
)

const (
	DefaultDetectorTimeout = 5 * time.Second
	DefaultMaxFindings     = 500
	DefaultMaxItems        = 20_000
)

// Options selects the detectors of one pass. Online passes name the detectors they need; an empty
// list runs the whole set.
type Options struct {
	Only           []string
	MaxFindings    int
	MaxItems       int
	CoverageErrors []string
}

func (options Options) normalized() Options {
	if options.MaxFindings <= 0 {
		options.MaxFindings = DefaultMaxFindings
	}
	if options.MaxItems <= 0 {
		options.MaxItems = DefaultMaxItems
	}
	return options
}

type Result struct {
	Findings []Finding
	Executed []string
	Skipped  []string
	Errors   []string
}

// Engine evaluates a fixed detector set. Detectors are stateless, so the engine itself is safe for
// concurrent use.
type Engine struct {
	registry  *Registry
	detectors []Detector
}

func NewEngine(registry *Registry) *Engine {
	if registry == nil {
		registry = DefaultRegistry()
	}
	return &Engine{registry: registry, detectors: registry.Detectors()}
}

func (engine *Engine) Version() string { return engine.registry.Version() }

func (engine *Engine) Detectors() []Detector { return engine.detectors }

func (engine *Engine) Evaluate(ctx context.Context, trace *observation.Trace, options Options) Result {
	options = options.normalized()
	result := Result{Findings: []Finding{}, Executed: []string{}, Skipped: []string{}, Errors: append([]string{}, options.CoverageErrors...)}
	if trace == nil {
		return result
	}
	if len(trace.Items()) > options.MaxItems {
		result.Errors = append(result.Errors, "trace_too_large")
		result.Findings = append(result.Findings, incompleteFinding(engine.Version(), "trace_too_large"))
		return result
	}
	selected := map[string]bool{}
	for _, id := range options.Only {
		selected[id] = true
	}
	available := map[string]bool{}
	for _, detector := range engine.detectors {
		available[detector.ID()] = true
	}
	for _, id := range options.Only {
		if !available[id] {
			result.Errors = append(result.Errors, "unknown_detector: "+id)
		}
	}
	for _, detector := range engine.detectors {
		if len(selected) > 0 && !selected[detector.ID()] {
			result.Skipped = append(result.Skipped, detector.ID())
			continue
		}
		findings, err := evaluateOne(ctx, detector, trace)
		if err != nil {
			result.Errors = append(result.Errors, detector.ID()+": "+err.Error())
			continue
		}
		for _, finding := range findings {
			if finding.Fingerprint == "" {
				result.Errors = append(result.Errors, detector.ID()+": invalid_finding")
				continue
			}
			result.Findings = append(result.Findings, finding)
		}
		result.Executed = append(result.Executed, detector.ID())
	}
	for _, finding := range result.Findings {
		if finding.Code == CodeAnalysisIncomplete {
			result.Errors = append(result.Errors, "analysis_incomplete")
			break
		}
	}
	result.Findings = NormalizeFindings(result.Findings)
	if len(result.Findings) > options.MaxFindings {
		result.Findings = result.Findings[:options.MaxFindings]
		result.Errors = append(result.Errors, "findings_truncated")
	}
	if len(result.Errors) > 0 && !hasCode(result.Findings, CodeAnalysisIncomplete) {
		result.Findings = append(result.Findings, incompleteFinding(engine.Version(), result.Errors[0]))
	}
	result.Findings = NormalizeFindings(result.Findings)
	return result
}

func hasCode(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func incompleteFinding(version, detail string) Finding {
	finding := NewFinding(
		DetectorRef{ID: "engine", Version: version}, CodeAnalysisIncomplete,
		observation.OriginEnvironment, observation.SinkNone, SeverityInfo, 1,
		"run", 0, Evidence{Section: "run", Path: "coverage", View: string(observation.ViewRedacted), Quote: detail},
	)
	return finding
}

var errDetectorTimeout = errors.New("detector timed out")

func evaluateOne(parent context.Context, detector Detector, trace *observation.Trace) ([]Finding, error) {
	ctx, cancel := context.WithTimeout(parent, DefaultDetectorTimeout)
	defer cancel()
	type outcome struct {
		findings []Finding
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- outcome{err: fmt.Errorf("panic: %v", recovered)}
			}
		}()
		done <- outcome{findings: detector.Evaluate(ctx, trace), err: ctx.Err()}
	}()
	select {
	case result := <-done:
		if result.err != nil {
			return nil, result.err
		}
		return result.findings, nil
	case <-ctx.Done():
		return nil, errDetectorTimeout
	}
}

// Coverage records what actually ran. A safe verdict is only possible when every requested
// detector completed and no finding was discarded for exceeding a cap.
type Coverage struct {
	Evaluated []string `json:"evaluated"`
	Skipped   []string `json:"skipped"`
	Errors    []string `json:"errors"`
	Complete  bool     `json:"complete"`
}

func BuildCoverage(result Result) Coverage {
	return Coverage{
		Evaluated: result.Executed, Skipped: result.Skipped, Errors: result.Errors,
		Complete: len(result.Errors) == 0,
	}
}
