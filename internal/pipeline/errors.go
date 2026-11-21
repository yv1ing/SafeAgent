package pipeline

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func failure(kind string, class model.FailureClass, code, message string, status int) *model.Error {
	return &model.Error{Kind: kind, Class: class, Code: code, Message: message, Status: status}
}

func invalid(code, message string) *model.Error {
	return failure("invalid_request", model.ClassInvalid, code, message, http.StatusBadRequest)
}

func internal(code, message string) *model.Error {
	return failure("internal", model.ClassInternal, code, message, http.StatusInternalServerError)
}

func blocked(code, message string, status int) *model.Error {
	return failure("blocked", model.ClassPolicy, code, message, status)
}

// sentence gives client-facing messages one consistent form: a capitalised clause that ends in a
// period. Details appended after a colon are treated the same way.
func sentence(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value[len(value)-1:], ".!?") {
		return value
	}
	return value + "."
}

func cancelled() *model.Error {
	return failure("timeout", model.ClassCancelled, "client_cancelled", "The client canceled the request.", 499)
}

// timedOut is an attempt that outlived its channel's overall timeout.
func timedOut() *model.Error {
	return failure("timeout", model.ClassTransient, "upstream_timeout", "The upstream did not finish within the channel timeout.", http.StatusGatewayTimeout)
}

func transient(code, message string) *model.Error {
	return failure("upstream_failure", model.ClassTransient, code, message, http.StatusBadGateway)
}

// failsOver reports whether another candidate may still serve the request. Only upstream health
// problems qualify: a rejected request would be rejected everywhere, and policy or client errors
// are final.
func failsOver(failure *model.Error) bool {
	switch failure.Class {
	case model.ClassTransient, model.ClassAuth, model.ClassRateLimit:
		return true
	}
	// These failures depend on the target protocol or channel, not on the request itself, so a
	// mixed-format route may still have a target that can represent the request.
	switch failure.Code {
	case "conversion_failed", "unsupported_channel_format":
		return true
	}
	return false
}

const upstreamDetailLimit = 512

// upstreamFailure classifies a non-2xx upstream response and reacts to it: credentials that were
// refused or throttled cool down so the next attempt uses another key. The second return value is
// the optional request field the upstream rejected, when that can be identified.
func (pipeline *Pipeline) upstreamFailure(response *http.Response, channel *entity.Channel, key routing.Key) (*model.Error, string) {
	status := response.StatusCode
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	switch {
	case status == http.StatusUnauthorized:
		pipeline.selector.CoolKey(channel.ID, key.ID, 10*time.Minute)
		return failure("upstream_failure", model.ClassAuth, "upstream_auth_failed", "The upstream rejected its credentials.", http.StatusBadGateway), ""
	case status == http.StatusForbidden:
		// A 403 is a refusal of this request (moderation, region, permission), not proof that the
		// key is bad. Cooling it or replaying the same request elsewhere only multiplies the refusal.
		return failure("invalid_request", model.ClassRejected, "upstream_forbidden", "The upstream refused the request.", http.StatusForbidden), ""
	case status == http.StatusTooManyRequests:
		pipeline.selector.CoolKey(channel.ID, key.ID, retryAfter(response.Header.Get("Retry-After")))
		return failure("upstream_failure", model.ClassRateLimit, "upstream_rate_limited", "The upstream rate limit has been reached.", http.StatusTooManyRequests), ""
	case status >= 500 || status == http.StatusRequestTimeout:
		return transient("upstream_server_error", sentence("The upstream returned status "+strconv.Itoa(status))), ""
	}
	message := "The upstream rejected the request"
	if detail := providerMessage(payload); detail != "" {
		message += ": " + truncate(detail, upstreamDetailLimit)
	}
	message = sentence(message)
	if status < 400 || status >= 500 {
		status = http.StatusBadRequest
	}
	return failure("invalid_request", model.ClassRejected, "upstream_rejected", message, status), rejectedField(payload)
}

// providerMessage extracts the human-readable reason from a provider's JSON error body. It is the
// only useful hint for a malformed request; anything else in the body (request ids, account
// details, proxy pages) stays out of the client response.
func providerMessage(payload []byte) string {
	root, err := model.DecodeObject(payload)
	if err != nil {
		return ""
	}
	if text, ok := root["error"].(string); ok {
		return strings.TrimSpace(text)
	}
	for _, candidate := range []map[string]any{model.Object(root["error"]), root} {
		if text := model.String(candidate["message"]); text != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// rejectedField identifies a request parameter an upstream refused so the next attempt can drop it.
// Only an explicit "unsupported" or "unknown" complaint counts: a validation error that mentions
// "thinking" because reasoning must be replayed must not disable reasoning for the whole channel.
func rejectedField(payload []byte) string {
	root, err := model.DecodeObject(payload)
	if err != nil {
		return ""
	}
	errorObject := model.Object(root["error"])
	param := strings.ToLower(strings.TrimSpace(model.String(errorObject["param"])))
	detail := strings.ToLower(providerMessage(payload))
	if detail == "" || !unsupportedDetail(detail) {
		return ""
	}
	detail = strings.NewReplacer("`", "", "\"", "", "'", "").Replace(detail)
	for _, entry := range rejectedParameters {
		for _, name := range entry.names {
			if param == name || strings.HasSuffix(param, "."+name) || mentionsParameter(detail, name) {
				return entry.field
			}
		}
	}
	return ""
}

var rejectedParameters = []struct {
	names []string
	field string
}{
	{[]string{"top_k"}, "top_k"},
	{[]string{"budget_tokens", "thinking_budget", "reasoning_budget"}, "budget_tokens"},
	{[]string{"service_tier"}, "service_tier"},
	{[]string{"logprobs", "top_logprobs"}, "logprobs"},
	{[]string{"thinking", "reasoning", "reasoning_effort", "include_thoughts"}, "thinking"},
	{[]string{"custom_tool"}, "custom_tool"},
	{[]string{"stop_sequences", "stop_sequence", "stop"}, "stop"},
	{[]string{"tools", "tool_choice"}, "tools"},
}

func unsupportedDetail(detail string) bool {
	for _, marker := range []string{
		"unsupported", "not supported", "does not support", "doesn't support",
		"unknown parameter", "unrecognized", "not recognized", "extra fields",
		"unexpected parameter", "cannot be used", "not allowed",
	} {
		if strings.Contains(detail, marker) {
			return true
		}
	}
	return false
}

func mentionsParameter(detail, name string) bool {
	if !containsWord(detail, name) {
		return false
	}
	for _, marker := range []string{"parameter", "field", "argument", "option", "property"} {
		for _, separator := range []string{" ", ": ", "=", " ("} {
			if strings.Contains(detail, marker+separator+name) {
				return true
			}
		}
	}
	for _, phrase := range []string{
		name + " is not supported", name + " is unsupported", "unsupported " + name,
		"does not support " + name, "doesn't support " + name,
		"unknown " + name, "unrecognized " + name,
	} {
		if strings.Contains(detail, phrase) {
			return true
		}
	}
	return false
}

func containsWord(text, word string) bool {
	for offset := 0; ; {
		index := strings.Index(text[offset:], word)
		if index < 0 {
			return false
		}
		index += offset
		beforeOK := index == 0 || !wordByte(text[index-1])
		after := index + len(word)
		afterOK := after >= len(text) || !wordByte(text[after])
		if beforeOK && afterOK {
			return true
		}
		offset = index + len(word)
	}
}

func wordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '_'
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func retryAfter(value string) time.Duration {
	const fallback = time.Minute
	if value == "" {
		return fallback
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return capCooling(time.Duration(seconds) * time.Second)
	}
	if at, err := http.ParseTime(value); err == nil {
		return capCooling(time.Until(at))
	}
	return fallback
}

func capCooling(duration time.Duration) time.Duration {
	return min(max(duration, time.Second), 10*time.Minute)
}
