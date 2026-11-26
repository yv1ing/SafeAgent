// Package pipeline is the online request pipeline: admission, redaction, policy checkpoints,
// routing, upstream attempts, streaming and the terminal call seal.
package pipeline

import (
	"context"
	"log/slog"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	_ "github.com/yv1ing/SafeAgent/internal/analysis/builtin"
	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/chat"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/gemini"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/messages"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/responses"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/llm/provider/builtin"
	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

const persistenceTimeout = 10 * time.Second

type Pipeline struct {
	repository Repository
	codecs     codec.Registry
	providers  *provider.Registry
	clients    *upstreamClients
	settings   Settings
	selector   *routing.Selector
	limiter    *limit.Limiter
	secret     []byte
	ids        func() string
	logger     *slog.Logger
	engines    *engineCache
	analysis   *analysis.Engine
	policies   *policyCache
	veto       Veto
	inflight   *inflight
}

type Options struct {
	Settings  Settings
	Providers *provider.Registry
	Selector  *routing.Selector
	Limiter   *limit.Limiter
	Logger    *slog.Logger
	IDs       func() string
	Veto      Veto
}

// New wires a pipeline; every option has a usable default.
func New(repository Repository, secret []byte, options Options) *Pipeline {
	settings := options.Settings
	if settings.MaxRequestBytes <= 0 {
		settings.MaxRequestBytes = 8 << 20
	}
	if settings.MaxResponseBytes <= 0 {
		settings.MaxResponseBytes = 32 << 20
	}
	if settings.MaxStreamBytes <= 0 {
		settings.MaxStreamBytes = 64 << 20
	}
	if settings.StreamIdle <= 0 {
		settings.StreamIdle = 60 * time.Second
	}
	if settings.Keepalive <= 0 {
		settings.Keepalive = 15 * time.Second
	}
	if settings.ToolHoldBytes <= 0 {
		settings.ToolHoldBytes = 1 << 20
	}
	if settings.MaxAttempts <= 0 {
		settings.MaxAttempts = 3
	}
	if options.Providers == nil {
		options.Providers = provider.NewRegistry(builtin.All()...)
	}
	if options.Selector == nil {
		options.Selector = routing.NewSelector(routing.NewBreakers(nil), routing.NewKeyPool(nil), routing.NewSticky(nil), 3)
	}
	if options.Limiter == nil {
		options.Limiter = limit.New(nil)
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.IDs == nil {
		options.IDs = func() string { return secure.Random(12) }
	}
	return &Pipeline{
		repository: repository,
		secret:     secret,
		codecs:     codec.NewRegistry(chat.New(), messages.New(), responses.New(), gemini.New()),
		providers:  options.Providers,
		clients:    newUpstreamClients(),
		settings:   settings,
		selector:   options.Selector,
		limiter:    options.Limiter,
		ids:        options.IDs,
		logger:     options.Logger,
		engines:    &engineCache{entries: map[string]engineEntry{}},
		analysis:   analysis.NewEngine(nil),
		policies:   &policyCache{entries: map[string]policyEntry{}},
		veto:       options.Veto,
		inflight:   newInflight(),
	}
}

func persistenceContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), persistenceTimeout)
}

// Envelope seals reasoning signatures towards clients so they can be verified when echoed back.
type Envelope struct {
	Secret []byte
}

func (envelope Envelope) Seal(signature model.Signature) (string, error) {
	return model.SealSignature(envelope.Secret, signature)
}

func (envelope Envelope) Open(encoded string) (model.Signature, bool) {
	return model.OpenSignature(envelope.Secret, encoded)
}

// endpoint is one enabled format of one channel with its catalog defaults and operator overrides.
type endpoint struct {
	channel       *entity.Channel
	model         *entity.Model
	endpoint      provider.EndpointSpec
	format        model.Format
	upstreamModel string
}

func applyCapabilityOverride(endpoint *provider.EndpointSpec, field string) {
	switch field {
	case "top_k":
		endpoint.Compat.IgnoreTopK = true
	case "budget_tokens":
		endpoint.Compat.IgnoreBudgetTokens = true
	case "service_tier":
		endpoint.Compat.IgnoreServiceTier = true
	case "stop":
		endpoint.Capability.StopSequences = false
	case "logprobs":
		endpoint.Capability.Logprobs = false
	case "thinking":
		endpoint.Compat.NoReasoning = true
	case "tools":
		endpoint.Compat.NoTools = true
	case "custom_tool":
		endpoint.Compat.NoCustomToolType = true
	}
}
