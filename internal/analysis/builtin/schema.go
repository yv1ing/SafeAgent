package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/observation"
)

const (
	maximumSchemaDepth  = 16
	maximumSchemaChecks = 512
)

func evaluateSchema(_ context.Context, trace *observation.Trace) []analysis.Finding {
	findings := []analysis.Finding{}
	declared := map[string]observation.ToolSpec{}
	for _, tool := range trace.Tools {
		declared[tool.Name] = tool
	}
	choice, choiceNames := toolChoice(trace.Context)
	continued := trace.PreviousCallID != ""

	for _, ref := range trace.ItemRefs() {
		item := ref.Item
		switch item.Kind {
		case observation.ItemToolCall:
			call := item.ToolCall
			if call == nil || call.Kind == "server" {
				continue
			}
			// tool_choice and the declared tool set describe the response this request asks for.
			// They must not be retroactively applied to tool calls in earlier messages.
			if item.Locator.Section != "response" {
				continue
			}
			name := observation.NewValue(call.Name, call.Name, nil)
			if _, found := declared[call.Name]; !found {
				findings = append(findings, makeWholeFinding("schema", analysis.CodeUndeclaredTool, ref, item.Origin, call.Sink,
					"message", "tool_call.name", name))
			}
			if !toolSelected(choice, choiceNames, call.Name) {
				findings = append(findings, makeWholeFinding("schema", analysis.CodeToolChoiceViolation, ref, item.Origin, call.Sink,
					"message", "tool_call.name", name))
			}
			findings = append(findings, inspectArguments(ref, item, declared)...)
		case observation.ItemToolResult:
			result := item.ToolResult
			if result == nil || result.CallID == "" || continued {
				continue
			}
			if _, known := trace.ToolCallByID(result.CallID); !known {
				value := observation.Value{Raw: result.CallID, Shown: result.CallID}
				findings = append(findings, makeWholeFinding("schema", analysis.CodeUnmatchedToolResult, ref, item.Origin, observation.SinkNone,
					"message", "tool_result.call_id", value))
			}
		}
	}
	return findings
}

func toolChoice(context map[string]any) (string, []string) {
	raw, _ := context["tool_choice"].(map[string]any)
	mode, _ := raw["mode"].(string)
	names := []string{}
	switch list := raw["names"].(type) {
	case []any:
		for _, entry := range list {
			if name, ok := entry.(string); ok {
				names = append(names, name)
			}
		}
	case []string:
		names = list
	}
	return mode, names
}

func toolSelected(mode string, names []string, name string) bool {
	switch mode {
	case "none":
		return false
	case "tool", "allowed":
		if len(names) == 0 {
			return true
		}
		for _, candidate := range names {
			if candidate == name {
				return true
			}
		}
		return false
	}
	return true
}

func inspectArguments(ref observation.ItemRef, item observation.Item, declared map[string]observation.ToolSpec) []analysis.Finding {
	call := item.ToolCall
	if call.Kind == "custom" {
		return nil
	}
	raw := strings.TrimSpace(call.Arguments.Raw)
	if raw == "" {
		raw = "{}"
	}
	arguments, err := decodeObject(raw)
	if err != nil {
		return []analysis.Finding{makeWholeFinding("schema", analysis.CodeInvalidToolArguments, ref, item.Origin, call.Sink,
			"message", "tool_call.arguments", call.Arguments)}
	}
	tool, found := declared[call.Name]
	if !found {
		return nil
	}
	schema, ok := tool.Parameters.(map[string]any)
	if !ok || len(schema) == 0 {
		return nil
	}
	checker := &schemaChecker{ref: ref, item: item, ordinals: map[string]int{}}
	checker.value(arguments, schema, 0, "tool_call.arguments")
	return checker.findings
}

var errIncompleteJSON = errors.New("incomplete JSON")
var errTrailingJSON = errors.New("trailing JSON")
var errInvalidObject = errors.New("expected JSON object")

func decodeValue(raw string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errIncompleteJSON
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errTrailingJSON
	}
	return value, nil
}

func decodeObject(raw string) (map[string]any, error) {
	value, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errInvalidObject
	}
	return object, nil
}

type schemaChecker struct {
	ref           observation.ItemRef
	item          observation.Item
	findings      []analysis.Finding
	checks        int
	violations    int
	ordinals      map[string]int
	partial       bool
	indeterminate bool
	probe         bool
}

func (checker *schemaChecker) add(code, path, detail string) {
	checker.violations++
	if checker.ordinals == nil {
		checker.ordinals = map[string]int{}
	}
	ordinal := checker.ordinals[path]
	checker.ordinals[path] = ordinal + 1
	origin := checker.item.Origin
	if code == analysis.CodeAnalysisIncomplete {
		origin = observation.OriginEnvironment
	}
	start, end := matchOffsets(checker.item.ToolCall.Arguments.Raw, detail)
	finding := analysis.NewFinding(
		analysis.DetectorRef{ID: "schema", Version: version}, code, origin, checker.item.ToolCall.Sink, "",
		confidenceFor(code), locationKey(checker.ref, path), ordinal,
		analysis.EvidenceAt(checker.ref, "message", path, checker.item.ToolCall.Arguments, start, end),
	)
	finding.Attribution = []analysis.Attribution{{Turn: checker.ref.Turn, Item: checker.ref.Position, ToolCallID: checker.item.Locator.CallID, Source: "message"}}
	if !checker.probe {
		checker.findings = append(checker.findings, finding)
	}
}

func (checker *schemaChecker) indeterminateAt(path, detail string) {
	checker.indeterminate = true
	if !checker.partial {
		checker.partial = true
		checker.add(analysis.CodeAnalysisIncomplete, path, detail)
	}
}

func (checker *schemaChecker) exhausted(depth int, path string) bool {
	checker.checks++
	if depth <= maximumSchemaDepth && checker.checks <= maximumSchemaChecks {
		return false
	}
	checker.indeterminateAt(path, "")
	return true
}

var unsupportedSchemaKeywords = []string{
	"$ref", "$dynamicRef", "allOf", "not", "if", "then", "else", "dependentSchemas",
	"dependentRequired", "prefixItems", "contains", "unevaluatedProperties", "unevaluatedItems",
	"propertyNames", "uniqueItems", "multipleOf", "patternProperties", "additionalItems",
}

func (checker *schemaChecker) value(value any, schema map[string]any, depth int, path string) {
	if checker.exhausted(depth, path) {
		return
	}
	if len(schema) > 64 {
		checker.indeterminateAt(path, "")
		return
	}
	for _, keyword := range unsupportedSchemaKeywords {
		if _, present := schema[keyword]; present {
			checker.indeterminateAt(path, keyword)
			break
		}
	}
	if kind, present := schema["type"]; present {
		if !knownType(kind) {
			checker.indeterminateAt(path, "")
		} else if !matchesType(value, kind) {
			checker.add(analysis.CodeArgumentTypeMismatch, path, "")
			return
		}
	}
	if !checker.alternatives(value, schema, depth, path) {
		return
	}
	if items, ok := schema["items"].([]any); ok && len(items) > 0 {
		checker.indeterminateAt(path, "items")
	}
	if pattern, ok := schema["pattern"].(string); ok && pattern != "" {
		if _, err := regexp.Compile(pattern); err != nil {
			checker.indeterminateAt(path, "pattern")
		}
	}
	if violation := constraintViolation(value, schema); violation != "" {
		checker.add(analysis.CodeArgumentConstraintViolation, path, "")
	}
	if object, ok := value.(map[string]any); ok {
		checker.object(object, schema, depth, path)
	}
	if array, ok := value.([]any); ok {
		if schema["items"] == false && len(array) > 0 {
			checker.add(analysis.CodeArgumentConstraintViolation, path, "items")
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for index, item := range array {
				checker.value(item, itemSchema, depth+1, path+"["+strconv.Itoa(index)+"]")
			}
		}
	}
}

// alternatives checks anyOf and oneOf without letting a partial branch count as a match. A probe
// checker runs the branch quietly; a branch matches only when it produced no violation and did not
// stop on an unsupported keyword.
func (checker *schemaChecker) alternatives(value any, schema map[string]any, depth int, path string) bool {
	for _, keyword := range []string{"anyOf", "oneOf"} {
		raw, present := schema[keyword]
		if !present {
			continue
		}
		branches, _ := raw.([]any)
		if len(branches) == 0 {
			checker.indeterminateAt(path, keyword)
			continue
		}
		matches := 0
		indeterminate := false
		for _, branch := range branches {
			branchSchema, ok := branch.(map[string]any)
			if !ok {
				indeterminate = true
				continue
			}
			probe := &schemaChecker{ref: checker.ref, item: checker.item, checks: checker.checks, ordinals: map[string]int{}, probe: true}
			probe.value(value, branchSchema, depth+1, path)
			checker.checks = probe.checks
			switch {
			case probe.indeterminate:
				indeterminate = true
			case probe.violations == 0:
				matches++
			}
		}
		if indeterminate {
			checker.indeterminateAt(path, keyword)
			continue
		}
		if matches == 0 {
			checker.add(analysis.CodeArgumentTypeMismatch, path, "")
			return false
		}
		if keyword == "oneOf" && matches > 1 {
			checker.add(analysis.CodeArgumentConstraintViolation, path, "")
		}
	}
	return true
}

func (checker *schemaChecker) object(object, schema map[string]any, depth int, path string) {
	required, _ := schema["required"].([]any)
	if len(object) > 256 || len(required) > 256 {
		checker.indeterminateAt(path, "")
		return
	}
	for _, entry := range required {
		name, ok := entry.(string)
		if !ok {
			checker.indeterminateAt(path, "")
		} else if _, exists := object[name]; !exists {
			checker.add(analysis.CodeMissingRequiredArgument, path, name)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	for _, name := range sortedKeys(object) {
		child := path + "." + name
		property, declared := properties[name]
		switch {
		case declared && isSchema(property):
			checker.value(object[name], property.(map[string]any), depth+1, child)
		case declared && property == false:
			checker.add(analysis.CodeArgumentConstraintViolation, child, name)
		case declared:
		case schema["additionalProperties"] == false:
			checker.add(analysis.CodeArgumentConstraintViolation, child, name)
		case isSchema(schema["additionalProperties"]):
			checker.value(object[name], schema["additionalProperties"].(map[string]any), depth+1, child)
		}
	}
}

func isSchema(value any) bool {
	_, ok := value.(map[string]any)
	return ok
}

func constraintViolation(value any, schema map[string]any) string {
	if allowed, present := schema["enum"].([]any); present {
		found := false
		for _, candidate := range allowed {
			if sameJSON(candidate, value) {
				found = true
				break
			}
		}
		if !found {
			return "not one of the enumerated values"
		}
	}
	if expected, present := schema["const"]; present && !sameJSON(expected, value) {
		return "not the constant value"
	}
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return ""
		}
		if minimum, ok := toFloat(schema["minimum"]); ok && number < minimum {
			return "below minimum"
		}
		if maximum, ok := toFloat(schema["maximum"]); ok && number > maximum {
			return "above maximum"
		}
		if minimum, ok := toFloat(schema["exclusiveMinimum"]); ok && number <= minimum {
			return "not above exclusiveMinimum"
		}
		if maximum, ok := toFloat(schema["exclusiveMaximum"]); ok && number >= maximum {
			return "not below exclusiveMaximum"
		}
	case string:
		if violation := sizeViolation(utf8.RuneCountInString(typed), schema, "minLength", "maxLength", "length"); violation != "" {
			return violation
		}
		if pattern, ok := schema["pattern"].(string); ok && pattern != "" {
			expression, err := regexp.Compile(pattern)
			if err != nil {
				return ""
			}
			if !expression.MatchString(typed) {
				return "does not match pattern"
			}
		}
	case []any:
		return sizeViolation(len(typed), schema, "minItems", "maxItems", "item count")
	case map[string]any:
		return sizeViolation(len(typed), schema, "minProperties", "maxProperties", "property count")
	}
	return ""
}

func sizeViolation(size int, schema map[string]any, minimumKeyword, maximumKeyword, noun string) string {
	if minimum, ok := toInt(schema[minimumKeyword]); ok && int64(size) < minimum {
		return noun + " below " + minimumKeyword
	}
	if maximum, ok := toInt(schema[maximumKeyword]); ok && int64(size) > maximum {
		return noun + " above " + maximumKeyword
	}
	return ""
}

func matchesType(value any, kind any) bool {
	if alternatives, ok := kind.([]any); ok {
		for _, alternative := range alternatives {
			if matchesType(value, alternative) {
				return true
			}
		}
		return false
	}
	switch kind {
	case "null":
		return value == nil
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		return ok && integerNumber(number)
	}
	return true
}

func knownType(kind any) bool {
	if alternatives, ok := kind.([]any); ok {
		if len(alternatives) == 0 || len(alternatives) > 7 {
			return false
		}
		for _, alternative := range alternatives {
			if !knownType(alternative) {
				return false
			}
		}
		return true
	}
	switch kind {
	case "null", "object", "array", "string", "boolean", "number", "integer":
		return true
	}
	return false
}

func integerNumber(number json.Number) bool {
	mantissa, exponentText, hasExponent := strings.Cut(strings.ToLower(string(number)), "e")
	digits := strings.ReplaceAll(strings.TrimPrefix(mantissa, "-"), ".", "")
	if strings.Trim(digits, "0") == "" {
		return true
	}
	exponent := int64(0)
	if hasExponent {
		parsed, err := strconv.ParseInt(exponentText, 10, 32)
		if err != nil {
			return !strings.HasPrefix(exponentText, "-")
		}
		exponent = parsed
	}
	fractionDigits := 0
	if decimal := strings.IndexByte(mantissa, '.'); decimal >= 0 {
		fractionDigits = len(mantissa) - decimal - 1
	}
	trailingZeros := len(digits) - len(strings.TrimRight(digits, "0"))
	return exponent+int64(trailingZeros) >= int64(fractionDigits)
}

func sameJSON(left, right any) bool {
	if leftNumber, leftOK := numericValue(left); leftOK {
		rightNumber, rightOK := numericValue(right)
		return rightOK && leftNumber == rightNumber
	}
	return string(marshal(left)) == string(marshal(right))
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	}
	return 0, false
}

func marshal(value any) []byte {
	payload, _ := json.Marshal(value)
	return payload
}

func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for position := index; position > 0 && values[position] < values[position-1]; position-- {
			values[position], values[position-1] = values[position-1], values[position]
		}
	}
}

func toInt(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Int64()
		return result, err == nil
	case float64:
		return int64(typed), typed == float64(int64(typed))
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	}
	return 0, false
}

func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Float64()
		return result, err == nil
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	}
	return 0, false
}
