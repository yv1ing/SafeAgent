// Package redact detects and replaces sensitive values with deterministic placeholders. It
// supports reversible replacement within one call scope and never exposes a secret to a reader.
package redact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type Action string

const (
	ActionReplace Action = "replace"
	ActionBlock   Action = "block"
	ActionAlert   Action = "alert"
)

type RuleKind string

const (
	KindBuiltin RuleKind = "builtin"
	KindRegex   RuleKind = "regex"
)

const (
	LabelPattern             = `^[A-Z][A-Z0-9_]{1,22}$`
	MaximumRules             = 100
	MaximumPattern           = 4096
	maximumPlaceholderBytes  = 1 + 23 + 1 + longDigestLength + 1
	shortDigestLength        = 8
	longDigestLength         = 16
	placeholderDomainContext = "safeagent/placeholder/v1\x00"
)

var (
	ErrImmutablePosition = errors.New("sensitive content appears in a position that cannot be redacted")

	labelExpression = regexp.MustCompile(LabelPattern)
)

type Rule struct {
	Kind     RuleKind `json:"kind" enum:"builtin,regex"`
	Detector string   `json:"detector,omitempty"`
	Pattern  string   `json:"pattern,omitempty"`
	Capture  string   `json:"capture,omitempty"`
	Label    string   `json:"label"`
	Action   Action   `json:"action" enum:"replace,block,alert"`
	Enabled  bool     `json:"enabled"`
}

type compiledRule struct {
	rule       Rule
	detector   Detector
	expression *regexp.Regexp
	group      int
}

type match struct {
	start, end int
}

func (rule compiledRule) find(text string) []match {
	indexes := rule.expression.FindAllStringSubmatchIndex(text, -1)
	matches := make([]match, 0, len(indexes))
	for _, found := range indexes {
		start, end := found[rule.group*2], found[rule.group*2+1]
		if start < 0 || end <= start {
			continue
		}
		if rule.detector.Validate != nil && !rule.detector.Validate(text, start, end) {
			continue
		}
		matches = append(matches, match{start: start, end: end})
	}
	return matches
}

func compile(rules []Rule) ([]compiledRule, error) {
	if len(rules) > MaximumRules {
		return nil, fmt.Errorf("at most %d rules are supported", MaximumRules)
	}
	compiled := make([]compiledRule, 0, len(rules))
	for index, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if rule.Action != ActionReplace && rule.Action != ActionBlock && rule.Action != ActionAlert {
			return nil, fmt.Errorf("rule %d: unsupported action %q", index, rule.Action)
		}
		if !labelExpression.MatchString(rule.Label) {
			return nil, fmt.Errorf("rule %d: label must be an upper-case identifier", index)
		}
		var detector Detector
		var pattern string
		switch rule.Kind {
		case KindBuiltin:
			if rule.Capture != "" {
				return nil, fmt.Errorf("rule %d: builtin detectors do not accept a capture group", index)
			}
			found, ok := lookupDetector(rule.Detector)
			if !ok {
				return nil, fmt.Errorf("rule %d: unknown detector %q", index, rule.Detector)
			}
			detector, pattern = found, found.Pattern
		case KindRegex:
			if len(rule.Pattern) == 0 || len(rule.Pattern) > MaximumPattern {
				return nil, fmt.Errorf("rule %d: pattern must be 1..%d bytes", index, MaximumPattern)
			}
			pattern = rule.Pattern
		default:
			return nil, fmt.Errorf("rule %d: unsupported kind %q", index, rule.Kind)
		}
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("rule %d: invalid regular expression", index)
		}
		if expression.MatchString("") {
			return nil, fmt.Errorf("rule %d: pattern matches empty text", index)
		}
		group := detector.Group
		if rule.Capture != "" {
			if group = expression.SubexpIndex(rule.Capture); group < 0 {
				return nil, fmt.Errorf("rule %d: unknown capture group %q", index, rule.Capture)
			}
		}
		if group > expression.NumSubexp() {
			return nil, fmt.Errorf("rule %d: unknown capture group", index)
		}
		compiled = append(compiled, compiledRule{rule: rule, detector: detector, expression: expression, group: group})
	}
	return compiled, nil
}

func Validate(rules []Rule) error {
	_, err := compile(rules)
	return err
}

type Hit struct {
	Label  string `json:"label"`
	Action Action `json:"action"`
	Path   string `json:"path"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Secret bool   `json:"secret,omitempty"`
}

type Result struct {
	Hits    []Hit
	Blocked bool
	values  int
}

func (result *Result) Values() int {
	return result.values
}

type Engine struct {
	rules     []compiledRule
	secret    []byte
	namespace string
}

func NewEngine(rules []Rule, secret []byte, namespace string) (*Engine, error) {
	compiled, err := compile(rules)
	if err != nil {
		return nil, err
	}
	return &Engine{rules: compiled, secret: secret, namespace: namespace}, nil
}

func (engine *Engine) Empty() bool {
	return len(engine.rules) == 0
}

// Alert rules are skipped: they never alter or reject content, so they cannot make a position immutable.
func (engine *Engine) structural(text string) bool {
	if text == "" {
		return false
	}
	for _, rule := range engine.rules {
		if rule.rule.Action != ActionAlert && len(rule.find(text)) > 0 {
			return true
		}
	}
	return false
}

func (engine *Engine) digest(label, value string) string {
	mac := hmac.New(sha256.New, engine.secret)
	mac.Write([]byte(placeholderDomainContext))
	mac.Write([]byte(engine.namespace))
	mac.Write([]byte{0})
	mac.Write([]byte(label))
	mac.Write([]byte{0})
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil)[:longDigestLength/2])
}

type valueKey struct {
	label, value string
}

type placeholderEntry struct {
	label, value, digest, token string
	secret                      bool
}

type span struct {
	start, end int
	label      string
	secret     bool
}

// Scope owns the placeholders of one request. Short placeholders are issued only after every value of
// the request has been registered, so two values sharing a short digest both get the long form
// regardless of the order they appear in.
type Scope struct {
	engine      *Engine
	entries     map[valueKey]*placeholderEntry
	byToken     map[string]*placeholderEntry
	shortOwners map[string]valueKey
	collided    map[string]bool
	candidates  map[string]bool
}

func (engine *Engine) NewScope() *Scope {
	return &Scope{
		engine:      engine,
		entries:     map[valueKey]*placeholderEntry{},
		byToken:     map[string]*placeholderEntry{},
		shortOwners: map[string]valueKey{},
		collided:    map[string]bool{},
		candidates:  map[string]bool{},
	}
}

type scanned struct {
	hits    []Hit
	spans   []span
	blocked bool
}

func (scope *Scope) scan(text, path string) scanned {
	var found scanned
	for _, rule := range scope.engine.rules {
		for _, item := range rule.find(text) {
			label := rule.rule.Label
			found.hits = append(found.hits, Hit{Label: label, Action: rule.rule.Action, Path: path, Start: item.start, End: item.end, Secret: rule.detector.Secret})
			switch rule.rule.Action {
			case ActionBlock:
				found.blocked = true
			case ActionReplace:
				found.spans = append(found.spans, span{start: item.start, end: item.end, label: label, secret: rule.detector.Secret})
			}
		}
	}
	found.spans = mergeSpans(found.spans)
	return found
}

func mergeSpans(spans []span) []span {
	sort.SliceStable(spans, func(left, right int) bool {
		if spans[left].start != spans[right].start {
			return spans[left].start < spans[right].start
		}
		return spans[left].end > spans[right].end
	})
	merged := make([]span, 0, len(spans))
	for _, item := range spans {
		if len(merged) > 0 && item.start < merged[len(merged)-1].end {
			last := &merged[len(merged)-1]
			last.end = max(last.end, item.end)
			last.secret = last.secret || item.secret
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func (scope *Scope) register(text string, spans []span) {
	for _, item := range spans {
		scope.entry(item.label, text[item.start:item.end], item.secret)
	}
}

func (scope *Scope) entry(label, value string, secret bool) *placeholderEntry {
	key := valueKey{label: label, value: value}
	if entry, found := scope.entries[key]; found {
		entry.secret = entry.secret || secret
		return entry
	}
	entry := &placeholderEntry{label: label, value: value, digest: scope.engine.digest(label, value), secret: secret}
	scope.entries[key] = entry
	short := label + "_" + entry.digest[:shortDigestLength]
	scope.candidates["["+short+"]"] = true
	scope.candidates["["+label+"_"+entry.digest+"]"] = true
	if owner, taken := scope.shortOwners[short]; taken && owner != key {
		scope.collided[short] = true
	} else {
		scope.shortOwners[short] = key
	}
	return entry
}

// An issued placeholder is never withdrawn: a value that already holds its short form keeps it even
// if a later, unregistered value collides with it.
func (scope *Scope) render(text string, spans []span) string {
	var output strings.Builder
	position := 0
	for _, item := range spans {
		output.WriteString(text[position:item.start])
		output.WriteString(scope.issue(scope.entry(item.label, text[item.start:item.end], item.secret)))
		position = item.end
	}
	output.WriteString(text[position:])
	return output.String()
}

func (scope *Scope) issue(entry *placeholderEntry) string {
	if entry.token != "" {
		return entry.token
	}
	length := shortDigestLength
	if scope.collided[entry.label+"_"+entry.digest[:shortDigestLength]] {
		length = longDigestLength
	}
	entry.token = "[" + entry.label + "_" + entry.digest[:length] + "]"
	scope.byToken[entry.token] = entry
	return entry.token
}

// Replace redacts one text on its own; ApplyRequest is the entry point for whole requests.
func (scope *Scope) Replace(text, path string) (string, *Result) {
	applier := &applier{scope: scope, result: &Result{}}
	return applier.text(text, path), applier.result
}

func (scope *Scope) isPlaceholder(value string) bool {
	return scope.candidates[value]
}
