package console

import (
	"net/http"
	"net/url"
	"slices"
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

// validEndpoint accepts an absolute http(s) base URL without credentials.
func validEndpoint(value string) bool {
	if value == "" || len(value) > MaximumEndpointLength || strings.ContainsAny(value, " \r\n\t") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
}

func validateModels(models []entity.ChannelModel) ([]entity.ChannelModel, error) {
	if len(models) > MaximumModels {
		return nil, Invalid("invalid_model")
	}
	seen := make(map[string]bool, len(models))
	clean := make([]entity.ChannelModel, 0, len(models))
	for _, modelEntry := range models {
		name, err := RequireText(modelEntry.Name, MaximumModelNameLength, "invalid_model")
		if err != nil || seen[name] {
			return nil, Invalid("invalid_model")
		}
		seen[name] = true
		if modelEntry.InputPrice < 0 || modelEntry.OutputPrice < 0 || modelEntry.CacheReadPrice < 0 || modelEntry.CacheWritePrice < 0 ||
			modelEntry.InputPrice > MaximumPriceMicros || modelEntry.OutputPrice > MaximumPriceMicros ||
			modelEntry.CacheReadPrice > MaximumPriceMicros || modelEntry.CacheWritePrice > MaximumPriceMicros {
			return nil, Invalid("invalid_price")
		}
		modelEntry.Name = name
		clean = append(clean, modelEntry)
	}
	return clean, nil
}

// normalize validates input, applies defaults and returns the cleaned copy. The provider catalog
// decides which formats exist and which base URL each one defaults to.
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
	input.Custom = spec.Custom
	endpoints := make([]entity.ChannelEndpoint, 0, len(input.Endpoints))
	seen := map[string]bool{}
	for _, entry := range input.Endpoints {
		if seen[entry.Format] {
			return input, Invalid("invalid_format")
		}
		if _, known := providerEndpoint(spec, entry.Format); !known {
			return input, Invalid("invalid_format")
		}
		if entry.BaseURL != "" && !validEndpoint(entry.BaseURL) {
			return input, Invalid("invalid_endpoint")
		}
		if entry.Path != "" && !validEndpointPath(entry.Path) {
			return input, Invalid("invalid_endpoint")
		}
		seen[entry.Format] = true
		endpoints = append(endpoints, entity.ChannelEndpoint{Format: entry.Format, BaseURL: entry.BaseURL, Path: entry.Path, Disabled: entry.Disabled})
	}
	if spec.Custom {
		usable := false
		for _, entry := range endpoints {
			if entry.BaseURL != "" && !entry.Disabled {
				usable = true
			}
		}
		if !usable {
			return input, Invalid("invalid_endpoint")
		}
	}
	input.Endpoints = endpoints
	if input.Currency == "" {
		input.Currency = DefaultCurrency
	}
	if !slices.Contains(Currencies, input.Currency) {
		return input, Invalid("invalid_currency")
	}
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
	input.Models, err = validateModels(input.Models)
	return input, err
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
