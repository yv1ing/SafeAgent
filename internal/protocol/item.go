package protocol

import "encoding/json"

// Item is one message, reasoning block, tool invocation, tool result or provider-native object.
// Items carry a stable ordinal so concurrent stream items keep their original order. A message
// item owns ordered content parts; tool items own their payload directly.
type Item struct {
	ID           string          `json:"id,omitempty"`
	Kind         ItemKind        `json:"kind" required:"true"`
	Role         Role            `json:"role,omitempty"`
	Ordinal      int             `json:"ordinal"`
	Status       string          `json:"status,omitempty"`
	Parts        []Part          `json:"parts,omitempty"`
	ToolCall     *ToolCall       `json:"tool_call,omitempty"`
	CallID       string          `json:"call_id,omitempty"`
	Name         string          `json:"name,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	Reasoning    *Reasoning      `json:"reasoning,omitempty"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
	Native       json.RawMessage `json:"native,omitempty"`
	Raw          *Raw            `json:"raw,omitempty"`
}

type Part struct {
	ID           string          `json:"id,omitempty"`
	Kind         PartKind        `json:"kind" required:"true"`
	Ordinal      int             `json:"ordinal"`
	Text         string          `json:"text,omitempty"`
	Media        *Media          `json:"media,omitempty"`
	Citations    []Citation      `json:"citations,omitempty"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
	Native       json.RawMessage `json:"native,omitempty"`
	Raw          *Raw            `json:"raw,omitempty"`
}

type Request struct {
	Source               Format             `json:"source,omitempty"`
	Model                string             `json:"model"`
	Instructions         string             `json:"instructions,omitempty"`
	Items                []Item             `json:"items"`
	Tools                []Tool             `json:"tools,omitempty"`
	ToolChoice           *ToolChoice        `json:"tool_choice,omitempty"`
	ParallelToolCalls    *bool              `json:"parallel_tool_calls,omitempty"`
	ResponseFormat       *ResponseFormat    `json:"response_format,omitempty"`
	Reasoning            *ReasoningConfig   `json:"reasoning,omitempty"`
	MaxOutputTokens      *int64             `json:"max_output_tokens,omitempty"`
	Temperature          *float64           `json:"temperature,omitempty"`
	TopP                 *float64           `json:"top_p,omitempty"`
	TopK                 *int64             `json:"top_k,omitempty"`
	Stop                 []string           `json:"stop,omitempty"`
	Stream               bool               `json:"stream"`
	StreamOptions        *StreamOptions     `json:"stream_options,omitempty"`
	PreviousResponseID   string             `json:"previous_response_id,omitempty"`
	Store                *bool              `json:"store,omitempty"`
	Background           *bool              `json:"background,omitempty"`
	Conversation         string             `json:"conversation,omitempty"`
	Include              []string           `json:"include,omitempty"`
	Truncation           string             `json:"truncation,omitempty"`
	MaxToolCalls         *int64             `json:"max_tool_calls,omitempty"`
	PromptCacheRetention string             `json:"prompt_cache_retention,omitempty"`
	SafetyIdentifier     string             `json:"safety_identifier,omitempty"`
	Verbosity            string             `json:"verbosity,omitempty"`
	Logprobs             *bool              `json:"logprobs,omitempty"`
	TopLogprobs          *int64             `json:"top_logprobs,omitempty"`
	LogitBias            map[string]float64 `json:"logit_bias,omitempty"`
	Modalities           []string           `json:"modalities,omitempty"`
	Metadata             map[string]string  `json:"metadata,omitempty"`
	User                 string             `json:"user,omitempty"`
	Seed                 *int64             `json:"seed,omitempty"`
	ServiceTier          string             `json:"service_tier,omitempty"`
	PromptCacheKey       string             `json:"prompt_cache_key,omitempty"`
	FrequencyPenalty     *float64           `json:"frequency_penalty,omitempty"`
	PresencePenalty      *float64           `json:"presence_penalty,omitempty"`
	Raw                  *Raw               `json:"raw,omitempty"`
	Losses               []Loss             `json:"losses,omitempty"`
}

type Response struct {
	Source  Format       `json:"source,omitempty"`
	Meta    ResponseMeta `json:"meta"`
	Items   []Item       `json:"items"`
	Finish  Finish       `json:"finish"`
	Usage   *Usage       `json:"usage,omitempty"`
	Failure *Error       `json:"failure,omitempty"`
	Raw     *Raw         `json:"raw,omitempty"`
}

func (item Item) Text() string {
	var text string
	for _, part := range item.Parts {
		switch part.Kind {
		case PartText, PartRefusal, PartReasoningText, PartReasoningSummary, PartRedactedReasoning:
			text += part.Text
		}
	}
	return text
}

func (item Item) HasText() bool {
	for _, part := range item.Parts {
		if part.Text != "" {
			return true
		}
	}
	return false
}

func (item Item) Empty() bool {
	switch item.Kind {
	case ItemMessage:
		for _, part := range item.Parts {
			if part.Text != "" || part.Media != nil {
				return false
			}
		}
		return true
	case ItemReasoning:
		if item.Reasoning != nil && item.Reasoning.Signature != nil {
			return false
		}
		for _, part := range item.Parts {
			if part.Text != "" || len(part.Native) > 0 {
				return false
			}
		}
		return true
	}
	return false
}

func TextParts(text string) []Part {
	if text == "" {
		return nil
	}
	return []Part{{Kind: PartText, Text: text}}
}

func MessageItem(role Role, text string) Item {
	return Item{Kind: ItemMessage, Role: role, Parts: TextParts(text)}
}

func CloneRequest(request *Request) *Request {
	if request == nil {
		return nil
	}
	clone := *request
	clone.Stop = append([]string(nil), request.Stop...)
	clone.Include = append([]string(nil), request.Include...)
	clone.Modalities = append([]string(nil), request.Modalities...)
	clone.Metadata = cloneMap(request.Metadata)
	clone.LogitBias = cloneMap(request.LogitBias)
	clone.ToolChoice = cloneToolChoice(request.ToolChoice)
	clone.ResponseFormat = cloneResponseFormat(request.ResponseFormat)
	clone.Reasoning = cloneReasoningConfig(request.Reasoning)
	clone.StreamOptions = cloneStreamOptions(request.StreamOptions)
	clone.Store = clonePointer(request.Store)
	clone.Background = clonePointer(request.Background)
	clone.ParallelToolCalls = clonePointer(request.ParallelToolCalls)
	clone.Logprobs = clonePointer(request.Logprobs)
	clone.MaxOutputTokens = clonePointer(request.MaxOutputTokens)
	clone.MaxToolCalls = clonePointer(request.MaxToolCalls)
	clone.TopLogprobs = clonePointer(request.TopLogprobs)
	clone.TopK = clonePointer(request.TopK)
	clone.Temperature = clonePointer(request.Temperature)
	clone.TopP = clonePointer(request.TopP)
	clone.FrequencyPenalty = clonePointer(request.FrequencyPenalty)
	clone.PresencePenalty = clonePointer(request.PresencePenalty)
	clone.Seed = clonePointer(request.Seed)
	clone.Raw = request.Raw.Clone()
	clone.Items = make([]Item, len(request.Items))
	for index, item := range request.Items {
		clone.Items[index] = item.Clone()
	}
	clone.Tools = make([]Tool, len(request.Tools))
	for index, tool := range request.Tools {
		clone.Tools[index] = tool.Clone()
	}
	return &clone
}

func (item Item) Clone() Item {
	clone := item
	clone.Parts = make([]Part, len(item.Parts))
	for index, part := range item.Parts {
		clone.Parts[index] = part.Clone()
	}
	if item.ToolCall != nil {
		call := *item.ToolCall
		if item.ToolCall.Signature != nil {
			signature := *item.ToolCall.Signature
			call.Signature = &signature
		}
		clone.ToolCall = &call
	}
	if item.Reasoning != nil {
		reasoning := *item.Reasoning
		if item.Reasoning.Signature != nil {
			signature := *item.Reasoning.Signature
			reasoning.Signature = &signature
		}
		clone.Reasoning = &reasoning
	}
	if item.CacheControl != nil {
		cache := *item.CacheControl
		clone.CacheControl = &cache
	}
	clone.Native = append(json.RawMessage(nil), item.Native...)
	clone.Raw = item.Raw.Clone()
	return clone
}

func (part Part) Clone() Part {
	clone := part
	if part.Media != nil {
		media := *part.Media
		clone.Media = &media
	}
	if part.CacheControl != nil {
		cache := *part.CacheControl
		clone.CacheControl = &cache
	}
	clone.Citations = make([]Citation, len(part.Citations))
	for index, citation := range part.Citations {
		clone.Citations[index] = citation
		clone.Citations[index].Raw = citation.Raw.Clone()
	}
	clone.Native = append(json.RawMessage(nil), part.Native...)
	clone.Raw = part.Raw.Clone()
	return clone
}

func (tool Tool) Clone() Tool {
	clone := tool
	clone.Parameters = append(json.RawMessage(nil), tool.Parameters...)
	clone.Format = append(json.RawMessage(nil), tool.Format...)
	if tool.Server != nil {
		server := *tool.Server
		server.Config = append(json.RawMessage(nil), tool.Server.Config...)
		clone.Server = &server
	}
	if tool.Strict != nil {
		strict := *tool.Strict
		clone.Strict = &strict
	}
	if tool.CacheControl != nil {
		cache := *tool.CacheControl
		clone.CacheControl = &cache
	}
	clone.Raw = tool.Raw.Clone()
	return clone
}

func cloneToolChoice(choice *ToolChoice) *ToolChoice {
	if choice == nil {
		return nil
	}
	clone := *choice
	clone.Names = append([]string(nil), choice.Names...)
	return &clone
}

func cloneResponseFormat(format *ResponseFormat) *ResponseFormat {
	if format == nil {
		return nil
	}
	clone := *format
	clone.Schema = append(json.RawMessage(nil), format.Schema...)
	clone.Strict = clonePointer(format.Strict)
	return &clone
}

func cloneReasoningConfig(reasoning *ReasoningConfig) *ReasoningConfig {
	if reasoning == nil {
		return nil
	}
	clone := *reasoning
	clone.BudgetTokens = clonePointer(reasoning.BudgetTokens)
	clone.IncludeThoughts = clonePointer(reasoning.IncludeThoughts)
	return &clone
}

func cloneStreamOptions(options *StreamOptions) *StreamOptions {
	if options == nil {
		return nil
	}
	clone := *options
	return &clone
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneMap[K comparable, V any](value map[K]V) map[K]V {
	if value == nil {
		return nil
	}
	clone := make(map[K]V, len(value))
	for key, entry := range value {
		clone[key] = entry
	}
	return clone
}
