// Package observe is the only adapter between wire protocols and the protocol-neutral observation
// model. A protocol upgrade changes this package and nothing in analysis or the console.
package observe

import (
	"encoding/json"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
)

const AdapterVersion = "observe/3"

// Redactor returns the shown representation of one text and the raw ranges it masks. A nil
// Redactor makes both views identical.
type Redactor func(text, path string) (string, []observation.Span)

type Options struct {
	TraceID   string
	Principal observation.Principal
	Redactor  Redactor
}

func (options Options) value(raw, path string) observation.Value {
	if options.Redactor == nil {
		return observation.NewValue(raw, raw, nil)
	}
	shown, spans := options.Redactor(raw, path)
	return observation.NewValue(raw, shown, spans)
}

// Request builds the input side of a trace: instructions, declared tools and every item the client
// sent. The result is deterministic, so the same request and redaction rules always hash to the same
// observation.
func Request(request *protocol.Request, options Options) *observation.Trace {
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
			Items: []observation.Item{{Kind: observation.ItemText, Origin: observation.OriginSystem, Trust: observation.Trusted, Text: instructions}},
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
func AppendResponse(trace *observation.Trace, response *protocol.Response, options Options) {
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

func appendItems(trace *observation.Trace, items []protocol.Item, response bool, options Options) {
	for index := range items {
		item := &items[index]
		role := item.Role
		if response {
			role = protocol.RoleAssistant
		}
		path := "items[" + strconv.Itoa(index) + "]"
		translated := translateItem(item, path, role, response, options)
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
func Stored(request auditevent.Request, response *auditevent.Response, options Options) *observation.Trace {
	expanded := request.Params
	expanded.Items = request.Items
	expanded.Tools = nil
	if len(request.Tools) > 0 {
		expanded.Tools = request.Tools
	}
	trace := Request(&expanded, options)
	if response != nil {
		AppendResponse(trace, &response.Response, options)
	}
	return trace
}

// Ancestor is one earlier call of a conversation, as it was recorded.
type Ancestor struct {
	Found    bool
	Request  auditevent.Request
	Response *auditevent.Response
}

// History prepends the ancestor conversation to the items a call added. The bool reports that an
// ancestor's request was missing, so the caller can mark the trace as truncated instead of
// presenting a partial conversation as complete.
func History(request auditevent.Request, ancestors []Ancestor) (auditevent.Request, bool) {
	history := make([]protocol.Item, 0, len(ancestors))
	for _, ancestor := range ancestors {
		if !ancestor.Found {
			return request, true
		}
		history = append(history, ancestor.Request.Items...)
		if ancestor.Response != nil && len(ancestor.Response.Response.Items) > 0 {
			history = append(history, ancestor.Response.Response.Items...)
		}
	}
	request.Items = append(history, request.Items...)
	return request, false
}

func ToolSpecOf(tool protocol.Tool, options Options) observation.ToolSpec {
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

func roleOf(role protocol.Role) observation.Role {
	switch role {
	case protocol.RoleSystem:
		return observation.RoleSystem
	case protocol.RoleDeveloper:
		return observation.RoleDeveloper
	case protocol.RoleAssistant:
		return observation.RoleAssistant
	case protocol.RoleTool:
		return observation.RoleTool
	default:
		return observation.RoleUser
	}
}

func translateItem(item *protocol.Item, path string, role protocol.Role, response bool, options Options) []observation.Item {
	switch item.Kind {
	case protocol.ItemMessage:
		var items []observation.Item
		for index := range item.Parts {
			part := &item.Parts[index]
			partPath := path + ".parts[" + strconv.Itoa(index) + "]"
			if translated, ok := partItem(part, partPath, role, response, options); ok {
				items = append(items, translated)
			}
		}
		return items
	case protocol.ItemReasoning:
		origin, trust := originOf(role, response)
		translated := observation.Item{
			Kind: observation.ItemReasoning, Origin: origin, Trust: trust,
			Text: options.value(item.Text(), path+".text"), Locator: observation.Locator{Section: sectionOf(role, response), Path: path},
		}
		return []observation.Item{translated}
	case protocol.ItemToolCall:
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
			Locator: observation.Locator{Section: sectionOf(role, response), Path: path, CallID: call.ID},
		}
		return []observation.Item{translated}
	case protocol.ItemToolResult:
		origin, trust := originOf(protocol.RoleTool, response)
		result := &observation.ToolResult{CallID: item.CallID, Name: item.Name, IsError: item.IsError}
		for index := range item.Parts {
			part := &item.Parts[index]
			partPath := path + ".parts[" + strconv.Itoa(index) + "]"
			if translated, ok := partItem(part, partPath, protocol.RoleTool, response, options); ok {
				result.Content = append(result.Content, translated)
			}
		}
		return []observation.Item{{
			Kind: observation.ItemToolResult, Origin: origin, Trust: trust, ToolResult: result,
			Locator: observation.Locator{Section: sectionOf(protocol.RoleTool, response), Path: path, CallID: item.CallID},
		}}
	case protocol.ItemServerToolCall:
		if item.ToolCall == nil {
			return []observation.Item{{
				Kind: observation.ItemNative, Origin: observation.OriginAssistant, Trust: observation.Trusted,
				Attributes: map[string]any{"kind": "server_tool_call", "name": toolCallName(item)},
				Locator:    observation.Locator{Section: sectionOf(role, response), Path: path},
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
			Locator: observation.Locator{Section: sectionOf(role, response), Path: path, CallID: call.ID},
		}}
	case protocol.ItemServerToolResult:
		return []observation.Item{{
			Kind: observation.ItemNative, Origin: observation.OriginToolResult, Trust: observation.Untrusted,
			Attributes: map[string]any{"kind": "server_tool_result"},
			Locator:    observation.Locator{Section: sectionOf(role, response), Path: path},
		}}
	case protocol.ItemNative:
		return []observation.Item{{
			Kind: observation.ItemNative, Origin: observation.OriginAssistant, Trust: TrustOf(role, response),
			Attributes: map[string]any{"kind": "native"},
			Locator:    observation.Locator{Section: sectionOf(role, response), Path: path},
		}}
	}
	return nil
}

func partItem(part *protocol.Part, path string, role protocol.Role, response bool, options Options) (observation.Item, bool) {
	origin, trust := originOf(role, response)
	item := observation.Item{Origin: origin, Trust: trust, Locator: observation.Locator{Section: sectionOf(role, response), Path: path}}
	switch part.Kind {
	case protocol.PartText:
		item.Kind = observation.ItemText
		item.Text = options.value(part.Text, path+".text")
	case protocol.PartRefusal:
		item.Kind = observation.ItemRefusal
		item.Text = options.value(part.Text, path+".text")
	case protocol.PartMedia:
		if part.Media == nil {
			return item, false
		}
		item.Kind = observation.ItemMedia
		item.Media = &observation.Media{Kind: part.Media.Kind, MIMEType: part.Media.MIMEType, Filename: part.Media.Filename, Size: len(part.Media.Data)}
	case protocol.PartNative:
		item.Kind = observation.ItemNative
		item.Attributes = map[string]any{"kind": "native"}
	default:
		item.Kind = observation.ItemNative
		item.Attributes = map[string]any{"kind": string(part.Kind)}
	}
	return item, true
}

func toolCallName(item *protocol.Item) string {
	if item.ToolCall != nil {
		return item.ToolCall.Name
	}
	return ""
}

// originOf classifies content by who produced it. External observations are always untrusted: they
// are data, even when their text says otherwise.
func originOf(role protocol.Role, response bool) (observation.Origin, observation.Trust) {
	if role == protocol.RoleTool {
		return observation.OriginToolResult, observation.Untrusted
	}
	if response || role == protocol.RoleAssistant {
		return observation.OriginAssistant, observation.Trusted
	}
	switch role {
	case protocol.RoleSystem:
		return observation.OriginSystem, observation.Trusted
	case protocol.RoleDeveloper:
		return observation.OriginDeveloper, observation.Trusted
	}
	return observation.OriginUser, observation.Trusted
}

func TrustOf(role protocol.Role, response bool) observation.Trust {
	_, trust := originOf(role, response)
	return trust
}

func sectionOf(role protocol.Role, response bool) string {
	if response {
		return "response"
	}
	return "message"
}

func parseInput(arguments string) any {
	if arguments == "" {
		return nil
	}
	value, err := protocol.DecodeJSON([]byte(arguments))
	if err != nil {
		return nil
	}
	return value
}

// ItemOf translates one response item for online phase evaluation. The online path shares this
// translation with the replay path; only the surrounding trace differs.
func ItemOf(item protocol.Item, path string, options Options) (observation.Item, bool) {
	translated := translateItem(&item, path, protocol.RoleAssistant, true, options)
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
