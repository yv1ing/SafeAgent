// Package adapter is the only adapter between wire protocols and the protocol-neutral observation
// model. A protocol upgrade changes this package and nothing in detection or the console.
package adapter

import (
	"encoding/json"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/observation"
)

const AdapterVersion = "observe/4"

// Redactor returns the shown representation of one text together with the masked ranges of both
// representations. A nil Redactor makes the views identical.
type Redactor func(text, path string) observation.Value

type Options struct {
	TraceID   string
	CallID    string
	Origins   []observation.Locator
	Principal observation.Principal
	Redactor  Redactor
}

func (options Options) value(raw, path string) observation.Value {
	if options.Redactor == nil {
		return observation.NewValue(raw, raw, nil, nil)
	}
	return options.Redactor(raw, path)
}

// Request builds the input side of a trace: instructions, declared tools and every item the client
// sent. The result is deterministic, so the same request and redaction rules always hash to the same
// observation.
func Request(request *model.Request, options Options) *observation.Trace {
	trace := &observation.Trace{ID: options.TraceID, Principal: options.Principal}
	instructions := options.value(request.Instructions, "instructions")
	trace.Task, trace.TaskShown = instructions.Raw, instructions.Shown
	trace.Context = map[string]any{}
	for key, value := range request.Metadata {
		trace.Context[key] = value
	}
	if request.ToolChoice != nil {
		trace.Context["tool_choice"] = map[string]any{"mode": request.ToolChoice.Mode, "names": request.ToolChoice.Names}
	}
	if request.ParallelToolCalls != nil {
		trace.Context["parallel_tool_calls"] = *request.ParallelToolCalls
	}
	if request.PreviousResponseID != "" {
		trace.PreviousCallID = request.PreviousResponseID
	}
	if request.Instructions != "" {
		trace.Turns = append(trace.Turns, observation.Turn{
			Index: 0, Role: observation.RoleSystem,
			Items: []observation.Item{{
				Kind: observation.ItemText, Origin: observation.OriginSystem, Trust: observation.Trusted, Text: instructions,
				Locator: observation.Locator{CallID: options.CallID, Section: observation.SectionInstructions},
			}},
		})
	}
	for _, tool := range request.Tools {
		trace.Tools = append(trace.Tools, ToolSpecOf(tool, options))
	}
	appendItems(trace, request.Items, false, options)
	trace.RebuildSteps()
	return trace
}

// AppendResponse adds the assistant response as the final turn and updates the outcome.
func AppendResponse(trace *observation.Trace, response *model.Response, options Options) {
	if trace == nil || response == nil {
		return
	}
	appendItems(trace, response.Items, true, options)
	trace.Outcome.Finish = string(response.Finish.Kind)
	if response.Failure != nil {
		trace.Outcome.ErrorCode = response.Failure.Code
	}
	trace.RebuildSteps()
}

func appendItems(trace *observation.Trace, items []model.Item, response bool, options Options) {
	for index := range items {
		item := &items[index]
		role := item.Role
		if response {
			role = model.RoleAssistant
		}
		path := "items[" + strconv.Itoa(index) + "]"
		translated := translateItem(item, itemBase(index, items[index], response, options), path, role, response, options)
		if len(translated) == 0 {
			continue
		}
		if count := len(trace.Turns); count > 0 && trace.Turns[count-1].Role == roleOf(role) {
			trace.Turns[count-1].Items = append(trace.Turns[count-1].Items, translated...)
			continue
		}
		trace.Turns = append(trace.Turns, observation.Turn{Index: len(trace.Turns), Role: roleOf(role), Items: translated})
	}
}

// Stored rebuilds a trace from the persisted event payloads. It is the asynchronous and replay
// path; the online path builds incrementally from the same item translation.
func Stored(request model.Request, response *model.Response, options Options) *observation.Trace {
	expanded := request
	expanded.Tools = nil
	trace := Request(&expanded, options)
	if response != nil {
		AppendResponse(trace, response, options)
	}
	return trace
}

// Ancestor is one earlier call of a conversation, as it was recorded.
type Ancestor struct {
	Found    bool
	CallID   string
	Request  model.Request
	Response *model.Response
}

// History prepends the ancestor conversation to the items a call added and returns one origin per
// resulting item, so every merged item keeps the identity of the call that stored it. The bool
// reports that an ancestor request was missing, which makes the trace truncated rather than
// silently partial.
func History(request model.Request, ancestors []Ancestor) (model.Request, []observation.Locator, bool) {
	history, origins, missing := AncestorItems(ancestors)
	if missing {
		return request, nil, true
	}
	if len(history) == 0 {
		return request, origins, false
	}
	request.Items = append(history, request.Items...)
	return request, origins, false
}

// AncestorItems flattens the ancestor conversation into the items a continuation carries and one
// origin per item. The origin is the identity of the call that stored the item, so a merged request
// never attributes an ancestor item to the current call.
func AncestorItems(ancestors []Ancestor) ([]model.Item, []observation.Locator, bool) {
	history := make([]model.Item, 0, len(ancestors))
	origins := make([]observation.Locator, 0, len(ancestors))
	for _, ancestor := range ancestors {
		if !ancestor.Found {
			return nil, nil, true
		}
		for _, item := range ancestor.Request.Items {
			history = append(history, item)
			origins = append(origins, observation.Locator{CallID: ancestor.CallID, Section: observation.SectionRequest, Ordinal: item.Ordinal})
		}
		if ancestor.Response != nil {
			for index, item := range ancestor.Response.Items {
				history = append(history, item)
				origins = append(origins, observation.Locator{CallID: ancestor.CallID, Section: observation.SectionResponse, Ordinal: index})
			}
		}
	}
	return history, origins, false
}

func ToolSpecOf(tool model.Tool, options Options) observation.ToolSpec {
	description := options.value(tool.Description, "tools."+tool.Name+".description")
	spec := observation.ToolSpec{
		Name: tool.Name, Kind: string(tool.Kind), Description: description.Raw,
		DescriptionShown: description.Shown, DescriptionRedactions: description.Redactions,
	}
	if len(tool.Parameters) > 0 {
		var parameters any
		if err := json.Unmarshal(tool.Parameters, &parameters); err == nil {
			spec.Parameters = parameters
		}
		shown := options.value(string(tool.Parameters), "tools."+tool.Name+".parameters").Shown
		if shown != string(tool.Parameters) {
			var parameters any
			if err := json.Unmarshal([]byte(shown), &parameters); err == nil {
				spec.ParametersShown = parameters
			}
		}
	}
	spec.Tags = TagsForTool(tool.Name, tool.Description, observation.Target{})
	return spec
}

func roleOf(role model.Role) observation.Role {
	switch role {
	case model.RoleSystem:
		return observation.RoleSystem
	case model.RoleDeveloper:
		return observation.RoleDeveloper
	case model.RoleAssistant:
		return observation.RoleAssistant
	case model.RoleTool:
		return observation.RoleTool
	default:
		return observation.RoleUser
	}
}

// itemBase is the identity of one stored item: the call that stored it, its section and its ordinal.
// Request items carry the ordinal the recorder assigned; a history prefix overrides the call and
// section so an ancestor item keeps pointing at the ancestor call.
func itemBase(index int, item model.Item, response bool, options Options) observation.Locator {
	if response {
		return observation.Locator{CallID: options.CallID, Section: observation.SectionResponse, Ordinal: index}
	}
	if index < len(options.Origins) {
		origin := options.Origins[index]
		origin.Path = ""
		origin.ToolID = ""
		return origin
	}
	return observation.Locator{CallID: options.CallID, Section: observation.SectionRequest, Ordinal: item.Ordinal}
}

// locator pins one nested path inside an item identity.
func locator(base observation.Locator, path string) observation.Locator {
	base.Path = path
	return base
}

// toolLocator additionally records the logical tool call id the item belongs to.
func toolLocator(base observation.Locator, path, toolID string) observation.Locator {
	base.Path = path
	base.ToolID = toolID
	return base
}

func translateItem(item *model.Item, base observation.Locator, path string, role model.Role, response bool, options Options) []observation.Item {
	switch item.Kind {
	case model.ItemMessage:
		var items []observation.Item
		for index := range item.Parts {
			part := &item.Parts[index]
			partPath := path + ".parts[" + strconv.Itoa(index) + "]"
			if translated, ok := partItem(part, base, partPath, role, response, options); ok {
				items = append(items, translated)
			}
		}
		return items
	case model.ItemReasoning:
		origin, trust := originOf(role, response)
		translated := observation.Item{
			Kind: observation.ItemReasoning, Origin: origin, Trust: trust,
			Text: options.value(item.Text(), path+".text"), Locator: locator(base, path),
		}
		return []observation.Item{translated}
	case model.ItemToolCall:
		if item.ToolCall == nil {
			return nil
		}
		origin, trust := originOf(role, response)
		call := item.ToolCall
		arguments := options.value(call.Arguments, path+".tool_call.arguments")
		input := parseInput(call.Arguments)
		target := TargetOf(call.Name, input)
		toolCall := &observation.ToolCall{
			ID: call.ID, Name: call.Name, Kind: string(call.Kind), Arguments: arguments,
			Input: input, Target: target, Capabilities: Capabilities(call.Name, target),
			Sink: SinkOf(call.Name, target, input),
		}
		if arguments.Shown != arguments.Raw {
			if shownInput := parseInput(arguments.Shown); shownInput != nil {
				toolCall.InputShown = shownInput
				toolCall.TargetShown = TargetOf(call.Name, shownInput)
			}
		}
		translated := observation.Item{
			Kind: observation.ItemToolCall, Origin: origin, Trust: trust, ToolCall: toolCall,
			Locator: toolLocator(base, path, call.ID),
		}
		return []observation.Item{translated}
	case model.ItemToolResult:
		origin, trust := originOf(model.RoleTool, response)
		resultLocator := toolLocator(base, path, item.CallID)
		result := &observation.ToolResult{CallID: item.CallID, Name: item.Name, IsError: item.IsError}
		for index := range item.Parts {
			part := &item.Parts[index]
			partPath := path + ".parts[" + strconv.Itoa(index) + "]"
			if translated, ok := partItem(part, resultLocator, partPath, model.RoleTool, response, options); ok {
				result.Content = append(result.Content, translated)
			}
		}
		return []observation.Item{{
			Kind: observation.ItemToolResult, Origin: origin, Trust: trust, ToolResult: result,
			Locator: resultLocator,
		}}
	case model.ItemServerToolCall:
		if item.ToolCall == nil {
			return []observation.Item{{
				Kind: observation.ItemNative, Origin: observation.OriginAssistant, Trust: observation.Trusted,
				Attributes: map[string]any{"kind": "server_tool_call", "name": toolCallName(item)},
				Locator:    locator(base, path),
			}}
		}
		origin, trust := originOf(role, response)
		call := item.ToolCall
		arguments := options.value(call.Arguments, path+".tool_call.arguments")
		input := parseInput(call.Arguments)
		target := TargetOf(call.Name, input)
		toolCall := &observation.ToolCall{
			ID: call.ID, Name: call.Name, Kind: string(call.Kind), Arguments: arguments,
			Input: input, Target: target, Capabilities: Capabilities(call.Name, target),
			Sink: SinkOf(call.Name, target, input),
		}
		if arguments.Shown != arguments.Raw {
			if shownInput := parseInput(arguments.Shown); shownInput != nil {
				toolCall.InputShown = shownInput
				toolCall.TargetShown = TargetOf(call.Name, shownInput)
			}
		}
		return []observation.Item{{
			Kind: observation.ItemToolCall, Origin: origin, Trust: trust, ToolCall: toolCall,
			Locator: toolLocator(base, path, call.ID),
		}}
	case model.ItemServerToolResult:
		return []observation.Item{{
			Kind: observation.ItemNative, Origin: observation.OriginToolResult, Trust: observation.Untrusted,
			Attributes: map[string]any{"kind": "server_tool_result"},
			Locator:    locator(base, path),
		}}
	case model.ItemNative:
		return []observation.Item{{
			Kind: observation.ItemNative, Origin: observation.OriginAssistant, Trust: TrustOf(role, response),
			Attributes: map[string]any{"kind": "native"},
			Locator:    locator(base, path),
		}}
	}
	return nil
}

func partItem(part *model.Part, base observation.Locator, path string, role model.Role, response bool, options Options) (observation.Item, bool) {
	origin, trust := originOf(role, response)
	item := observation.Item{Origin: origin, Trust: trust, Locator: locator(base, path)}
	switch part.Kind {
	case model.PartText:
		item.Kind = observation.ItemText
		item.Text = options.value(part.Text, path+".text")
	case model.PartRefusal:
		item.Kind = observation.ItemRefusal
		item.Text = options.value(part.Text, path+".text")
	case model.PartMedia:
		if part.Media == nil {
			return item, false
		}
		item.Kind = observation.ItemMedia
		item.Media = &observation.Media{Kind: part.Media.Kind, MIMEType: part.Media.MIMEType, Filename: part.Media.Filename, Size: len(part.Media.Data)}
	case model.PartNative:
		item.Kind = observation.ItemNative
		item.Attributes = map[string]any{"kind": "native"}
	default:
		item.Kind = observation.ItemNative
		item.Attributes = map[string]any{"kind": string(part.Kind)}
	}
	return item, true
}

func toolCallName(item *model.Item) string {
	if item.ToolCall != nil {
		return item.ToolCall.Name
	}
	return ""
}

// originOf classifies content by who produced it. External observations are always untrusted: they
// are data, even when their text says otherwise.
func originOf(role model.Role, response bool) (observation.Origin, observation.Trust) {
	if role == model.RoleTool {
		return observation.OriginToolResult, observation.Untrusted
	}
	if response || role == model.RoleAssistant {
		return observation.OriginAssistant, observation.Trusted
	}
	switch role {
	case model.RoleSystem:
		return observation.OriginSystem, observation.Trusted
	case model.RoleDeveloper:
		return observation.OriginDeveloper, observation.Trusted
	}
	return observation.OriginUser, observation.Trusted
}

func TrustOf(role model.Role, response bool) observation.Trust {
	_, trust := originOf(role, response)
	return trust
}

func parseInput(arguments string) any {
	if arguments == "" {
		return nil
	}
	value, err := model.DecodeJSON([]byte(arguments))
	if err != nil {
		return nil
	}
	return value
}

// ItemOf translates one response item for online phase evaluation. The online path shares this
// translation with the replay path; only the surrounding trace differs.
func ItemOf(item model.Item, path string, options Options) (observation.Item, bool) {
	base := observation.Locator{CallID: options.CallID, Section: observation.SectionResponse}
	translated := translateItem(&item, base, path, model.RoleAssistant, true, options)
	if len(translated) == 0 {
		return observation.Item{}, false
	}
	return translated[0], true
}

// AddAssistantItem appends an item to the last assistant turn, creating the turn when the trace
// does not end with one.
func AddAssistantItem(trace *observation.Trace, item observation.Item) {
	if len(trace.Turns) > 0 {
		last := &trace.Turns[len(trace.Turns)-1]
		if last.Role == observation.RoleAssistant {
			last.Items = append(last.Items, item)
			trace.RebuildSteps()
			return
		}
	}
	trace.Turns = append(trace.Turns, observation.Turn{Index: len(trace.Turns), Role: observation.RoleAssistant, Items: []observation.Item{item}})
	trace.RebuildSteps()
}
