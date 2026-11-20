// Package protocol defines the canonical item model shared by the four wire protocols. It depends
// only on the standard library; codecs live in protocol/format.
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
	VendorSafeAgent Vendor = "safeagent"
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

type ItemKind string

const (
	ItemMessage          ItemKind = "message"
	ItemReasoning        ItemKind = "reasoning"
	ItemToolCall         ItemKind = "tool_call"
	ItemToolResult       ItemKind = "tool_result"
	ItemServerToolCall   ItemKind = "server_tool_call"
	ItemServerToolResult ItemKind = "server_tool_result"
	ItemNative           ItemKind = "native"
)

func (kind ItemKind) IsTool() bool {
	return kind == ItemToolCall || kind == ItemServerToolCall
}

func (kind ItemKind) IsToolResult() bool {
	return kind == ItemToolResult || kind == ItemServerToolResult
}

type PartKind string

const (
	PartText              PartKind = "text"
	PartRefusal           PartKind = "refusal"
	PartReasoningText     PartKind = "reasoning_text"
	PartReasoningSummary  PartKind = "reasoning_summary"
	PartRedactedReasoning PartKind = "redacted_reasoning"
	PartToolArguments     PartKind = "tool_arguments"
	PartMedia             PartKind = "media"
	PartNative            PartKind = "native"
)

type ToolKind string

const (
	ToolFunction ToolKind = "function"
	ToolCustom   ToolKind = "custom"
	ToolServer   ToolKind = "server"
)

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
	Raw       *Raw   `json:"raw,omitempty"`
}

type Reasoning struct {
	Visibility string     `json:"visibility" enum:"full,summary"`
	Signature  *Signature `json:"signature,omitempty"`
}

type ToolCall struct {
	Kind      ToolKind   `json:"kind" enum:"function,custom,server"`
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Arguments string     `json:"arguments,omitempty"`
	Signature *Signature `json:"signature,omitempty"`
}

func (call *ToolCall) Input() string {
	if call == nil {
		return ""
	}
	return call.Arguments
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

type Tool struct {
	Kind         ToolKind        `json:"kind" enum:"function,custom,server"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Parameters   json.RawMessage `json:"parameters,omitempty"`
	Format       json.RawMessage `json:"format,omitempty"`
	Server       *ServerTool     `json:"server,omitempty"`
	Strict       *bool           `json:"strict,omitempty"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
	Raw          *Raw            `json:"raw,omitempty"`
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

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type Usage struct {
	Input        int64            `json:"input_tokens"`
	Output       int64            `json:"output_tokens"`
	CacheRead    int64            `json:"cache_read_tokens"`
	CacheWrite   int64            `json:"cache_write_tokens"`
	CacheWrite1h int64            `json:"cache_write_1h_tokens,omitempty"`
	Reasoning    int64            `json:"reasoning_tokens"`
	Details      map[string]int64 `json:"details,omitempty"`
	Raw          *Raw             `json:"raw,omitempty"`
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

type Finish struct {
	Kind             FinishReason `json:"kind"`
	Raw              string       `json:"raw,omitempty"`
	StopSequence     string       `json:"stop_sequence,omitempty"`
	IncompleteReason string       `json:"incomplete_reason,omitempty"`
}

type ResponseMeta struct {
	ID                string `json:"id"`
	Model             string `json:"model"`
	Created           int64  `json:"created,omitempty"`
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
	ServiceTier       string `json:"service_tier,omitempty"`
}

type FailureClass string

const (
	ClassCancelled FailureClass = "cancelled"
	ClassPolicy    FailureClass = "policy"
	ClassInvalid   FailureClass = "invalid_request"
	ClassTransient FailureClass = "upstream_transient"
	ClassRejected  FailureClass = "upstream_rejected"
	ClassAuth      FailureClass = "upstream_auth"
	ClassRateLimit FailureClass = "rate_limited"
	ClassNoChannel FailureClass = "no_channel"
	ClassInternal  FailureClass = "internal"
)

type Error struct {
	Kind    string       `json:"kind" required:"true" enum:"invalid_request,unsupported,unauthorized,rate_limited,upstream_failure,timeout,blocked,internal"`
	Class   FailureClass `json:"class,omitempty"`
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Status  int          `json:"status,omitempty"`
}

func (failure *Error) Error() string {
	if failure.Message == "" {
		return failure.Code
	}
	return failure.Message
}
