// Package diagnosis runs the asynchronous analysis of one call. It rebuilds the trace from stored
// events, runs every detector and the optional decision model, and stores an immutable analysis.
// It never applies the online policy: enforcement belongs to the gateway, diagnosis belongs here.
package diagnosis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	_ "github.com/yv1ing/SafeAgent/internal/analysis/builtin"
	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observe"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	ScopeLimitCalls     = 5000
	maximumHistoryBytes = 32 << 20
)

var (
	ErrScopeTooLarge = errors.New("scope_too_large")
	ErrCallRunning   = errors.New("the call has not finished")
)

type Service struct {
	store    *repository.Store
	engine   *analysis.Engine
	logger   *slog.Logger
	ids      func() string
	decision decisionCache
}

type Options struct {
	Logger   *slog.Logger
	Registry *analysis.Registry
}

func New(store *repository.Store, options Options) *Service {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store: store, engine: analysis.NewEngine(options.Registry), logger: logger,
		ids:      func() string { return "an_" + secure.Random(12) },
		decision: decisionCache{entries: map[string]decisionEntry{}},
	}
}

// RedactorForCall uses the call's immutable redaction snapshot when one exists. A rule edited after
// the call was admitted must not change how that historical call is shown or analyzed.
func (service *Service) RedactorForCall(ctx context.Context, project *entity.Project, callID string) (observe.Redactor, func() []redact.Hit, error) {
	rules, err := service.redactionRules(ctx, project, callID)
	if err != nil {
		return nil, nil, err
	}
	engine, err := redact.NewEngine(rules, service.store.Secret, project.ID)
	if err != nil {
		return nil, nil, err
	}
	scope := engine.NewScope()
	hits := []redact.Hit{}
	redactor := func(text, path string) (string, []observation.Span) {
		shown, result := scope.Replace(text, path)
		if result == nil {
			return shown, nil
		}
		if result.Blocked {
			// Never fall back to the raw text: a member or a redacted decision-model view must
			// see that the value was withheld, not the value itself.
			return "[blocked]", []observation.Span{{Start: 0, End: len(text), Label: "blocked"}}
		}
		spans := make([]observation.Span, 0, len(result.Hits))
		for _, hit := range result.Hits {
			hits = append(hits, hit)
			spans = append(spans, observation.Span{Start: hit.Start, End: hit.End, Label: hit.Label, Secret: hit.Secret})
		}
		return shown, spans
	}
	return redactor, func() []redact.Hit { return hits }, nil
}

func (service *Service) redactionRules(ctx context.Context, project *entity.Project, callID string) ([]redact.Rule, error) {
	if callID != "" {
		snapshot, err := service.store.SnapshotOfCall(ctx, callID)
		if err != nil {
			return nil, err
		}
		if snapshot != nil {
			var stored []redact.Rule
			if err := json.Unmarshal(snapshot.Rules, &stored); err != nil {
				return nil, fmt.Errorf("redaction snapshot of call %s is unreadable: %w", callID, err)
			}
			return stored, nil
		}
	}
	rules, err := service.store.Rules(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	converted := make([]redact.Rule, 0, len(rules))
	for _, rule := range rules {
		converted = append(converted, redact.Rule{
			Kind: redact.RuleKind(rule.Kind), Detector: rule.Detector, Pattern: rule.Pattern,
			Capture: rule.Capture, Label: rule.Label, Action: redact.Action(rule.Action), Enabled: rule.Enabled,
		})
	}
	return converted, nil
}

// BuildTrace rebuilds the observation of one call from its stored events, including the ancestor
// conversation and the principal of the agent key that made the call.
func (service *Service) BuildTrace(ctx context.Context, call *entity.Call) (*observation.Trace, auditevent.Request, *auditevent.Response, []redact.Hit, error) {
	project, err := service.store.Project(ctx, call.ProjectID)
	if err != nil {
		return nil, auditevent.Request{}, nil, nil, err
	}
	key, err := service.store.AgentKey(ctx, call.ProjectID, call.AgentKeyID)
	if err != nil {
		return nil, auditevent.Request{}, nil, nil, err
	}
	events, err := service.store.CallEvents(ctx, call.ProjectID, call.ID)
	if err != nil {
		return nil, auditevent.Request{}, nil, nil, err
	}
	var request auditevent.Request
	var response *auditevent.Response
	requestFound := false
	for _, event := range events {
		switch event.Kind {
		case auditevent.KindRequest:
			if err := json.Unmarshal(event.Payload, &request); err != nil {
				return nil, request, response, nil, fmt.Errorf("request event of %s: %w", call.ID, err)
			}
			requestFound = true
		case auditevent.KindResponse:
			decoded := new(auditevent.Response)
			if err := json.Unmarshal(event.Payload, decoded); err != nil {
				return nil, request, response, nil, fmt.Errorf("response event of %s: %w", call.ID, err)
			}
			response = decoded
		}
	}
	if !requestFound {
		return nil, request, response, nil, fmt.Errorf("request event of %s is missing", call.ID)
	}
	if call.State == "completed" && response == nil {
		return nil, request, response, nil, fmt.Errorf("response event of completed call %s is missing", call.ID)
	}
	redactor, hits, err := service.RedactorForCall(ctx, project, call.ID)
	if err != nil {
		return nil, request, response, nil, err
	}
	request, historyTruncated := service.expandHistory(ctx, call, request)
	trace := observe.Stored(request, response, observe.Options{
		TraceID: call.ID,
		Principal: observation.Principal{
			ProjectID: call.ProjectID, AgentKeyID: call.AgentKeyID,
			TrustLevel: key.TrustLevel, Role: key.Role,
		},
		Redactor: redactor,
	})
	if trace.PreviousCallID == "" {
		trace.PreviousCallID = call.ParentCallID
	}
	if historyTruncated {
		trace.Context["history_truncated"] = true
	}
	return trace, request, response, hits(), nil
}

// expandHistory prepends the ancestor conversation to the call's own messages. Requests are stored
// as the messages a call added, so without this step an asynchronous analysis would see a different
// trajectory from the online gate, which receives the client's full history.
func (service *Service) expandHistory(ctx context.Context, call *entity.Call, request auditevent.Request) (auditevent.Request, bool) {
	if call.ParentCallID == "" {
		return request, false
	}
	chain, err := service.store.CallChain(ctx, call.ID)
	if err != nil {
		return request, true
	}
	if len(chain) == 0 {
		return request, true
	}
	if len(chain) == 1 {
		// The call names a parent but the parent was pruned or never existed.
		return request, chain[0].ParentCallID != ""
	}
	ancestors := chain[:len(chain)-1]
	// A non-empty parent id on the oldest loaded ancestor means the recursive walk stopped at
	// its depth limit or a pruned link; the history is incomplete even when the request exists.
	truncated := chain[0].ParentCallID != ""
	if len(ancestors) > 200 {
		ancestors = ancestors[len(ancestors)-200:]
		truncated = true
	}
	// Load from the nearest ancestor backwards. The byte budget bounds memory when a long
	// conversation repeatedly approached max_request_bytes; dropping the oldest turns is visible
	// as history_truncated rather than being presented as complete history.
	entries := make([]observe.Ancestor, 0, len(ancestors))
	total := 0
	for index := len(ancestors) - 1; index >= 0; index-- {
		ancestor := ancestors[index]
		events, err := service.store.EventsOfKinds(ctx, []string{ancestor.ID}, auditevent.KindRequest, auditevent.KindResponse)
		if err != nil {
			return request, true
		}
		var record auditevent.Request
		var response *auditevent.Response
		requestFound := false
		size := 0
		for _, event := range events {
			size += len(event.Payload)
			switch event.Kind {
			case auditevent.KindRequest:
				if json.Unmarshal(event.Payload, &record) == nil {
					requestFound = true
				}
			case auditevent.KindResponse:
				decoded := new(auditevent.Response)
				if json.Unmarshal(event.Payload, decoded) == nil {
					response = decoded
				}
			}
		}
		if total+size > maximumHistoryBytes {
			truncated = true
			break
		}
		total += size
		entries = append([]observe.Ancestor{{Found: requestFound, Request: record, Response: response}}, entries...)
	}
	request, missing := observe.History(request, entries)
	return request, truncated || missing
}

// AnalyzeCall diagnoses one call. The identity (call, input hash, detector set, decision model) is
// unique, so a job retry returns the existing result instead of writing a duplicate.
func (service *Service) AnalyzeCall(ctx context.Context, projectID, callID, batchID string) (*analysis.Analysis, error) {
	call, err := service.store.Call(ctx, projectID, callID)
	if err != nil {
		return nil, err
	}
	if call.State == "running" {
		return nil, ErrCallRunning
	}
	started := time.Now().UTC()
	detectorVersion := service.engine.Version()
	versions := analysis.Versions{
		Adapter: observe.AdapterVersion, Detectors: detectorVersion,
		Catalogue: analysis.CatalogueVersion, Taxonomy: analysis.TaxonomyVersion,
		DecisionModel: service.decisionModelName(ctx, projectID),
	}
	trace, _, _, _, err := service.BuildTrace(ctx, call)
	if err != nil {
		return service.recordFailure(ctx, call, batchID, versions, "", started, err)
	}
	inputHash := analysis.InputHash(trace)
	if existing, err := service.store.AnalysisByIdentity(ctx, call.ID, inputHash, detectorVersion, versions.DecisionModel); err != nil {
		return nil, err
	} else if existing != nil {
		view, err := service.AnalysisView(ctx, projectID, existing.ID)
		if err != nil {
			return nil, err
		}
		return view.Analysis, nil
	}
	record := analysis.NewAnalysis(service.ids(), projectID, call.ID, call.AgentKeyID, batchID, versions, inputHash, started)
	options := analysis.Options{}
	if truncated, _ := trace.Context["history_truncated"].(bool); truncated {
		options.CoverageErrors = []string{"history_truncated"}
	}
	result := service.engine.Evaluate(ctx, trace, options)
	coverage := analysis.BuildCoverage(result)
	labels, modelState, modelErr := service.Evaluate(ctx, projectID, trace)
	if modelErr != nil {
		service.logger.Error("decision model failed", "call", call.ID, "error", modelErr)
	}
	record.Complete(result.Findings, flowsOf(result.Findings), coverage, labels, modelState)
	if err := service.save(ctx, record); err != nil {
		return nil, err
	}
	return service.storedAnalysis(ctx, projectID, call.ID, inputHash, detectorVersion, versions.DecisionModel, record)
}

// storedAnalysis returns the row that actually holds the identity. A concurrent insert can win the
// unique key, and the caller must receive the persisted row rather than the losing candidate.
func (service *Service) storedAnalysis(ctx context.Context, projectID, callID, inputHash, detectorVersion, decisionModel string, candidate *analysis.Analysis) (*analysis.Analysis, error) {
	stored, err := service.store.AnalysisByIdentity(ctx, callID, inputHash, detectorVersion, decisionModel)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return candidate, nil
	}
	view, err := service.AnalysisView(ctx, projectID, stored.ID)
	if err != nil {
		return nil, err
	}
	return view.Analysis, nil
}

func (service *Service) decisionModelName(ctx context.Context, projectID string) string {
	stored, err := service.store.DecisionConfig(ctx, projectID)
	if err != nil || stored == nil || !stored.Enabled {
		return ""
	}
	return stored.Kind + ":" + stored.Model
}

func (service *Service) recordFailure(ctx context.Context, call *entity.Call, batchID string, versions analysis.Versions, inputHash string, started time.Time, cause error) (*analysis.Analysis, error) {
	record := analysis.NewAnalysis(service.ids(), call.ProjectID, call.ID, call.AgentKeyID, batchID, versions, inputHash, started)
	record.Fail(cause.Error())
	if err := service.save(ctx, record); err != nil {
		return nil, err
	}
	return service.storedAnalysis(ctx, call.ProjectID, call.ID, inputHash, versions.Detectors, versions.DecisionModel, record)
}

func (service *Service) save(ctx context.Context, record *analysis.Analysis) error {
	row := analysisRow(record)
	findings := findingRows(record)
	flows := flowRows(record)
	return service.store.SaveAnalysis(ctx, row, findings, flows)
}

// flowAnalysis is a convenience wrapper used by the console: one analysis with its findings.
type AnalysisView struct {
	Analysis *analysis.Analysis
	Findings []analysis.Finding
	Flows    []analysis.Flow
}

func (service *Service) AnalysisView(ctx context.Context, projectID, analysisID string) (*AnalysisView, error) {
	row, err := service.store.Analysis(ctx, projectID, analysisID)
	if err != nil {
		return nil, err
	}
	findings, err := service.store.FindingsOfAnalysis(ctx, analysisID)
	if err != nil {
		return nil, err
	}
	flows, err := service.store.FlowsOfAnalysis(ctx, analysisID)
	if err != nil {
		return nil, err
	}
	return &AnalysisView{Analysis: analysisFromEntity(row, findings, flows), Findings: analysisOfFindings(findings), Flows: analysisOfFlows(flows)}, nil
}

func (service *Service) AnalysesOfCall(ctx context.Context, projectID, callID string) ([]*AnalysisView, error) {
	rows, err := service.store.AnalysesOfCall(ctx, projectID, callID, 20)
	if err != nil {
		return nil, err
	}
	views := make([]*AnalysisView, 0, len(rows))
	for index := range rows {
		view, err := service.AnalysisView(ctx, projectID, rows[index].ID)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (service *Service) Diff(ctx context.Context, projectID, beforeID, afterID string) (analysis.AnalysisDiff, error) {
	before, err := service.AnalysisView(ctx, projectID, beforeID)
	if err != nil {
		return analysis.AnalysisDiff{}, err
	}
	after, err := service.AnalysisView(ctx, projectID, afterID)
	if err != nil {
		return analysis.AnalysisDiff{}, err
	}
	return analysis.CompareAnalyses(before.Analysis, after.Analysis), nil
}

// flowsOf extracts the distinct flows carried by findings.
func flowsOf(findings []analysis.Finding) []analysis.Flow {
	flows := []analysis.Flow{}
	seen := map[string]bool{}
	for _, finding := range findings {
		if finding.Flow == nil {
			continue
		}
		flow := *finding.Flow
		key := flowLocationKey(flow)
		if seen[key] {
			continue
		}
		seen[key] = true
		flows = append(flows, flow)
	}
	return flows
}

func flowLocationKey(flow analysis.Flow) string {
	return fmt.Sprintf("%d/%d/%s/%s/%s/%s", flow.SourceTurn, flow.SourceItem, flow.SourceOrigin, flow.SinkToolCallID, flow.Relation, flow.ContentHash)
}

// ValidatePolicies compiles the newest policy of every project at startup. A project whose document
// cannot be compiled must fail the process rather than discover the problem on its first request.
func (service *Service) ValidatePolicies(ctx context.Context) error {
	policies, err := service.store.ActivePolicies(ctx)
	if err != nil {
		return err
	}
	for index := range policies {
		var document policy.Document
		if err := json.Unmarshal(policies[index].Document, &document); err != nil {
			return fmt.Errorf("project %s policy v%d: %w", policies[index].ProjectID, policies[index].Version, err)
		}
		if _, err := policy.Compile(document); err != nil {
			return fmt.Errorf("project %s policy v%d: %w", policies[index].ProjectID, policies[index].Version, err)
		}
	}
	return nil
}
