// Package control holds the use cases of the control plane: the provider, channel, model and route
// graph, upstream discovery, the connect flow and the configuration health report. It keeps the
// graph's invariants in one place so the transport layer only maps input and output.
package control

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/llm/provider/builtin"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	DefaultTimeoutSeconds          = 600
	DefaultFirstByteTimeoutSeconds = 60
	MaximumTimeoutSeconds          = 3600
	MaximumFirstByteSeconds        = 600
	MaximumHeaders                 = 32
	MaximumHeaderValueLength       = 4096
	MaximumSecretLength            = 4096
	MaximumEndpointLength          = 2048
	MaximumKeyLabelLength          = 120
	MaximumModelName               = 200
	MaximumOutputTokens            = 1 << 20
	MaximumDisplayName             = 120
	MaximumBatchModels             = 200
	MaximumTargets                 = 32
	MaximumWeight                  = 1000
	MaximumAliases                 = 16
	DefaultCurrency                = "USD"
	// MaximumPriceMicros bounds a price per million tokens so that cost arithmetic cannot overflow.
	MaximumPriceMicros = 1_000_000_000_000
	maskedValue        = "****"
)

var Currencies = []string{"USD", "CNY", "EUR", "JPY", "GBP"}

var (
	headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,64}$")

	secretHeaders = map[string]bool{
		"authorization": true, "x-api-key": true, "x-goog-api-key": true,
		"api-key": true, "proxy-authorization": true,
	}
	reservedHeaders = map[string]bool{
		"authorization": true, "x-api-key": true, "x-goog-api-key": true, "host": true,
		"content-length": true, "transfer-encoding": true, "connection": true,
	}
)

// Service is the control-plane use-case set. The provider registry is the live catalog the pipeline
// reads, so every provider mutation reloads it after committing.
type Service struct {
	store     *repository.Store
	providers *provider.Registry
	selector  *routing.Selector
	logger    *slog.Logger
	// loadCatalog reads the stored provider rows. It is a field so a test can make the reload fail
	// without a database; production code always uses ProviderSpecs over the store.
	loadCatalog func(ctx context.Context) ([]provider.ProviderSpec, error)
	// mu serializes graph edits. The gateway is one process per database lease, so a process mutex
	// is the whole coordination surface for the console's read-modify-write flows.
	mu sync.Mutex
}

func New(store *repository.Store, providers *provider.Registry, selector *routing.Selector) *Service {
	return &Service{
		store: store, providers: providers, selector: selector, logger: slog.Default(),
		loadCatalog: func(ctx context.Context) ([]provider.ProviderSpec, error) {
			return ProviderSpecs(ctx, store)
		},
	}
}

// SeedProviders writes the built-in provider catalog. Existing rows are untouched, so an operator
// edit survives an upgrade.
func SeedProviders(ctx context.Context, store *repository.Store, specs []provider.ProviderSpec) error {
	seeds := make([]repository.ProviderSeed, 0, len(specs))
	for _, spec := range specs {
		endpoints, err := provider.MarshalEndpoints(spec)
		if err != nil {
			return err
		}
		seeds = append(seeds, repository.ProviderSeed{ID: spec.ID, Label: spec.Label, Endpoints: endpoints})
	}
	return store.SeedProviders(ctx, seeds)
}

// ProviderSpecs rebuilds the runtime catalog from the provider table.
func ProviderSpecs(ctx context.Context, store *repository.Store) ([]provider.ProviderSpec, error) {
	records, err := store.Providers(ctx, false)
	if err != nil {
		return nil, err
	}
	specs := make([]provider.ProviderSpec, 0, len(records))
	for _, record := range records {
		endpoints, err := provider.UnmarshalEndpoints(record.Endpoints)
		if err != nil {
			return nil, err
		}
		specs = append(specs, provider.ProviderSpec{
			ID: record.ID, Label: record.Label,
			Custom: record.Source == entity.ProviderCustom, Endpoints: endpoints,
		})
	}
	return specs, nil
}

// NewRegistry loads the runtime provider catalog from the table. The composition root seeds the
// built-in rows first and then builds the registry, so an operator edit stored in the table is what
// the gateway runs.
func NewRegistry(ctx context.Context, store *repository.Store) (*provider.Registry, error) {
	specs, err := ProviderSpecs(ctx, store)
	if err != nil {
		return nil, err
	}
	return provider.NewRegistry(specs...), nil
}

// reloadProviders refreshes the live catalog after a mutation has committed. A failure is logged
// instead of returned: the row is already durable, and failing the request would invite a retry that
// conflicts with the version just written. ConfigurationHealth reports the unreadable rows.
func (service *Service) reloadProviders(ctx context.Context) {
	specs, err := service.loadCatalog(ctx)
	if err != nil {
		service.logger.Error("the provider catalog could not be reloaded after a configuration change", "error", err)
		return
	}
	service.providers.Replace(specs)
}

func (service *Service) spec(id string) (provider.ProviderSpec, error) {
	spec, found := service.providers.Get(id)
	if !found {
		return provider.ProviderSpec{}, access.Invalid("invalid_provider")
	}
	return spec, nil
}

// MaskHeaders replaces credential header values so a browser never receives a stored secret.
func MaskHeaders(headers map[string]string) map[string]string {
	masked := make(map[string]string, len(headers))
	for name, value := range headers {
		if isSecretHeader(name) && value != "" {
			masked[name] = maskedValue
			continue
		}
		masked[name] = value
	}
	return masked
}

func isSecretHeader(name string) bool { return secretHeaders[strings.ToLower(name)] }

// mergeHeaders keeps the stored secret when the client echoes the mask back.
func mergeHeaders(existing, incoming map[string]string) map[string]string {
	merged := make(map[string]string, len(incoming))
	for name, value := range incoming {
		canonical := http.CanonicalHeaderKey(name)
		if isSecretHeader(name) && value == maskedValue {
			if original, found := existing[canonical]; found {
				merged[canonical] = original
			}
			continue
		}
		merged[canonical] = value
	}
	return merged
}

func validateHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) > MaximumHeaders {
		return nil, access.Invalid("invalid_header")
	}
	clean := make(map[string]string, len(headers))
	for name, value := range headers {
		name = strings.TrimSpace(name)
		if !headerNamePattern.MatchString(name) || reservedHeaders[strings.ToLower(name)] || strings.HasPrefix(strings.ToLower(name), "x-safeagent-") {
			return nil, access.Invalid("invalid_header")
		}
		if len(value) > MaximumHeaderValueLength || strings.ContainsAny(value, "\r\n\x00") {
			return nil, access.Invalid("invalid_header")
		}
		clean[http.CanonicalHeaderKey(name)] = value
	}
	return clean, nil
}

func validateForwardHeaders(names []string) ([]string, error) {
	if len(names) > MaximumHeaders {
		return nil, access.Invalid("invalid_header")
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !headerNamePattern.MatchString(name) || reservedHeaders[strings.ToLower(name)] || strings.HasPrefix(strings.ToLower(name), "x-safeagent-") {
			return nil, access.Invalid("invalid_header")
		}
		lower := strings.ToLower(name)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		clean = append(clean, lower)
	}
	return clean, nil
}

// validEndpoint accepts an absolute http(s) base URL without credentials, query or fragment.
func validEndpoint(value string) bool {
	if value == "" || len(value) > MaximumEndpointLength || strings.ContainsAny(value, " \r\n\t") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
}

func validateEndpointPath(value string) bool {
	return len(value) <= MaximumEndpointLength && strings.HasPrefix(value, "/") && !strings.ContainsAny(value, " \r\n\t")
}

// normalizeEndpoints validates a provider endpoint catalog and re-serializes it in canonical form.
func normalizeEndpoints(payload json.RawMessage) (json.RawMessage, map[model.Format]provider.EndpointSpec, error) {
	if len(payload) == 0 {
		return nil, nil, access.Invalid("invalid_endpoints")
	}
	endpoints := map[model.Format]provider.EndpointSpec{}
	if err := json.Unmarshal(payload, &endpoints); err != nil || len(endpoints) == 0 {
		return nil, nil, access.Invalid("invalid_endpoints")
	}
	for format, endpoint := range endpoints {
		if endpoint.Format != "" && endpoint.Format != format {
			return nil, nil, access.Invalid("invalid_endpoints")
		}
		endpoint.Format = format
		if endpoint.BaseURL != "" && !validEndpoint(endpoint.BaseURL) {
			return nil, nil, access.Invalid("invalid_endpoints")
		}
		if endpoint.Path != "" && !validateEndpointPath(endpoint.Path) {
			return nil, nil, access.Invalid("invalid_endpoints")
		}
		if endpoint.CountTokensPath != "" && !validateEndpointPath(endpoint.CountTokensPath) {
			return nil, nil, access.Invalid("invalid_endpoints")
		}
		endpoints[format] = endpoint
	}
	normalized, err := json.Marshal(endpoints)
	if err != nil {
		return nil, nil, err
	}
	return normalized, endpoints, nil
}

func modelFormat(name string) model.Format { return model.Format(name) }

func formatNames(formats []model.Format) []string {
	names := make([]string, 0, len(formats))
	for _, format := range formats {
		names = append(names, string(format))
	}
	return names
}

func formatSet(formats []model.Format) map[string]bool {
	set := make(map[string]bool, len(formats))
	for _, format := range formats {
		set[string(format)] = true
	}
	return set
}

func validCurrency(currency string) bool {
	for _, candidate := range Currencies {
		if candidate == currency {
			return true
		}
	}
	return false
}

func validModelKind(kind string) bool {
	return entity.ModelKind(kind).Valid()
}

// builtinSpec returns the code definition of a built-in provider, which is what a reset restores.
func builtinSpec(id string) (provider.ProviderSpec, bool) {
	for _, spec := range builtin.All() {
		if spec.ID == id {
			return spec, true
		}
	}
	return provider.ProviderSpec{}, false
}
