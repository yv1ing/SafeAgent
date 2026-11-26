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
	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observation/adapter"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	ScopeLimitCalls = 5000
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
func (service *Service) RedactorForCall(ctx context.Context, project *entity.Project, callID string) (adapter.Redactor, error) {
	rules, err := service.redactionRules(ctx, project, callID)
	if err != nil {
		return nil, err
	}
	engine, err := redact.NewEngine(rules, service.store.Secret, project.ID)
	if err != nil {
		return nil, err
	}
	return adapter.ScopeRedactor(engine.NewScope()), nil
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

// Rebuild is one call rebuilt from its stored events. Trace is the ancestor-expanded observation a
// diagnosis reads; Own holds only the items this call stored; Request and Response are the decoded
// payloads; Origins carries one locator per leading expanded-history item; Hits are the call's own
// relocated redaction hits.
type Rebuild struct {
	Trace     *observation.Trace
	Own       *observation.Trace
	Request   auditevent.Request
	Response  *auditevent.Response
	Origins   []observation.Locator
	Hits      []auditevent.Hit
	Truncated bool
}

// BuildCallTrace rebuilds one call addressed by project and call id.
func (service *Service) BuildCallTrace(ctx context.Context, projectID, callID string) (Rebuild, error) {
	call, err := service.store.Call(ctx, projectID, callID)
	if err != nil {
		return Rebuild{}, err
	}
	return service.BuildTrace(ctx, call)
}

// BuildTrace is the only session rebuild entry point. The online path produces the same observation
// shapes from the same adapter, so an online verdict and a stored finding describe the same node.
func (service *Service) BuildTrace(ctx context.Context, call *entity.Call) (Rebuild, error) {
	project, err := service.store.Project(ctx, call.ProjectID)
	if err != nil {
		return Rebuild{}, err
	}
	key, err := service.store.AgentKey(ctx, call.ProjectID, call.AgentKeyID)
	if err != nil {
		return Rebuild{}, err
	}
	events, err := service.store.CallEvents(ctx, call.ProjectID, call.ID)
	if err != nil {
		return Rebuild{}, err
	}
	var request auditevent.Request
	var response *auditevent.Response
	requestFound := false
	for _, event := range events {
		decoded, err := auditevent.Decode(event.Kind, event.Payload)
		if err != nil {
			return Rebuild{}, fmt.Errorf("%s event of %s: %w", event.Kind, call.ID, err)
		}
		switch event.Kind {
		case auditevent.KindRequest:
			if decoded.Request != nil {
				request = *decoded.Request
			}
			requestFound = true
		case auditevent.KindResponse:
			response = decoded.Response
		}
	}
	if !requestFound {
		return Rebuild{}, fmt.Errorf("request event of %s is missing", call.ID)
	}
	// A completed protocol exchange of a main call carries a response event. A blocked request
	// never reached the upstream, so its completed row legitimately has none.
	completedExchange := call.State == "completed" && (call.Outcome == "" || call.Outcome == classify.OutcomeOK)
	if call.Lane == classify.LaneMain && completedExchange && response == nil {
		return Rebuild{}, fmt.Errorf("response event of completed call %s is missing", call.ID)
	}
	rules, err := service.redactionRules(ctx, project, call.ID)
	if err != nil {
		return Rebuild{}, err
	}
	engine, err := redact.NewEngine(rules, service.store.Secret, project.ID)
	if err != nil {
		return Rebuild{}, err
	}
	options := adapter.Options{
		TraceID: call.ID,
		CallID:  call.ID,
		Principal: observation.Principal{
			ProjectID: call.ProjectID, AgentKeyID: call.AgentKeyID,
			TrustLevel: key.TrustLevel, Role: key.Role,
		},
		Redactor: adapter.ScopeRedactor(engine.NewScope()),
	}
	own := adapter.Stored(request, response, options)
	if own.PreviousCallID == "" {
		own.PreviousCallID = call.ParentCallID
	}
	rebuild := Rebuild{
		Trace: own, Own: own, Request: request, Response: response,
		Hits: append([]auditevent.Hit{}, request.Hits...),
	}
	if call.ParentCallID == "" {
		return rebuild, nil
	}
	expanded, origins, truncated := service.expandHistory(ctx, call, request)
	if truncated {
		own.Context["history_truncated"] = true
	}
	expandedOptions := options
	expandedOptions.Origins = origins
	trace := adapter.Stored(expanded, response, expandedOptions)
	if trace.PreviousCallID == "" {
		trace.PreviousCallID = call.ParentCallID
	}
	if truncated {
		trace.Context["history_truncated"] = true
	}
	rebuild.Trace, rebuild.Origins, rebuild.Truncated = trace, origins, truncated
	return rebuild, nil
}

// expandHistory prepends the ancestor conversation to the call's own messages. Requests are stored
// as the messages a call added, so without this step an asynchronous analysis would see a different
// trajectory from the online gate, which receives the client's full history.
func (service *Service) expandHistory(ctx context.Context, call *entity.Call, request auditevent.Request) (auditevent.Request, []observation.Locator, bool) {
	if call.ParentCallID == "" {
		return request, nil, false
	}
	chain, err := service.store.CallChain(ctx, call.ID)
	if err != nil {
		return request, nil, true
	}
	if len(chain) == 0 {
		return request, nil, true
	}
	if len(chain) == 1 {
		// The call names a parent but the parent was pruned or never existed.
		return request, nil, chain[0].ParentCallID != ""
	}
	ancestors := chain[:len(chain)-1]
	// A non-empty parent id on the oldest loaded ancestor means the recursive walk stopped at
	// its depth limit or a pruned link; the history is incomplete even when the request exists.
	truncated := chain[0].ParentCallID != ""
	ids := make([]string, len(ancestors))
	for index, ancestor := range ancestors {
		ids[index] = ancestor.ID
	}
	events, err := service.store.EventsOfKinds(ctx, ids, auditevent.KindRequest, auditevent.KindResponse)
	if err != nil {
		return request, nil, true
	}
	requests := map[string]auditevent.Request{}
	responses := map[string]*auditevent.Response{}
	for _, event := range events {
		decoded, err := auditevent.Decode(event.Kind, event.Payload)
		if err != nil {
			continue
		}
		switch event.Kind {
		case auditevent.KindRequest:
			if decoded.Request != nil {
				requests[event.CallID] = *decoded.Request
			}
		case auditevent.KindResponse:
			responses[event.CallID] = decoded.Response
		}
	}
	entries := make([]adapter.Ancestor, 0, len(ancestors))
	for _, ancestor := range ancestors {
		record, found := requests[ancestor.ID]
		if !found {
			truncated = true
			continue
		}
		entries = append(entries, adapter.Ancestor{Found: true, CallID: ancestor.ID, Request: record, Response: responses[ancestor.ID]})
	}
	entries, trimmed := adapter.TrimAncestors(entries)
	expanded, origins, missing := adapter.History(request, entries)
	return expanded, origins, truncated || trimmed || missing
}

// AnalyzeCall diagnoses one call. The identity (call, input hash, detector set, decision model) is
// unique, so a job retry returns the existing result instead of writing a duplicate.
func (service *Service) AnalyzeCall(ctx context.Context, actor access.Actor, projectID, callID, batchID string) (*analysis.Analysis, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	return service.analyzeCall(ctx, projectID, callID, batchID)
}

func (service *Service) analyzeCall(ctx context.Context, projectID, callID, batchID string) (*analysis.Analysis, error) {
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
		Adapter: adapter.AdapterVersion, Detectors: detectorVersion,
		Catalogue: analysis.CatalogueVersion, Taxonomy: analysis.TaxonomyVersion,
		DecisionModel: service.decisionModelName(ctx, projectID),
	}
	rebuild, err := service.BuildTrace(ctx, call)
	if err != nil {
		return service.recordFailure(ctx, call, batchID, versions, "", started, err)
	}
	trace := rebuild.Trace
	inputHash := analysis.InputHash(trace)
	if existing, err := service.store.AnalysisByIdentity(ctx, call.ID, inputHash, detectorVersion, versions.DecisionModel); err != nil {
		return nil, err
	} else if existing != nil {
		view, err := service.analysisView(ctx, projectID, existing.ID)
		if err != nil {
			return nil, err
		}
		return view.Analysis, nil
	}
	record := analysis.NewAnalysis(service.ids(), projectID, call.ID, call.AgentKeyID, batchID, versions, inputHash, started)
	options := analysis.Options{}
	if rebuild.Truncated {
		options.CoverageErrors = []string{"history_truncated"}
	}
	result := service.engine.Evaluate(ctx, trace, options)
	anchorFindings(trace, result.Findings)
	coverage := analysis.BuildCoverage(result)
	labels, modelState, modelErr := service.Evaluate(ctx, projectID, trace)
	if modelErr != nil {
		service.logger.Error("decision model failed", "call", call.ID, "error", modelErr)
	}
	record.Complete(result.Findings, flowsOf(result.Findings), coverage, labels, modelState)
	if err := service.save(ctx, record, trace); err != nil {
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
	view, err := service.analysisView(ctx, projectID, stored.ID)
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
	if err := service.save(ctx, record, nil); err != nil {
		return nil, err
	}
	return service.storedAnalysis(ctx, call.ProjectID, call.ID, inputHash, versions.Detectors, versions.DecisionModel, record)
}

func (service *Service) save(ctx context.Context, record *analysis.Analysis, trace *observation.Trace) error {
	row := analysisRow(record)
	findings := findingRows(record, trace)
	flows := flowRows(record, trace)
	return service.store.SaveAnalysis(ctx, row, findings, flows)
}

// flowAnalysis is a convenience wrapper used by the console: one analysis with its findings.
type AnalysisView struct {
	Analysis *analysis.Analysis
	Findings []analysis.Finding
	Flows    []analysis.Flow
}

func (service *Service) AnalysisView(ctx context.Context, actor access.Actor, projectID, analysisID string) (*AnalysisView, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	return service.analysisView(ctx, projectID, analysisID)
}

func (service *Service) analysisView(ctx context.Context, projectID, analysisID string) (*AnalysisView, error) {
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

func (service *Service) AnalysesOfCall(ctx context.Context, actor access.Actor, projectID, callID string) ([]*AnalysisView, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	rows, err := service.store.AnalysesOfCall(ctx, projectID, callID, 20)
	if err != nil {
		return nil, err
	}
	views := make([]*AnalysisView, 0, len(rows))
	for index := range rows {
		view, err := service.analysisView(ctx, projectID, rows[index].ID)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (service *Service) Diff(ctx context.Context, actor access.Actor, projectID, beforeID, afterID string) (analysis.AnalysisDiff, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return analysis.AnalysisDiff{}, err
	}
	before, err := service.analysisView(ctx, projectID, beforeID)
	if err != nil {
		return analysis.AnalysisDiff{}, err
	}
	after, err := service.analysisView(ctx, projectID, afterID)
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
	if flow.SourceLocator.CallID != "" || flow.SourceLocator.Section != "" {
		return fmt.Sprintf("%s/%s/%d/%s/%s/%s/%s", flow.SourceLocator.CallID, flow.SourceLocator.Section,
			flow.SourceLocator.Ordinal, flow.SourceOrigin, flow.SinkToolCallID, flow.Relation, flow.ContentHash)
	}
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
