// Package console is the HTTP transport of the management API. Every endpoint is declared once in
// register; the same declarations drive the gin router and the OpenAPI contract.
package console

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
)

// unlogged lists changes that are not worth an operation log entry: the rule tester changes nothing.
var unlogged = map[string]bool{"testRule": true, "testPolicy": true, "validatePolicy": true}

const (
	apiPrefix       = "/console/api"
	sessionCookie   = "safeagent_session"
	sessionLifetime = 7 * 24 * time.Hour
)

// Services is the application-service container shared with the transport. The alias keeps the
// router's field access unchanged while the service layer owns the construction.
type Services = service.Services

type Options struct {
	SecureCookies bool
	PublicOrigin  string
}

type Console struct {
	Services
	options       Options
	endpoints     []endpoint
	loginLimit    *limit.Bucket
	ruleLimit     *limit.Bucket
	analysisLimit *limit.Bucket
}

type handler func(ctx *gin.Context, identity *Identity) (any, error)

type endpoint struct {
	method    string
	path      string
	operation string
	input     any
	output    any
	handle    handler
	anonymous bool
}

func New(services Services, options Options) *Console {
	console := &Console{
		Services:      services,
		options:       options,
		loginLimit:    limit.NewBucket(10, time.Minute),
		ruleLimit:     limit.NewBucket(30, time.Minute),
		analysisLimit: limit.NewBucket(30, time.Minute),
	}
	console.register()
	return console
}

func (console *Console) register() {
	console.registerSession()
	console.registerUsers()
	console.registerProjects()
	console.registerMembers()
	console.registerModels()
	console.registerChannels()
	console.registerKeys()
	console.registerModelRoutes()
	console.registerRules()
	console.registerCalls()
	console.registerConversations()
	console.registerStats()
	console.registerOperations()
	console.registerPolicies()
	console.registerDecisionModel()
	console.registerBatches()
	console.registerMonitor()
	console.registerRisk()
}

func (console *Console) add(method, path, operation string, input, output any, handle handler) {
	console.endpoints = append(console.endpoints, endpoint{method: method, path: path, operation: operation, input: input, output: output, handle: handle})
}

func (console *Console) addAnonymous(method, path, operation string, input, output any, handle handler) {
	console.endpoints = append(console.endpoints, endpoint{method: method, path: path, operation: operation, input: input, output: output, handle: handle, anonymous: true})
}

func (console *Console) Register(router *gin.Engine) {
	for _, entry := range console.endpoints {
		router.Handle(entry.method, ginPath(apiPrefix+entry.path), console.serve(entry))
	}
	router.GET(apiPrefix+"/openapi.json", serveSpecification)
}

// ginPath turns an OpenAPI template such as /projects/{project_id} into /projects/:project_id.
func ginPath(path string) string {
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		if strings.HasPrefix(segment, "{") {
			segments[index] = ":" + strings.Trim(segment, "{}")
		}
	}
	return strings.Join(segments, "/")
}

func (console *Console) serve(entry endpoint) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Header("Cache-Control", "no-store")
		if entry.method != http.MethodGet && !console.sameOrigin(ctx.Request) {
			writeError(ctx, errInvalidOrigin)
			return
		}
		var identity *Identity
		if !entry.anonymous {
			var err error
			if identity, err = console.authenticate(ctx); err != nil {
				writeError(ctx, err)
				return
			}
			if identity.User.MustChangePassword && !passwordChangeExempt[entry.operation] {
				writeError(ctx, errPasswordChangeRequired)
				return
			}
		}
		response, err := entry.handle(ctx, identity)
		if err != nil {
			if status, _ := resolve(err); status == http.StatusInternalServerError {
				slog.Error("console request failed", "operation", entry.operation, "error", err)
			}
			writeError(ctx, err)
			return
		}
		if entry.method != http.MethodGet && identity != nil && !unlogged[entry.operation] {
			console.recordOperation(ctx, entry.operation, identity)
		}
		if response == nil {
			ctx.Status(http.StatusNoContent)
			return
		}
		ctx.JSON(http.StatusOK, response)
	}
}
