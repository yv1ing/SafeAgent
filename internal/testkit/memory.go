// Package testkit holds the shared fixtures of the unit tests: an in-memory trajectory repository
// and fake upstream servers for each protocol.
package testkit

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

const InstanceSecret = "0123456789abcdef0123456789abcdef"

// Memory is a single-project, single-key in-memory store. It implements the pipeline persistence
// port and projects calls from appended trajectory events the same way the database does.
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

	Trajectories map[string]*entity.TrajectorySummary
	Sessions     map[string]string
	Calls        []*entity.Call
	Events       []trajectory.Event
	Segments     map[string]map[string]trajectory.Segment
	Blobs        map[string][]byte
	Sealed       []string
	ToolCalls    map[string]string
	OnSeal       func(call *entity.Call)
	SealError    error
	Settled      []int64
	Reserved     []int64
	FailCreate   bool
	Policy       *entity.PolicyVersion
}

func NewMemory(upstreamURL, upstreamFormat string) *Memory {
	sealed, _ := secure.Seal([]byte(InstanceSecret), []byte("sk-upstream"))
	registry := ProviderRegistry(upstreamFormat, upstreamURL)
	spec, _ := registry.Get("test")
	endpoints, _ := provider.MarshalEndpoints(spec)
	return &Memory{
		Key:      &entity.AgentKey{ID: "key_1", ProjectID: "project_1", Enabled: true, Hash: secure.Digest("agent-key"), Role: "agent"},
		Proj:     &entity.Project{ID: "project_1", Name: "test", RetentionDays: 30, RequestsPerMinute: 1000, Concurrency: 100, RulesVersion: 1, CoverageThreshold: 0.9},
		RuleList: []entity.Rule{{ID: "r1", ProjectID: "project_1", Kind: "builtin", Detector: "email", Label: "EMAIL", Action: "replace", Enabled: true, Version: 1}},
		Route:    &entity.Route{ID: "route_1", ProjectID: "project_1", Name: "gpt-5", Enabled: true, Version: 1},
		Targets: []entity.RouteTarget{{
			ID: "target_1", RouteID: "route_1", ChannelID: "channel_1", ModelID: "model_1", Priority: 1, Weight: 1, Enabled: true,
		}},
		Providers: []entity.Provider{{ID: "test", Source: entity.ProviderBuiltin, Label: spec.Label, Endpoints: endpoints, Enabled: true, Version: 1}},
		Models: []entity.Model{{
			ID: "model_1", ProviderID: "test", UpstreamName: "gpt-5-upstream", Kind: entity.ModelGeneration,
			InputPrice: 1_000_000, OutputPrice: 2_000_000, Currency: "USD", Enabled: true, Version: 1,
		}},
		Channel:  []entity.Channel{{ID: "channel_1", Name: "mock", ProviderID: "test", Enabled: true, Version: 1, AllowPrivateNetwork: true}},
		Secrets:  []entity.ChannelKey{{ID: "ck_1", ChannelID: "channel_1", Secret: sealed, Enabled: true}},
		Registry: registry, ToolCalls: map[string]string{},
		Trajectories: map[string]*entity.TrajectorySummary{}, Sessions: map[string]string{},
		Segments: map[string]map[string]trajectory.Segment{}, Blobs: map[string][]byte{},
	}
}

func (store *Memory) AddTarget(modelID, upstreamName, format string) {
	store.Models = append(store.Models, entity.Model{ID: modelID, ProviderID: "test", UpstreamName: upstreamName, Kind: entity.ModelGeneration, Currency: "USD", Enabled: true, Version: 1})
	store.Targets = append(store.Targets, entity.RouteTarget{ID: "target_" + modelID, RouteID: store.Route.ID, ChannelID: "channel_1", ModelID: modelID, Format: format, Priority: 2, Weight: 1, Enabled: true})
}

func (store *Memory) SetProvider(spec provider.ProviderSpec) {
	endpoints, _ := provider.MarshalEndpoints(spec)
	store.Registry = provider.NewRegistry(spec)
	store.Providers = []entity.Provider{{ID: spec.ID, Source: entity.ProviderBuiltin, Label: spec.Label, Endpoints: endpoints, Enabled: true, Version: 1}}
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

func (store *Memory) Routes(_ context.Context, _ string, includeDeleted bool) ([]repository.RouteRecord, error) {
	if store.Route == nil || (!includeDeleted && store.Route.DeletedAt != nil) {
		return []repository.RouteRecord{}, nil
	}
	return []repository.RouteRecord{{Route: *store.Route, Targets: store.mergeTargets(), Aliases: store.Aliases}}, nil
}

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

func (store *Memory) DefaultRoute(_ context.Context, _ string) (repository.RouteResolution, error) {
	if store.Route == nil || store.Proj.DefaultRouteID == "" || store.Proj.DefaultRouteID != store.Route.ID || !store.Route.Enabled {
		return repository.RouteResolution{}, repository.ErrNotFound
	}
	return repository.RouteResolution{Route: *store.Route, Targets: store.mergeTargets(), Resolution: repository.ResolutionFallback}, nil
}

func (store *Memory) RouteCandidates(_ context.Context, routeID string) ([]repository.RouteTarget, error) {
	if store.Route == nil || store.Route.ID != routeID {
		return nil, repository.ErrNotFound
	}
	return store.mergeTargets(), nil
}

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

func (store *Memory) MarkCallCancelled(_ context.Context, callID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if call := store.callByID(callID); call != nil {
		now := time.Now().UTC()
		call.CancelledAt = &now
		call.State, call.Outcome = "cancelled", "cancelled"
		call.UpdatedAt = now
	}
	return nil
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

func (store *Memory) ActivePolicy(context.Context, string) (*entity.PolicyVersion, error) {
	return store.Policy, nil
}

func (store *Memory) EnsureTrajectory(_ context.Context, projectID, agentKeyID, sessionID, goal string, threshold float64) (*entity.TrajectorySummary, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.FailCreate {
		return nil, context.DeadlineExceeded
	}
	if sessionID != "" {
		if id, found := store.Sessions[sessionID]; found {
			if summary := store.Trajectories[id]; summary != nil && summary.State == "open" {
				return summary, nil
			}
		}
	}
	now := time.Now().UTC()
	summary := &entity.TrajectorySummary{
		ID: trajectory.NewID("trj"), ProjectID: projectID, AgentKeyID: agentKeyID, SessionID: sessionID,
		Goal: goal, State: "open", Coverage: 1, FirstEventAt: now, LastEventAt: now, CreatedAt: now,
	}
	store.Trajectories[summary.ID] = summary
	if sessionID != "" {
		store.Sessions[sessionID] = summary.ID
	}
	return summary, nil
}

func (store *Memory) Append(_ context.Context, batch trajectory.Batch) (repository.AppendResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.FailCreate || store.SealError != nil {
		if store.SealError != nil {
			return repository.AppendResult{}, store.SealError
		}
		return repository.AppendResult{}, context.DeadlineExceeded
	}
	summary := store.Trajectories[batch.TrajectoryID]
	if summary == nil {
		return repository.AppendResult{}, repository.ErrNotFound
	}
	if store.Segments[batch.TrajectoryID] == nil {
		store.Segments[batch.TrajectoryID] = map[string]trajectory.Segment{}
	}
	for _, segment := range batch.Segments {
		store.Segments[batch.TrajectoryID][segment.ID] = segment
	}
	for _, blob := range batch.Blobs {
		store.Blobs[blob.Hash] = blob.Bytes
	}
	previous := ""
	seq := int64(0)
	for _, event := range store.Events {
		if event.TrajectoryID == batch.TrajectoryID {
			previous, seq = event.Hash, event.Seq
		}
	}
	for _, draft := range batch.Events {
		seq++
		id := draft.ID
		if id == "" {
			id = trajectory.NewID("evt")
		}
		event := trajectory.Event{
			ID: id, TrajectoryID: batch.TrajectoryID, ProjectID: batch.ProjectID, Seq: seq,
			Kind: draft.Kind, Boundary: draft.Boundary, Actor: draft.Actor, Principal: draft.Principal,
			CallID: draft.CallID, ActionID: draft.ActionID, At: draft.At, Inputs: draft.Inputs,
			Outputs: draft.Outputs, Auth: draft.Auth, Checks: draft.Checks, Obligations: draft.Obligations,
			Snapshot: draft.Snapshot, Capture: draft.Capture, Payload: trajectory.Marshal(draft.Payload), PrevHash: previous,
		}
		if event.At.IsZero() {
			event.At = time.Now().UTC()
		}
		if err := trajectory.Sign(&event, []byte(InstanceSecret)); err != nil {
			return repository.AppendResult{}, err
		}
		store.Events = append(store.Events, event)
		previous = event.Hash
		store.applyEvent(event)
	}
	summary.EventCount = int64(seq)
	summary.LastEventAt = time.Now().UTC()
	return repository.AppendResult{Summary: *summary, Events: len(batch.Events)}, nil
}

func (store *Memory) applyEvent(event trajectory.Event) {
	switch event.Kind {
	case trajectory.KindActionProposed:
		proposed, err := trajectory.DecodePayload[trajectory.ActionProposed](&event)
		if err != nil || proposed.CallID == "" {
			return
		}
		call := store.callByID(proposed.CallID)
		if call == nil {
			call = &entity.Call{
				ID: proposed.CallID, TrajectoryID: event.TrajectoryID, ProjectID: store.Proj.ID,
				AgentKeyID: event.Principal, ClientFormat: proposed.ClientFormat, Operation: proposed.Operation,
				Purpose: proposed.Purpose, PurposeSource: proposed.PurposeSource, Lane: proposed.Lane,
				PublicModel: proposed.PublicModel, Stream: proposed.Stream, SessionID: proposed.SessionID,
				ParentCallID: proposed.ParentCallID, State: "running", CreatedAt: event.At, UpdatedAt: event.At,
				ToolCallIDs: []string{}, RedactionLabels: []string{}, RedactionValueIDs: []string{}, Losses: []entity.Loss{}, Repairs: []entity.Repair{},
			}
			call.CallSeq = int64(len(store.Calls) + 1)
			store.Calls = append(store.Calls, call)
		}
		if proposed.ActionType == trajectory.ActionTool && proposed.ToolCallID != "" {
			call.ToolCallIDs = append(call.ToolCallIDs, proposed.ToolCallID)
			store.ToolCalls[proposed.ToolCallID] = call.ID
		}
		if proposed.ChannelID != "" {
			call.ChannelID, call.ModelID, call.UpstreamModel, call.UpstreamFormat = proposed.ChannelID, proposed.ModelID, proposed.UpstreamModel, proposed.Format
		}
		if proposed.ActionType == trajectory.ActionProviderAttempt {
			call.Attempts++
		}
		call.UpdatedAt = event.At
	case trajectory.KindModelChange:
		change, err := trajectory.DecodePayload[trajectory.ModelChange](&event)
		if err != nil || event.CallID == "" {
			return
		}
		if call := store.callByID(event.CallID); call != nil {
			call.RouteID, call.Resolution = change.RouteID, change.Resolution
			if change.PublicModel != "" {
				call.PublicModel = change.PublicModel
			}
		}
	case trajectory.KindActionExecuted:
		executed, err := trajectory.DecodePayload[trajectory.ActionExecuted](&event)
		if err != nil || executed.CallID == "" {
			return
		}
		call := store.callByID(executed.CallID)
		if call == nil || executed.ActionType != trajectory.ActionModel {
			return
		}
		call.State, call.Outcome, call.ErrorCode = executed.State, executed.Outcome, executed.ErrorCode
		call.DurationMilliseconds = executed.DurationMS
		call.FirstTokenMilliseconds = executed.FirstTokenMS
		call.InputTokens, call.OutputTokens = executed.Usage.Input, executed.Usage.Output
		call.UsageKnown = executed.Usage.Known
		call.CostMicros, call.Currency = executed.CostMicros, executed.Currency
		call.ResponseID, call.ResponseStore = executed.ResponseID, executed.ResponseStore
		call.ConversationHash = executed.ConversationHash
		for _, id := range executed.ToolCallIDs {
			call.ToolCallIDs = append(call.ToolCallIDs, id)
			store.ToolCalls[id] = call.ID
		}
		terminated := event.At
		call.TerminatedAt = &terminated
		store.Sealed = append(store.Sealed, call.ID)
		if store.OnSeal != nil {
			store.OnSeal(call)
		}
	case trajectory.KindGateDecision:
		decision, err := trajectory.DecodePayload[trajectory.GateDecision](&event)
		if err != nil || decision.CallID == "" {
			return
		}
		call := store.callByID(decision.CallID)
		if call == nil {
			return
		}
		if decision.Terminal && call.State == "running" {
			call.State, call.Outcome, call.ErrorCode = decision.CallState, classify.Outcome(decision.Outcome), decision.Code
			if call.State == "" {
				call.State = "completed"
			}
			terminated := event.At
			call.TerminatedAt = &terminated
			store.Sealed = append(store.Sealed, call.ID)
		}
	case trajectory.KindObservation:
		observation, err := trajectory.DecodePayload[trajectory.Observation](&event)
		if err != nil || observation.CallID == "" {
			return
		}
		if call := store.callByID(observation.CallID); call != nil {
			seen := map[string]bool{}
			for _, hit := range observation.Redactions {
				if hit.ValueID != "" {
					seen[hit.ValueID] = true
				}
			}
			call.RedactionValues += len(seen)
		}
	}
}

func (store *Memory) SegmentContent(_ context.Context, _, segmentID string) ([]byte, string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	content, found := store.Blobs[segmentID]
	if !found {
		return nil, "", repository.ErrNotFound
	}
	return content, "application/json", nil
}

func (store *Memory) CallsOfTrajectory(_ context.Context, _, trajectoryID string) ([]entity.Call, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := []entity.Call{}
	for _, call := range store.Calls {
		if call.TrajectoryID == trajectoryID {
			result = append(result, *call)
		}
	}
	return result, nil
}

func (store *Memory) EventsOfCalls(_ context.Context, _ string, callIDs []string) ([]trajectory.Event, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	wanted := map[string]bool{}
	for _, id := range callIDs {
		wanted[id] = true
	}
	var events []trajectory.Event
	for _, event := range store.Events {
		if wanted[event.CallID] {
			events = append(events, event)
		}
	}
	sort.SliceStable(events, func(left, right int) bool { return events[left].Seq < events[right].Seq })
	return events, nil
}

func (store *Memory) EventsOf(callID string) []trajectory.Event {
	events, _ := store.EventsOfCalls(context.Background(), store.Proj.ID, []string{callID})
	return events
}

func (store *Memory) callByID(id string) *entity.Call {
	for _, call := range store.Calls {
		if call.ID == id {
			return call
		}
	}
	return nil
}

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

func (store *Memory) EventsOfTrajectory(trajectoryID string) []trajectory.Event {
	store.mu.Lock()
	defer store.mu.Unlock()
	events := []trajectory.Event{}
	for _, event := range store.Events {
		if event.TrajectoryID == trajectoryID {
			events = append(events, event)
		}
	}
	return events
}

func (store *Memory) TrajectoryState(context.Context, string, string) (*state.State, error) {
	return state.New("trj_test", store.Proj.ID), nil
}
