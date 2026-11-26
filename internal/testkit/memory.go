// Package testkit holds the shared fixtures of the unit tests: an in-memory repository that
// satisfies the gateway's persistence port and fake upstream servers for each protocol.
package testkit

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// Memory is a single-project, single-key in-memory store. Its hooks let tests observe ordering.
type Memory struct {
	mu        sync.Mutex
	Key       *entity.AgentKey
	Proj      *entity.Project
	RuleList  []entity.Rule
	Route     *entity.Route
	Targets   []entity.RouteTarget
	Aliases   []entity.RouteAlias
	Providers []entity.Provider
	Models    []entity.Model
	Channel   []entity.Channel
	Overrides []entity.CapabilityOverride
	Secrets   []entity.ChannelKey
	Registry  *provider.Registry

	Calls      []*entity.Call
	Events     []entity.Event
	Sealed     []string
	ToolCalls  map[string]string
	OnSeal     func(call *entity.Call)
	SealError  error
	Settled    []int64
	Reserved   []int64
	FailCreate bool
	Policy     *entity.PolicyVersion
	Analysis   []entity.SealAnalysis
}

// InstanceSecret is the key tests use for sealed upstream credentials.
const InstanceSecret = "0123456789abcdef0123456789abcdef"

// NewMemory returns a store with one project, one agent key, one route "gpt-5" and one channel.
func NewMemory(upstreamURL, upstreamFormat string) *Memory {
	sealed, _ := secure.Seal([]byte(InstanceSecret), []byte("sk-upstream"))
	registry := ProviderRegistry(upstreamFormat, upstreamURL)
	spec, _ := registry.Get("test")
	endpoints, _ := provider.MarshalEndpoints(spec)
	return &Memory{
		Key:      &entity.AgentKey{ID: "key_1", ProjectID: "project_1", Enabled: true, Hash: secure.Digest("agent-key"), Role: "agent"},
		Proj:     &entity.Project{ID: "project_1", Name: "test", RetentionDays: 30, RequestsPerMinute: 1000, Concurrency: 100, RulesVersion: 1},
		RuleList: []entity.Rule{{ID: "r1", ProjectID: "project_1", Kind: "builtin", Detector: "email", Label: "EMAIL", Action: "replace", Enabled: true, Version: 1}},
		Route:    &entity.Route{ID: "route_1", ProjectID: "project_1", Name: "gpt-5", Enabled: true, Version: 1},
		Targets: []entity.RouteTarget{{
			ID: "target_1", RouteID: "route_1", ChannelID: "channel_1", ModelID: "model_1",
			Priority: 1, Weight: 1, Enabled: true,
		}},
		Providers: []entity.Provider{{
			ID: "test", Source: entity.ProviderBuiltin, Label: spec.Label, Endpoints: endpoints, Enabled: true, Version: 1,
		}},
		Models: []entity.Model{{
			ID: "model_1", ProviderID: "test", UpstreamName: "gpt-5-upstream", Kind: entity.ModelGeneration,
			InputPrice: 1_000_000, OutputPrice: 2_000_000, Currency: "USD", Enabled: true, Version: 1,
		}},
		Channel: []entity.Channel{{
			ID: "channel_1", Name: "mock", ProviderID: "test", Enabled: true, Version: 1, AllowPrivateNetwork: true,
		}},
		Secrets:   []entity.ChannelKey{{ID: "ck_1", ChannelID: "channel_1", Secret: sealed, Enabled: true}},
		ToolCalls: map[string]string{},
		Registry:  registry,
	}
}

// AddTarget appends one routed channel/model pair and its catalog rows.
func (store *Memory) AddTarget(modelID, upstreamName, format string) {
	store.Models = append(store.Models, entity.Model{
		ID: modelID, ProviderID: "test", UpstreamName: upstreamName, Kind: entity.ModelGeneration,
		Currency: "USD", Enabled: true, Version: 1,
	})
	store.Targets = append(store.Targets, entity.RouteTarget{
		ID: "target_" + modelID, RouteID: store.Route.ID, ChannelID: "channel_1", ModelID: modelID,
		Format: format, Priority: 2, Weight: 1, Enabled: true,
	})
}

// SetProvider replaces the single in-memory provider catalog row.
func (store *Memory) SetProvider(spec provider.ProviderSpec) {
	endpoints, _ := provider.MarshalEndpoints(spec)
	store.Registry = provider.NewRegistry(spec)
	store.Providers = []entity.Provider{{
		ID: spec.ID, Source: entity.ProviderBuiltin, Label: spec.Label, Endpoints: endpoints, Enabled: true, Version: 1,
	}}
}

func (store *Memory) AgentKeyByHash(_ context.Context, hash string) (*entity.AgentKey, error) {
	if store.Key == nil || store.Key.Hash != hash {
		return nil, repository.ErrNotFound
	}
	return store.Key, nil
}

func (store *Memory) TouchAgentKey(context.Context, string) {}

func (store *Memory) Project(context.Context, string) (*entity.Project, error) {
	return store.Proj, nil
}

func (store *Memory) Rules(context.Context, string) ([]entity.Rule, error) {
	return store.RuleList, nil
}

// Routes returns the in-memory route catalog as the console and the pipeline read it.
func (store *Memory) Routes(_ context.Context, _ string, includeDeleted bool) ([]repository.RouteRecord, error) {
	if store.Route == nil || (!includeDeleted && store.Route.DeletedAt != nil) {
		return []repository.RouteRecord{}, nil
	}
	return []repository.RouteRecord{{
		Route: *store.Route, Targets: store.mergeTargets(), Aliases: store.Aliases,
	}}, nil
}

// ResolveRoute resolves an exact route name and then an alias, like the database store.
func (store *Memory) ResolveRoute(_ context.Context, _, name string) (repository.RouteResolution, error) {
	if store.Route == nil || store.Route.DeletedAt != nil {
		return repository.RouteResolution{}, repository.ErrNotFound
	}
	if store.Route.Enabled && store.Route.Name == name {
		return repository.RouteResolution{Route: *store.Route, Targets: store.mergeTargets(), Resolution: repository.ResolutionExact}, nil
	}
	for _, alias := range store.Aliases {
		if alias.Alias == name && alias.RouteID == store.Route.ID && store.Route.Enabled {
			return repository.RouteResolution{Route: *store.Route, Targets: store.mergeTargets(), Resolution: repository.ResolutionAlias}, nil
		}
	}
	return repository.RouteResolution{}, repository.ErrNotFound
}

// DefaultRoute resolves the project's fallback route.
func (store *Memory) DefaultRoute(_ context.Context, _ string) (repository.RouteResolution, error) {
	if store.Route == nil || store.Proj.DefaultRouteID == "" || store.Proj.DefaultRouteID != store.Route.ID || !store.Route.Enabled {
		return repository.RouteResolution{}, repository.ErrNotFound
	}
	return repository.RouteResolution{Route: *store.Route, Targets: store.mergeTargets(), Resolution: repository.ResolutionFallback}, nil
}

// RouteCandidates returns the enabled rows a route target points at, joined with channel, model and
// provider like the SQL implementation.
func (store *Memory) RouteCandidates(_ context.Context, routeID string) ([]repository.RouteTarget, error) {
	if store.Route == nil || store.Route.ID != routeID {
		return nil, repository.ErrNotFound
	}
	return store.mergeTargets(), nil
}

// mergeTargets joins the target rows with the catalog rows, dropping dangling or disabled rows.
func (store *Memory) mergeTargets() []repository.RouteTarget {
	result := make([]repository.RouteTarget, 0, len(store.Targets))
	for _, target := range store.Targets {
		if !target.Enabled || target.RouteID != store.Route.ID {
			continue
		}
		channel, found := store.channelByID(target.ChannelID)
		if !found || !channel.Enabled || channel.DeletedAt != nil {
			continue
		}
		model, found := store.modelByID(target.ModelID)
		if !found || !model.Enabled || model.DeletedAt != nil {
			continue
		}
		entry, found := store.providerByID(channel.ProviderID)
		if !found || !entry.Enabled {
			continue
		}
		result = append(result, repository.RouteTarget{Target: target, Channel: channel, Model: model, Provider: entry})
	}
	return result
}

func (store *Memory) channelByID(id string) (entity.Channel, bool) {
	for _, channel := range store.Channel {
		if channel.ID == id {
			return channel, true
		}
	}
	return entity.Channel{}, false
}

func (store *Memory) modelByID(id string) (entity.Model, bool) {
	for _, model := range store.Models {
		if model.ID == id {
			return model, true
		}
	}
	return entity.Model{}, false
}

func (store *Memory) providerByID(id string) (entity.Provider, bool) {
	for _, entry := range store.Providers {
		if entry.ID == id {
			return entry, true
		}
	}
	return entity.Provider{}, false
}

func (store *Memory) EnabledChannelKeys(context.Context) ([]entity.ChannelKey, error) {
	keys := make([]entity.ChannelKey, 0, len(store.Secrets))
	for _, secret := range store.Secrets {
		channel, found := store.channelByID(secret.ChannelID)
		if found && channel.Enabled && secret.Enabled {
			keys = append(keys, secret)
		}
	}
	return keys, nil
}

func (store *Memory) CapabilityOverrides(_ context.Context, channelIDs []string) ([]entity.CapabilityOverride, error) {
	wanted := map[string]bool{}
	for _, id := range channelIDs {
		wanted[id] = true
	}
	overrides := make([]entity.CapabilityOverride, 0, len(store.Overrides))
	for _, override := range store.Overrides {
		if wanted[override.ChannelID] {
			overrides = append(overrides, override)
		}
	}
	return overrides, nil
}

func (store *Memory) AddCapabilityOverride(_ context.Context, override entity.CapabilityOverride) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range store.Overrides {
		existing := store.Overrides[index]
		if existing.ChannelID == override.ChannelID && existing.ModelID == override.ModelID && existing.Format == override.Format && existing.Field == override.Field {
			store.Overrides[index].Reason = override.Reason
			return nil
		}
	}
	if override.CreatedAt.IsZero() {
		override.CreatedAt = time.Now().UTC()
	}
	store.Overrides = append(store.Overrides, override)
	return nil
}

func (store *Memory) DeleteResponse(_ context.Context, _, responseID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, call := range store.Calls {
		if call.ResponseID == responseID && responseID != "" && call.ResponseDeletedAt == nil {
			now := time.Now().UTC()
			call.ResponseDeletedAt = &now
			return nil
		}
	}
	return repository.ErrNotFound
}

func (store *Memory) MarkCallCancelled(_ context.Context, callID string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if call := store.callByID(callID); call != nil {
		now := time.Now().UTC()
		call.CancelledAt = &now
		call.UpdatedAt = now
	}
}

func (store *Memory) ReserveTokens(_ context.Context, _ string, estimate, limit int64) (bool, time.Time, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.Reserved = append(store.Reserved, estimate)
	if limit > 0 && estimate > limit {
		return false, time.Now().UTC(), nil
	}
	return true, time.Now().UTC(), nil
}

func (store *Memory) SettleTokens(_ context.Context, _ string, _ time.Time, _, actual int64) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.Settled = append(store.Settled, actual)
	return nil
}

func (store *Memory) CreateCall(_ context.Context, call *entity.Call) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.FailCreate {
		return context.DeadlineExceeded
	}
	store.Calls = append(store.Calls, call)
	return nil
}

// AppendEvent fails like the database would when the call row does not exist yet.
func (store *Memory) AppendEvent(event *entity.Event) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, call := range store.Calls {
		if call.ID == event.CallID {
			store.Events = append(store.Events, *event)
			return nil
		}
	}
	return repository.ErrNotFound
}

func (store *Memory) ActivePolicy(context.Context, string) (*entity.PolicyVersion, error) {
	return store.Policy, nil
}

func (store *Memory) SealCall(_ context.Context, call *entity.Call, state, code string, toolCallIDs []string, analysis entity.SealAnalysis) error {
	if store.OnSeal != nil {
		store.OnSeal(call)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.SealError != nil {
		return store.SealError
	}
	call.State = state
	call.ErrorCode = code
	store.Sealed = append(store.Sealed, call.ID)
	store.Analysis = append(store.Analysis, analysis)
	for _, id := range toolCallIDs {
		store.ToolCalls[id] = call.ID
	}
	return nil
}

func (store *Memory) AbandonCall(string) {}

func (store *Memory) FailCall(_ context.Context, call *entity.Call, code string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if call.State != "running" {
		return nil
	}
	call.State, call.ErrorCode = "failed", code
	return nil
}

func (store *Memory) callByID(id string) *entity.Call {
	for _, call := range store.Calls {
		if call.ID == id {
			return call
		}
	}
	return nil
}

// Call returns one call by project and id.
func (store *Memory) Call(_ context.Context, _, callID string) (*entity.Call, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if call := store.callByID(callID); call != nil {
		return call, nil
	}
	return nil, repository.ErrNotFound
}

func (store *Memory) CallByResponseID(_ context.Context, _, responseID string) (*entity.Call, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, call := range store.Calls {
		if call.ResponseID == responseID && responseID != "" && call.ResponseDeletedAt == nil {
			return call, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (store *Memory) CallByToolCall(_ context.Context, _, _, toolCallID string) (*entity.Call, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if call := store.callByID(store.ToolCalls[toolCallID]); call != nil {
		return call, nil
	}
	return nil, repository.ErrNotFound
}

func (store *Memory) CallByConversationHash(_ context.Context, _, hash string) (*entity.Call, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := len(store.Calls) - 1; index >= 0; index-- {
		if store.Calls[index].ConversationHash == hash && hash != "" {
			return store.Calls[index], nil
		}
	}
	return nil, repository.ErrNotFound
}

func (store *Memory) CallChain(_ context.Context, callID string) ([]entity.Call, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var chain []entity.Call
	for call := store.callByID(callID); call != nil; call = store.callByID(call.ParentCallID) {
		chain = append([]entity.Call{*call}, chain...)
	}
	if len(chain) == 0 {
		return nil, repository.ErrNotFound
	}
	return chain, nil
}

func (store *Memory) EventsOfKinds(_ context.Context, callIDs []string, kinds ...string) ([]entity.Event, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	wanted := map[string]bool{}
	for _, id := range callIDs {
		wanted[id] = true
	}
	allowed := map[string]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}
	var events []entity.Event
	for _, event := range store.Events {
		if wanted[event.CallID] && allowed[event.Kind] {
			events = append(events, event)
		}
	}
	sort.SliceStable(events, func(left, right int) bool {
		if events[left].CallID != events[right].CallID {
			return events[left].CallID < events[right].CallID
		}
		return events[left].Sequence < events[right].Sequence
	})
	return events, nil
}

// EventsOf returns the recorded events of one call, in order, for assertions.
func (store *Memory) EventsOf(callID string) []entity.Event {
	store.mu.Lock()
	defer store.mu.Unlock()
	var events []entity.Event
	for _, event := range store.Events {
		if event.CallID == callID {
			events = append(events, event)
		}
	}
	return events
}
