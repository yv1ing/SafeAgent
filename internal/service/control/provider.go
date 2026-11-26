package control

import (
	"context"
	"encoding/json"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// ProviderInput is the editable part of a provider. Endpoints is the stored endpoint catalog JSON,
// so the transport can round-trip exactly what the provider definition holds.
type ProviderInput struct {
	Label     string
	Endpoints json.RawMessage
	Enabled   bool
	Version   int64
}

type normalizedProvider struct {
	Label     string
	Endpoints json.RawMessage
	Enabled   bool
}

func normalizeProvider(input ProviderInput) (normalizedProvider, error) {
	label, err := access.RequireName(input.Label, "invalid_name")
	if err != nil {
		return normalizedProvider{}, err
	}
	endpoints, _, err := normalizeEndpoints(input.Endpoints)
	if err != nil {
		return normalizedProvider{}, err
	}
	return normalizedProvider{Label: label, Endpoints: endpoints, Enabled: input.Enabled}, nil
}

// providerEntity is the stored projection of a normalized provider; create, update and connect share
// it so an input field cannot be dropped on one path only.
func providerEntity(normalized normalizedProvider) *entity.Provider {
	return &entity.Provider{Label: normalized.Label, Endpoints: normalized.Endpoints, Enabled: normalized.Enabled}
}

func (service *Service) Providers(ctx context.Context, actor access.Actor, includeDeleted bool) ([]ProviderInfo, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	records, err := service.store.Providers(ctx, includeDeleted)
	if err != nil {
		return nil, access.Translate(err)
	}
	infos := make([]ProviderInfo, 0, len(records))
	for _, record := range records {
		infos = append(infos, providerInfo(record))
	}
	return infos, nil
}

func (service *Service) Provider(ctx context.Context, actor access.Actor, id string) (*ProviderInfo, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	record, err := service.store.ProviderRecord(ctx, id)
	if err != nil {
		return nil, access.Translate(err)
	}
	info := providerInfo(*record)
	return &info, nil
}

func (service *Service) CreateProvider(ctx context.Context, actor access.Actor, input ProviderInput) (*ProviderInfo, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	normalized, err := normalizeProvider(input)
	if err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	created, err := service.store.CreateProvider(ctx, providerEntity(normalized))
	if err != nil {
		return nil, access.Translate(err)
	}
	service.reloadProviders(ctx)
	info := providerInfoFromEntity(*created)
	return &info, nil
}

func (service *Service) UpdateProvider(ctx context.Context, actor access.Actor, id string, input ProviderInput) (*ProviderInfo, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if err := access.RequireVersion(input.Version); err != nil {
		return nil, err
	}
	normalized, err := normalizeProvider(input)
	if err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	updated, err := service.store.UpdateProvider(ctx, id, *providerEntity(normalized), input.Version)
	if err != nil {
		return nil, access.Translate(err)
	}
	service.reloadProviders(ctx)
	info := providerInfoFromEntity(*updated)
	return &info, nil
}

func (service *Service) DeleteProvider(ctx context.Context, actor access.Actor, id string) error {
	if err := actor.RequireAdmin(); err != nil {
		return err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.store.DeleteProvider(ctx, id); err != nil {
		if err == repository.ErrBuiltinProvider {
			return access.Conflict("builtin_provider")
		}
		return access.Translate(err)
	}
	service.reloadProviders(ctx)
	return nil
}

// ResetProvider restores a built-in row to the endpoint catalog this build ships.
func (service *Service) ResetProvider(ctx context.Context, actor access.Actor, id string) (*ProviderInfo, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	spec, found := builtinSpec(id)
	if !found {
		return nil, access.NotFound()
	}
	endpoints, err := provider.MarshalEndpoints(spec)
	if err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	reset, err := service.store.ResetProvider(ctx, repository.ProviderSeed{ID: spec.ID, Label: spec.Label, Endpoints: endpoints})
	if err != nil {
		if err == repository.ErrNotBuiltinProvider {
			return nil, access.Conflict("not_builtin_provider")
		}
		return nil, access.Translate(err)
	}
	service.reloadProviders(ctx)
	info := providerInfoFromEntity(*reset)
	return &info, nil
}
