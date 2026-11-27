package trajectory

import (
	"encoding/json"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
)

type Start struct {
	ProjectID         string        `json:"project_id"`
	AgentKeyID        string        `json:"agent_key_id"`
	SessionID         string        `json:"session_id,omitempty"`
	Goal              string        `json:"goal,omitempty"`
	Harness           string        `json:"harness,omitempty"`
	Lane              classify.Lane `json:"lane"`
	Model             string        `json:"model,omitempty"`
	Operation         string        `json:"operation,omitempty"`
	PolicyVersion     int64         `json:"policy_version,omitempty"`
	PolicyHash        string        `json:"policy_hash,omitempty"`
	CoverageThreshold float64       `json:"coverage_threshold"`
	StartedAt         time.Time     `json:"started_at"`
}

type Observation struct {
	Source          string         `json:"source"`
	Role            string         `json:"role,omitempty"`
	MediaType       string         `json:"media_type,omitempty"`
	Integrity       string         `json:"integrity"`
	Confidentiality string         `json:"confidentiality"`
	Labels          []LabelRef     `json:"labels,omitempty"`
	Preview         string         `json:"preview,omitempty"`
	CallID          string         `json:"call_id,omitempty"`
	ToolCallID      string         `json:"tool_call_id,omitempty"`
	IsError         bool           `json:"is_error,omitempty"`
	Redactions      []RedactionHit `json:"redactions,omitempty"`
}

type RedactionHit struct {
	Label      string `json:"label"`
	Action     string `json:"action"`
	ValueID    string `json:"value_id"`
	CallID     string `json:"call_id"`
	Section    string `json:"section"`
	Ordinal    int    `json:"ordinal"`
	Path       string `json:"path"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	ShownStart int    `json:"shown_start"`
	ShownEnd   int    `json:"shown_end"`
	Secret     bool   `json:"secret,omitempty"`
}

type ContextAssemble struct {
	Kind     string     `json:"kind"`
	Segments []string   `json:"segments"`
	Labels   []LabelRef `json:"labels,omitempty"`
	Tools    []string   `json:"tools,omitempty"`
	Registry string     `json:"registry_hash,omitempty"`
	CallID   string     `json:"call_id,omitempty"`
}

type ContextCompact struct {
	Before              string   `json:"before"`
	After               string   `json:"after"`
	DroppedConstraints  []string `json:"dropped_constraints,omitempty"`
	RetainedConstraints []string `json:"retained_constraints,omitempty"`
}

type MemoryWrite struct {
	EntryID        string   `json:"entry_id"`
	SourceSegments []string `json:"source_segments"`
	Authority      string   `json:"authority"`
	Verified       bool     `json:"verified"`
	ContentHash    string   `json:"content_hash"`
}

type MemoryConsolidate struct {
	Before    []string `json:"before"`
	After     []string `json:"after"`
	Revoked   []string `json:"revoked,omitempty"`
	Authority string   `json:"authority,omitempty"`
}

type MemoryRead struct {
	QueryHash string   `json:"query_hash"`
	Rank      int      `json:"rank,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	Segments  []string `json:"segments"`
	Integrity string   `json:"integrity,omitempty"`
	EntryIDs  []string `json:"entry_ids,omitempty"`
}

type ActionType string

const (
	ActionModel           ActionType = "model"
	ActionProviderAttempt ActionType = "provider_attempt"
	ActionTool            ActionType = "tool"
)

type ActionProposed struct {
	ActionID             string                 `json:"action_id"`
	ActionType           ActionType             `json:"action_type"`
	CallID               string                 `json:"call_id,omitempty"`
	CallSeq              int64                  `json:"call_seq,omitempty"`
	ToolCallID           string                 `json:"tool_call_id,omitempty"`
	ToolName             string                 `json:"tool_name,omitempty"`
	ToolKind             string                 `json:"tool_kind,omitempty"`
	ArgumentsHash        string                 `json:"arguments_hash,omitempty"`
	Target               string                 `json:"target,omitempty"`
	Sink                 string                 `json:"sink,omitempty"`
	InputSegments        []string               `json:"input_segments,omitempty"`
	RequiredCapabilities []string               `json:"required_capabilities,omitempty"`
	InvocationHash       string                 `json:"invocation_hash,omitempty"`
	Operation            classify.Operation     `json:"operation,omitempty"`
	Purpose              classify.Purpose       `json:"purpose,omitempty"`
	PurposeSource        classify.PurposeSource `json:"purpose_source,omitempty"`
	Lane                 classify.Lane          `json:"lane,omitempty"`
	ClientFormat         string                 `json:"client_format,omitempty"`
	PublicModel          string                 `json:"public_model,omitempty"`
	Stream               bool                   `json:"stream,omitempty"`
	SessionID            string                 `json:"session_id,omitempty"`
	ParentCallID         string                 `json:"parent_call_id,omitempty"`
	RequestSegment       string                 `json:"request_segment,omitempty"`
	Attempt              int                    `json:"attempt,omitempty"`
	ChannelID            string                 `json:"channel_id,omitempty"`
	ChannelName          string                 `json:"channel_name,omitempty"`
	ModelID              string                 `json:"model_id,omitempty"`
	UpstreamModel        string                 `json:"upstream_model,omitempty"`
	Format               string                 `json:"format,omitempty"`
}

type ActionAuthorized struct {
	ActionID       string `json:"action_id"`
	Decision       string `json:"decision"`
	Capability     string `json:"capability,omitempty"`
	ApprovalID     string `json:"approval_id,omitempty"`
	InvocationHash string `json:"invocation_hash,omitempty"`
	ExecutionCount int64  `json:"execution_count,omitempty"`
	RuleID         string `json:"rule_id,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// Loss and Repair mirror a protocol conversion loss without importing the wire model.
type Loss struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Class   string `json:"class,omitempty"`
	Detail  string `json:"detail,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	ItemKey string `json:"item_key,omitempty"`
}

type Repair struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Detail  string `json:"detail,omitempty"`
	ItemKey string `json:"item_key,omitempty"`
}

type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Reasoning  int64 `json:"reasoning"`
	Known      bool  `json:"known"`
}

type ActionExecuted struct {
	ActionID         string           `json:"action_id"`
	ActionType       ActionType       `json:"action_type"`
	CallID           string           `json:"call_id,omitempty"`
	Outcome          classify.Outcome `json:"outcome,omitempty"`
	State            string           `json:"state,omitempty"`
	ErrorCode        string           `json:"error_code,omitempty"`
	DurationMS       int              `json:"duration_ms,omitempty"`
	FirstTokenMS     int              `json:"first_token_ms,omitempty"`
	Usage            Usage            `json:"usage,omitempty"`
	CostMicros       *int64           `json:"cost_micros,omitempty"`
	Currency         string           `json:"currency,omitempty"`
	ResponseID       string           `json:"response_id,omitempty"`
	ResponseStore    bool             `json:"response_store,omitempty"`
	ConversationHash string           `json:"conversation_hash,omitempty"`
	Losses           []Loss           `json:"losses,omitempty"`
	Repairs          []Repair         `json:"repairs,omitempty"`
	OutputSegments   []string         `json:"output_segments,omitempty"`
	ResponseSegment  string           `json:"response_segment,omitempty"`
	Committed        bool             `json:"committed,omitempty"`
	Partial          bool             `json:"partial,omitempty"`
	Truncated        bool             `json:"truncated,omitempty"`
	Blocked          bool             `json:"blocked,omitempty"`
	ToolCallIDs      []string         `json:"tool_call_ids,omitempty"`
	ExternalEffect   bool             `json:"external_effect,omitempty"`
}

type ExternalEffect struct {
	ActionID       string `json:"action_id"`
	Kind           string `json:"kind"`
	Target         string `json:"target"`
	InvocationHash string `json:"invocation_hash"`
}

type RollbackPayload struct {
	Snapshot Snapshot `json:"snapshot"`
	Reason   string   `json:"reason,omitempty"`
	Verified bool     `json:"verified"`
}

type DelegationSpawn struct {
	ChildTrajectory string     `json:"child_trajectory"`
	TaskHash        string     `json:"task_hash"`
	Capabilities    []string   `json:"capabilities"`
	Labels          []LabelRef `json:"labels,omitempty"`
}

type LabelRef struct {
	Segment         string `json:"segment"`
	Integrity       string `json:"integrity"`
	Confidentiality string `json:"confidentiality"`
}

type DelegationReturn struct {
	ChildTrajectory string   `json:"child_trajectory"`
	Checkpoint      string   `json:"checkpoint,omitempty"`
	ResultSegments  []string `json:"result_segments,omitempty"`
	Consumed        []string `json:"consumed_capabilities,omitempty"`
}

type ToolRegistryChange struct {
	RegistryHash string   `json:"registry_hash"`
	Tools        []string `json:"tools"`
	Changed      []string `json:"changed,omitempty"`
}

type PolicyChange struct {
	Version        int64           `json:"version"`
	Hash           string          `json:"hash"`
	RulesVersion   int64           `json:"rules_version"`
	RulesHash      string          `json:"rules_hash"`
	RedactionRules json.RawMessage `json:"redaction_rules"`
}

type ModelChange struct {
	ModelID     string `json:"model_id,omitempty"`
	Upstream    string `json:"upstream_model,omitempty"`
	ChannelID   string `json:"channel_id,omitempty"`
	RouteID     string `json:"route_id,omitempty"`
	RouteName   string `json:"route_name,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	PublicModel string `json:"public_model,omitempty"`
	Format      string `json:"format,omitempty"`
}

type ApprovalRequested struct {
	ApprovalID     string    `json:"approval_id"`
	ActionID       string    `json:"action_id"`
	InvocationHash string    `json:"invocation_hash"`
	ExpiresAt      time.Time `json:"expires_at"`
	Scope          string    `json:"scope,omitempty"`
	Reason         string    `json:"reason,omitempty"`
}

type ApprovalGranted struct {
	ApprovalID     string    `json:"approval_id"`
	Approver       string    `json:"approver"`
	Scope          string    `json:"scope,omitempty"`
	Nonce          string    `json:"nonce"`
	SingleUse      bool      `json:"single_use"`
	ExpiresAt      time.Time `json:"expires_at"`
	InvocationHash string    `json:"invocation_hash"`
}

type ApprovalDenied struct {
	ApprovalID string `json:"approval_id"`
	Reason     string `json:"reason,omitempty"`
}

type ApprovalConsumed struct {
	ApprovalID     string `json:"approval_id"`
	InvocationHash string `json:"invocation_hash"`
	ExecutionCount int64  `json:"execution_count"`
}

type CaptureHeartbeat struct {
	Recorder  string     `json:"recorder"`
	Placement string     `json:"placement"`
	Attested  bool       `json:"attested"`
	Coverage  []Boundary `json:"coverage"`
	Sequence  int64      `json:"sequence"`
	Gap       int64      `json:"gap,omitempty"`
}

type CheckpointPayload struct {
	CheckpointID string `json:"checkpoint_id"`
	FromSeq      int64  `json:"from_seq"`
	ToSeq        int64  `json:"to_seq"`
	MerkleRoot   string `json:"merkle_root"`
	Signature    string `json:"signature"`
	PrevHash     string `json:"prev_hash"`
	WitnessRef   string `json:"witness_ref,omitempty"`
}

type TrajectoryEnd struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

type GateDecision struct {
	DecisionID string  `json:"decision_id"`
	Gate       string  `json:"gate"`
	Action     string  `json:"action"`
	Blocked    bool    `json:"blocked"`
	Terminal   bool    `json:"terminal"`
	RuleID     string  `json:"rule_id,omitempty"`
	Code       string  `json:"code,omitempty"`
	Severity   string  `json:"severity,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	CallID     string  `json:"call_id,omitempty"`
	ActionID   string  `json:"action_id,omitempty"`
	CallState  string  `json:"call_state,omitempty"`
	Outcome    string  `json:"outcome,omitempty"`
	ErrorCode  string  `json:"error_code,omitempty"`
	Checks     []Check `json:"checks,omitempty"`
}

func DecodePayload[T any](event *Event) (T, error) {
	var value T
	err := json.Unmarshal(event.Payload, &value)
	return value, err
}
