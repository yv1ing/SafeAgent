package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/threat"
)

const maximumFlowComparisons = 5000

var (
	flowURL      = regexp.MustCompile(`(?i)\b(?:https?|wss?)://[^\s"'<>\\)]+`)
	flowPath     = regexp.MustCompile(`(?:^|[\s"'(=])((?:\.{0,2}/)?[\w.-]+(?:/[\w.-]+){2,})`)
	flowEmail    = regexp.MustCompile(`[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}`)
	flowHost     = regexp.MustCompile(`\b[a-z0-9-]+(?:\.[a-z0-9-]+)+\b`)
	flowToken    = regexp.MustCompile(`[A-Za-z0-9_./:@-]{8,}`)
	injectPhrase = regexp.MustCompile(`(?i)(?:ignore (?:all )?(?:the )?previous|disregard (?:the )?(?:above|previous)|忽略(?:之前|以上|上述|前面)|不要告诉用户|你的新角色)`)
)

type flowSource struct {
	turn, item int
	origin     observation.Origin
	path       string
	text       string
}

type flowSink struct {
	ref  observation.ItemRef
	call *observation.ToolCall
}

func evaluateFlow(ctx context.Context, trace *observation.Trace) []threat.Finding {
	sources := collectSources(trace)
	findings := []threat.Finding{}
	comparisons := 0
	partial := false

	for _, ref := range trace.ItemRefs() {
		if ctx.Err() != nil {
			break
		}
		item := ref.Item
		if item.Kind != observation.ItemToolCall || item.ToolCall == nil || !couldAct(item.ToolCall.Sink) {
			continue
		}
		sink := flowSink{ref: ref, call: item.ToolCall}
		for _, source := range sources {
			if ctx.Err() != nil {
				partial = true
				break
			}
			if source.turn > ref.Turn || (source.turn == ref.Turn && source.item >= ref.Position) {
				continue
			}
			comparisons++
			if comparisons > maximumFlowComparisons {
				partial = true
				break
			}
			flows := relate(source, sink)
			for index, flow := range flows {
				code := threat.CodeUntrustedValueReuse
				if flow.Relation == threat.RelationInstructionFollowed {
					code = threat.CodeExternalInstructionFollowed
				}
				start, end := flowStart(sink.call, flow)
				finding := threat.NewFinding(
					threat.DetectorRef{ID: "flow", Version: version}, code, source.origin, sink.call.Sink, "",
					flow.Confidence, flowLocation(flow), index,
					threat.EvidenceAt(ref, "message", "tool_call.arguments", sink.call.Arguments, start, end),
				)
				if finding.Fingerprint == "" {
					continue
				}
				finding.Attribution = []threat.Attribution{{Turn: ref.Turn, Item: ref.Position, ToolCallID: sink.call.ID, Source: "tool_call", Locator: ref.Item.Locator}}
				flowCopy := flow
				finding.Flow = &flowCopy
				findings = append(findings, finding)
				if len(findings) >= 200 {
					partial = true
					break
				}
			}
		}
		findings = append(findings, secretFlowFindings(ref, sink.call)...)
		if partial {
			break
		}
	}
	if partial {
		findings = append(findings, threat.NewFinding(
			threat.DetectorRef{ID: "flow", Version: version}, threat.CodeAnalysisIncomplete,
			observation.OriginEnvironment, observation.SinkNone, "", 1, "flow/comparisons", 0,
			threat.Evidence{Section: "run", Path: "flow", View: string(observation.ViewRedacted), Quote: "flow comparison cap reached"},
		))
	}
	return findings
}

// couldAct includes Unknown: a call whose destination could not be classified may still act, so the
// relation detector must not silently treat it as a read-only call.
func couldAct(sink observation.Sink) bool {
	return sink.HasSideEffect() || sink == observation.SinkUnknown
}

func collectSources(trace *observation.Trace) []flowSource {
	sources := []flowSource{}
	for index, tool := range trace.Tools {
		if tool.Description == "" {
			continue
		}
		sources = append(sources, flowSource{
			turn: -1, item: index, origin: observation.OriginToolDescription,
			path: "tools." + tool.Name + ".description", text: tool.Description,
		})
	}
	for _, ref := range trace.ItemRefs() {
		item := ref.Item
		if item.Origin != observation.OriginToolResult || item.ToolResult == nil {
			continue
		}
		if text := observation.ToolResultText(item.ToolResult); text != "" {
			sources = append(sources, flowSource{
				turn: ref.Turn, item: ref.Position, origin: observation.OriginToolResult,
				path: item.Locator.Path + ".tool_result.text", text: text,
			})
		}
	}
	return sources
}

// relate compares one external source with one tool call. Value reuse needs a specific token shared
// by both; instruction following needs a token from the sentence that carries the injected
// instruction. Co-occurrence alone is not a relation.
func relate(source flowSource, sink flowSink) []threat.Flow {
	arguments := sink.call.Arguments.Raw
	flows := []threat.Flow{}
	if arguments == "" || source.text == "" {
		return flows
	}
	seen := map[string]bool{}
	for _, snippet := range snippets(source.text) {
		if !strings.Contains(arguments, snippet) {
			continue
		}
		hash := contentHash(snippet)
		key := threat.RelationValueReuse + "\x00" + hash
		if seen[key] {
			continue
		}
		seen[key] = true
		flows = append(flows, threat.Flow{
			SourceTurn: source.turn, SourceItem: source.item, SourceOrigin: source.origin, SourcePath: source.path,
			SinkToolCallID: sink.call.ID, Sink: sink.call.Sink, Relation: threat.RelationValueReuse,
			ContentHash: hash, Confidence: confidenceOfSnippet(snippet), Matched: snippet,
		})
	}
	for _, token := range instructionTokens(source.text) {
		if !strings.Contains(arguments, token) {
			continue
		}
		hash := contentHash(token)
		key := threat.RelationInstructionFollowed + "\x00" + hash
		if seen[key] {
			continue
		}
		seen[key] = true
		flows = append(flows, threat.Flow{
			SourceTurn: source.turn, SourceItem: source.item, SourceOrigin: source.origin, SourcePath: source.path,
			SinkToolCallID: sink.call.ID, Sink: sink.call.Sink, Relation: threat.RelationInstructionFollowed,
			ContentHash: hash, Confidence: 0.55, Matched: token,
		})
	}
	return flows
}

func snippets(text string) []string {
	result := []string{}
	add := func(value string) {
		value = strings.Trim(value, `"'()[]{}<>,;`)
		if len(value) < 8 || len(value) > 512 {
			return
		}
		for _, existing := range result {
			if existing == value {
				return
			}
		}
		result = append(result, value)
	}
	for _, match := range flowURL.FindAllString(text, 16) {
		add(match)
	}
	for _, match := range flowPath.FindAllStringSubmatch(text, 16) {
		add(match[1])
	}
	for _, match := range flowEmail.FindAllString(text, 16) {
		add(match)
	}
	for _, match := range flowHost.FindAllString(text, 16) {
		add(match)
	}
	for _, match := range flowToken.FindAllString(text, 64) {
		if specificToken(match) {
			add(match)
		}
	}
	if len(result) > 64 {
		result = result[:64]
	}
	return result
}

// specificToken keeps identifiers that are unlikely to occur by coincidence: they mix letters and
// digits or contain a separator. A plain word like "content" proves nothing.
func specificToken(token string) bool {
	hasLetter, hasDigit := false, false
	for _, character := range token {
		switch {
		case character >= '0' && character <= '9':
			hasDigit = true
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z':
			hasLetter = true
		}
	}
	if !hasLetter {
		return false
	}
	return hasDigit || strings.ContainsAny(token, "./:@_-")
}

func confidenceOfSnippet(snippet string) float64 {
	if len(snippet) >= 16 && specificToken(snippet) {
		return 0.8
	}
	return 0.6
}

// instructionTokens returns specific tokens from the sentence that states the injected instruction.
func instructionTokens(text string) []string {
	location := injectPhrase.FindStringIndex(text)
	if location == nil {
		return nil
	}
	start := 0
	if boundary := strings.LastIndexAny(text[:location[0]], ".!?。！？\n"); boundary >= 0 {
		start = boundary + 1
	}
	end := len(text)
	if boundary := strings.IndexAny(text[location[1]:], ".!?。！？\n"); boundary >= 0 {
		end = location[1] + boundary
	}
	sentence := text[start:end]
	tokens := []string{}
	for _, token := range flowToken.FindAllString(sentence, 32) {
		if len(token) >= 6 && specificToken(token) {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func secretFlowFindings(ref observation.ItemRef, call *observation.ToolCall) []threat.Finding {
	if !hasSecretPlaceholder(call.Arguments) {
		return nil
	}
	finding := threat.NewFinding(
		threat.DetectorRef{ID: "flow", Version: version}, threat.CodeSecretFlow,
		observation.OriginAssistant, call.Sink, "", 0.9, locationKey(ref, "tool_call.arguments"), 0,
		threat.EvidenceAt(ref, "message", "tool_call.arguments", call.Arguments, 0, len(call.Arguments.Raw)),
	)
	finding.Attribution = []threat.Attribution{{Turn: ref.Turn, Item: ref.Position, ToolCallID: call.ID, Source: "tool_call", Locator: ref.Item.Locator}}
	return []threat.Finding{finding}
}

func flowStart(call *observation.ToolCall, flow threat.Flow) (int, int) {
	start := strings.Index(call.Arguments.Raw, flow.Matched)
	if start < 0 {
		return 0, len(call.Arguments.Raw)
	}
	return start, start + len(flow.Matched)
}

func flowLocation(flow threat.Flow) string {
	return "flow/" + strconv.Itoa(flow.SourceTurn) + "/" + strconv.Itoa(flow.SourceItem) + "/" + flow.SinkToolCallID + "/" + flow.Relation + "/" + flow.ContentHash
}

func contentHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
