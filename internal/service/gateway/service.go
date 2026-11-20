// Package gateway is the online request pipeline: admission, redaction, request and tool-call
// checkpoints, upstream attempts, streaming and the terminal call seal.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	_ "github.com/yv1ing/SafeAgent/internal/analysis/builtin"
	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/chat"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/gemini"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/messages"
	"github.com/yv1ing/SafeAgent/internal/protocol/format/responses"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const persistenceTimeout = 10 * time.Second

type Pipeline struct {
	repository Repository
	codecs     format.Registry
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
}

type Options struct {
	Settings Settings
	Selector *routing.Selector
	Limiter  *limit.Limiter
	Logger   *slog.Logger
	IDs      func() string
	Veto     Veto
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
	if settings.ToolHoldBytes <= 0 {
		settings.ToolHoldBytes = 1 << 20
	}
	if settings.MaxAttempts <= 0 {
		settings.MaxAttempts = 3
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
		codecs:     format.NewRegistry(chat.New(), messages.New(), responses.New(), gemini.New()),
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
	}
}

type policyEntry struct {
	version  int64
	compiled *policy.Compiled
}

type policyCache struct {
	mu      sync.RWMutex
	entries map[string]policyEntry
}

// compiledPolicy loads the newest saved policy of a project. A version is compiled once, so a busy
// project pays the CEL cost only after a save.
func (pipeline *Pipeline) compiledPolicy(ctx context.Context, projectID string) (*policy.Compiled, int64, error) {
	stored, err := pipeline.repository.ActivePolicy(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}
	version := int64(0)
	document := policy.DefaultDocument()
	if stored != nil {
		if err := json.Unmarshal(stored.Document, &document); err != nil {
			return nil, 0, err
		}
		version = stored.Version
	}
	pipeline.policies.mu.RLock()
	entry, found := pipeline.policies.entries[projectID]
	pipeline.policies.mu.RUnlock()
	if found && entry.version == version {
		return entry.compiled, entry.version, nil
	}
	compiled, err := policy.Compile(document)
	if err != nil {
		return nil, 0, err
	}
	pipeline.policies.mu.Lock()
	pipeline.policies.entries[projectID] = policyEntry{version: version, compiled: compiled}
	pipeline.policies.mu.Unlock()
	return compiled, version, nil
}

func persistenceContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), persistenceTimeout)
}

// Envelope seals reasoning signatures towards clients so they can be verified when echoed back.
type Envelope struct {
	Secret []byte
}

func (envelope Envelope) Seal(signature protocol.Signature) (string, error) {
	return protocol.SealSignature(envelope.Secret, signature)
}

func (envelope Envelope) Open(encoded string) (protocol.Signature, bool) {
	return protocol.OpenSignature(envelope.Secret, encoded)
}

// Authenticate maps a presented agent key to its project.
func (pipeline *Pipeline) Authenticate(ctx context.Context, key string) (*Identity, *protocol.Error) {
	if key == "" {
		return nil, failure("unauthorized", protocol.ClassAuth, "missing_api_key", "An API key is required.", http.StatusUnauthorized)
	}
	agentKey, err := pipeline.repository.AgentKeyByHash(ctx, secure.Digest(key))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, failure("unauthorized", protocol.ClassAuth, "invalid_api_key", "The API key is invalid.", http.StatusUnauthorized)
		}
		pipeline.logger.Error("agent key lookup failed", "error", err)
		return nil, internal("store_failure", "The API key could not be checked.")
	}
	project, err := pipeline.repository.Project(ctx, agentKey.ProjectID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, failure("unauthorized", protocol.ClassAuth, "invalid_project", "The project is unavailable.", http.StatusUnauthorized)
		}
		pipeline.logger.Error("project lookup failed", "error", err)
		return nil, internal("store_failure", "The project could not be loaded.")
	}
	pipeline.repository.TouchAgentKey(ctx, agentKey.ID)
	return &Identity{Project: project, AgentKey: agentKey}, nil
}

// BearerToken extracts the agent key from the headers agents conventionally use, then from the
// ?key= value the transport passed for Gemini routes.
func BearerToken(header http.Header, queryKey string) string {
	if value := header.Get("Authorization"); value != "" {
		if len(value) > 7 && strings.EqualFold(value[:7], "bearer ") {
			return strings.TrimSpace(value[7:])
		}
		return ""
	}
	for _, name := range []string{"x-api-key", "x-goog-api-key"} {
		if value := header.Get(name); value != "" {
			return value
		}
	}
	return queryKey
}

// Models lists the public model names the caller's project can use.
func (pipeline *Pipeline) Models(ctx context.Context, key string) ([]entity.Route, *protocol.Error) {
	identity, denied := pipeline.Authenticate(ctx, key)
	if denied != nil {
		return nil, denied
	}
	routes, err := pipeline.repository.Routes(ctx, identity.Project.ID)
	if err != nil {
		return nil, internal("store_failure", "The model list could not be loaded.")
	}
	return routes, nil
}

// engineCache keeps one compiled rule set per project; the project's rules_version invalidates it.
type engineEntry struct {
	version int64
	engine  *redact.Engine
	rules   []redact.Rule
}

type engineCache struct {
	mu      sync.RWMutex
	entries map[string]engineEntry
}

func (pipeline *Pipeline) redactionEngine(ctx context.Context, project *entity.Project) (*redact.Engine, []redact.Rule, error) {
	cache := pipeline.engines
	cache.mu.RLock()
	entry, found := cache.entries[project.ID]
	cache.mu.RUnlock()
	if found && entry.version == project.RulesVersion {
		return entry.engine, entry.rules, nil
	}
	stored, err := pipeline.repository.Rules(ctx, project.ID)
	if err != nil {
		return nil, nil, err
	}
	rules := make([]redact.Rule, 0, len(stored))
	for _, rule := range stored {
		rules = append(rules, redact.Rule{Kind: redact.RuleKind(rule.Kind), Detector: rule.Detector, Pattern: rule.Pattern, Capture: rule.Capture, Label: rule.Label, Action: redact.Action(rule.Action), Enabled: rule.Enabled})
	}
	engine, err := redact.NewEngine(rules, pipeline.secret, project.ID)
	if err != nil {
		return nil, nil, err
	}
	cache.mu.Lock()
	cache.entries[project.ID] = engineEntry{version: project.RulesVersion, engine: engine, rules: rules}
	cache.mu.Unlock()
	return engine, rules, nil
}

// channels is the resolved view of what a route can reach: enabled channels and their live keys.
type channels struct {
	byID map[string]*entity.Channel
	keys map[string][]routing.Key
}

func (pipeline *Pipeline) resolve(ctx context.Context, projectID, modelName string) ([]routing.Target, channels, error) {
	route, err := pipeline.repository.RouteByName(ctx, projectID, modelName)
	if err != nil {
		return nil, channels{}, err
	}
	stored, err := pipeline.repository.Channels(ctx)
	if err != nil {
		return nil, channels{}, err
	}
	resolved := channels{byID: map[string]*entity.Channel{}, keys: map[string][]routing.Key{}}
	for index := range stored {
		if stored[index].Enabled {
			resolved.byID[stored[index].ID] = &stored[index]
		}
	}
	var targets []routing.Target
	for _, target := range route.Targets {
		if resolved.byID[target.ChannelID] != nil {
			targets = append(targets, routing.Target{ChannelID: target.ChannelID, UpstreamModel: target.UpstreamModel, Priority: target.Priority, Weight: target.Weight})
		}
	}
	secrets, err := pipeline.repository.EnabledChannelKeys(ctx)
	if err != nil {
		return nil, channels{}, err
	}
	for _, secret := range secrets {
		if resolved.byID[secret.ChannelID] == nil {
			continue
		}
		plain, err := secure.Open(pipeline.secret, secret.Secret)
		if err != nil {
			pipeline.logger.Error("upstream key could not be opened", "key", secret.ID, "error", err)
			continue
		}
		resolved.keys[secret.ChannelID] = append(resolved.keys[secret.ChannelID], routing.Key{ID: secret.ID, Secret: string(plain)})
	}
	return targets, resolved, nil
}

// unsealRequest verifies the reasoning signatures a client echoed back and returns what could not
// be verified.
func unsealRequest(peer format.Peer, request *protocol.Request) []protocol.Loss {
	var losses []protocol.Loss
	for index := range request.Messages {
		blocks := request.Messages[index].Blocks
		for position := range blocks {
			losses = append(losses, peer.Unseal(&blocks[position])...)
		}
	}
	return losses
}
