package console

import (
	"context"
	"encoding/json"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/policy"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	DefaultRetentionDays          = 30
	DefaultAuxiliaryRetentionDays = 3
	DefaultRequestsPerMinute      = 120
	DefaultConcurrency            = 8
	DefaultStreamReviewTimeoutMS  = 300
	DefaultCoverageThreshold      = 0.9
	MaximumRetentionDays          = 3650
	MaximumOutputTokens           = 1 << 20
	MaximumRequestsPerMinute      = 1_000_000
	MaximumConcurrency            = 100_000
	MaximumStreamReviewBytes      = 64 << 10
	MaximumStreamReviewTimeoutMS  = 5_000
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
	AuxiliaryRetentionDays *int
	DefaultRouteID         *string
	RequestsPerMinute      int
	Concurrency            int
	DailyTokenQuota        int64
	DefaultMaxOutputTokens int
	StreamReviewBytes      int
	StreamReviewTimeoutMS  int
	CoverageThreshold      float64
	Version                int64
}

// ProjectCreateInput leaves a field nil to take its default; an explicit zero limit means unlimited.
type ProjectCreateInput struct {
	Name                   string
	RetentionDays          *int
	AuxiliaryRetentionDays *int
	DefaultRouteID         string
	RequestsPerMinute      *int
	Concurrency            *int
	DailyTokenQuota        *int64
	DefaultMaxOutputTokens *int
	StreamReviewBytes      *int
	StreamReviewTimeoutMS  *int
	CoverageThreshold      *float64
}

func (input ProjectCreateInput) resolve() ProjectInput {
	resolved := ProjectInput{
		Name: input.Name, RetentionDays: DefaultRetentionDays,
		AuxiliaryRetentionDays: pointer(DefaultAuxiliaryRetentionDays),
		DefaultRouteID:         &input.DefaultRouteID,
		RequestsPerMinute:      DefaultRequestsPerMinute,
		Concurrency:            DefaultConcurrency, StreamReviewTimeoutMS: DefaultStreamReviewTimeoutMS,
		CoverageThreshold: DefaultCoverageThreshold,
	}
	if input.RetentionDays != nil {
		resolved.RetentionDays = *input.RetentionDays
	}
	if input.AuxiliaryRetentionDays != nil {
		resolved.AuxiliaryRetentionDays = input.AuxiliaryRetentionDays
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
	if input.StreamReviewBytes != nil {
		resolved.StreamReviewBytes = *input.StreamReviewBytes
	}
	if input.StreamReviewTimeoutMS != nil {
		resolved.StreamReviewTimeoutMS = *input.StreamReviewTimeoutMS
	}
	if input.CoverageThreshold != nil {
		resolved.CoverageThreshold = *input.CoverageThreshold
	}
	return resolved
}

func (input ProjectInput) normalize() (ProjectInput, error) {
	name, err := access.RequireName(input.Name, "invalid_name")
	if err != nil {
		return input, err
	}
	input.Name = name
	if input.RetentionDays < 1 || input.RetentionDays > MaximumRetentionDays {
		return input, access.Invalid("invalid_retention_days")
	}
	if input.AuxiliaryRetentionDays != nil && (*input.AuxiliaryRetentionDays < 0 || *input.AuxiliaryRetentionDays > MaximumRetentionDays) {
		return input, access.Invalid("invalid_retention_days")
	}
	if input.RequestsPerMinute < 0 || input.RequestsPerMinute > MaximumRequestsPerMinute ||
		input.Concurrency < 0 || input.Concurrency > MaximumConcurrency || input.DailyTokenQuota < 0 || input.DefaultMaxOutputTokens < 0 || input.DefaultMaxOutputTokens > MaximumOutputTokens {
		return input, access.Invalid("invalid_limit")
	}
	if input.StreamReviewBytes < 0 || input.StreamReviewBytes > MaximumStreamReviewBytes ||
		input.StreamReviewTimeoutMS < 0 || input.StreamReviewTimeoutMS > MaximumStreamReviewTimeoutMS {
		return input, access.Invalid("invalid_stream_review")
	}
	if input.StreamReviewTimeoutMS == 0 {
		input.StreamReviewTimeoutMS = DefaultStreamReviewTimeoutMS
	}
	if input.CoverageThreshold <= 0 || input.CoverageThreshold > 1 {
		input.CoverageThreshold = DefaultCoverageThreshold
	}
	return input, nil
}

func (input ProjectInput) entity() entity.Project {
	auxiliaryRetention := DefaultAuxiliaryRetentionDays
	if input.AuxiliaryRetentionDays != nil {
		auxiliaryRetention = *input.AuxiliaryRetentionDays
	}
	defaultRouteID := ""
	if input.DefaultRouteID != nil {
		defaultRouteID = *input.DefaultRouteID
	}
	return entity.Project{
		Name: input.Name, RetentionDays: input.RetentionDays,
		AuxiliaryRetentionDays: auxiliaryRetention, DefaultRouteID: defaultRouteID,
		RequestsPerMinute: input.RequestsPerMinute,
		Concurrency:       input.Concurrency, DailyTokenQuota: input.DailyTokenQuota,
		DefaultMaxOutputTokens: input.DefaultMaxOutputTokens, StreamReviewBytes: input.StreamReviewBytes,
		StreamReviewTimeoutMS: input.StreamReviewTimeoutMS, CoverageThreshold: input.CoverageThreshold,
	}
}

// changesAdministratorFields reports whether input alters anything only administrators may set.
func (input ProjectInput) changesAdministratorFields(existing *entity.Project) bool {
	auxiliaryChanged := input.AuxiliaryRetentionDays != nil && *input.AuxiliaryRetentionDays != existing.AuxiliaryRetentionDays
	routeChanged := input.DefaultRouteID != nil && *input.DefaultRouteID != existing.DefaultRouteID
	return input.RetentionDays != existing.RetentionDays ||
		auxiliaryChanged || routeChanged ||
		input.RequestsPerMinute != existing.RequestsPerMinute ||
		input.Concurrency != existing.Concurrency ||
		input.DailyTokenQuota != existing.DailyTokenQuota ||
		input.DefaultMaxOutputTokens != existing.DefaultMaxOutputTokens ||
		input.StreamReviewBytes != existing.StreamReviewBytes ||
		input.StreamReviewTimeoutMS != existing.StreamReviewTimeoutMS ||
		input.CoverageThreshold != existing.CoverageThreshold
}

func (service *ProjectService) List(ctx context.Context, actor access.Actor) ([]entity.Project, error) {
	return service.store.VisibleProjects(ctx, actor.UserID, actor.Administrator)
}

func (service *ProjectService) Get(ctx context.Context, actor access.Actor, projectID string) (*entity.Project, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	project, err := service.store.Project(ctx, projectID)
	return project, access.Translate(err)
}

func (service *ProjectService) Create(ctx context.Context, actor access.Actor, input ProjectCreateInput) (*entity.Project, error) {
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
		return nil, access.Translate(err)
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

// inherit fills the fields a partial transport update omits, so an omitted auxiliary retention or
// default route keeps its stored value instead of clearing it.
func (input ProjectInput) inherit(existing *entity.Project) ProjectInput {
	if input.AuxiliaryRetentionDays == nil {
		value := existing.AuxiliaryRetentionDays
		input.AuxiliaryRetentionDays = &value
	}
	if input.DefaultRouteID == nil {
		value := existing.DefaultRouteID
		input.DefaultRouteID = &value
	}
	return input
}

func pointer(value int) *int { return &value }

// Update changes a project. Members may rename it; limits and retention stay
// with administrators, and a member who submits a different value for them is refused.
func (service *ProjectService) Update(ctx context.Context, actor access.Actor, projectID string, input ProjectInput) (*entity.Project, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if err := access.RequireVersion(input.Version); err != nil {
		return nil, err
	}
	normalized, err := input.normalize()
	if err != nil {
		return nil, err
	}
	existing, err := service.store.Project(ctx, projectID)
	if err != nil {
		return nil, access.Translate(err)
	}
	normalized = normalized.inherit(existing)
	if !actor.Administrator && normalized.changesAdministratorFields(existing) {
		return nil, access.Forbidden()
	}
	updated, err := service.store.UpdateProject(ctx, projectID, normalized.entity(), normalized.Version)
	return updated, access.Translate(err)
}

func (service *ProjectService) Delete(ctx context.Context, actor access.Actor, projectID string) (*entity.Project, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	project, err := service.store.Project(ctx, projectID)
	if err != nil {
		return nil, access.Translate(err)
	}
	return project, access.Translate(service.store.DeleteProject(ctx, projectID))
}
