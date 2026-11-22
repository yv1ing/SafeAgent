package console

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
)

const internalErrorCode = "internal_error"

// APIError is the body of every failed request. Message is English prose for people reading raw
// responses; clients localize on Code.
type APIError struct {
	Code    string `json:"code" required:"true"`
	Message string `json:"message" required:"true"`
}

// failure is an outcome decided by the transport itself rather than by a use case.
type failure struct {
	status int
	code   string
}

func (value failure) Error() string { return value.code }

var (
	errInvalidRequest         = failure{http.StatusBadRequest, "invalid_request"}
	errInvalidOrigin          = failure{http.StatusForbidden, "invalid_origin"}
	errUnauthenticated        = failure{http.StatusUnauthorized, "unauthenticated"}
	errPasswordChangeRequired = failure{http.StatusForbidden, "password_change_required"}
	errRateLimited            = failure{http.StatusTooManyRequests, "rate_limited"}
)

var kindStatus = map[service.Kind]int{
	service.KindInvalid:         http.StatusBadRequest,
	service.KindUnauthenticated: http.StatusUnauthorized,
	service.KindForbidden:       http.StatusForbidden,
	service.KindNotFound:        http.StatusNotFound,
	service.KindConflict:        http.StatusConflict,
}

// resolve maps an error onto the status and code sent to the client. Anything unrecognized is an
// internal error whose detail stays in the server log.
func resolve(err error) (int, string) {
	var expected *service.Failure
	if errors.As(err, &expected) {
		if status, found := kindStatus[expected.Kind]; found {
			return status, expected.Code
		}
	}
	var direct failure
	if errors.As(err, &direct) {
		return direct.status, direct.code
	}
	return http.StatusInternalServerError, internalErrorCode
}

func writeError(ctx *gin.Context, err error) {
	status, code := resolve(err)
	ctx.AbortWithStatusJSON(status, APIError{Code: code, Message: messageFor(code)})
}

func messageFor(code string) string {
	if message, found := errorMessages[code]; found {
		return message
	}
	return errorMessages[internalErrorCode]
}

var errorMessages = map[string]string{
	"internal_error":           "The server cannot complete the request.",
	"invalid_request":          "The request is malformed.",
	"invalid_origin":           "The request did not originate from the console.",
	"unauthenticated":          "The session is missing or has expired.",
	"password_change_required": "The password must be changed before any other operation.",
	"forbidden":                "The action is not permitted.",
	"not_found":                "The resource does not exist.",
	"call_running":             "The call is still running and cannot be analyzed.",
	"scope_too_large":          "The selected scope is too large for one batch.",
	"rate_limited":             "The request rate limit has been reached.",
	"version_conflict":         "The record was modified concurrently. Reload the page and retry.",
	"version_required":         "The current record version is required.",
	"invalid_credentials":      "The username or password is incorrect.",
	"weak_password":            "The password must contain 14 to 72 bytes with lower case, upper case and digits.",
	"invalid_username":         "The username must contain 1 to 64 letters, digits, dots, underscores or hyphens.",
	"username_taken":           "The username is already in use.",
	"cannot_demote_self":       "An account cannot disable or demote itself.",
	"invalid_name":             "The name must contain 1 to 120 characters and no control characters.",
	"invalid_label":            "The label is too long or contains control characters.",
	"invalid_retention_days":   "The retention period must be between 1 and 3650 days.",
	"invalid_limit":            "A limit is negative or exceeds its maximum.",
	"invalid_policy":           "The policy document is invalid.",
	"invalid_decision_model":   "The decision model configuration is invalid.",
	"invalid_format":           "The protocol format is not supported.",
	"invalid_provider":         "The provider is not supported.",
	"invalid_currency":         "The currency is not supported.",
	"invalid_endpoint":         "The endpoint must be an HTTP or HTTPS URL without credentials, query or fragment.",
	"invalid_timeout":          "A timeout is outside its permitted range.",
	"invalid_header":           "A header name or value is invalid or reserved.",
	"invalid_model":            "The model is empty, duplicated, unknown, disabled, or not enabled on the channel.",
	"invalid_kind":             "The model kind must be generation, embedding or moderation.",
	"model_immutable":          "A model provider and kind cannot be changed.",
	"provider_immutable":       "A channel provider cannot be changed.",
	"model_name_taken":         "A model with this upstream name already exists for the provider.",
	"model_in_use":             "A channel or route still references this model.",
	"channel_in_use":           "A route still depends on this channel, its formats or its enabled models.",
	"invalid_price":            "A model price is negative or above its maximum.",
	"invalid_secret":           "The secret is empty, too long or contains line breaks.",
	"upstream_unreachable":     "The upstream could not be reached.",
	"upstream_rejected":        "The upstream refused the discovery request.",
	"invalid_targets":          "Each target must name an enabled channel and one of its enabled models, share a format, use one kind, and not repeat.",
	"route_name_taken":         "A route with this name already exists in the project.",
	"invalid_rule":             "The rule is invalid.",
	"rule_limit_reached":       "The project has reached its rule limit.",
	"invalid_window":           "The window must be 24h, 7d or 30d.",
	"invalid_bucket":           "The bucket does not fit the window.",
	"invalid_group":            "The group must be model or agent_key.",
	"invalid_state":            "The call state is not recognized.",
	"invalid_severity":         "The severity must be info, medium or high.",
	"invalid_cursor":           "The pagination cursor is malformed.",
	"invalid_role":             "The role must be agent, user or service.",
}
