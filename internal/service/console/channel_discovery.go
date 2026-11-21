package console

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/netguard"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// DiscoverModels lists the upstream models a channel exposes. It only reads the provider's model
// listing endpoint and never sends a billable request.
func (service *ChannelService) DiscoverModels(ctx context.Context, actor Actor, channelID string) ([]string, error) {
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
	var lastErr error
	for _, format := range spec.Formats() {
		if !service.exposes(channel, string(format)) {
			continue
		}
		endpoint, ok := providerEndpoint(spec, string(format))
		if !ok {
			continue
		}
		baseURL := endpoint.BaseURL
		for _, override := range channel.Endpoints {
			if override.Format == string(format) && override.BaseURL != "" {
				baseURL = override.BaseURL
			}
		}
		discoveryURL := endpoint.DiscoveryURL(baseURL)
		if discoveryURL == "" {
			continue
		}
		names, err := fetchModels(ctx, endpoint, discoveryURL, secret, channel.AllowPrivateNetwork)
		if err != nil {
			lastErr = err
			continue
		}
		return names, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, Invalid("invalid_format")
}

func (service *ChannelService) exposes(channel *entity.Channel, format string) bool {
	for _, entry := range channel.Endpoints {
		if entry.Format == format && entry.Disabled {
			return false
		}
	}
	return true
}

func fetchModels(ctx context.Context, endpoint provider.EndpointSpec, url, secret string, allowPrivate bool) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, Invalid("invalid_endpoint")
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
