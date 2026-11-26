// Package pipeline is the online request pipeline: admission, redaction, policy checkpoints,
// routing, upstream attempts, streaming and the terminal call seal.
package pipeline

import (
	"context"
	"encoding/json"
	"sync"

	_ "github.com/yv1ing/SafeAgent/internal/analysis/builtin"
	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

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
