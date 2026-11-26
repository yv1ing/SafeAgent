// Package observation defines the protocol-neutral behavior model of one agent trace. Nothing in
// this package knows about wire formats: adapters translate protocol requests, responses and stored
// audit events into these types, and every analyzer reads only this model.
package observation

// View selects which representation of a value an analyzer sees. Raw is what the client or upstream
// exchanged; Redacted is what the gateway may show a project member.
type View string

const (
	ViewRaw      View = "raw"
	ViewRedacted View = "redacted"
)

// Section names the part of a call an item belongs to. Together with the owning call id and the
// item ordinal it is the stable identity a finding, a redaction hit or a trajectory node uses.
type Section string

const (
	SectionRequest      Section = "request"
	SectionResponse     Section = "response"
	SectionInstructions Section = "instructions"
	SectionParams       Section = "params"
	SectionTools        Section = "tools"
)

// Origin says where a piece of content entered the trajectory. It is the "source" half of the
// source-to-sink model and the first input of the three-axis derivation.
type Origin string

const (
	OriginSystem          Origin = "system"
	OriginDeveloper       Origin = "developer"
	OriginUser            Origin = "user"
	OriginAssistant       Origin = "assistant"
	OriginToolResult      Origin = "tool_result"
	OriginToolDescription Origin = "tool_description"
	OriginEnvironment     Origin = "environment"
)

// Trust says whether content may be treated as instructions. External content is data even when it
// looks like an instruction.
type Trust string

const (
	Trusted   Trust = "trusted"
	Untrusted Trust = "untrusted"
	Unknown   Trust = "unknown"
)

// Sink says what a tool call can do with the values it receives. It is the "destination" half of
// the model; an unclassifiable call is Unknown rather than silently internal.
type Sink string

const (
	SinkNone             Sink = "none"
	SinkExternalSend     Sink = "external_send"
	SinkShellExec        Sink = "shell_exec"
	SinkFileRead         Sink = "file_read"
	SinkFileWrite        Sink = "file_write"
	SinkCredentialAccess Sink = "credential_access"
	SinkPayment          Sink = "payment"
	SinkUnknown          Sink = "unknown"
)

func (sink Sink) HasSideEffect() bool {
	switch sink {
	case SinkExternalSend, SinkShellExec, SinkFileWrite, SinkCredentialAccess, SinkPayment:
		return true
	}
	return false
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
	ItemText       ItemKind = "text"
	ItemReasoning  ItemKind = "reasoning"
	ItemRefusal    ItemKind = "refusal"
	ItemToolCall   ItemKind = "tool_call"
	ItemToolResult ItemKind = "tool_result"
	ItemMedia      ItemKind = "media"
	ItemNative     ItemKind = "native"
)

// Value carries both representations of a piece of text. The adapter fills Raw and Shown from the
// same source; Redactions locates the raw byte ranges that differ and ShownRedactions the same
// ranges in the shown text, which is what a reader highlights.
type Value struct {
	Raw             string      `json:"raw"`
	Shown           string      `json:"shown"`
	Redactions      []Span      `json:"redactions,omitempty"`
	ShownRedactions []ShownSpan `json:"shown_redactions,omitempty"`
}

func NewValue(raw, shown string, spans []Span, shownSpans []ShownSpan) Value {
	if shown == "" && raw != "" {
		shown = raw
	}
	return Value{Raw: raw, Shown: shown, Redactions: spans, ShownRedactions: shownSpans}
}

func (value Value) Get(view View) string {
	if view == ViewRedacted && value.Shown != "" {
		return value.Shown
	}
	return value.Raw
}

// Span is a redacted range in the raw representation.
type Span struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Label   string `json:"label"`
	ValueID string `json:"value_id,omitempty"`
	Secret  bool   `json:"secret,omitempty"`
}

// ShownSpan is the same range in the shown text, so a reader can highlight the placeholder without
// re-deriving offsets from the rule set.
type ShownSpan struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Label   string `json:"label"`
	Action  string `json:"action,omitempty"`
	ValueID string `json:"value_id,omitempty"`
}

// Locator identifies one item of one call. CallID is the call that stored the item, which for an
// ancestor item is the ancestor call, not the call under analysis. Ordinal is the item ordinal
// inside its section; Path narrows down a part inside that item.
type Locator struct {
	CallID  string  `json:"call_id,omitempty"`
	Section Section `json:"section,omitempty"`
	Ordinal int     `json:"ordinal,omitempty"`
	Path    string  `json:"path,omitempty"`
	ToolID  string  `json:"tool_id,omitempty"`
}

// Target is the normalized destination of a tool call. Normalization keeps policy attributes stable
// across equivalent spellings.
type Target struct {
	Kind    string `json:"kind,omitempty"`
	Value   string `json:"value,omitempty"`
	Scheme  string `json:"scheme,omitempty"`
	Host    string `json:"host,omitempty"`
	Path    string `json:"path,omitempty"`
	Command string `json:"command,omitempty"`
}

type ToolCall struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Arguments    Value    `json:"arguments"`
	Input        any      `json:"input,omitempty"`
	InputShown   any      `json:"input_shown,omitempty"`
	Target       Target   `json:"target,omitempty"`
	TargetShown  Target   `json:"target_shown,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Sink         Sink     `json:"sink"`
}

type ToolResult struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
	Content []Item `json:"content,omitempty"`
}

type Media struct {
	Kind     string `json:"kind"`
	MIMEType string `json:"mime_type,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     int    `json:"size,omitempty"`
}

// Item is one typed part of a turn. Tool results carry nested items. Origin and Trust travel with
// the content itself, so a detector never has to infer them from the turn's role.
type Item struct {
	Kind       ItemKind       `json:"kind"`
	Origin     Origin         `json:"origin"`
	Trust      Trust          `json:"trust"`
	Text       Value          `json:"text,omitempty"`
	ToolCall   *ToolCall      `json:"tool_call,omitempty"`
	ToolResult *ToolResult    `json:"tool_result,omitempty"`
	Media      *Media         `json:"media,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Locator    Locator        `json:"locator,omitempty"`
}

type Turn struct {
	Index int    `json:"index"`
	Role  Role   `json:"role"`
	Items []Item `json:"items"`
}

// ToolTags are ABAC attributes of a declared tool.
type ToolTags struct {
	Boundary    string   `json:"boundary,omitempty"`
	Sensitivity string   `json:"sensitivity,omitempty"`
	Integrity   string   `json:"integrity,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type ToolSpec struct {
	Name                  string   `json:"name"`
	Kind                  string   `json:"kind"`
	Description           string   `json:"description,omitempty"`
	DescriptionShown      string   `json:"description_shown,omitempty"`
	DescriptionRedactions []Span   `json:"description_redactions,omitempty"`
	Parameters            any      `json:"parameters,omitempty"`
	ParametersShown       any      `json:"parameters_shown,omitempty"`
	Tags                  ToolTags `json:"tags,omitempty"`
}

type Principal struct {
	ProjectID  string `json:"project_id"`
	AgentKeyID string `json:"agent_key_id"`
	Role       string `json:"role,omitempty"`
	TrustLevel int    `json:"trust_level"`
}

type Outcome struct {
	State     string `json:"state,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Finish    string `json:"finish,omitempty"`
}

// Trace is the subject of analysis: one agent call, including its tools, its outcome and the
// metadata a policy may inspect. Cross-call context lives in Context so a single analysis run stays
// reproducible from one call's events.
type Trace struct {
	ID             string         `json:"id"`
	Task           string         `json:"task,omitempty"`
	TaskShown      string         `json:"task_shown,omitempty"`
	Turns          []Turn         `json:"turns"`
	Tools          []ToolSpec     `json:"tools,omitempty"`
	Steps          []ToolStep     `json:"steps,omitempty"`
	Outcome        Outcome        `json:"outcome,omitempty"`
	Context        map[string]any `json:"context,omitempty"`
	Principal      Principal      `json:"principal"`
	PreviousCallID string         `json:"previous_call_id,omitempty"`
}

// ToolStep is the ordered tool chain of a trace: one entry per invocation and, when present, its
// result. Policies and the flow analysis read this projection.
type ToolStep struct {
	Turn        int    `json:"turn"`
	Item        int    `json:"item"`
	CallID      string `json:"call_id,omitempty"`
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	Result      Value  `json:"result,omitempty"`
	IsError     bool   `json:"is_error,omitempty"`
	Target      Target `json:"target,omitempty"`
	TargetShown Target `json:"target_shown,omitempty"`
	Sink        Sink   `json:"sink"`
}
