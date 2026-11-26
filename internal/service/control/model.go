package control

import (
	"context"
	"errors"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// ModelInput is the editable part of one catalog model. Formats are not stored: they are derived
// from the provider endpoints and the model kind.
type ModelInput struct {
	ProviderID      string
	UpstreamName    string
	DisplayName     string
	Kind            string
	InputPrice      int64
	OutputPrice     int64
	CacheReadPrice  int64
	CacheWritePrice int64
	Currency        string
	Enabled         bool
	Version         int64
}

// ModelView is a catalog model together with its derived formats.
type ModelView struct {
	Model   entity.Model
	Formats []string
}

type ModelFilter struct {
	ProviderID     string
	Unassociated   bool
	IncludeDeleted bool
}

func deriveModelFormats(spec provider.ProviderSpec, model entity.Model) []model.Format {
	return provider.FormatsForKind(spec.Endpoints, string(model.Kind))
}

func normalizeModel(input ModelInput, spec provider.ProviderSpec) (entity.Model, error) {
	name, err := access.RequireText(input.UpstreamName, MaximumModelName, "invalid_model")
	if err != nil {
		return entity.Model{}, err
	}
	displayName, err := access.OptionalText(input.DisplayName, MaximumDisplayName, "invalid_model")
	if err != nil {
		return entity.Model{}, err
	}
	kind := strings.TrimSpace(input.Kind)
	if !validModelKind(kind) {
		return entity.Model{}, access.Invalid("invalid_kind")
	}
	if len(provider.FormatsForKind(spec.Endpoints, kind)) == 0 {
		return entity.Model{}, access.Invalid("invalid_format")
	}
	if input.InputPrice < 0 || input.OutputPrice < 0 || input.CacheReadPrice < 0 || input.CacheWritePrice < 0 ||
		input.InputPrice > MaximumPriceMicros || input.OutputPrice > MaximumPriceMicros ||
		input.CacheReadPrice > MaximumPriceMicros || input.CacheWritePrice > MaximumPriceMicros {
		return entity.Model{}, access.Invalid("invalid_price")
	}
	currency := input.Currency
	if currency == "" {
		currency = DefaultCurrency
	}
	if !validCurrency(currency) {
		return entity.Model{}, access.Invalid("invalid_currency")
	}
	return entity.Model{
		ProviderID: input.ProviderID, UpstreamName: name, DisplayName: displayName,
		Kind:       entity.ModelKind(kind),
		InputPrice: input.InputPrice, OutputPrice: input.OutputPrice,
		CacheReadPrice: input.CacheReadPrice, CacheWritePrice: input.CacheWritePrice,
		Currency: currency, Enabled: input.Enabled,
	}, nil
}

func (service *Service) modelView(model *entity.Model) ModelView {
	view := ModelView{Model: *model, Formats: []string{}}
	if spec, found := service.providers.Get(model.ProviderID); found {
		view.Formats = formatNames(deriveModelFormats(spec, *model))
	}
	return view
}

func (service *Service) Models(ctx context.Context, actor access.Actor, filter ModelFilter) ([]ModelView, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	models, err := service.store.Models(ctx, repository.ModelFilter{
		ProviderID: filter.ProviderID, Unassociated: filter.Unassociated, IncludeDeleted: filter.IncludeDeleted,
	})
	if err != nil {
		return nil, access.Translate(err)
	}
	views := make([]ModelView, 0, len(models))
	for index := range models {
		views = append(views, service.modelView(&models[index]))
	}
	return views, nil
}

func (service *Service) Model(ctx context.Context, actor access.Actor, id string) (*ModelView, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	model, err := service.store.Model(ctx, id)
	if err != nil {
		return nil, access.Translate(err)
	}
	view := service.modelView(model)
	return &view, nil
}

func (service *Service) CreateModel(ctx context.Context, actor access.Actor, input ModelInput) (*ModelView, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	spec, err := service.spec(input.ProviderID)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeModel(input, spec)
	if err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	created, err := service.store.CreateModel(ctx, &normalized)
	if err != nil {
		return nil, translateModelConflict(err)
	}
	view := service.modelView(created)
	return &view, nil
}

// CreateModels imports a batch; one duplicate name fails the whole batch and is reported by name.
func (service *Service) CreateModels(ctx context.Context, actor access.Actor, inputs []ModelInput) ([]ModelView, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if len(inputs) == 0 || len(inputs) > MaximumBatchModels {
		return nil, access.Invalid("invalid_model")
	}
	models := make([]*entity.Model, 0, len(inputs))
	for _, input := range inputs {
		spec, err := service.spec(input.ProviderID)
		if err != nil {
			return nil, err
		}
		normalized, err := normalizeModel(input, spec)
		if err != nil {
			return nil, err
		}
		models = append(models, &normalized)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.store.CreateModels(ctx, models); err != nil {
		return nil, translateModelConflict(err)
	}
	views := make([]ModelView, 0, len(models))
	for index := range models {
		views = append(views, service.modelView(models[index]))
	}
	return views, nil
}

func (service *Service) UpdateModel(ctx context.Context, actor access.Actor, id string, input ModelInput) (*ModelView, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if err := access.RequireVersion(input.Version); err != nil {
		return nil, err
	}
	spec, err := service.spec(input.ProviderID)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeModel(input, spec)
	if err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	updated, err := service.store.UpdateModel(ctx, id, normalized, input.Version)
	if err != nil {
		return nil, translateModelConflict(err)
	}
	view := service.modelView(updated)
	return &view, nil
}

// DeleteModel refuses while routes still point at the model unless the operator acknowledged the
// impact; detachTargets decides whether those targets are removed or merely disabled.
func (service *Service) DeleteModel(ctx context.Context, actor access.Actor, id string, acknowledge, detachTargets bool) error {
	if err := actor.RequireAdmin(); err != nil {
		return err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	references, err := service.store.RoutesReferencingModel(ctx, id)
	if err != nil {
		return access.Translate(err)
	}
	if len(references) > 0 && !acknowledge {
		return access.Conflict("impact_requires_acknowledge")
	}
	return access.Translate(service.store.DeleteModel(ctx, id, detachTargets))
}

func (service *Service) RestoreModel(ctx context.Context, actor access.Actor, id string) (*ModelView, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	restored, err := service.store.RestoreModel(ctx, id)
	if err != nil {
		return nil, access.Translate(err)
	}
	view := service.modelView(restored)
	return &view, nil
}

func (service *Service) DetachModelTargets(ctx context.Context, actor access.Actor, id string) (int, error) {
	if err := actor.RequireAdmin(); err != nil {
		return 0, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	detached, err := service.store.DetachRouteTargetsOfModel(ctx, id)
	return detached, access.Translate(err)
}

func translateModelConflict(err error) error {
	var conflict *repository.ModelNameConflictError
	if errors.As(err, &conflict) {
		return access.Conflict("model_name_taken")
	}
	return access.Translate(err)
}
