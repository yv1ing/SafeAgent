package console

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func isSecretHeader(name string) bool { return secretHeaders[strings.ToLower(name)] }

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
		return nil, Invalid("invalid_header")
	}
	clean := make(map[string]string, len(headers))
	for name, value := range headers {
		name = strings.TrimSpace(name)
		if !headerNamePattern.MatchString(name) || reservedHeaders[strings.ToLower(name)] || strings.HasPrefix(strings.ToLower(name), "x-safeagent-") {
			return nil, Invalid("invalid_header")
		}
		if len(value) > MaximumHeaderValueLength || strings.ContainsAny(value, "\r\n\x00") {
			return nil, Invalid("invalid_header")
		}
		clean[http.CanonicalHeaderKey(name)] = value
	}
	return clean, nil
}

func validEndpointPath(value string) bool {
	return len(value) <= MaximumEndpointLength && strings.HasPrefix(value, "/") && !strings.ContainsAny(value, " \r\n\t")
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

func validateEndpoints(spec provider.ProviderSpec, endpoints []entity.ChannelEndpoint) ([]entity.ChannelEndpoint, error) {
	if len(endpoints) == 0 || len(endpoints) > MaximumFormats {
		return nil, Invalid("invalid_format")
	}
	seen := make(map[string]bool, len(endpoints))
	clean := make([]entity.ChannelEndpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		format := strings.TrimSpace(endpoint.Format)
		if _, known := providerEndpoint(spec, format); !known || seen[format] {
			return nil, Invalid("invalid_format")
		}
		seen[format] = true
		if endpoint.BaseURL != "" && !validEndpoint(endpoint.BaseURL) {
			return nil, Invalid("invalid_endpoint")
		}
		if endpoint.Path != "" && !validEndpointPath(endpoint.Path) {
			return nil, Invalid("invalid_endpoint")
		}
		if spec.Custom && endpoint.BaseURL == "" {
			return nil, Invalid("invalid_endpoint")
		}
		clean = append(clean, entity.ChannelEndpoint{Format: format, BaseURL: endpoint.BaseURL, Path: endpoint.Path})
	}
	return clean, nil
}

// normalize validates input and applies defaults. A channel enables an ordered list of provider
// formats; model ids are checked against the catalog by the service, which needs storage.
func (input ChannelInput) normalize(providers *provider.Registry) (ChannelInput, error) {
	name, err := RequireName(input.Name, "invalid_name")
	if err != nil {
		return input, err
	}
	input.Name = name
	spec, found := providers.Get(input.ProviderID)
	if !found {
		return input, Invalid("invalid_provider")
	}
	input.Endpoints, err = validateEndpoints(spec, input.Endpoints)
	if err != nil {
		return input, err
	}
	modelIDs := make([]string, 0, len(input.ModelIDs))
	seen := make(map[string]bool, len(input.ModelIDs))
	for _, modelID := range input.ModelIDs {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" || seen[modelID] {
			return input, Invalid("invalid_model")
		}
		seen[modelID] = true
		modelIDs = append(modelIDs, modelID)
	}
	input.ModelIDs = modelIDs
	if input.TimeoutSeconds < 0 || input.TimeoutSeconds > MaximumTimeoutSeconds ||
		input.FirstByteTimeoutSeconds < 0 || input.FirstByteTimeoutSeconds > MaximumFirstByteSeconds {
		return input, Invalid("invalid_timeout")
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = DefaultTimeoutSeconds
	}
	if input.FirstByteTimeoutSeconds == 0 {
		input.FirstByteTimeoutSeconds = DefaultFirstByteTimeoutSeconds
	}
	if input.Headers, err = validateHeaders(input.Headers); err != nil {
		return input, err
	}
	if input.ForwardHeaders, err = validateForwardHeaders(input.ForwardHeaders); err != nil {
		return input, err
	}
	if input.DefaultMaxOutputTokens < 0 || input.DefaultMaxOutputTokens > MaximumOutputTokens {
		return input, Invalid("invalid_limit")
	}
	return input, nil
}

func validateForwardHeaders(names []string) ([]string, error) {
	if len(names) > MaximumHeaders {
		return nil, Invalid("invalid_header")
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !headerNamePattern.MatchString(name) || reservedHeaders[strings.ToLower(name)] || strings.HasPrefix(strings.ToLower(name), "x-safeagent-") {
			return nil, Invalid("invalid_header")
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
