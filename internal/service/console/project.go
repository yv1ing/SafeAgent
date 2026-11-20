package console

import (
	"context"
	"encoding/json"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	DefaultRetentionDays     = 30
	DefaultRequestsPerMinute = 120
	DefaultConcurrency       = 8
	MaximumRetentionDays     = 3650
	MaximumRequestsPerMinute = 1_000_000
	MaximumConcurrency       = 100_000
)

type ProjectService struct {
	store *repository.Store
}

func NewProjectService(store *repository.Store) *ProjectService { return &ProjectService{store: store} }

// ProjectInput is a full project definition. Limits and retention belong to administrators; a member's
// update may carry them only unchanged.
type ProjectInput struct {
	Name                   string
	RetentionDays          int
	RequestsPerMinute      int
	Concurrency            int
	DailyTokenQuota        int64
	DefaultMaxOutputTokens int
	Version                int64
}

// ProjectCreateInput leaves a field nil to take its default; an explicit zero limit means unlimited.
type ProjectCreateInput struct {
	Name                   string
	RetentionDays          *int
	RequestsPerMinute      *int
	Concurrency            *int
	DailyTokenQuota        *int64
	DefaultMaxOutputTokens *int
}

func (input ProjectCreateInput) resolve() ProjectInput {
	resolved := ProjectInput{
		Name: input.Name, RetentionDays: DefaultRetentionDays, RequestsPerMinute: DefaultRequestsPerMinute,
		Concurrency: DefaultConcurrency,
	}
	if input.RetentionDays != nil {
		resolved.RetentionDays = *input.RetentionDays
	}
	if input.RequestsPerMinute != nil {
		resolved.RequestsPerMinute = *input.RequestsPerMinute
	}
	if input.Concurrency != nil {
		resolved.Concurrency = *input.Concurrency
	}
	if input.DailyTokenQuota != nil {
		resolved.DailyTokenQuota = *input.DailyTokenQuota
	}
	if input.DefaultMaxOutputTokens != nil {
		resolved.DefaultMaxOutputTokens = *input.DefaultMaxOutputTokens
	}
	return resolved
}

func (input ProjectInput) normalize() (ProjectInput, error) {
	name, err := RequireName(input.Name, "invalid_name")
	if err != nil {
		return input, err
	}
	input.Name = name
	if input.RetentionDays < 1 || input.RetentionDays > MaximumRetentionDays {
		return input, Invalid("invalid_retention_days")
	}
	if input.RequestsPerMinute < 0 || input.RequestsPerMinute > MaximumRequestsPerMinute ||
		input.Concurrency < 0 || input.Concurrency > MaximumConcurrency || input.DailyTokenQuota < 0 || input.DefaultMaxOutputTokens < 0 || input.DefaultMaxOutputTokens > MaximumOutputTokens {
		return input, Invalid("invalid_limit")
	}
	return input, nil
}

func (input ProjectInput) entity() entity.Project {
	return entity.Project{Name: input.Name, RetentionDays: input.RetentionDays, RequestsPerMinute: input.RequestsPerMinute, Concurrency: input.Concurrency, DailyTokenQuota: input.DailyTokenQuota, DefaultMaxOutputTokens: input.DefaultMaxOutputTokens}
}

// changesAdministratorFields reports whether input alters anything only administrators may set.
func (input ProjectInput) changesAdministratorFields(existing *entity.Project) bool {
	return input.RetentionDays != existing.RetentionDays ||
		input.RequestsPerMinute != existing.RequestsPerMinute ||
		input.Concurrency != existing.Concurrency ||
		input.DailyTokenQuota != existing.DailyTokenQuota ||
		input.DefaultMaxOutputTokens != existing.DefaultMaxOutputTokens
}

func (service *ProjectService) List(ctx context.Context, actor Actor) ([]entity.Project, error) {
	return service.store.VisibleProjects(ctx, actor.UserID, actor.Administrator)
}

// Require checks that the actor may read the project; it is the single gate the transport uses
// before a handler calls another service.
func (service *ProjectService) Require(ctx context.Context, actor Actor, projectID string) error {
	return actor.RequireProject(ctx, service.store, projectID)
}

func (service *ProjectService) Get(ctx context.Context, actor Actor, projectID string) (*entity.Project, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	project, err := service.store.Project(ctx, projectID)
	return project, Translate(err)
}

func (service *ProjectService) Create(ctx context.Context, actor Actor, input ProjectCreateInput) (*entity.Project, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	resolved, err := input.resolve().normalize()
	if err != nil {
		return nil, err
	}
	project := resolved.entity()
	document := policy.DefaultDocument()
	compiled, err := policy.Compile(document)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	created, err := service.store.CreateProject(ctx, &project, defaultRules(), &entity.PolicyVersion{
		ID: "pol_" + secure.Random(12), Document: encoded, Hash: compiled.Hash(),
		Note: "Built-in default", CreatedBy: "system", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, Translate(err)
	}
	return service.store.Project(ctx, created.ID)
}

// defaultRules turns on the detectors that are safe to enable without tuning.
func defaultRules() []entity.Rule {
	rules := make([]entity.Rule, 0, len(redact.Detectors))
	for _, detector := range redact.Detectors {
		if !detector.Default {
			continue
		}
		rules = append(rules, entity.Rule{ID: secure.Random(12), Kind: string(redact.KindBuiltin), Detector: detector.ID, Label: detector.Label, Action: string(redact.ActionReplace), Enabled: true, Version: 1})
	}
	return rules
}

// Update changes a project. Members may rename it; limits and retention stay
// with administrators, and a member who submits a different value for them is refused.
func (service *ProjectService) Update(ctx context.Context, actor Actor, projectID string, input ProjectInput) (*entity.Project, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if err := RequireVersion(input.Version); err != nil {
		return nil, err
	}
	normalized, err := input.normalize()
	if err != nil {
		return nil, err
	}
	if !actor.Administrator {
		existing, err := service.store.Project(ctx, projectID)
		if err != nil {
			return nil, Translate(err)
		}
		if normalized.changesAdministratorFields(existing) {
			return nil, Forbidden()
		}
	}
	updated, err := service.store.UpdateProject(ctx, projectID, normalized.entity(), normalized.Version)
	return updated, Translate(err)
}

func (service *ProjectService) Delete(ctx context.Context, actor Actor, projectID string) (*entity.Project, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	project, err := service.store.Project(ctx, projectID)
	if err != nil {
		return nil, Translate(err)
	}
	return project, Translate(service.store.DeleteProject(ctx, projectID))
}
