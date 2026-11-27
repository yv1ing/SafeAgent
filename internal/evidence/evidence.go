// Package evidence builds and verifies offline evidence bundles. It does not read the database.
package evidence

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

const (
	checkpointKeyLabel = "SafeAgent/checkpoint/key/v1"
	BundleVersion      = 1
)

type Checkpoint struct {
	ID           string    `json:"id"`
	TrajectoryID string    `json:"trajectory_id"`
	FromSeq      int64     `json:"from_seq"`
	ToSeq        int64     `json:"to_seq"`
	MerkleRoot   string    `json:"merkle_root"`
	Signature    string    `json:"signature"`
	PrevHash     string    `json:"prev_hash"`
	WitnessRef   string    `json:"witness_ref,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func MerkleRoot(hashes []string) string {
	if len(hashes) == 0 {
		return trajectory.ContentHash([]byte("empty"))
	}
	level := make([]string, len(hashes))
	copy(level, hashes)
	for len(level) > 1 {
		next := make([]string, 0, (len(level)+1)/2)
		for index := 0; index < len(level); index += 2 {
			right := level[index]
			if index+1 < len(level) {
				right = level[index+1]
			}
			next = append(next, leafHash(level[index]+right))
		}
		level = next
	}
	return level[0]
}

func leafHash(value string) string {
	payload, _ := json.Marshal(struct {
		Domain string `json:"domain"`
		Value  string `json:"value"`
	}{"SafeAgent/Merkle/v1", value})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func NewCheckpoint(trajectoryID string, events []trajectory.Event, secret []byte, previous *Checkpoint) Checkpoint {
	hashes := make([]string, 0, len(events))
	for _, event := range events {
		hashes = append(hashes, event.Hash)
	}
	checkpoint := Checkpoint{
		ID: trajectory.NewID("ckp"), TrajectoryID: trajectoryID,
		MerkleRoot: MerkleRoot(hashes), CreatedAt: time.Now().UTC(),
	}
	if len(events) > 0 {
		checkpoint.FromSeq, checkpoint.ToSeq = events[0].Seq, events[len(events)-1].Seq
		checkpoint.PrevHash = events[len(events)-1].Hash
	}
	if previous != nil {
		checkpoint.PrevHash = previous.Signature
	}
	checkpoint.Signature = signCheckpoint(checkpoint, secret)
	return checkpoint
}

func signCheckpoint(checkpoint Checkpoint, secret []byte) string {
	payload, _ := json.Marshal(struct {
		ID           string `json:"id"`
		TrajectoryID string `json:"trajectory_id"`
		FromSeq      int64  `json:"from_seq"`
		ToSeq        int64  `json:"to_seq"`
		MerkleRoot   string `json:"merkle_root"`
		PrevHash     string `json:"prev_hash"`
		WitnessRef   string `json:"witness_ref,omitempty"`
	}{checkpoint.ID, checkpoint.TrajectoryID, checkpoint.FromSeq, checkpoint.ToSeq, checkpoint.MerkleRoot, checkpoint.PrevHash, checkpoint.WitnessRef})
	key := hmac.New(sha256.New, secret)
	key.Write([]byte(checkpointKeyLabel))
	mac := hmac.New(sha256.New, key.Sum(nil))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

type Segment struct {
	ID              string `json:"id"`
	MediaType       string `json:"media_type"`
	Content         []byte `json:"content,omitempty"`
	Integrity       string `json:"integrity"`
	Confidentiality string `json:"confidentiality"`
}

type Approval struct {
	ID             string    `json:"id"`
	InvocationHash string    `json:"invocation_hash"`
	Approver       string    `json:"approver,omitempty"`
	State          string    `json:"state"`
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
	ExecutionCount int64     `json:"execution_count"`
}

type Policy struct {
	Version int64           `json:"version"`
	Hash    string          `json:"hash"`
	Rules   json.RawMessage `json:"rules,omitempty"`
}

type Manifest struct {
	Version     int       `json:"version"`
	BundleHash  string    `json:"bundle_hash"`
	Verifier    string    `json:"verifier"`
	GeneratedAt time.Time `json:"generated_at"`
}

type Bundle struct {
	TrajectoryID string             `json:"trajectory_id"`
	FromSeq      int64              `json:"from_seq"`
	ToSeq        int64              `json:"to_seq"`
	Events       []trajectory.Event `json:"events"`
	Segments     []Segment          `json:"segments"`
	Edges        []trajectory.Edge  `json:"edges"`
	State        *state.State       `json:"state,omitempty"`
	Checkpoint   Checkpoint         `json:"checkpoint"`
	Approvals    []Approval         `json:"approvals"`
	Policy       Policy             `json:"policy"`
	Manifest     Manifest           `json:"manifest"`
}

type Report struct {
	Valid     bool      `json:"valid"`
	Status    string    `json:"status"`
	Events    int       `json:"events"`
	Segments  int       `json:"segments"`
	Failures  []string  `json:"failures,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

func Build(trajectoryID string, events []trajectory.Event, segments []Segment, edges []trajectory.Edge, current *state.State, checkpoint Checkpoint, approvals []Approval, policy Policy) Bundle {
	bundle := Bundle{
		TrajectoryID: trajectoryID, Events: events, Segments: segments, Edges: edges, State: current,
		Checkpoint: checkpoint, Approvals: approvals, Policy: policy,
		Manifest: Manifest{Version: BundleVersion, Verifier: "safeagent/evidence/v1", GeneratedAt: time.Now().UTC()},
	}
	if len(events) > 0 {
		bundle.FromSeq, bundle.ToSeq = events[0].Seq, events[len(events)-1].Seq
	}
	payload, _ := json.Marshal(struct {
		TrajectoryID string
		FromSeq      int64
		ToSeq        int64
		Events       []trajectory.Event
		Segments     []Segment
		Edges        []trajectory.Edge
		Checkpoint   Checkpoint
		Approvals    []Approval
		Policy       Policy
		State        *state.State
	}{trajectoryID, bundle.FromSeq, bundle.ToSeq, events, segments, edges, checkpoint, approvals, policy, current})
	bundle.Manifest.BundleHash = trajectory.ContentHash(payload)
	return bundle
}

// VerifyRange checks the event chain, the Merkle root, the checkpoint signature and the range. It
// does not read segments or the manifest, so a store can verify a trajectory without building a
// bundle.
func VerifyRange(events []trajectory.Event, checkpoint Checkpoint, secret []byte) Report {
	report := Report{CheckedAt: time.Now().UTC(), Status: trajectory.StatusVerified, Valid: true}
	report.Events = len(events)
	chain := trajectory.VerifyChain(events, secret)
	if chain.Status != trajectory.StatusVerified {
		return fail(report, "chain:"+chain.Error)
	}
	hashes := make([]string, 0, len(events))
	for _, event := range events {
		hashes = append(hashes, event.Hash)
	}
	if root := MerkleRoot(hashes); root != checkpoint.MerkleRoot {
		return fail(report, "merkle_root")
	}
	if expected := signCheckpoint(checkpoint, secret); !hmac.Equal([]byte(expected), []byte(checkpoint.Signature)) {
		return fail(report, "checkpoint_signature")
	}
	if len(events) > 0 {
		for _, event := range events {
			if event.TrajectoryID != events[0].TrajectoryID {
				return fail(report, "trajectory_mismatch")
			}
		}
		if checkpoint.TrajectoryID != "" && checkpoint.TrajectoryID != events[0].TrajectoryID {
			return fail(report, "trajectory_mismatch")
		}
		if checkpoint.ToSeq != events[len(events)-1].Seq {
			return fail(report, "checkpoint_range")
		}
		if checkpoint.FromSeq != events[0].Seq {
			return fail(report, "checkpoint_range")
		}
	}
	return report
}

// Verify checks a complete bundle: the chain and checkpoint, every referenced segment, and the
// manifest. A bundle that omits a referenced segment fails instead of silently verifying.
func Verify(bundle Bundle, secret []byte) Report {
	report := Report{CheckedAt: time.Now().UTC(), Status: trajectory.StatusVerified, Valid: true}
	report.Events, report.Segments = len(bundle.Events), len(bundle.Segments)
	if bundle.Manifest.Version != BundleVersion {
		return fail(report, "manifest_version")
	}
	report = VerifyRange(bundle.Events, bundle.Checkpoint, secret)
	report.Segments = len(bundle.Segments)
	if !report.Valid {
		return report
	}
	if len(bundle.Events) > 0 && bundle.TrajectoryID != bundle.Events[0].TrajectoryID {
		return fail(report, "trajectory_mismatch")
	}
	segments := make(map[string]Segment, len(bundle.Segments))
	for _, segment := range bundle.Segments {
		segments[segment.ID] = segment
	}
	referenced := map[string]bool{}
	for _, event := range bundle.Events {
		for _, ref := range append(append([]trajectory.Ref{}, event.Inputs...), event.Outputs...) {
			referenced[ref.Segment] = true
		}
	}
	for id := range referenced {
		segment, found := segments[id]
		if !found {
			return fail(report, "segment_missing:"+id)
		}
		if segment.Content == nil {
			return fail(report, "segment_content_missing:"+id)
		}
	}
	for _, segment := range bundle.Segments {
		if segment.Content == nil {
			continue
		}
		if trajectory.ContentHash(segment.Content) != segment.ID {
			return fail(report, "segment_hash:"+segment.ID)
		}
	}
	manifest, _ := json.Marshal(struct {
		TrajectoryID string
		FromSeq      int64
		ToSeq        int64
		Events       []trajectory.Event
		Segments     []Segment
		Edges        []trajectory.Edge
		Checkpoint   Checkpoint
		Approvals    []Approval
		Policy       Policy
		State        *state.State
	}{bundle.TrajectoryID, bundle.FromSeq, bundle.ToSeq, bundle.Events, bundle.Segments, bundle.Edges, bundle.Checkpoint, bundle.Approvals, bundle.Policy, bundle.State})
	if trajectory.ContentHash(manifest) != bundle.Manifest.BundleHash {
		return fail(report, "manifest_hash")
	}
	return report
}

func fail(report Report, reason string) Report {
	report.Valid = false
	report.Status = trajectory.StatusBroken
	report.Failures = append(report.Failures, reason)
	return report
}

func Marshal(bundle Bundle) ([]byte, error) {
	return json.MarshalIndent(bundle, "", "  ")
}

func Unmarshal(payload []byte) (Bundle, error) {
	var bundle Bundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return Bundle{}, err
	}
	if bundle.TrajectoryID == "" {
		return Bundle{}, errors.New("the bundle has no trajectory id")
	}
	return bundle, nil
}

func Describe(report Report) string {
	if report.Valid {
		return fmt.Sprintf("verified %d events and %d segments", report.Events, report.Segments)
	}
	return "verification failed: " + strings.Join(report.Failures, ", ")
}
