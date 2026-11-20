package gateway

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func failure(kind string, class protocol.FailureClass, code, message string, status int) *protocol.Error {
	return &protocol.Error{Kind: kind, Class: class, Code: code, Message: message, Status: status}
}

func invalid(code, message string) *protocol.Error {
	return failure("invalid_request", protocol.ClassInvalid, code, message, http.StatusBadRequest)
}

func internal(code, message string) *protocol.Error {
	return failure("internal", protocol.ClassInternal, code, message, http.StatusInternalServerError)
}

func blocked(code, message string, status int) *protocol.Error {
	return failure("blocked", protocol.ClassPolicy, code, message, status)
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

func cancelled() *protocol.Error {
	return failure("timeout", protocol.ClassCancelled, "client_cancelled", "The client canceled the request.", 499)
}

// timedOut is an attempt that outlived its channel's overall timeout.
func timedOut() *protocol.Error {
	return failure("timeout", protocol.ClassTransient, "upstream_timeout", "The upstream did not finish within the channel timeout.", http.StatusGatewayTimeout)
}

func transient(code, message string) *protocol.Error {
	return failure("upstream_failure", protocol.ClassTransient, code, message, http.StatusBadGateway)
}

// failsOver reports whether another candidate may still serve the request. Only upstream health
// problems qualify: a rejected request would be rejected everywhere, and policy or client errors
// are final.
func failsOver(failure *protocol.Error) bool {
	switch failure.Class {
	case protocol.ClassTransient, protocol.ClassAuth, protocol.ClassRateLimit:
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
// refused or throttled cool down so the next attempt uses another key.
func (pipeline *Pipeline) upstreamFailure(response *http.Response, channel *entity.Channel, key routing.Key) *protocol.Error {
	status := response.StatusCode
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	switch {
	case status == http.StatusUnauthorized:
		pipeline.selector.CoolKey(channel.ID, key.ID, 10*time.Minute)
		return failure("upstream_failure", protocol.ClassAuth, "upstream_auth_failed", "The upstream rejected its credentials.", http.StatusBadGateway)
	case status == http.StatusForbidden:
		// A 403 is a refusal of this request (moderation, region, permission), not proof that the
		// key is bad. Cooling it or replaying the same request elsewhere only multiplies the refusal.
		return failure("invalid_request", protocol.ClassRejected, "upstream_forbidden", "The upstream refused the request.", http.StatusForbidden)
	case status == http.StatusTooManyRequests:
		pipeline.selector.CoolKey(channel.ID, key.ID, retryAfter(response.Header.Get("Retry-After")))
		return failure("upstream_failure", protocol.ClassRateLimit, "upstream_rate_limited", "The upstream rate limit has been reached.", http.StatusTooManyRequests)
	case status >= 500 || status == http.StatusRequestTimeout:
		return transient("upstream_server_error", sentence("The upstream returned status "+strconv.Itoa(status)))
	}
	message := "The upstream rejected the request"
	if detail := providerMessage(payload); detail != "" {
		message += ": " + truncate(detail, upstreamDetailLimit)
	}
	message = sentence(message)
	if status < 400 || status >= 500 {
		status = http.StatusBadRequest
	}
	return failure("invalid_request", protocol.ClassRejected, "upstream_rejected", message, status)
}

// providerMessage extracts the human-readable reason from a provider's JSON error body. It is the
// only useful hint for a malformed request; anything else in the body (request ids, account
// details, proxy pages) stays out of the client response.
func providerMessage(payload []byte) string {
	root, err := protocol.DecodeObject(payload)
	if err != nil {
		return ""
	}
	if text, ok := root["error"].(string); ok {
		return strings.TrimSpace(text)
	}
	for _, candidate := range []map[string]any{protocol.Object(root["error"]), root} {
		if text := protocol.String(candidate["message"]); text != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
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
