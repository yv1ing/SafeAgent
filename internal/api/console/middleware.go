package console

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

const requestMarker = "X-SafeAgent-Request"

// passwordChangeExempt lists what an account that must change its password may still do.
var passwordChangeExempt = map[string]bool{"me": true, "logout": true, "changePassword": true}

type Identity struct {
	User  *entity.User
	Token string
}

func (identity *Identity) actor() service.Actor {
	return service.Actor{UserID: identity.User.ID, Administrator: identity.User.Administrator}
}

func (console *Console) authenticate(ctx *gin.Context) (*Identity, error) {
	token, err := ctx.Cookie(sessionCookie)
	if err != nil || token == "" {
		return nil, errUnauthenticated
	}
	user, err := console.Account.Session(ctx.Request.Context(), token)
	if err != nil {
		return nil, err
	}
	return &Identity{User: user, Token: token}, nil
}

// sameOrigin guards state-changing requests against cross-site forgery. A custom header cannot be
// sent cross-origin without a CORS preflight, which this server never grants; when the browser
// also reports an Origin it must be the configured public origin, or the request's own host.
func (console *Console) sameOrigin(request *http.Request) bool {
	if request.Header.Get(requestMarker) != "1" {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if console.options.PublicOrigin != "" {
		return origin == console.options.PublicOrigin
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host == request.Host
}

func (console *Console) setSessionCookie(ctx *gin.Context, token string) {
	http.SetCookie(ctx.Writer, &http.Cookie{Name: sessionCookie, Value: token, Path: "/console", HttpOnly: true, Secure: console.options.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionLifetime / time.Second)})
}

func (console *Console) clearSessionCookie(ctx *gin.Context) {
	http.SetCookie(ctx.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/console", HttpOnly: true, Secure: console.options.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

// recordOperation appends a successful change to the operation log. The change already happened,
// so a storage failure is logged rather than reported to the client.
func (console *Console) recordOperation(ctx *gin.Context, operation string, identity *Identity) {
	parameters := make(map[string]string, len(ctx.Params))
	for _, parameter := range ctx.Params {
		parameters[parameter.Key] = parameter.Value
	}
	detail, _ := json.Marshal(parameters)
	entry := &entity.OperationLog{
		ID: secure.Random(12), UserID: identity.User.ID, Username: identity.User.Username,
		Action: operation, ProjectID: ctx.Param("project_id"), Detail: detail, CreatedAt: time.Now().UTC(),
	}
	if err := console.Operations.RecordOperation(ctx.Request.Context(), entry); err != nil {
		slog.Error("operation log write failed", "operation", operation, "error", err)
	}
}
