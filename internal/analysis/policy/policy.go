// Package policy compiles and evaluates the single ordered rule list of a project. A rule is a CEL
// condition over typed principal, tool, target, trace, finding and environment values; there is no
// separate trace-pattern language and no per-category default.
package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/ext"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/observation"
)

const (
	PhaseRequest    = "request"
	PhaseToolCall   = "tool_call"
	PhaseToolResult = "tool_result"
	PhaseResponse   = "response"

	ActionBlock  = "block"
	ActionRecord = "record"

	SeverityInfo   = analysis.SeverityInfo
	SeverityMedium = analysis.SeverityMedium
	SeverityHigh   = analysis.SeverityHigh
)

// Document is what a project saves. Saving compiles, validates and activates it in one step.
type Document struct {
	Rules []Rule `json:"rules"`
	Note  string `json:"note,omitempty"`
}

type Rule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority"`
	Phase    string `json:"phase"`
	When     string `json:"when"`
	Action   string `json:"action"`
	Severity string `json:"severity"`
	Reason   string `json:"reason,omitempty"`
}

func DefaultDocument() Document { return Document{Rules: []Rule{}} }

func (document Document) Hash() string {
	payload, _ := json.Marshal(document)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// The structs below are the typed CEL surface. Field names are part of the policy language; changing
// one is a breaking change for saved rules and must be recorded in the catalogue version.
type Principal struct {
	ProjectID  string `cel:"project_id"`
	AgentKeyID string `cel:"agent_key_id"`
	Role       string `cel:"role"`
	TrustLevel int    `cel:"trust_level"`
}

type Tool struct {
	Name         string   `cel:"name"`
	Kind         string   `cel:"kind"`
	Sink         string   `cel:"sink"`
	Capabilities []string `cel:"capabilities"`
	Boundary     string   `cel:"boundary"`
	Sensitivity  string   `cel:"sensitivity"`
	Integrity    string   `cel:"integrity"`
}

type Target struct {
	Kind    string `cel:"kind"`
	Value   string `cel:"value"`
	Scheme  string `cel:"scheme"`
	Host    string `cel:"host"`
	Path    string `cel:"path"`
	Command string `cel:"command"`
}

type Step struct {
	CallID      string `cel:"call_id"`
	Name        string `cel:"name"`
	Kind        string `cel:"kind"`
	Sink        string `cel:"sink"`
	TargetKind  string `cel:"target_kind"`
	TargetValue string `cel:"target_value"`
	HasResult   bool   `cel:"has_result"`
	IsError     bool   `cel:"is_error"`
}

type Trace struct {
	Steps []Step `cel:"steps"`
}

type Finding struct {
	Code       string  `cel:"code"`
	Origin     string  `cel:"origin"`
	Sink       string  `cel:"sink"`
	Severity   string  `cel:"severity"`
	Source     string  `cel:"source"`
	Mode       string  `cel:"mode"`
	Harm       string  `cel:"harm"`
	Confidence float64 `cel:"confidence"`
}

type ToolResult struct {
	CallID  string `cel:"call_id"`
	Name    string `cel:"name"`
	IsError bool   `cel:"is_error"`
	Text    string `cel:"text"`
	Trust   string `cel:"trust"`
}

type Response struct {
	Status    string `cel:"status"`
	Finish    string `cel:"finish"`
	Text      string `cel:"text"`
	ItemCount int    `cel:"item_count"`
	ToolCalls int    `cel:"tool_calls"`
	Truncated bool   `cel:"truncated"`
}

type Env struct {
	Phase string `cel:"phase"`
}

var typedValues = []any{
	reflect.TypeOf(Principal{}), reflect.TypeOf(Tool{}), reflect.TypeOf(Target{}),
	reflect.TypeOf(Step{}), reflect.TypeOf(Trace{}), reflect.TypeOf(Finding{}),
	reflect.TypeOf(ToolResult{}), reflect.TypeOf(Response{}), reflect.TypeOf(Env{}),
}

// Input is everything one rule evaluation can see.
type Input struct {
	Phase     string
	Principal observation.Principal
	Tool      observation.ToolCall
	Tags      observation.ToolTags
	Trace     *observation.Trace
	Findings  []analysis.Finding
	Results   []ToolResult
	Response  Response
}

type Outcome struct {
	Decisions []DecisionResult
	Block     *DecisionResult
}

type DecisionResult struct {
	RuleID   string
	Name     string
	Action   string
	Severity string
	Reason   string
}

type compiledRule struct {
	rule      Rule
	condition cel.Program
}

type Compiled struct {
	rules    []compiledRule
	hash     string
	hasBlock map[string]bool
}

func newEnvironment() (*cel.Env, error) {
	types := append(append([]any{}, typedValues...), ext.ParseStructTags(true))
	return cel.NewEnv(
		ext.NativeTypes(types...),
		cel.Variable("principal", cel.ObjectType("policy.Principal")),
		cel.Variable("tool", cel.ObjectType("policy.Tool")),
		cel.Variable("target", cel.ObjectType("policy.Target")),
		cel.Variable("trace", cel.ObjectType("policy.Trace")),
		cel.Variable("findings", cel.ListType(cel.ObjectType("policy.Finding"))),
		cel.Variable("results", cel.ListType(cel.ObjectType("policy.ToolResult"))),
		cel.Variable("response", cel.ObjectType("policy.Response")),
		cel.Variable("env", cel.ObjectType("policy.Env")),
		cel.Variable("args", cel.MapType(cel.StringType, cel.DynType)),
	)
}

func Compile(document Document) (*Compiled, error) {
	environment, err := newEnvironment()
	if err != nil {
		return nil, err
	}
	compiled := &Compiled{hash: document.Hash(), hasBlock: map[string]bool{}}
	seen := map[string]bool{}
	for index, rule := range document.Rules {
		if err := validateRule(rule, seen); err != nil {
			return nil, fmt.Errorf("rule %d: %w", index, err)
		}
		seen[rule.ID] = true
		if rule.Action == ActionBlock && rule.Enabled {
			compiled.hasBlock[rule.Phase] = true
		}
		if !rule.Enabled {
			continue
		}
		ast, issues := environment.Compile(rule.When)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("rule %s: %w", rule.ID, issues.Err())
		}
		if ast.OutputType() != cel.BoolType {
			return nil, fmt.Errorf("rule %s: condition must be a boolean", rule.ID)
		}
		program, err := environment.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", rule.ID, err)
		}
		compiled.rules = append(compiled.rules, compiledRule{rule: rule, condition: program})
	}
	sort.SliceStable(compiled.rules, func(left, right int) bool {
		if compiled.rules[left].rule.Priority != compiled.rules[right].rule.Priority {
			return compiled.rules[left].rule.Priority > compiled.rules[right].rule.Priority
		}
		return compiled.rules[left].rule.ID < compiled.rules[right].rule.ID
	})
	return compiled, nil
}

func validateRule(rule Rule, seen map[string]bool) error {
	if rule.ID == "" {
		return fmt.Errorf("a rule needs an id")
	}
	if seen[rule.ID] {
		return fmt.Errorf("duplicate rule id %q", rule.ID)
	}
	switch rule.Phase {
	case PhaseRequest, PhaseToolCall, PhaseToolResult, PhaseResponse:
	default:
		return fmt.Errorf("phase must be one of %q, %q, %q, %q", PhaseRequest, PhaseToolCall, PhaseToolResult, PhaseResponse)
	}
	if rule.Action != ActionBlock && rule.Action != ActionRecord {
		return fmt.Errorf("action must be %q or %q", ActionBlock, ActionRecord)
	}
	if rule.Severity != "" && rule.Severity != SeverityInfo && rule.Severity != SeverityMedium && rule.Severity != SeverityHigh {
		return fmt.Errorf("unsupported severity %q", rule.Severity)
	}
	if strings.TrimSpace(rule.When) == "" {
		return fmt.Errorf("the condition is required")
	}
	return nil
}

func (compiled *Compiled) Hash() string { return compiled.hash }

// HasBlockingRule reports whether a phase can block. When a guard fails, a project with a blocking
// rule is failed closed; a project that only records continues.
func (compiled *Compiled) HasBlockingRule(phase string) bool { return compiled.hasBlock[phase] }

func (compiled *Compiled) Evaluate(ctx context.Context, input Input) (Outcome, error) {
	activation := activationFor(input)
	outcome := Outcome{Decisions: []DecisionResult{}}
	for _, rule := range compiled.rules {
		if rule.rule.Phase != input.Phase {
			continue
		}
		value, _, err := rule.condition.Eval(activation)
		if err != nil {
			return Outcome{}, fmt.Errorf("rule %s: %w", rule.rule.ID, err)
		}
		matched, ok := value.Value().(bool)
		if !ok || !matched {
			continue
		}
		result := DecisionResult{
			RuleID: rule.rule.ID, Name: rule.rule.Name, Action: rule.rule.Action,
			Severity: rule.rule.Severity, Reason: rule.rule.Reason,
		}
		outcome.Decisions = append(outcome.Decisions, result)
		if rule.rule.Action == ActionBlock && outcome.Block == nil {
			block := result
			outcome.Block = &block
		}
	}
	return outcome, nil
}

func activationFor(input Input) map[string]any {
	return map[string]any{
		"principal": principalValue(input.Principal),
		"tool":      toolValue(input.Tool, input.Tags),
		"target":    targetValue(policyTarget(input.Tool)),
		"trace":     Trace{Steps: stepValues(input.Trace)},
		"findings":  findingValues(input.Findings),
		"results":   resultValues(input.Results),
		"response":  input.Response,
		"env":       Env{Phase: input.Phase},
		"args":      flatArguments(policyInput(input.Tool)),
	}
}

func resultValues(results []ToolResult) []ToolResult {
	if len(results) == 0 {
		return []ToolResult{}
	}
	return results
}

// policyTarget selects the view the call page shows. A policy must not receive a value that
// redaction removed from the caller's view.
func policyTarget(tool observation.ToolCall) observation.Target {
	if tool.TargetShown.Kind != "" {
		return tool.TargetShown
	}
	return tool.Target
}

func principalValue(principal observation.Principal) Principal {
	return Principal{
		ProjectID: principal.ProjectID, AgentKeyID: principal.AgentKeyID,
		Role: principal.Role, TrustLevel: principal.TrustLevel,
	}
}

func toolValue(tool observation.ToolCall, tags observation.ToolTags) Tool {
	return Tool{
		Name: tool.Name, Kind: tool.Kind, Sink: string(tool.Sink), Capabilities: append([]string{}, tool.Capabilities...),
		Boundary: tags.Boundary, Sensitivity: tags.Sensitivity, Integrity: tags.Integrity,
	}
}

func targetValue(target observation.Target) Target {
	return Target{Kind: target.Kind, Value: target.Value, Scheme: target.Scheme, Host: target.Host, Path: target.Path, Command: target.Command}
}

func stepValues(trace *observation.Trace) []Step {
	if trace == nil {
		return []Step{}
	}
	steps := make([]Step, 0, len(trace.Steps))
	for _, step := range trace.Steps {
		target := step.Target
		if step.TargetShown.Kind != "" {
			target = step.TargetShown
		}
		steps = append(steps, Step{
			CallID: step.CallID, Name: step.Name, Kind: step.Kind, Sink: string(step.Sink),
			TargetKind: target.Kind, TargetValue: target.Value,
			HasResult: step.Result.Raw != "", IsError: step.IsError,
		})
	}
	return steps
}

func findingValues(findings []analysis.Finding) []Finding {
	values := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		values = append(values, Finding{
			Code: finding.Code, Origin: string(finding.Origin), Sink: string(finding.Sink),
			Severity: finding.Severity, Source: string(finding.Axes.Source), Mode: string(finding.Axes.Mode),
			Harm: string(finding.Axes.Harm), Confidence: finding.Confidence,
		})
	}
	return values
}

func policyInput(tool observation.ToolCall) any {
	if tool.InputShown != nil {
		return tool.InputShown
	}
	return tool.Input
}

// flatArguments exposes only scalar arguments, keyed by their dotted path. Lists and objects would
// make a rule depend on a tool's exact JSON shape; a rule that needs them should match the finding
// code instead.
func flatArguments(input any) map[string]any {
	arguments := map[string]any{}
	flatten(arguments, "", input, 0)
	return arguments
}

func flatten(result map[string]any, prefix string, value any, depth int) {
	if depth > 8 {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			flatten(result, joinKey(prefix, key), typed[key], depth+1)
		}
	case []any:
		for _, entry := range typed {
			if scalar(entry) {
				flatten(result, prefix, entry, depth+1)
			}
		}
	case string, bool, float64, int, int64:
		if prefix != "" {
			result[prefix] = typed
		}
	case json.Number:
		if prefix != "" {
			if integer, err := typed.Int64(); err == nil {
				result[prefix] = integer
			} else if number, err := typed.Float64(); err == nil {
				result[prefix] = number
			}
		}
	}
}

func scalar(value any) bool {
	switch value.(type) {
	case string, bool, float64, int, int64, json.Number:
		return true
	}
	return false
}

func joinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
