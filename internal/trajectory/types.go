// Package trajectory defines the append-only event log that is the source of truth for every
// security decision. A trajectory is one delegated task; calls are intervals inside it.
package trajectory

import (
	"encoding/json"
	"time"
)

type ID = string

type Kind string

const (
	KindTrajectoryStart    Kind = "trajectory.start"
	KindObservation        Kind = "observation"
	KindContextAssemble    Kind = "context.assemble"
	KindContextCompact     Kind = "context.compact"
	KindMemoryWrite        Kind = "memory.write"
	KindMemoryConsolidate  Kind = "memory.consolidate"
	KindMemoryRead         Kind = "memory.read"
	KindActionProposed     Kind = "action.proposed"
	KindActionAuthorized   Kind = "action.authorized"
	KindActionExecuted     Kind = "action.executed"
	KindExternalEffect     Kind = "external.effect"
	KindRollback           Kind = "rollback"
	KindDelegationSpawn    Kind = "delegation.spawn"
	KindDelegationReturn   Kind = "delegation.return"
	KindToolRegistryChange Kind = "tool.registry.change"
	KindPolicyChange       Kind = "policy.change"
	KindModelChange        Kind = "model.change"
	KindApprovalRequested  Kind = "approval.requested"
	KindApprovalGranted    Kind = "approval.granted"
	KindApprovalDenied     Kind = "approval.denied"
	KindApprovalConsumed   Kind = "approval.consumed"
	KindCaptureHeartbeat   Kind = "capture.heartbeat"
	KindCheckpoint         Kind = "checkpoint"
	KindTrajectoryEnd      Kind = "trajectory.end"
	KindGateDecision       Kind = "gate.decision"
)

type Boundary string

const (
	BoundaryRoot     Boundary = "root"
	BoundaryIngress  Boundary = "ingress"
	BoundaryContext  Boundary = "context"
	BoundaryMemory   Boundary = "memory"
	BoundaryAction   Boundary = "action"
	BoundaryRecovery Boundary = "recovery"
	BoundaryControl  Boundary = "control"
)

type Actor string

const (
	ActorHarness  Actor = "harness"
	ActorModel    Actor = "model"
	ActorTool     Actor = "tool"
	ActorSubagent Actor = "subagent"
	ActorHuman    Actor = "human"
	ActorGateway  Actor = "gateway"
)

type Ref struct {
	Segment string `json:"segment"`
	Role    string `json:"role"`
}

type Auth struct {
	Capability     string `json:"capability,omitempty"`
	Approval       string `json:"approval,omitempty"`
	InvocationHash string `json:"invocation_hash,omitempty"`
	ExecutionCount int64  `json:"execution_count,omitempty"`
	Lease          string `json:"lease,omitempty"`
	Scope          string `json:"scope,omitempty"`
}

type Check struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Status     string   `json:"status"`
	Code       string   `json:"code,omitempty"`
	Severity   string   `json:"severity,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Detail     string   `json:"detail,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`
}

type Obligation struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	Requires string `json:"requires"`
	State    string `json:"state"`
	Deadline string `json:"deadline,omitempty"`
}

type Snapshot struct {
	Environment string `json:"environment,omitempty"`
	Transcript  string `json:"transcript,omitempty"`
	Memory      string `json:"memory,omitempty"`
	Session     string `json:"session,omitempty"`
	Cache       string `json:"cache,omitempty"`
	Opaque      bool   `json:"opaque,omitempty"`
}

type Capture struct {
	Recorder  string     `json:"recorder,omitempty"`
	Placement string     `json:"placement,omitempty"`
	Attested  bool       `json:"attested,omitempty"`
	Coverage  []Boundary `json:"coverage,omitempty"`
	Sequence  int64      `json:"sequence,omitempty"`
}

// Event is one immutable entry in a trajectory chain. The fields that gates and detectors read are
// typed; Payload carries event-kind data that is not part of the security state machine.
type Event struct {
	ID           string          `json:"id"`
	TrajectoryID string          `json:"trajectory_id"`
	ProjectID    string          `json:"project_id"`
	Seq          int64           `json:"seq"`
	Kind         Kind            `json:"kind"`
	Boundary     Boundary        `json:"boundary"`
	Actor        Actor           `json:"actor"`
	Principal    string          `json:"principal"`
	CallID       string          `json:"call_id,omitempty"`
	ActionID     string          `json:"action_id,omitempty"`
	At           time.Time       `json:"occurred_at"`
	Inputs       []Ref           `json:"inputs"`
	Outputs      []Ref           `json:"outputs"`
	Auth         *Auth           `json:"auth,omitempty"`
	Checks       []Check         `json:"checks"`
	Obligations  []Obligation    `json:"obligations"`
	Snapshot     *Snapshot       `json:"snapshot,omitempty"`
	Capture      Capture         `json:"capture"`
	Payload      json.RawMessage `json:"payload"`
	PrevHash     string          `json:"prev_hash"`
	Hash         string          `json:"hash"`
}

type Draft struct {
	ID          string
	Kind        Kind
	Boundary    Boundary
	Actor       Actor
	Principal   string
	CallID      string
	ActionID    string
	At          time.Time
	Inputs      []Ref
	Outputs     []Ref
	Auth        *Auth
	Checks      []Check
	Obligations []Obligation
	Snapshot    *Snapshot
	Capture     Capture
	Payload     any
}

type Segment struct {
	ID              string    `json:"id"`
	TrajectoryID    string    `json:"trajectory_id"`
	MediaType       string    `json:"media_type"`
	BytesRef        string    `json:"bytes_ref"`
	Size            int64     `json:"size"`
	Integrity       string    `json:"integrity"`
	Confidentiality string    `json:"confidentiality"`
	SourceEvent     string    `json:"source_event"`
	CreatedAt       time.Time `json:"created_at"`
}

const (
	IntegrityTrusted   = "trusted"
	IntegrityUntrusted = "untrusted"
	IntegrityUnknown   = "unknown"

	ConfidentialityPublic       = "public"
	ConfidentialityPrivate      = "private"
	ConfidentialityUserIdentity = "user_identity"
)

type Blob struct {
	Hash      string    `json:"hash"`
	Bytes     []byte    `json:"-"`
	MediaType string    `json:"media_type"`
	Encrypted bool      `json:"encrypted"`
	CreatedAt time.Time `json:"created_at"`
}

type Edge struct {
	TrajectoryID string  `json:"trajectory_id"`
	From         string  `json:"src"`
	To           string  `json:"dst"`
	Relation     string  `json:"relation"`
	EventID      string  `json:"event_id"`
	Confidence   float64 `json:"confidence"`
}

const (
	RelationDerivedFrom  = "derived_from"
	RelationUsedData     = "used_data"
	RelationAuthorizedBy = "authorized_by"
	RelationExecutedBy   = "executed_by"
	RelationDelegatedTo  = "delegated_to"
	RelationSupersedes   = "supersedes"
	RelationRevokedBy    = "revoked_by"
	RelationCheckpointOf = "checkpoint_of"
)

// Batch is what the request path submits. The repository assigns sequence numbers and hashes.
type Batch struct {
	TrajectoryID string
	ProjectID    string
	Events       []Draft
	Segments     []Segment
	Blobs        []Blob
}

type ChainReport struct {
	Status   string `json:"status"`
	Events   int64  `json:"events"`
	Head     string `json:"head"`
	BrokenAt int64  `json:"broken_at,omitempty"`
	Error    string `json:"error,omitempty"`
}

const (
	StatusVerified    = "verified"
	StatusInProgress  = "in_progress"
	StatusIncomplete  = "incomplete"
	StatusBroken      = "broken"
	StatusUnavailable = "unavailable"
)
