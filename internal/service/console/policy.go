package console

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	_ "github.com/yv1ing/SafeAgent/internal/analysis/builtin"
	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

type PolicyService struct {
	store    *repository.Store
	projects access.ProjectAccess
}

func NewPolicyService(store *repository.Store) *PolicyService {
	return &PolicyService{store: store, projects: store}
}

type PolicyVersion struct {
	ID        string
	Version   int64
	Document  policy.Document
	Hash      string
	Note      string
	CreatedBy string
	CreatedAt time.Time
}

func (service *PolicyService) List(ctx context.Context, actor access.Actor, projectID string) ([]PolicyVersion, error) {
	if err := actor.RequireProject(ctx, service.projects, projectID); err != nil {
		return nil, err
	}
	rows, err := service.store.PolicyVersions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	versions := make([]PolicyVersion, 0, len(rows))
	for index := range rows {
		version, err := decode(&rows[index])
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, nil
}

func (service *PolicyService) Active(ctx context.Context, actor access.Actor, projectID string) (PolicyVersion, error) {
	if err := actor.RequireProject(ctx, service.projects, projectID); err != nil {
		return PolicyVersion{}, err
	}
	row, err := service.store.ActivePolicy(ctx, projectID)
	if err != nil {
		return PolicyVersion{}, err
	}
	if row == nil {
		document := policy.DefaultDocument()
		return PolicyVersion{Version: 0, Document: document, Hash: document.Hash()}, nil
	}
	return decode(row)
}

// Save compiles and activates a new version. Policy editing is administrator-only; members can read
// the active document and its history.
func (service *PolicyService) Save(ctx context.Context, actor access.Actor, projectID string, document policy.Document, note string) (PolicyVersion, error) {
	if err := actor.RequireProject(ctx, service.projects, projectID); err != nil {
		return PolicyVersion{}, err
	}
	if err := actor.RequireAdmin(); err != nil {
		return PolicyVersion{}, err
	}
	return service.save(ctx, actor, projectID, document, note)
}

func (service *PolicyService) save(ctx context.Context, actor access.Actor, projectID string, document policy.Document, note string) (PolicyVersion, error) {
	compiled, err := policy.Compile(document)
	if err != nil {
		return PolicyVersion{}, access.Invalid("invalid_policy")
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return PolicyVersion{}, err
	}
	row := &entity.PolicyVersion{
		ID: "pol_" + secure.Random(12), ProjectID: projectID,
		Document: encoded, Hash: compiled.Hash(), Note: note, CreatedBy: actor.UserID, CreatedAt: time.Now().UTC(),
	}
	version, err := service.store.SavePolicyVersion(ctx, row)
	if err != nil {
		return PolicyVersion{}, access.Translate(err)
	}
	row.Version = version
	return decode(row)
}

// Rollback saves an older document as a new version. History is never rewritten.
func (service *PolicyService) Rollback(ctx context.Context, actor access.Actor, projectID string, version int64) (PolicyVersion, error) {
	if err := actor.RequireProject(ctx, service.projects, projectID); err != nil {
		return PolicyVersion{}, err
	}
	if err := actor.RequireAdmin(); err != nil {
		return PolicyVersion{}, err
	}
	row, err := service.store.PolicyVersion(ctx, projectID, version)
	if err != nil {
		return PolicyVersion{}, access.Translate(err)
	}
	previous, err := decode(row)
	if err != nil {
		return PolicyVersion{}, err
	}
	return service.save(ctx, actor, projectID, previous.Document, fmt.Sprintf("restore version %d", version))
}

func (service *PolicyService) Validate(ctx context.Context, actor access.Actor, projectID string, document policy.Document) error {
	if err := actor.RequireProject(ctx, service.projects, projectID); err != nil {
		return err
	}
	if err := actor.RequireAdmin(); err != nil {
		return err
	}
	if _, err := policy.Compile(document); err != nil {
		return access.Invalid("invalid_policy")
	}
	return nil
}

func decode(row *entity.PolicyVersion) (PolicyVersion, error) {
	var document policy.Document
	if err := json.Unmarshal(row.Document, &document); err != nil {
		return PolicyVersion{}, err
	}
	return PolicyVersion{
		ID: row.ID, Version: row.Version, Document: document, Hash: row.Hash,
		Note: row.Note, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt,
	}, nil
}

// PolicyTestResult previews one document: deterministic findings plus the decisions each rule produces.
type PolicyTestResult struct {
	Findings  []analysis.Finding
	Verdict   analysis.Verdict
	Coverage  analysis.Coverage
	Decisions []policy.DecisionResult
}

// Test compiles a document, runs the deterministic detector set over the supplied observation and
// evaluates every rule against it. It stores nothing.
func (service *PolicyService) Test(ctx context.Context, actor access.Actor, projectID string, document policy.Document, trace *observation.Trace) (*PolicyTestResult, error) {
	if err := actor.RequireProject(ctx, service.projects, projectID); err != nil {
		return nil, err
	}
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	compiled, err := policy.Compile(document)
	if err != nil {
		return nil, access.Invalid("invalid_policy")
	}
	if trace == nil {
		return nil, access.Invalid("invalid_request")
	}
	engine := analysis.NewEngine(analysis.DefaultRegistry())
	result := engine.Evaluate(ctx, trace, analysis.Options{})
	coverage := analysis.BuildCoverage(result)
	decisions := []policy.DecisionResult{}
	seen := map[string]bool{}
	evaluate := func(phase string, tool observation.ToolCall, tags observation.ToolTags) error {
		outcome, err := compiled.Evaluate(ctx, policy.Input{
			Phase: phase, Principal: trace.Principal, Tool: tool, Tags: tags, Trace: trace, Findings: result.Findings,
		})
		if err != nil {
			return err
		}
		for _, decision := range outcome.Decisions {
			key := phase + "/" + decision.RuleID
			if seen[key] {
				continue
			}
			seen[key] = true
			decisions = append(decisions, decision)
		}
		return nil
	}
	if err := evaluate(policy.PhaseRequest, observation.ToolCall{}, observation.ToolTags{}); err != nil {
		return nil, err
	}
	for _, ref := range trace.ItemRefs() {
		if ref.Item.ToolCall == nil {
			continue
		}
		tags := observation.ToolTags{}
		if spec, found := trace.ToolSpecByName(ref.Item.ToolCall.Name); found {
			tags = spec.Tags
		}
		if err := evaluate(policy.PhaseToolCall, *ref.Item.ToolCall, tags); err != nil {
			return nil, err
		}
	}
	return &PolicyTestResult{
		Findings: result.Findings, Coverage: coverage,
		Verdict:   analysis.DeriveVerdict(result.Findings, coverage, nil, analysis.ModelDisabled),
		Decisions: decisions,
	}, nil
}
