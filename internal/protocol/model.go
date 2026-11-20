// Package protocol defines the canonical request and response model shared by the four wire
// protocols. It depends only on the standard library; codecs live in protocol/format.
package protocol

import "encoding/json"

type Format string

const (
	Chat      Format = "openai_chat"
	Responses Format = "openai_responses"
	Messages  Format = "anthropic_messages"
	Gemini    Format = "google_gemini"
)

func (format Format) Valid() bool {
	return format == Chat || format == Responses || format == Messages || format == Gemini
}

type Vendor string

const (
	VendorOpenAI    Vendor = "openai"
	VendorAnthropic Vendor = "anthropic"
	VendorGoogle    Vendor = "google"
)

func (format Format) DefaultVendor() Vendor {
	switch format {
	case Messages:
		return VendorAnthropic
	case Gemini:
		return VendorGoogle
	default:
		return VendorOpenAI
	}
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type BlockKind string

const (
	KindText       BlockKind = "text"
	KindReasoning  BlockKind = "reasoning"
	KindRedacted   BlockKind = "redacted_reasoning"
	KindToolCall   BlockKind = "tool_call"
	KindToolResult BlockKind = "tool_result"
	KindMedia      BlockKind = "media"
	KindRefusal    BlockKind = "refusal"
	KindNative     BlockKind = "native"
)

type ToolKind string

const (
	ToolFunction ToolKind = "function"
	ToolCustom   ToolKind = "custom"
	ToolServer   ToolKind = "server"
)

// NativeField is the Extras key holding the complete provider object of a block that has no
// canonical form (native, redacted reasoning, server tool calls and results).
const NativeField = "native"

type Block struct {
	Kind         BlockKind     `json:"kind" required:"true"`
	Text         string        `json:"text,omitempty"`
	Reasoning    *Reasoning    `json:"reasoning,omitempty"`
	ToolCall     *ToolCall     `json:"tool_call,omitempty"`
	ToolResult   *ToolResult   `json:"tool_result,omitempty"`
	Media        *Media        `json:"media,omitempty"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
	Citations    []Citation    `json:"citations,omitempty"`
	Extras       Extras        `json:"extras,omitempty"`
}

type CacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type Citation struct {
	Kind      string `json:"kind,omitempty"`
	CitedText string `json:"cited_text,omitempty"`
	URL       string `json:"url,omitempty"`
	Title     string `json:"title,omitempty"`
	Start     *int64 `json:"start,omitempty"`
	End       *int64 `json:"end,omitempty"`
	Extras    Extras `json:"extras,omitempty"`
}

type Reasoning struct {
	Visibility string     `json:"visibility" enum:"full,summary"`
	ItemID     string     `json:"item_id,omitempty"`
	Signature  *Signature `json:"signature,omitempty"`
}

type ToolCall struct {
	Kind      ToolKind   `json:"kind" enum:"function,custom,server"`
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Arguments string     `json:"arguments"`
	Signature *Signature `json:"signature,omitempty"`
}

type ToolResult struct {
	CallID  string  `json:"call_id"`
	Name    string  `json:"name,omitempty"`
	IsError bool    `json:"is_error,omitempty"`
	Content []Block `json:"content,omitempty"`
}

type Media struct {
	Kind     string `json:"kind" enum:"image,audio,video,document"`
	MIMEType string `json:"mime_type,omitempty"`
	Data     string `json:"data,omitempty"`
	URL      string `json:"url,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	Filename string `json:"filename,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// Extras keeps the fields a codec does not model. They are replayed only towards the format they
// came from; any other target records extras_dropped.
type Extras struct {
	Format Format                     `json:"format,omitempty"`
	Fields map[string]json.RawMessage `json:"fields,omitempty"`
}

func (extras Extras) For(format Format) map[string]json.RawMessage {
	if extras.Format != format {
		return nil
	}
	return extras.Fields
}

type Message struct {
	Role   Role    `json:"role" required:"true"`
	Blocks []Block `json:"blocks"`
	Extras Extras  `json:"extras,omitempty"`
}

type Tool struct {
	Kind         ToolKind        `json:"kind" enum:"function,custom,server"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Parameters   json.RawMessage `json:"parameters,omitempty"`
	Format       json.RawMessage `json:"format,omitempty"`
	Server       *ServerTool     `json:"server,omitempty"`
	Strict       *bool           `json:"strict,omitempty"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
	Extras       Extras          `json:"extras,omitempty"`
}

// ServerTool is a provider-executed tool. Config is the complete tool object in Format, so the
// same format replays it exactly while other formats map it by capability or drop it.
type ServerTool struct {
	Format Format          `json:"format"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config,omitempty"`
}

// ToolChoice restricts tool use. Mode "allowed" limits calls to Names; Required additionally
// forces one of them to be called.
type ToolChoice struct {
	Mode     string   `json:"mode" enum:"auto,none,required,tool,allowed"`
	Names    []string `json:"names,omitempty"`
	Required bool     `json:"required,omitempty"`
}

type ResponseFormat struct {
	Kind        string          `json:"kind" enum:"text,json_object,json_schema"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type ReasoningConfig struct {
	Effort          string `json:"effort,omitempty"`
	BudgetTokens    *int64 `json:"budget_tokens,omitempty"`
	Summary         string `json:"summary,omitempty"`
	Display         string `json:"display,omitempty"`
	BetweenTools    bool   `json:"between_tools,omitempty"`
	IncludeThoughts *bool  `json:"include_thoughts,omitempty"`
}

type Request struct {
	Model                string             `json:"model"`
	Instructions         string             `json:"instructions,omitempty"`
	Messages             []Message          `json:"messages"`
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
	PreviousResponseID   string             `json:"previous_response_id,omitempty"`
	Store                *bool              `json:"store,omitempty"`
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
	Extras               Extras             `json:"extras,omitempty"`
}

type Usage struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_tokens"`
	CacheWrite int64 `json:"cache_write_tokens"`
	// CacheWrite1h is the portion of CacheWrite cached for an hour; it is a breakdown, not an
	// additional count.
	CacheWrite1h int64 `json:"cache_write_1h_tokens,omitempty"`
	Reasoning    int64 `json:"reasoning_tokens"`
}

type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
	FinishRefusal       FinishReason = "refusal"
	FinishPauseTurn     FinishReason = "pause_turn"
	FinishError         FinishReason = "error"
)

type ResponseMeta struct {
	ID                string `json:"id"`
	Model             string `json:"model"`
	Created           int64  `json:"created,omitempty"`
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
	ServiceTier       string `json:"service_tier,omitempty"`
}

type Response struct {
	ResponseMeta
	Blocks       []Block      `json:"blocks"`
	Finish       FinishReason `json:"finish"`
	StopSequence string       `json:"stop_sequence,omitempty"`
	Failure      *Error       `json:"failure,omitempty"`
	Usage        *Usage       `json:"usage,omitempty"`
	Extras       Extras       `json:"extras,omitempty"`
}

func CloneRequest(request *Request) *Request {
	clone := *request
	clone.Extras = cloneExtras(request.Extras)
	clone.Stop = append([]string(nil), request.Stop...)
	clone.Include = append([]string(nil), request.Include...)
	clone.Modalities = append([]string(nil), request.Modalities...)
	if request.Metadata != nil {
		clone.Metadata = make(map[string]string, len(request.Metadata))
		for key, value := range request.Metadata {
			clone.Metadata[key] = value
		}
	}
	if request.LogitBias != nil {
		clone.LogitBias = make(map[string]float64, len(request.LogitBias))
		for key, value := range request.LogitBias {
			clone.LogitBias[key] = value
		}
	}
	if request.ToolChoice != nil {
		choice := *request.ToolChoice
		choice.Names = append([]string(nil), request.ToolChoice.Names...)
		clone.ToolChoice = &choice
	}
	if request.ResponseFormat != nil {
		format := *request.ResponseFormat
		if request.ResponseFormat.Strict != nil {
			strict := *request.ResponseFormat.Strict
			format.Strict = &strict
		}
		clone.ResponseFormat = &format
	}
	if request.Reasoning != nil {
		reasoning := *request.Reasoning
		if request.Reasoning.BudgetTokens != nil {
			budget := *request.Reasoning.BudgetTokens
			reasoning.BudgetTokens = &budget
		}
		if request.Reasoning.IncludeThoughts != nil {
			include := *request.Reasoning.IncludeThoughts
			reasoning.IncludeThoughts = &include
		}
		clone.Reasoning = &reasoning
	}
	for _, pointer := range []**bool{&clone.Store, &clone.ParallelToolCalls, &clone.Logprobs} {
		if *pointer == nil {
			continue
		}
		value := **pointer
		*pointer = &value
	}
	for _, pointer := range []**int64{&clone.MaxOutputTokens, &clone.MaxToolCalls, &clone.TopLogprobs} {
		if *pointer == nil {
			continue
		}
		value := **pointer
		*pointer = &value
	}
	for _, pointer := range []**float64{&clone.Temperature, &clone.TopP, &clone.FrequencyPenalty, &clone.PresencePenalty} {
		if *pointer == nil {
			continue
		}
		value := **pointer
		*pointer = &value
	}
	clone.Messages = make([]Message, len(request.Messages))
	for index, message := range request.Messages {
		clone.Messages[index] = Message{Role: message.Role, Extras: cloneExtras(message.Extras), Blocks: make([]Block, len(message.Blocks))}
		for position, block := range message.Blocks {
			clone.Messages[index].Blocks[position] = CloneBlock(block)
		}
	}
	clone.Tools = make([]Tool, len(request.Tools))
	for index, tool := range request.Tools {
		clone.Tools[index] = tool
		clone.Tools[index].Extras = cloneExtras(tool.Extras)
		if tool.Server != nil {
			server := *tool.Server
			clone.Tools[index].Server = &server
		}
	}
	return &clone
}

func CloneBlock(block Block) Block {
	clone := block
	clone.Extras = cloneExtras(block.Extras)
	clone.Citations = make([]Citation, len(block.Citations))
	for index, citation := range block.Citations {
		clone.Citations[index] = citation
		clone.Citations[index].Extras = cloneExtras(citation.Extras)
	}
	if block.CacheControl != nil {
		cache := *block.CacheControl
		clone.CacheControl = &cache
	}
	if block.Reasoning != nil {
		reasoning := *block.Reasoning
		if block.Reasoning.Signature != nil {
			signature := *block.Reasoning.Signature
			reasoning.Signature = &signature
		}
		clone.Reasoning = &reasoning
	}
	if block.ToolCall != nil {
		call := *block.ToolCall
		if block.ToolCall.Signature != nil {
			signature := *block.ToolCall.Signature
			call.Signature = &signature
		}
		clone.ToolCall = &call
	}
	if block.ToolResult != nil {
		result := *block.ToolResult
		result.Content = make([]Block, len(block.ToolResult.Content))
		for index, nested := range block.ToolResult.Content {
			result.Content[index] = CloneBlock(nested)
		}
		clone.ToolResult = &result
	}
	if block.Media != nil {
		media := *block.Media
		clone.Media = &media
	}
	return clone
}

func cloneExtras(extras Extras) Extras {
	if extras.Fields == nil {
		return extras
	}
	fields := make(map[string]json.RawMessage, len(extras.Fields))
	for key, value := range extras.Fields {
		fields[key] = value
	}
	extras.Fields = fields
	return extras
}
