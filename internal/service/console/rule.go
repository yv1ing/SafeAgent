package console

import (
	"context"
	"errors"

	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const testScope = "console-test"

type RuleService struct {
	store *repository.Store
}

func NewRuleService(store *repository.Store) *RuleService { return &RuleService{store: store} }

type RuleInput struct {
	Kind     string
	Detector string
	Pattern  string
	Capture  string
	Label    string
	Action   string
	Enabled  bool
	Version  int64
}

func (input RuleInput) entity() entity.Rule {
	return entity.Rule{Kind: input.Kind, Detector: input.Detector, Pattern: input.Pattern, Capture: input.Capture, Label: input.Label, Action: input.Action, Enabled: input.Enabled}
}

// redactRule is the engine's view of input; it is always enabled so validation and tests exercise the pattern
// even for a rule the project has switched off.
func (input RuleInput) redactRule() redact.Rule {
	return redact.Rule{Kind: redact.RuleKind(input.Kind), Detector: input.Detector, Pattern: input.Pattern, Capture: input.Capture, Label: input.Label, Action: redact.Action(input.Action), Enabled: true}
}

func (input RuleInput) validate() error {
	if err := redact.Validate([]redact.Rule{input.redactRule()}); err != nil {
		return Invalid("invalid_rule")
	}
	return nil
}

// A blocking redaction rule refuses whole requests, so only an administrator may introduce or
// keep one; members may still manage replacing and alerting rules.
func requireRuleAdmin(actor Actor, actions ...string) error {
	for _, action := range actions {
		if action == string(redact.ActionBlock) {
			return actor.RequireAdmin()
		}
	}
	return nil
}

func (service *RuleService) Detectors() []redact.Detector { return redact.Detectors }

func (service *RuleService) List(ctx context.Context, actor Actor, projectID string) ([]entity.Rule, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	return service.store.Rules(ctx, projectID)
}

func (service *RuleService) Create(ctx context.Context, actor Actor, projectID string, input RuleInput) (*entity.Rule, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if err := input.validate(); err != nil {
		return nil, err
	}
	if err := requireRuleAdmin(actor, input.Action); err != nil {
		return nil, err
	}
	created, err := service.store.CreateRule(ctx, projectID, input.entity(), redact.MaximumRules)
	if errors.Is(err, repository.ErrRuleLimit) {
		return nil, Conflict("rule_limit_reached")
	}
	return created, Translate(err)
}

func (service *RuleService) Update(ctx context.Context, actor Actor, projectID, ruleID string, input RuleInput) (*entity.Rule, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if err := RequireVersion(input.Version); err != nil {
		return nil, err
	}
	if err := input.validate(); err != nil {
		return nil, err
	}
	existing, err := service.store.Rule(ctx, projectID, ruleID)
	if err != nil {
		return nil, Translate(err)
	}
	if err := requireRuleAdmin(actor, input.Action, existing.Action); err != nil {
		return nil, err
	}
	updated, err := service.store.UpdateRule(ctx, projectID, ruleID, input.entity(), input.Version)
	return updated, Translate(err)
}

func (service *RuleService) Delete(ctx context.Context, actor Actor, projectID, ruleID string) (*entity.Rule, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	rule, err := service.store.Rule(ctx, projectID, ruleID)
	if err != nil {
		return nil, Translate(err)
	}
	if err := requireRuleAdmin(actor, rule.Action); err != nil {
		return nil, err
	}
	return rule, Translate(service.store.DeleteRule(ctx, projectID, ruleID, rule.Action, rule.Version))
}

type RuleTestResult struct {
	Replaced string
	Blocked  bool
	Hits     []redact.Hit
}

// Test runs one rule in isolation against a sample. Project rules are not consulted, so the result
// never depends on the caller's stored configuration.
func (service *RuleService) Test(ctx context.Context, actor Actor, projectID string, input RuleInput, text string) (*RuleTestResult, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if err := input.validate(); err != nil {
		return nil, err
	}
	engine, err := redact.NewEngine([]redact.Rule{input.redactRule()}, []byte(testScope), testScope)
	if err != nil {
		return nil, Invalid("invalid_rule")
	}
	replaced, result := engine.NewScope().Replace(text, "text")
	return &RuleTestResult{Replaced: replaced, Blocked: result.Blocked, Hits: result.Hits}, nil
}
