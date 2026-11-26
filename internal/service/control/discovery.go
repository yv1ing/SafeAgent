package control

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/netguard"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// DiscoverModels lists the upstream models one format of a channel exposes. It only reads the
// provider's model listing endpoint and never sends a billable request.
func (service *Service) DiscoverModels(ctx context.Context, actor access.Actor, channelID, format string) ([]string, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	channel, err := service.store.Channel(ctx, channelID)
	if err != nil {
		return nil, access.Translate(err)
	}
	spec, err := service.spec(channel.ProviderID)
	if err != nil {
		return nil, err
	}
	format = strings.TrimSpace(format)
	endpoint, found := spec.Endpoints[modelFormat(format)]
	if !found {
		return nil, access.Invalid("invalid_format")
	}
	secret, err := service.channelSecret(ctx, channelID)
	if err != nil {
		return nil, err
	}
	discoveryURL := endpoint.DiscoveryURL(channelEndpointBase(channel.Channel, endpoint))
	if discoveryURL == "" {
		return nil, access.Invalid("invalid_format")
	}
	return fetchModels(ctx, endpoint, discoveryURL, secret, channel.Headers, channel.AllowPrivateNetwork)
}

func (service *Service) channelSecret(ctx context.Context, channelID string) (string, error) {
	keys, err := service.store.ChannelKeys(ctx, channelID)
	if err != nil {
		return "", access.Translate(err)
	}
	for _, key := range keys {
		if !key.Enabled {
			continue
		}
		opened, err := secure.Open(service.store.Secret, key.Secret)
		if err != nil {
			continue
		}
		return string(opened), nil
	}
	return "", access.Invalid("invalid_secret")
}

func fetchModels(ctx context.Context, endpoint provider.EndpointSpec, url, secret string, channelHeaders map[string]string, allowPrivate bool) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, access.Invalid("invalid_endpoint")
	}
	for name, value := range channelHeaders {
		request.Header.Set(name, value)
	}
	headers := map[string]string{}
	endpoint.ApplyAuth(headers, secret)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("Accept", "application/json")
	response, err := netguard.Client(allowPrivate, 15*time.Second).Do(request)
	if err != nil {
		return nil, access.Invalid("upstream_unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, access.Invalid("upstream_rejected")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, access.Invalid("upstream_unreachable")
	}
	names, err := provider.ParseModels(endpoint.Discovery.Shape, payload)
	if err != nil {
		return nil, access.Invalid("upstream_rejected")
	}
	seen := map[string]bool{}
	unique := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		unique = append(unique, name)
	}
	return unique, nil
}

// ImportModelsResult reports what a discovery import changed. Existing catalog models are skipped,
// because a model is catalog-wide and already reachable through the provider.
type ImportModelsResult struct {
	Created []string
	Skipped []string
}

// ImportModels creates the missing catalog models of a provider from a discovery result.
func (service *Service) ImportModels(ctx context.Context, actor access.Actor, channelID, format string, names []string) (*ImportModelsResult, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if len(names) == 0 || len(names) > MaximumBatchModels*4 {
		return nil, access.Invalid("invalid_model")
	}
	channel, err := service.store.Channel(ctx, channelID)
	if err != nil {
		return nil, access.Translate(err)
	}
	spec, err := service.spec(channel.ProviderID)
	if err != nil {
		return nil, err
	}
	if _, found := spec.Endpoints[modelFormat(strings.TrimSpace(format))]; !found {
		return nil, access.Invalid("invalid_format")
	}
	models := make([]*entity.Model, 0, len(names))
	result := &ImportModelsResult{Created: []string{}, Skipped: []string{}}
	seen := map[string]bool{}
	for _, rawName := range names {
		name, err := access.RequireText(rawName, MaximumModelName, "invalid_model")
		if err != nil {
			return nil, err
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		existing, err := service.store.ModelByProviderName(ctx, channel.ProviderID, name)
		if err == nil {
			if existing.Kind != entity.ModelGeneration {
				result.Skipped = append(result.Skipped, name)
			}
			continue
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, access.Translate(err)
		}
		models = append(models, &entity.Model{
			ProviderID: channel.ProviderID, UpstreamName: name, Kind: entity.ModelGeneration,
			Currency: DefaultCurrency, Enabled: true,
		})
		result.Created = append(result.Created, name)
	}
	if len(models) == 0 {
		return result, nil
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.store.CreateModels(ctx, models); err != nil {
		return nil, translateModelConflict(err)
	}
	return result, nil
}
