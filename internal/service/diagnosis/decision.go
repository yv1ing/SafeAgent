package diagnosis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/analysis/decision"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observe"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

var (
	ErrDecisionModelUnavailable = errors.New("the decision model is unavailable")
	ErrInvalidDecisionConfig    = errors.New("invalid decision model configuration")
)

type decisionEntry struct {
	updated time.Time
	client  decision.Client
	config  decision.Config
}

type decisionCache struct {
	mu      sync.RWMutex
	entries map[string]decisionEntry
}

// DecisionConfig returns the usable configuration of a project. The API key is decrypted here and
// never leaves the process.
func (service *Service) DecisionConfig(ctx context.Context, projectID string) (*decision.Config, error) {
	stored, err := service.store.DecisionConfig(ctx, projectID)
	if err != nil || stored == nil {
		return nil, err
	}
	config := decision.Config{
		Kind: stored.Kind, Endpoint: stored.Endpoint, Model: stored.Model,
		Timeout:      time.Duration(stored.TimeoutSeconds) * time.Second,
		AllowPrivate: stored.AllowPrivate, UseRaw: stored.UseRaw,
		DailyTokenBudget: stored.DailyTokenBudget, AuthorizeBelow: stored.AuthorizeBelow,
	}
	if stored.APIKey != "" {
		plain, err := secure.Open(service.store.Secret, stored.APIKey)
		if err != nil {
			return nil, err
		}
		config.APIKey = string(plain)
	}
	return &config, nil
}

// StoredDecisionConfig returns the stored row without decrypting the key.
func (service *Service) StoredDecisionConfig(ctx context.Context, projectID string) (*entity.DecisionModelConfig, error) {
	stored, err := service.store.DecisionConfig(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return &entity.DecisionModelConfig{ProjectID: projectID, Kind: decision.KindTypeSafe}, nil
	}
	return stored, nil
}

// SaveDecisionConfig stores a configuration. An empty apiKey keeps the stored key; a non-empty one
// replaces it.
func (service *Service) SaveDecisionConfig(ctx context.Context, projectID string, config entity.DecisionModelConfig, apiKey string) (*entity.DecisionModelConfig, error) {
	if config.Kind != decision.KindTypeSafe && config.Kind != decision.KindOpenAI {
		return nil, fmt.Errorf("%w: unsupported kind", ErrInvalidDecisionConfig)
	}
	if config.Model == "" {
		return nil, fmt.Errorf("%w: the model name is required", ErrInvalidDecisionConfig)
	}
	if config.TimeoutSeconds <= 0 {
		config.TimeoutSeconds = 20
	}
	if config.TimeoutSeconds > 120 {
		return nil, fmt.Errorf("%w: the timeout must be at most 120 seconds", ErrInvalidDecisionConfig)
	}
	if config.AuthorizeBelow <= 0 || config.AuthorizeBelow > 1 {
		config.AuthorizeBelow = 0.15
	}
	if config.DailyTokenBudget < 0 {
		return nil, fmt.Errorf("%w: the budget must not be negative", ErrInvalidDecisionConfig)
	}
	candidate := decision.Config{
		Kind: config.Kind, Endpoint: config.Endpoint, Model: config.Model, APIKey: apiKey,
		Timeout: time.Duration(config.TimeoutSeconds) * time.Second, AllowPrivate: config.AllowPrivate,
	}
	if _, err := decision.NewClient(candidate); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDecisionConfig, err)
	}
	config.ProjectID = projectID
	if apiKey != "" {
		sealed, err := secure.Seal(service.store.Secret, []byte(apiKey))
		if err != nil {
			return nil, err
		}
		config.APIKey = sealed
	} else {
		existing, err := service.store.DecisionConfig(ctx, projectID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			config.APIKey = existing.APIKey
		}
	}
	if err := service.store.SaveDecisionConfig(ctx, &config); err != nil {
		return nil, err
	}
	service.decision.mu.Lock()
	delete(service.decision.entries, projectID)
	service.decision.mu.Unlock()
	return service.store.DecisionConfig(ctx, projectID)
}

func (service *Service) client(ctx context.Context, projectID string) (decision.Client, decision.Config, error) {
	stored, err := service.store.DecisionConfig(ctx, projectID)
	if err != nil || stored == nil {
		return nil, decision.Config{}, ErrDecisionModelUnavailable
	}
	if !stored.Enabled {
		return nil, decision.Config{}, ErrDecisionModelUnavailable
	}
	service.decision.mu.RLock()
	entry, found := service.decision.entries[projectID]
	service.decision.mu.RUnlock()
	if found && entry.updated.Equal(stored.UpdatedAt) {
		return entry.client, entry.config, nil
	}
	config, err := service.DecisionConfig(ctx, projectID)
	if err != nil {
		return nil, decision.Config{}, err
	}
	if config == nil {
		return nil, decision.Config{}, ErrDecisionModelUnavailable
	}
	client, err := decision.NewClient(*config)
	if err != nil {
		return nil, decision.Config{}, err
	}
	service.decision.mu.Lock()
	service.decision.entries[projectID] = decisionEntry{updated: stored.UpdatedAt, client: client, config: *config}
	service.decision.mu.Unlock()
	return client, *config, nil
}

func (service *Service) reserve(ctx context.Context, projectID string, config decision.Config, estimate int64) (time.Time, bool, error) {
	allowed, day, err := service.store.ReserveDecisionTokens(ctx, projectID, estimate, config.DailyTokenBudget)
	if err != nil {
		return day, false, err
	}
	return day, allowed, nil
}

// Evaluate asks the fixed five questions about one trajectory. It returns a nil label with a model
// state that explains why no answer was produced.
func (service *Service) Evaluate(ctx context.Context, projectID string, trace *observation.Trace) (*analysis.ModelLabels, string, error) {
	client, config, err := service.client(ctx, projectID)
	if err != nil {
		if errors.Is(err, ErrDecisionModelUnavailable) {
			return nil, analysis.ModelDisabled, nil
		}
		return nil, analysis.ModelFailed, err
	}
	state := buildState(trace, config.UseRaw, false)
	estimate := estimateTokens(state)
	day, allowed, err := service.reserve(ctx, projectID, config, estimate)
	if err != nil {
		return nil, analysis.ModelFailed, err
	}
	if !allowed {
		return nil, analysis.ModelBudgetExceeded, nil
	}
	answers, usage, err := client.Ask(ctx, state, decision.FixedQuestions())
	if err != nil {
		_ = service.store.SettleDecisionTokens(ctx, projectID, day, estimate, estimate)
		return nil, analysis.ModelFailed, err
	}
	if err := service.store.SettleDecisionTokens(ctx, projectID, day, estimate, usage.Total()); err != nil {
		service.logger.Error("decision model usage could not be settled", "project", projectID, "error", err)
	}
	labels, err := decision.Interpret(answers)
	if err != nil {
		return nil, analysis.ModelFailed, err
	}
	return &analysis.ModelLabels{
		Unsafe: labels.Unsafe, Probability: labels.Probability, Confidence: labels.Confidence,
		ConfidenceKnown: labels.ConfidenceKnown,
		Source:          labels.Source, Mode: labels.Mode, Harm: labels.Harm, Severity: labels.Severity,
		Model: config.Model, AskedAt: time.Now().UTC(),
	}, analysis.ModelOK, nil
}

// Authorize asks only whether the pending tool call may run. The state deliberately excludes tool
// output and the model's own previous text, so injected content cannot vote on its own release.
func (service *Service) Authorize(ctx context.Context, trace *observation.Trace, tool observation.ToolCall) (decision.Answer, error) {
	projectID := trace.Principal.ProjectID
	client, config, err := service.client(ctx, projectID)
	if err != nil {
		return decision.Answer{}, err
	}
	state := buildState(trace, config.UseRaw, true)
	state.Turns = append(state.Turns, decision.Turn{
		Role: string(observation.RoleAssistant),
		Tools: []decision.ToolCall{{
			ID: tool.ID, Name: tool.Name, Arguments: valueFor(tool.Arguments, config.UseRaw), Sink: string(tool.Sink),
			Target: targetValue(tool, config.UseRaw),
		}},
	})
	estimate := estimateTokens(state)
	day, allowed, err := service.reserve(ctx, projectID, config, estimate)
	if err != nil {
		return decision.Answer{}, err
	}
	if !allowed {
		return decision.Answer{}, errors.New("the decision model budget is exhausted")
	}
	answers, usage, err := client.Ask(ctx, state, []decision.Question{decision.AuthorizationQuestion()})
	if err != nil {
		_ = service.store.SettleDecisionTokens(ctx, projectID, day, estimate, estimate)
		return decision.Answer{}, err
	}
	if err := service.store.SettleDecisionTokens(ctx, projectID, day, estimate, usage.Total()); err != nil {
		service.logger.Error("decision model usage could not be settled", "project", projectID, "error", err)
	}
	return decision.InterpretAuthorization(answers)
}

// AuthorizationThreshold is the probability below which a side-effecting call is vetoed.
func (service *Service) Threshold(ctx context.Context, projectID string) float64 {
	stored, err := service.store.DecisionConfig(ctx, projectID)
	if err != nil || stored == nil || stored.AuthorizeBelow <= 0 {
		return 0.15
	}
	return stored.AuthorizeBelow
}

func valueFor(value observation.Value, useRaw bool) string {
	if useRaw {
		return value.Raw
	}
	return value.Shown
}

func targetValue(tool observation.ToolCall, useRaw bool) string {
	if !useRaw && tool.TargetShown.Kind != "" {
		return tool.TargetShown.Value
	}
	return tool.Target.Value
}

func estimateTokens(state decision.State) int64 {
	payload, _ := json.Marshal(state)
	return int64(len(payload)/4) + 256
}

func buildState(trace *observation.Trace, useRaw, authorizationOnly bool) decision.State {
	view := observation.ViewRedacted
	if useRaw {
		view = observation.ViewRaw
	}
	state := decision.State{
		Principal: decision.Principal{
			AgentKeyID: trace.Principal.AgentKeyID, Role: trace.Principal.Role, TrustLevel: trace.Principal.TrustLevel,
		},
		Tools: make([]decision.Tool, 0, len(trace.Tools)),
		Turns: []decision.Turn{},
	}
	if useRaw {
		state.Task = trace.Task
	} else {
		state.Task = trace.TaskShown
	}
	for _, tool := range trace.Tools {
		description := tool.DescriptionShown
		parameters := tool.Parameters
		if useRaw {
			description = tool.Description
		} else if tool.ParametersShown != nil {
			parameters = tool.ParametersShown
		}
		state.Tools = append(state.Tools, decision.Tool{
			Name: tool.Name, Kind: tool.Kind, Description: description,
			Sink:     string(observe.SinkOf(tool.Name, observation.Target{}, nil)),
			Boundary: tool.Tags.Boundary, Sensitivity: tool.Tags.Sensitivity, Integrity: tool.Tags.Integrity,
			Capabilities: tool.Tags.Tags, Parameters: parameters,
		})
	}
	for _, turn := range trace.Turns {
		entry := decision.Turn{Role: string(turn.Role)}
		for _, item := range turn.Items {
			switch item.Kind {
			case observation.ItemText, observation.ItemRefusal:
				if item.Origin == observation.OriginToolResult {
					if !authorizationOnly {
						if text := item.Text.Get(view); text != "" {
							state.Untrusted = append(state.Untrusted, text)
						}
					}
					continue
				}
				if authorizationOnly && (item.Origin == observation.OriginAssistant) {
					continue
				}
				if text := item.Text.Get(view); text != "" {
					entry.Text = append(entry.Text, text)
				}
			case observation.ItemToolCall:
				if authorizationOnly || item.ToolCall == nil || item.ToolCall.Kind == "server" {
					continue
				}
				entry.Tools = append(entry.Tools, decision.ToolCall{
					ID: item.ToolCall.ID, Name: item.ToolCall.Name, Arguments: item.ToolCall.Arguments.Get(view),
					Sink: string(item.ToolCall.Sink), Target: targetValue(*item.ToolCall, useRaw),
				})
			case observation.ItemToolResult:
				if authorizationOnly || item.ToolResult == nil {
					continue
				}
				if text := toolResultText(item.ToolResult, view); text != "" {
					state.Untrusted = append(state.Untrusted, text)
				}
			}
		}
		if len(entry.Text) > 0 || len(entry.Tools) > 0 {
			state.Turns = append(state.Turns, entry)
		}
	}
	return state
}

// toolResultText joins the selected view of the result's content. ToolResultText always returns the
// raw text, which must not be sent when the project uses the redacted view.
func toolResultText(result *observation.ToolResult, view observation.View) string {
	texts := make([]string, 0, len(result.Content))
	for _, item := range result.Content {
		if text := item.Text.Get(view); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}
