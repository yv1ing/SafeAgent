package console

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

var defaultVendors = map[string]string{
	"openai_chat": "openai", "openai_responses": "openai",
	"anthropic_messages": "anthropic", "google_gemini": "google",
}

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
		if !headerNamePattern.MatchString(name) || reservedHeaders[strings.ToLower(name)] {
			return nil, Invalid("invalid_header")
		}
		if len(value) > MaximumHeaderValueLength || strings.ContainsAny(value, "\r\n\x00") {
			return nil, Invalid("invalid_header")
		}
		clean[http.CanonicalHeaderKey(name)] = value
	}
	return clean, nil
}

// validEndpoint accepts an absolute http(s) URL without credentials.
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
	for _, model := range models {
		name, err := RequireText(model.Name, MaximumModelNameLength, "invalid_model")
		if err != nil || seen[name] {
			return nil, Invalid("invalid_model")
		}
		seen[name] = true
		if model.InputPrice < 0 || model.OutputPrice < 0 || model.CacheReadPrice < 0 || model.CacheWritePrice < 0 ||
			model.InputPrice > MaximumPriceMicros || model.OutputPrice > MaximumPriceMicros ||
			model.CacheReadPrice > MaximumPriceMicros || model.CacheWritePrice > MaximumPriceMicros {
			return nil, Invalid("invalid_price")
		}
		model.Name = name
		clean = append(clean, model)
	}
	return clean, nil
}

// normalize validates input, applies defaults and returns the cleaned copy.
func (input ChannelInput) normalize() (ChannelInput, error) {
	name, err := RequireName(input.Name, "invalid_name")
	if err != nil {
		return input, err
	}
	input.Name = name
	defaultVendor, known := defaultVendors[input.Format]
	if !known {
		return input, Invalid("invalid_format")
	}
	if input.Vendor == "" {
		input.Vendor = defaultVendor
	}
	if !slices.Contains(Vendors, input.Vendor) {
		return input, Invalid("invalid_vendor")
	}
	if input.Currency == "" {
		input.Currency = DefaultCurrency
	}
	if !slices.Contains(Currencies, input.Currency) {
		return input, Invalid("invalid_currency")
	}
	if !validEndpoint(input.Endpoint) || input.CountTokensEndpoint != "" && !validEndpoint(input.CountTokensEndpoint) {
		return input, Invalid("invalid_endpoint")
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
	input.Models, err = validateModels(input.Models)
	return input, err
}
