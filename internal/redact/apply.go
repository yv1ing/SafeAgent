package redact

import (
	"encoding/base64"
	"sort"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// ApplyRequest redacts the request in place. Redaction runs twice over the same request: the first,
// dry pass registers every value so placeholder collisions are known before any placeholder is
// issued; the second pass rewrites the request. When Result.Blocked is set the request must not be
// forwarded, because the blocked texts have been cleared.
func (scope *Scope) ApplyRequest(request *model.Request) (*Result, error) {
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
	applier.result.Marks = applier.marks
	return applier.result, nil
}

// Replace redacts one text on its own; ApplyRequest is the entry point for whole requests. The text
// is registered in a dry pass before any placeholder is issued, exactly like ApplyRequest, so both
// paths choose the same long or short form for colliding digests.
func (scope *Scope) Replace(text, path string) (string, *Result) {
	if scope.engine.Empty() {
		return text, &Result{}
	}
	(&applier{scope: scope, result: &Result{}, dry: true}).text(text, path)
	applier := &applier{scope: scope, result: &Result{}}
	shown := applier.text(text, path)
	applier.result.Marks = applier.marks
	return shown, applier.result
}

type applier struct {
	scope  *Scope
	result *Result
	marks  []Mark
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
		applier.recordHits(text, path, found.occurrences)
		applier.result.Blocked = true
		return ""
	}
	shown, marks := applier.scope.render(text, found.spans)
	applier.marks = append(applier.marks, marks...)
	applier.recordHits(text, path, found.occurrences)
	return shown
}

func (applier *applier) recordHits(text, path string, occurrences []occurrence) {
	for _, item := range occurrences {
		applier.result.Hits = append(applier.result.Hits, Hit{
			Label: item.label, Action: item.action, Path: path,
			Start: item.start, End: item.end,
			ValueID: applier.scope.valueID(item.label, text[item.start:item.end]),
			Secret:  item.secret,
		})
	}
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

func (applier *applier) request(request *model.Request) error {
	request.Instructions = applier.text(request.Instructions, "instructions")
	request.User = applier.text(request.User, "user")
	request.SafetyIdentifier = applier.text(request.SafetyIdentifier, "safety_identifier")
	request.PromptCacheKey = applier.text(request.PromptCacheKey, "prompt_cache_key")
	// These values are structural: a rule match cannot be rewritten in place, so the request is
	// refused instead of forwarding the value upstream.
	if err := applier.immutables(request.Model); err != nil {
		return err
	}
	if err := applier.immutables(request.Stop...); err != nil {
		return err
	}
	if err := applier.immutables(request.Include...); err != nil {
		return err
	}
	if err := applier.immutables(request.Modalities...); err != nil {
		return err
	}
	if err := applier.immutables(request.ServiceTier, request.Verbosity, request.Truncation, request.PromptCacheRetention); err != nil {
		return err
	}
	for key := range request.LogitBias {
		if err := applier.immutable(key); err != nil {
			return err
		}
	}
	for _, setting := range request.SafetySettings {
		if err := applier.immutables(setting.Category, setting.Threshold); err != nil {
			return err
		}
	}
	if request.Reasoning != nil {
		if err := applier.immutables(request.Reasoning.Effort, request.Reasoning.Summary, request.Reasoning.Display); err != nil {
			return err
		}
	}
	if request.ToolChoice != nil {
		if err := applier.immutables(request.ToolChoice.Names...); err != nil {
			return err
		}
	}
	if err := applier.metadata(request); err != nil {
		return err
	}
	for index := range request.Items {
		if err := applier.item(&request.Items[index], "items["+strconv.Itoa(index)+"]"); err != nil {
			return err
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
	return applier.raw(request.Raw, "raw")
}

func (applier *applier) metadata(request *model.Request) error {
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

func (applier *applier) tool(tool *model.Tool, path string) error {
	if err := applier.immutable(tool.Name); err != nil {
		return err
	}
	tool.Description = applier.text(tool.Description, path+".description")
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
	return applier.raw(tool.Raw, path+".raw")
}

func (applier *applier) item(item *model.Item, path string) error {
	if item.ToolCall != nil {
		if err := applier.toolCall(item.ToolCall, path+".tool_call"); err != nil {
			return err
		}
	}
	if err := applier.immutables(item.CallID, item.Name); err != nil {
		return err
	}
	originalReasoning := ""
	if item.Kind == model.ItemReasoning {
		originalReasoning = item.Text()
	}
	for index := range item.Parts {
		if err := applier.part(&item.Parts[index], path+".parts["+strconv.Itoa(index)+"]"); err != nil {
			return err
		}
	}
	if len(item.Native) > 0 {
		redacted, err := applier.redactJSON(item.Native, path+".native")
		if err != nil {
			return err
		}
		item.Native = redacted
	}
	if item.Kind == model.ItemReasoning {
		applier.reasoningSignature(item, originalReasoning)
	}
	return applier.raw(item.Raw, path+".raw")
}

func (applier *applier) part(part *model.Part, path string) error {
	switch part.Kind {
	case model.PartText, model.PartRefusal, model.PartReasoningText, model.PartReasoningSummary:
		part.Text = applier.text(part.Text, path+".text")
	case model.PartRedactedReasoning:
		if err := applier.immutable(part.Text); err != nil {
			return err
		}
	case model.PartMedia:
		if err := applier.media(part.Media, path+".media"); err != nil {
			return err
		}
	}
	for index := range part.Citations {
		citation := &part.Citations[index]
		citationPath := path + ".citations[" + strconv.Itoa(index) + "]"
		if err := applier.immutable(citation.URL); err != nil {
			return err
		}
		citation.CitedText = applier.text(citation.CitedText, citationPath+".cited_text")
		citation.Title = applier.text(citation.Title, citationPath+".title")
		if err := applier.raw(citation.Raw, citationPath+".raw"); err != nil {
			return err
		}
	}
	if len(part.Native) > 0 {
		redacted, err := applier.redactJSON(part.Native, path+".native")
		if err != nil {
			return err
		}
		part.Native = redacted
	}
	return applier.raw(part.Raw, path+".raw")
}

// reasoningSignature drops a signature whose text was rewritten by redaction. Its signature was
// issued for the text the client was shown, which is the restored form of what the provider
// produced; the text stays either way.
func (applier *applier) reasoningSignature(item *model.Item, original string) {
	if item.Text() == original || item.Reasoning == nil || item.Reasoning.Signature == nil {
		return
	}
	if item.Reasoning.Signature.TextDigest != model.TextDigest(original) {
		item.Reasoning.Signature = nil
	}
}

func (applier *applier) toolCall(call *model.ToolCall, path string) error {
	if err := applier.immutables(call.ID, call.Name); err != nil {
		return err
	}
	if call.Kind == model.ToolCustom {
		call.Arguments = applier.text(call.Arguments, path+".arguments")
		return nil
	}
	arguments, err := applier.redactJSON([]byte(call.Arguments), path+".arguments")
	call.Arguments = string(arguments)
	return err
}

func (applier *applier) media(media *model.Media, path string) error {
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
	if !applier.dry && redacted != string(decoded) {
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

func (applier *applier) raw(raw *model.Raw, path string) error {
	if raw == nil {
		return nil
	}
	names := make([]string, 0, len(raw.Fields))
	for name := range raw.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		transformed, err := applier.redactJSON(raw.Fields[name], path+"."+name)
		if err != nil {
			return err
		}
		raw.Fields[name] = transformed
	}
	return nil
}
