package console

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/netguard"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// DiscoverModels lists the upstream models one enabled channel format exposes. It only reads the
// provider's model listing endpoint and never sends a billable request.
func (service *ChannelService) DiscoverModels(ctx context.Context, actor Actor, channelID, format string) ([]string, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	channel, err := service.store.Channel(ctx, channelID)
	if err != nil {
		return nil, Translate(err)
	}
	spec, found := service.providers.Get(channel.ProviderID)
	if !found {
		return nil, Invalid("invalid_provider")
	}
	format = strings.TrimSpace(format)
	formatEndpoint, ok := providerEndpoint(spec, format)
	if !ok {
		return nil, Invalid("invalid_format")
	}
	enabled := channelEndpoint(*channel, format)
	if enabled == nil {
		return nil, Invalid("invalid_format")
	}
	keys, err := service.store.ChannelKeys(ctx, channelID)
	if err != nil {
		return nil, Translate(err)
	}
	secret := ""
	for _, key := range keys {
		if !key.Enabled {
			continue
		}
		opened, err := secure.Open(service.store.Secret, key.Secret)
		if err != nil {
			continue
		}
		secret = string(opened)
		break
	}
	if secret == "" {
		return nil, Invalid("invalid_secret")
	}
	baseURL := formatEndpoint.BaseURL
	if enabled.BaseURL != "" {
		baseURL = enabled.BaseURL
	}
	discoveryURL := formatEndpoint.DiscoveryURL(baseURL)
	if discoveryURL == "" {
		return nil, Invalid("invalid_format")
	}
	return fetchModels(ctx, formatEndpoint, discoveryURL, secret, channel.Headers, channel.AllowPrivateNetwork)
}

func channelEndpoint(channel entity.Channel, format string) *entity.ChannelEndpoint {
	for index := range channel.Endpoints {
		if channel.Endpoints[index].Format == format {
			return &channel.Endpoints[index]
		}
	}
	return nil
}

func fetchModels(ctx context.Context, endpoint provider.EndpointSpec, url, secret string, channelHeaders map[string]string, allowPrivate bool) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, Invalid("invalid_endpoint")
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
		return nil, Invalid("upstream_unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, Invalid("upstream_rejected")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, Invalid("upstream_unreachable")
	}
	names, err := provider.ParseModels(endpoint.Discovery.Shape, payload)
	if err != nil {
		return nil, Invalid("upstream_rejected")
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

type ImportModelsInput struct {
	Format string
	Names  []string
}

type ImportModelsResult struct {
	Channel *entity.Channel
	Added   []string
	Created []string
	Skipped []string
}

// ImportModels confirms a discovery result: missing catalog models are created with only the
// discovered format, existing generation models gain that format, and every model is enabled on
// the channel.
func (service *ChannelService) ImportModels(ctx context.Context, actor Actor, channelID string, input ImportModelsInput) (*ImportModelsResult, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	configurationMu.Lock()
	defer configurationMu.Unlock()
	channel, err := service.store.Channel(ctx, channelID)
	if err != nil {
		return nil, Translate(err)
	}
	spec, found := service.providers.Get(channel.ProviderID)
	if !found {
		return nil, Invalid("invalid_provider")
	}
	format := strings.TrimSpace(input.Format)
	if _, ok := providerEndpoint(spec, format); !ok || channelEndpoint(*channel, format) == nil {
		return nil, Invalid("invalid_format")
	}
	if len(input.Names) == 0 || len(channel.ModelIDs)+len(input.Names) > MaximumChannelModels {
		return nil, Invalid("invalid_model")
	}
	result := &ImportModelsResult{Added: []string{}, Created: []string{}, Skipped: []string{}}
	modelIDs := slices.Clone(channel.ModelIDs)
	seen := make(map[string]bool, len(modelIDs))
	for _, modelID := range modelIDs {
		seen[modelID] = true
	}
	for _, rawName := range input.Names {
		name, err := RequireText(rawName, MaximumModelName, "invalid_model")
		if err != nil {
			return nil, err
		}
		model, err := service.store.ModelByProviderName(ctx, channel.ProviderID, name)
		if errors.Is(err, repository.ErrNotFound) {
			created, createErr := service.store.CreateModel(ctx, &entity.Model{
				ProviderID: channel.ProviderID, UpstreamName: name, Kind: entity.ModelGeneration,
				Formats: []string{format}, Currency: DefaultCurrency, Enabled: true,
			})
			if repository.IsUniqueViolation(createErr) {
				return nil, Conflict("model_name_taken")
			}
			if createErr != nil {
				return nil, Translate(createErr)
			}
			model = created
			result.Created = append(result.Created, name)
		} else if err != nil {
			return nil, Translate(err)
		} else if model.Kind != entity.ModelGeneration {
			result.Skipped = append(result.Skipped, name)
			continue
		} else if !model.Supports(format) {
			model.Formats = append(model.Formats, format)
			if _, updateErr := service.store.UpdateModel(ctx, model.ID, *model, model.Version); updateErr != nil {
				return nil, Translate(updateErr)
			}
		}
		if !seen[model.ID] {
			seen[model.ID] = true
			modelIDs = append(modelIDs, model.ID)
			result.Added = append(result.Added, model.ID)
		}
	}
	if !slices.Equal(modelIDs, channel.ModelIDs) {
		channel.ModelIDs = modelIDs
		updated, updateErr := service.store.UpdateChannel(ctx, channel.ID, *channel, channel.Version)
		if updateErr != nil {
			return nil, Translate(updateErr)
		}
		channel = updated
	}
	result.Channel = channel
	return result, nil
}
