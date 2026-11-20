package console

import (
	"context"

	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	keyPrefix                   = "sk-sa-"
	MaximumKeyRequestsPerMinute = 100_000
	MaximumKeyConcurrency       = 10_000
)

type KeyService struct {
	store *repository.Store
}

func NewKeyService(store *repository.Store) *KeyService { return &KeyService{store: store} }

type KeyInput struct {
	Label             string
	RequestsPerMinute int
	Concurrency       int
	TrustLevel        int
	Role              string
	Enabled           bool
}

func (input KeyInput) normalize() (KeyInput, error) {
	label, err := RequireName(input.Label, "invalid_label")
	if err != nil {
		return input, err
	}
	input.Label = label
	if input.RequestsPerMinute < 0 || input.RequestsPerMinute > MaximumKeyRequestsPerMinute || input.Concurrency < 0 || input.Concurrency > MaximumKeyConcurrency {
		return input, Invalid("invalid_limit")
	}
	if input.Role == "" {
		input.Role = "agent"
	}
	if input.Role != "agent" && input.Role != "user" && input.Role != "service" {
		return input, Invalid("invalid_role")
	}
	if input.TrustLevel < 0 || input.TrustLevel > 100 {
		return input, Invalid("invalid_limit")
	}
	return input, nil
}

type KeyResult struct {
	Key    *entity.AgentKey
	Secret string
}

func (service *KeyService) Keys(ctx context.Context, actor Actor, projectID string) ([]entity.AgentKey, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	return service.store.AgentKeys(ctx, projectID)
}

func (service *KeyService) CreateKey(ctx context.Context, actor Actor, projectID string, input KeyInput) (*KeyResult, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	normalized, err := input.normalize()
	if err != nil {
		return nil, err
	}
	if !actor.Administrator && (normalized.TrustLevel != 0 || normalized.Role != "agent") {
		return nil, Forbidden()
	}
	secret := keyPrefix + secure.Random(24)
	key, err := service.store.CreateAgentKey(ctx, projectID, normalized.Label, secret, normalized.RequestsPerMinute, normalized.Concurrency, normalized.TrustLevel, normalized.Role)
	if err != nil {
		return nil, Translate(err)
	}
	return &KeyResult{Key: key, Secret: secret}, nil
}

func (service *KeyService) UpdateKey(ctx context.Context, actor Actor, projectID, keyID string, input KeyInput) (*entity.AgentKey, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	normalized, err := input.normalize()
	if err != nil {
		return nil, err
	}
	existing, err := service.store.AgentKey(ctx, projectID, keyID)
	if err != nil {
		return nil, Translate(err)
	}
	if !actor.Administrator && (existing.TrustLevel != normalized.TrustLevel || existing.Role != normalized.Role) {
		return nil, Forbidden()
	}
	updated, err := service.store.UpdateAgentKey(ctx, projectID, keyID, normalized.Label, normalized.RequestsPerMinute, normalized.Concurrency, normalized.TrustLevel, normalized.Role, normalized.Enabled)
	if err != nil {
		return nil, Translate(err)
	}
	return updated, nil
}

func (service *KeyService) RevokeKey(ctx context.Context, actor Actor, projectID, keyID string) (*entity.AgentKey, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if err := service.store.RevokeAgentKey(ctx, projectID, keyID); err != nil {
		return nil, Translate(err)
	}
	key, err := service.store.AgentKey(ctx, projectID, keyID)
	return key, Translate(err)
}
