// Package provider describes the built-in providers, the endpoints each one exposes and the
// capability and compatibility of every endpoint. A channel stores only a provider id, its keys
// and a few overrides; everything else comes from this catalog.
package provider

import (
	"encoding/json"
	"fmt"
	neturl "net/url"
	"strings"
	"sync"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type TransportMode string

const (
	TransportAuto   TransportMode = "auto"
	TransportStream TransportMode = "stream"
	TransportJSON   TransportMode = "json"
)

type AuthStyle string

const (
	AuthBearer AuthStyle = "bearer"
	AuthAPIKey AuthStyle = "x-api-key"
	AuthGoogle AuthStyle = "x-goog-api-key"
	AuthQuery  AuthStyle = "query"
)

type DiscoveryShape string

const (
	DiscoveryOpenAI    DiscoveryShape = "openai"
	DiscoveryAnthropic DiscoveryShape = "anthropic"
	DiscoveryGemini    DiscoveryShape = "gemini"
)

type AuxiliaryKind string

const (
	AuxiliaryEmbeddings  AuxiliaryKind = "embeddings"
	AuxiliaryModerations AuxiliaryKind = "moderations"
)

type DiscoverySpec struct {
	Path  string
	Shape DiscoveryShape
}

type AuxiliarySpec struct {
	Kind AuxiliaryKind
	Path string
}

type ModelAlias struct {
	Prefix string
	To     string
}

// EndpointSpec is one wire format of one provider. BaseURL and Path are separate so a channel can
// override the base URL without redefining the path, and so {model} and {action} expansion stays
// uniform.
type EndpointSpec struct {
	Format          model.Format
	BaseURL         string
	Path            string
	CountTokensPath string
	Auth            AuthStyle
	StaticHeaders   map[string]string
	Capability      model.Capability
	Compat          model.Compat
	Discovery       DiscoverySpec
	Auxiliary       []AuxiliarySpec
	Transport       TransportMode
	Aliases         []ModelAlias
}

type ProviderSpec struct {
	ID        string
	Label     string
	Custom    bool
	Endpoints map[model.Format]EndpointSpec
}

func (spec ProviderSpec) Endpoint(format model.Format) (EndpointSpec, bool) {
	endpoint, found := spec.Endpoints[format]
	return endpoint, found
}

func (spec ProviderSpec) Formats() []model.Format {
	formats := make([]model.Format, 0, len(spec.Endpoints))
	for _, format := range formatOrder {
		if _, found := spec.Endpoints[format]; found {
			formats = append(formats, format)
		}
	}
	return formats
}

// KindGeneration, KindEmbedding and KindModeration are the model kinds a catalog entry can have. A
// generation model may use any endpoint format the provider exposes; an embedding or moderation
// model may only use a format whose endpoint declares the matching auxiliary path.
const (
	KindGeneration = "generation"
	KindEmbedding  = "embedding"
	KindModeration = "moderation"
)

var formatOrder = []model.Format{model.Chat, model.Responses, model.Messages, model.Gemini}

// FormatsForKind derives the formats a catalog model of one kind can use on a provider. The order is
// the canonical format order, so a stored listing is stable.
func FormatsForKind(endpoints map[model.Format]EndpointSpec, kind string) []model.Format {
	formats := make([]model.Format, 0, len(endpoints))
	switch kind {
	case KindGeneration:
		for _, format := range formatOrder {
			if _, found := endpoints[format]; found {
				formats = append(formats, format)
			}
		}
	case KindEmbedding, KindModeration:
		wanted := AuxiliaryEmbeddings
		if kind == KindModeration {
			wanted = AuxiliaryModerations
		}
		for _, format := range formatOrder {
			endpoint, found := endpoints[format]
			if !found {
				continue
			}
			for _, auxiliary := range endpoint.Auxiliary {
				if auxiliary.Kind == wanted {
					formats = append(formats, format)
					break
				}
			}
		}
	}
	return formats
}

// URL builds the request URL. For a path with {model} the model is substituted, otherwise the base
// URL and path are joined.
func (endpoint EndpointSpec) URL(baseURL, upstreamModel string, stream bool) string {
	base := strings.TrimRight(firstNonEmpty(baseURL, endpoint.BaseURL), "/")
	path := endpoint.Path
	action := ""
	if endpoint.Format == model.Gemini {
		action = "generateContent"
		if stream {
			action = "streamGenerateContent"
		}
	}
	path = strings.ReplaceAll(path, "{action}", action)
	path = strings.ReplaceAll(path, "{model}", upstreamModel)
	url := base + "/" + strings.TrimLeft(path, "/")
	if endpoint.Format == model.Gemini && stream && !strings.Contains(url, "alt=sse") {
		separator := "?"
		if strings.Contains(url, "?") {
			separator = "&"
		}
		url += separator + "alt=sse"
	}
	return url
}

func (endpoint EndpointSpec) ApplyQuery(url, secret string) string {
	if endpoint.Auth != AuthQuery || secret == "" {
		return url
	}
	separator := "?"
	if strings.Contains(url, "?") {
		separator = "&"
	}
	return url + separator + "key=" + neturl.QueryEscape(secret)
}

func (endpoint EndpointSpec) CountTokensURL(baseURL, upstreamModel string) string {
	if endpoint.CountTokensPath == "" {
		return ""
	}
	base := strings.TrimRight(firstNonEmpty(baseURL, endpoint.BaseURL), "/")
	path := strings.ReplaceAll(endpoint.CountTokensPath, "{model}", upstreamModel)
	return base + "/" + strings.TrimLeft(path, "/")
}

func (endpoint EndpointSpec) AuxiliaryURL(baseURL string, auxiliary AuxiliarySpec) string {
	base := strings.TrimRight(firstNonEmpty(baseURL, endpoint.BaseURL), "/")
	return base + "/" + strings.TrimLeft(auxiliary.Path, "/")
}

func (endpoint EndpointSpec) ApplyAlias(upstreamModel string) string {
	if len(endpoint.Aliases) == 0 {
		return upstreamModel
	}
	best := ""
	result := upstreamModel
	for _, alias := range endpoint.Aliases {
		if strings.HasPrefix(upstreamModel, alias.Prefix) && len(alias.Prefix) > len(best) {
			best, result = alias.Prefix, alias.To
		}
	}
	return result
}

func (endpoint EndpointSpec) ApplyAuth(headers map[string]string, secret string) {
	for name, value := range endpoint.StaticHeaders {
		headers[name] = value
	}
	switch endpoint.Auth {
	case AuthAPIKey:
		headers["x-api-key"] = secret
	case AuthGoogle:
		headers["x-goog-api-key"] = secret
	case AuthQuery:
	case AuthBearer:
		headers["Authorization"] = "Bearer " + secret
	}
}

// Registry is the live provider catalog. The console replaces it after a provider mutation, so
// readers take the read lock and see either the old or the new catalog, never a partial one.
type Registry struct {
	mu    sync.RWMutex
	specs map[string]ProviderSpec
	order []string
}

func NewRegistry(specs ...ProviderSpec) *Registry {
	registry := &Registry{}
	registry.Replace(specs)
	return registry
}

func (registry *Registry) Replace(specs []ProviderSpec) {
	replacement := make(map[string]ProviderSpec, len(specs))
	order := make([]string, 0, len(specs))
	for _, spec := range specs {
		replacement[spec.ID] = spec
		order = append(order, spec.ID)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.specs = replacement
	registry.order = order
}

func (registry *Registry) Get(id string) (ProviderSpec, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	spec, found := registry.specs[id]
	return spec, found
}

// MarshalEndpoints stores the endpoint catalog in the providers table. Capability, compatibility,
// discovery and auxiliary declarations travel with their endpoint, so one column is the whole
// provider definition apart from its identity and label.
func MarshalEndpoints(spec ProviderSpec) ([]byte, error) {
	payload, err := json.Marshal(spec.Endpoints)
	if err != nil {
		return nil, fmt.Errorf("marshal provider %s endpoints: %w", spec.ID, err)
	}
	return payload, nil
}

// UnmarshalEndpoints rebuilds the endpoint catalog from its stored form.
func UnmarshalEndpoints(payload []byte) (map[model.Format]EndpointSpec, error) {
	endpoints := map[model.Format]EndpointSpec{}
	if len(payload) == 0 {
		return endpoints, nil
	}
	if err := json.Unmarshal(payload, &endpoints); err != nil {
		return nil, fmt.Errorf("decode provider endpoints: %w", err)
	}
	return endpoints, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
