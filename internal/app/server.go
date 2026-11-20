package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/api/console"
	apigateway "github.com/yv1ing/SafeAgent/internal/api/gateway"
	"github.com/yv1ing/SafeAgent/internal/platform/config"
	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	serviceconsole "github.com/yv1ing/SafeAgent/internal/service/console"
	diagnosis "github.com/yv1ing/SafeAgent/internal/service/diagnosis"
	"github.com/yv1ing/SafeAgent/internal/service/gateway"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

type dependencies struct {
	database  *repository.Store
	selector  *routing.Selector
	settings  gateway.Settings
	diagnosis *diagnosis.Service
}

func assemble(configuration config.Config, database *repository.Store) (*gin.Engine, dependencies) {
	selector := routing.NewSelector(routing.NewBreakers(nil), routing.NewKeyPool(nil), routing.NewSticky(nil), configuration.MaxAttempts)
	settings := gateway.Settings{
		MaxRequestBytes:  configuration.MaxRequestBytes,
		MaxResponseBytes: configuration.MaxResponseBytes,
		MaxStreamBytes:   configuration.MaxStreamBytes,
		StreamIdle:       time.Duration(configuration.StreamIdleSeconds) * time.Second,
		ToolHoldBytes:    configuration.ToolHoldBytes,
		MaxAttempts:      configuration.MaxAttempts,
	}
	diagnosisService := diagnosis.New(database, diagnosis.Options{})
	pipeline := gateway.New(database, database.Secret, gateway.Options{Settings: settings, Selector: selector, Limiter: limit.New(nil), Veto: diagnosisService})
	services := serviceconsole.NewServices(database, selector, diagnosisService)

	router := gin.New()
	router.ContextWithFallback = true
	_ = router.SetTrustedProxies(configuration.TrustedProxies)
	router.Use(securityHeaders, recoverPanic)
	apigateway.NewHandler(pipeline, settings).Register(router)
	console.New(services, console.Options{SecureCookies: configuration.SecureCookies, PublicOrigin: configuration.PublicOrigin}).Register(router)
	router.GET("/health", health(database))
	return router, dependencies{database: database, selector: selector, settings: settings, diagnosis: diagnosisService}
}

func NewServer(configuration config.Config, database *repository.Store, frontend fs.FS) (*http.Server, *diagnosis.Service) {
	gin.SetMode(gin.ReleaseMode)
	router, dependencies := assemble(configuration, database)
	serveFrontend(router, frontend)
	return &http.Server{
		Addr:              configuration.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}, dependencies.diagnosis
}

func health(database *repository.Store) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		check, cancel := context.WithTimeout(ctx.Request.Context(), time.Second)
		defer cancel()
		if err := database.Ping(check); err != nil {
			ctx.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func securityHeaders(ctx *gin.Context) {
	ctx.Header("X-Content-Type-Options", "nosniff")
	ctx.Header("Referrer-Policy", "no-referrer")
	ctx.Header("X-Frame-Options", "DENY")
	ctx.Next()
}

func recoverPanic(ctx *gin.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("request panic", "method", ctx.Request.Method, "path", ctx.Request.URL.Path)
			ctx.AbortWithStatus(http.StatusInternalServerError)
		}
	}()
	ctx.Next()
}
func serveFrontend(router *gin.Engine, frontend fs.FS) {
	files := http.FileServer(http.FS(frontend))
	router.NoRoute(func(ctx *gin.Context) {
		if ctx.Request.Method != http.MethodGet || !strings.HasPrefix(ctx.Request.URL.Path, "/console") {
			ctx.Status(http.StatusNotFound)
			return
		}
		// An unknown API route is an API 404, never the SPA shell: a client that mistypes a path
		// must not receive HTML with a 200.
		if strings.HasPrefix(ctx.Request.URL.Path, "/console/api/") {
			ctx.Header("Content-Type", "application/json")
			ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "the endpoint does not exist"})
			return
		}
		ctx.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if ctx.Request.URL.Path == "/console" {
			ctx.Redirect(http.StatusTemporaryRedirect, "/console/")
			return
		}
		path := strings.TrimPrefix(ctx.Request.URL.Path, "/console/")
		if path == "" || !strings.Contains(path, ".") {
			ctx.Request.URL.Path = "/"
		} else {
			ctx.Request.URL.Path = "/" + path
		}
		files.ServeHTTP(ctx.Writer, ctx.Request)
	})
}
