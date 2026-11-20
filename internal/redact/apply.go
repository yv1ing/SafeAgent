package redact

import (
	"encoding/base64"
	"sort"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

// ApplyRequest redacts the request in place. Redaction runs twice over the same request: the first,
// dry pass registers every value so placeholder collisions are known before any placeholder is
// issued; the second pass rewrites the request. When Result.Blocked is set the request must not be
// forwarded, because the blocked texts have been cleared.
func (scope *Scope) ApplyRequest(request *protocol.Request) (*Result, error) {
	if scope.engine.Empty() {
		return &Result{}, nil
	}
	if err := (&applier{scope: scope, result: &Result{}, dry: true}).request(request); err != nil {
		return nil, err
	}
	applier := &applier{scope: scope, result: &Result{}}
	if err := applier.request(request); err != nil {
		return nil, err
	}
	return applier.result, nil
}

type applier struct {
	scope  *Scope
	result *Result
	dry    bool
}

func (applier *applier) text(text, path string) string {
	if text == "" || applier.scope.engine.Empty() {
		return text
	}
	found := applier.scope.scan(text, path)
	if applier.dry {
		if !found.blocked {
			applier.scope.register(text, found.spans)
		}
		return text
	}
	if found.blocked {
		applier.result.Hits = append(applier.result.Hits, found.hits...)
		applier.result.Blocked = true
		return ""
	}
	applier.result.Hits = append(applier.result.Hits, found.hits...)
	applier.result.values += len(found.spans)
	return applier.scope.render(text, found.spans)
}

func (applier *applier) immutable(value string) error {
	if applier.scope.engine.structural(value) || (!applier.dry && applier.scope.isPlaceholder(value)) {
		return ErrImmutablePosition
	}
	return nil
}

func (applier *applier) immutables(values ...string) error {
	for _, value := range values {
		if err := applier.immutable(value); err != nil {
			return err
		}
	}
	return nil
}

func (applier *applier) request(request *protocol.Request) error {
	request.Instructions = applier.text(request.Instructions, "instructions")
	request.User = applier.text(request.User, "user")
	request.SafetyIdentifier = applier.text(request.SafetyIdentifier, "safety_identifier")
	request.PromptCacheKey = applier.text(request.PromptCacheKey, "prompt_cache_key")
	if err := applier.immutables(request.Stop...); err != nil {
		return err
	}
	if request.ToolChoice != nil {
		if err := applier.immutables(request.ToolChoice.Names...); err != nil {
			return err
		}
	}
	if err := applier.metadata(request); err != nil {
		return err
	}
	for index := range request.Messages {
		message := &request.Messages[index]
		path := "messages[" + strconv.Itoa(index) + "]"
		if err := applier.extras(&message.Extras, path+".extras"); err != nil {
			return err
		}
		for position := range message.Blocks {
			if err := applier.block(&message.Blocks[position], path+".blocks["+strconv.Itoa(position)+"]"); err != nil {
				return err
			}
		}
	}
	for index := range request.Tools {
		if err := applier.tool(&request.Tools[index], "tools["+strconv.Itoa(index)+"]"); err != nil {
			return err
		}
	}
	if format := request.ResponseFormat; format != nil {
		if err := applier.immutable(format.Name); err != nil {
			return err
		}
		format.Description = applier.text(format.Description, "response_format.description")
		schema, err := applier.redactJSON(format.Schema, "response_format.schema")
		if err != nil {
			return err
		}
		format.Schema = schema
	}
	return applier.extras(&request.Extras, "extras")
}

func (applier *applier) metadata(request *protocol.Request) error {
	if len(request.Metadata) == 0 {
		return nil
	}
	keys := make([]string, 0, len(request.Metadata))
	for key := range request.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	redacted := make(map[string]string, len(keys))
	for index, key := range keys {
		path := "metadata[" + strconv.Itoa(index) + "]"
		redactedKey := applier.text(key, path+".key")
		if _, taken := redacted[redactedKey]; taken && !applier.result.Blocked {
			return ErrImmutablePosition
		}
		redacted[redactedKey] = applier.text(request.Metadata[key], path+".value")
	}
	if !applier.dry {
		request.Metadata = redacted
	}
	return nil
}

func (applier *applier) tool(tool *protocol.Tool, path string) error {
	if err := applier.immutable(tool.Name); err != nil {
		return err
	}
	tool.Description = applier.text(tool.Description, path+".description")
	if err := applier.extras(&tool.Extras, path+".extras"); err != nil {
		return err
	}
	var err error
	if tool.Parameters, err = applier.redactJSON(tool.Parameters, path+".parameters"); err != nil {
		return err
	}
	if tool.Format, err = applier.redactJSON(tool.Format, path+".format"); err != nil {
		return err
	}
	if tool.Server != nil {
		if tool.Server.Config, err = applier.redactJSON(tool.Server.Config, path+".server.config"); err != nil {
			return err
		}
	}
	return nil
}

func (applier *applier) block(block *protocol.Block, path string) error {
	switch block.Kind {
	case protocol.KindText, protocol.KindRefusal:
		block.Text = applier.text(block.Text, path+".text")
	case protocol.KindReasoning:
		applier.reasoning(block, path+".reasoning")
	case protocol.KindRedacted:
		if err := applier.immutable(block.Text); err != nil {
			return err
		}
	case protocol.KindToolCall:
		if err := applier.toolCall(block.ToolCall, path+".tool_call"); err != nil {
			return err
		}
	case protocol.KindToolResult:
		if err := applier.toolResult(block.ToolResult, path+".tool_result"); err != nil {
			return err
		}
	case protocol.KindMedia:
		if err := applier.media(block.Media, path+".media"); err != nil {
			return err
		}
	}
	if err := applier.extras(&block.Extras, path+".extras"); err != nil {
		return err
	}
	for index := range block.Citations {
		citation := &block.Citations[index]
		citationPath := path + ".citations[" + strconv.Itoa(index) + "]"
		if err := applier.immutable(citation.URL); err != nil {
			return err
		}
		citation.CitedText = applier.text(citation.CitedText, citationPath+".cited_text")
		citation.Title = applier.text(citation.Title, citationPath+".title")
		if err := applier.extras(&citation.Extras, citationPath+".extras"); err != nil {
			return err
		}
	}
	return nil
}

// reasoning redacts a reasoning text the client echoed back. Its signature was issued for the text
// the client was shown, which is the restored form of what the provider produced; redacting it
// again yields the provider's text, so the signature stays valid as long as the echoed text is the
// text it was issued for. Without that proof a rewritten text loses its signature.
func (applier *applier) reasoning(block *protocol.Block, path string) {
	original := block.Text
	block.Text = applier.text(original, path)
	if block.Text == original || block.Reasoning == nil || block.Reasoning.Signature == nil {
		return
	}
	if block.Reasoning.Signature.TextDigest != protocol.TextDigest(original) {
		block.Reasoning.Signature = nil
	}
}

func (applier *applier) toolCall(call *protocol.ToolCall, path string) error {
	if call == nil {
		return nil
	}
	if err := applier.immutables(call.ID, call.Name); err != nil {
		return err
	}
	if call.Kind == protocol.ToolCustom {
		call.Arguments = applier.text(call.Arguments, path+".arguments")
		return nil
	}
	arguments, err := applier.redactJSON([]byte(call.Arguments), path+".arguments")
	call.Arguments = string(arguments)
	return err
}

func (applier *applier) toolResult(result *protocol.ToolResult, path string) error {
	if result == nil {
		return nil
	}
	if err := applier.immutables(result.CallID, result.Name); err != nil {
		return err
	}
	for index := range result.Content {
		if err := applier.block(&result.Content[index], path+".content["+strconv.Itoa(index)+"]"); err != nil {
			return err
		}
	}
	return nil
}

func (applier *applier) media(media *protocol.Media, path string) error {
	if media == nil {
		return nil
	}
	if err := applier.immutables(media.URL, media.FileID, media.Filename); err != nil {
		return err
	}
	if media.Data == "" || !textualMIME(media.MIMEType) {
		return nil
	}
	decoded, err := decodeBase64(media.Data)
	if err != nil {
		return ErrImmutablePosition
	}
	redacted := applier.text(string(decoded), path+".data")
	if !applier.dry {
		media.Data = base64.StdEncoding.EncodeToString([]byte(redacted))
	}
	return nil
}

func decodeBase64(value string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	}
	var err error
	for _, encoding := range encodings {
		decoded, decodeErr := encoding.DecodeString(value)
		if decodeErr == nil {
			return decoded, nil
		}
		err = decodeErr
	}
	return nil, err
}

func textualMIME(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/") || mimeType == "application/json" || mimeType == "application/xml"
}

func (applier *applier) extras(extras *protocol.Extras, path string) error {
	names := make([]string, 0, len(extras.Fields))
	for name := range extras.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		transformed, err := applier.redactJSON(extras.Fields[name], path+"."+name)
		if err != nil {
			return err
		}
		extras.Fields[name] = transformed
	}
	return nil
}
