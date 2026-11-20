// Package testkit holds the shared fixtures of the unit tests: an in-memory repository that
// satisfies the gateway's persistence port and fake upstream servers for each protocol.
package testkit

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// Memory is a single-project, single-key in-memory store. Its hooks let tests observe ordering.
type Memory struct {
	mu       sync.Mutex
	Key      *entity.AgentKey
	Proj     *entity.Project
	RuleList []entity.Rule
	Route    *entity.Route
	Channel  []entity.Channel
	Secrets  []entity.ChannelKey

	Calls      []*entity.Call
	Events     []entity.Event
	Sealed     []string
	ToolCalls  map[string]string
	OnSeal     func(call *entity.Call)
	SealError  error
	Settled    []int64
	Reserved   []int64
	QuotaLimit int64
	FailCreate bool
	Policy     *entity.PolicyVersion
	Analysis   []entity.SealAnalysis
}

// InstanceSecret is the key tests use for sealed upstream credentials.
const InstanceSecret = "0123456789abcdef0123456789abcdef"

// NewMemory returns a store with one project, one agent key, one route "gpt-5" and one channel.
func NewMemory(upstreamURL, upstreamFormat string) *Memory {
	sealed, _ := secure.Seal([]byte(InstanceSecret), []byte("sk-upstream"))
	return &Memory{
		Key:      &entity.AgentKey{ID: "key_1", ProjectID: "project_1", Enabled: true, Hash: secure.Digest("agent-key"), Role: "agent"},
		Proj:     &entity.Project{ID: "project_1", Name: "test", RetentionDays: 30, RequestsPerMinute: 1000, Concurrency: 100},
		RuleList: []entity.Rule{{ID: "r1", ProjectID: "project_1", Kind: "builtin", Detector: "email", Label: "EMAIL", Action: "replace", Enabled: true, Version: 1}},
		Route:    &entity.Route{ID: "route_1", ProjectID: "project_1", Name: "gpt-5", Targets: []entity.Target{{ChannelID: "channel_1", UpstreamModel: "gpt-5-upstream", Priority: 1, Weight: 1}}},
		Channel: []entity.Channel{{
			ID: "channel_1", Name: "mock", Format: upstreamFormat, Vendor: vendorOf(upstreamFormat), Endpoint: upstreamURL,
			Enabled: true, Version: 1, AllowPrivateNetwork: true, Currency: "USD",
			Models: []entity.ChannelModel{{Name: "gpt-5-upstream", InputPrice: 1_000_000, OutputPrice: 2_000_000}},
		}},
		Secrets:   []entity.ChannelKey{{ID: "ck_1", ChannelID: "channel_1", Secret: sealed, Enabled: true}},
		ToolCalls: map[string]string{},
	}
}

func vendorOf(format string) string {
	switch format {
	case "anthropic_messages":
		return "anthropic"
	case "google_gemini":
		return "google"
	}
	return "openai"
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

func (store *Memory) RouteByName(_ context.Context, _, name string) (*entity.Route, error) {
	if store.Route == nil || store.Route.Name != name {
		return nil, repository.ErrNotFound
	}
	return store.Route, nil
}

func (store *Memory) Routes(context.Context, string) ([]entity.Route, error) {
	return []entity.Route{*store.Route}, nil
}

func (store *Memory) Channels(context.Context) ([]entity.Channel, error) { return store.Channel, nil }

func (store *Memory) EnabledChannelKeys(context.Context) ([]entity.ChannelKey, error) {
	return store.Secrets, nil
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

func (store *Memory) FailCall(_ context.Context, callID, code string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, call := range store.Calls {
		if call.ID == callID && call.State == "running" {
			call.State, call.ErrorCode = "failed", code
		}
	}
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

func (store *Memory) CallByResponseID(_ context.Context, _, responseID string) (*entity.Call, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, call := range store.Calls {
		if call.ResponseID == responseID && responseID != "" {
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
