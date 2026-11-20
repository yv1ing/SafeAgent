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

const AdapterVersion = "observe/2"

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

// Request builds the input side of a trace: instructions, declared tools and every message the
// client sent. The result is deterministic, so the same request and redaction rules always hash to
// the same observation.
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
	for position, message := range request.Messages {
		turn := observation.Turn{Index: len(trace.Turns), Role: roleOf(message.Role)}
		for blockIndex, block := range message.Blocks {
			path := "messages[" + strconv.Itoa(position) + "].blocks[" + strconv.Itoa(blockIndex) + "]"
			if item, ok := itemOf(block, path, message.Role, false, options); ok {
				turn.Items = append(turn.Items, item)
			}
		}
		if len(turn.Items) > 0 {
			trace.Turns = append(trace.Turns, turn)
		}
	}
	trace.RebuildSteps()
	return trace
}

// AppendResponse adds the assistant response as the final turn and updates the outcome.
func AppendResponse(trace *observation.Trace, response *protocol.Response, options Options) {
	if trace == nil || response == nil {
		return
	}
	turn := observation.Turn{Index: len(trace.Turns), Role: observation.RoleAssistant}
	for blockIndex, block := range response.Blocks {
		path := "response.blocks[" + strconv.Itoa(blockIndex) + "]"
		if item, ok := itemOf(block, path, protocol.RoleAssistant, true, options); ok {
			turn.Items = append(turn.Items, item)
		}
	}
	if len(turn.Items) > 0 {
		trace.Turns = append(trace.Turns, turn)
	}
	trace.Outcome.Finish = string(response.Finish)
	if response.Failure != nil {
		trace.Outcome.ErrorCode = response.Failure.Code
	}
	trace.RebuildSteps()
}

// Stored rebuilds a trace from the persisted event payloads. It is the asynchronous and replay
// path; the online path builds incrementally from the same item translation.
func Stored(request auditevent.Request, response *auditevent.Response, options Options) *observation.Trace {
	expanded := request.Params
	expanded.Messages = request.Messages
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

// History prepends the ancestor conversation to the messages a call added. The added bool reports
// that an ancestor's request was missing, so the caller can mark the trace as truncated instead of
// presenting a partial conversation as complete.
func History(request auditevent.Request, ancestors []Ancestor) (auditevent.Request, bool) {
	history := make([]protocol.Message, 0, len(ancestors))
	for _, ancestor := range ancestors {
		if !ancestor.Found {
			return request, true
		}
		history = append(history, ancestor.Request.Messages...)
		if ancestor.Response != nil && len(ancestor.Response.Response.Blocks) > 0 {
			history = append(history, protocol.Message{Role: protocol.RoleAssistant, Blocks: ancestor.Response.Response.Blocks})
		}
	}
	request.Messages = append(history, request.Messages...)
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

func itemOf(block protocol.Block, path string, role protocol.Role, response bool, options Options) (observation.Item, bool) {
	origin, trust := originOf(role, response)
	item := observation.Item{Origin: origin, Trust: trust, Locator: observation.Locator{Section: sectionOf(role, response), Path: path}}
	switch block.Kind {
	case protocol.KindText:
		item.Kind = observation.ItemText
		item.Text = options.value(block.Text, path+".text")
	case protocol.KindRefusal:
		item.Kind = observation.ItemRefusal
		item.Text = options.value(block.Text, path+".text")
	case protocol.KindReasoning:
		item.Kind = observation.ItemReasoning
		item.Text = options.value(block.Text, path+".text")
	case protocol.KindToolCall:
		if block.ToolCall == nil {
			return item, false
		}
		call := block.ToolCall
		item.Kind, item.Origin, item.Trust = observation.ItemToolCall, observation.OriginAssistant, observation.Trusted
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
		item.ToolCall = toolCall
		item.Locator.CallID = call.ID
	case protocol.KindToolResult:
		if block.ToolResult == nil {
			return item, false
		}
		result := block.ToolResult
		item.Kind, item.Origin, item.Trust = observation.ItemToolResult, observation.OriginToolResult, observation.Untrusted
		item.Locator.CallID = result.CallID
		item.ToolResult = &observation.ToolResult{CallID: result.CallID, Name: result.Name, IsError: result.IsError}
		for index, nested := range result.Content {
			if child, ok := itemOf(nested, path+".tool_result.content["+strconv.Itoa(index)+"]", protocol.RoleTool, response, options); ok {
				item.ToolResult.Content = append(item.ToolResult.Content, child)
			}
		}
	case protocol.KindMedia:
		if block.Media == nil {
			return item, false
		}
		item.Kind = observation.ItemMedia
		item.Media = &observation.Media{Kind: block.Media.Kind, MIMEType: block.Media.MIMEType, Filename: block.Media.Filename, Size: len(block.Media.Data)}
	case protocol.KindRedacted:
		item.Kind = observation.ItemNative
		item.Attributes = map[string]any{"kind": string(protocol.KindRedacted), "provider": string(block.Extras.Format)}
	default:
		item.Kind = observation.ItemNative
		item.Attributes = map[string]any{"kind": string(block.Kind)}
	}
	return item, true
}

// originOf classifies content by who produced it. External observations are always untrusted: they
// are data, even when their text says otherwise.
func originOf(role protocol.Role, response bool) (observation.Origin, observation.Trust) {
	if response || role == protocol.RoleAssistant {
		return observation.OriginAssistant, observation.Trusted
	}
	switch role {
	case protocol.RoleSystem:
		return observation.OriginSystem, observation.Trusted
	case protocol.RoleDeveloper:
		return observation.OriginDeveloper, observation.Trusted
	case protocol.RoleTool:
		return observation.OriginToolResult, observation.Untrusted
	}
	return observation.OriginUser, observation.Trusted
}

func sectionOf(role protocol.Role, response bool) string {
	if response {
		return "response"
	}
	if role == protocol.RoleAssistant {
		return "message"
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

// BlockItem translates one response block for online phase evaluation. The online path shares this
// translation with the replay path; only the surrounding trace differs.
func BlockItem(block protocol.Block, path string, options Options) (observation.Item, bool) {
	return itemOf(block, path, protocol.RoleAssistant, true, options)
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
