// Package provider describes the built-in providers, the endpoints each one exposes and the
// capability and compatibility of every endpoint. A channel stores only a provider id, its keys
// and a few overrides; everything else comes from this catalog.
package provider

import (
	neturl "net/url"
	"strings"

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
	for _, format := range []model.Format{model.Chat, model.Responses, model.Messages, model.Gemini} {
		if _, found := spec.Endpoints[format]; found {
			formats = append(formats, format)
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

type Registry struct {
	specs map[string]ProviderSpec
	order []string
}

func NewRegistry(specs ...ProviderSpec) *Registry {
	registry := &Registry{specs: make(map[string]ProviderSpec, len(specs))}
	for _, spec := range specs {
		registry.specs[spec.ID] = spec
		registry.order = append(registry.order, spec.ID)
	}
	return registry
}

func (registry *Registry) Get(id string) (ProviderSpec, bool) {
	spec, found := registry.specs[id]
	return spec, found
}

func (registry *Registry) List() []ProviderSpec {
	specs := make([]ProviderSpec, 0, len(registry.order))
	for _, id := range registry.order {
		specs = append(specs, registry.specs[id])
	}
	return specs
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
