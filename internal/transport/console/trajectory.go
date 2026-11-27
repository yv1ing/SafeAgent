package console

import (
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/evidence"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

type TrajectorySummaryView struct {
	ID           string  `json:"id" required:"true"`
	ProjectID    string  `json:"project_id" required:"true"`
	AgentKeyID   string  `json:"agent_key_id" required:"true"`
	SessionID    string  `json:"session_id" required:"true"`
	Goal         string  `json:"goal" required:"true"`
	State        string  `json:"state" required:"true" enum:"open,ended,interrupted"`
	Coverage     float64 `json:"coverage" required:"true"`
	Risk         float64 `json:"risk" required:"true"`
	MaxSeverity  string  `json:"max_severity" required:"true"`
	EventCount   int64   `json:"event_count" required:"true"`
	CallCount    int64   `json:"call_count" required:"true"`
	FindingCount int64   `json:"finding_count" required:"true"`
	FirstEventAt string  `json:"first_event_at" required:"true"`
	LastEventAt  string  `json:"last_event_at" required:"true"`
	EndedAt      string  `json:"ended_at" required:"true"`
}

type TrajectoryPage struct {
	Items []TrajectorySummaryView `json:"items" required:"true"`
}

type TrajectoryStateView struct {
	TrajectoryID      string             `json:"trajectory_id" required:"true"`
	Version           int64              `json:"version" required:"true"`
	HeadEvent         string             `json:"head_event" required:"true"`
	Goal              string             `json:"goal" required:"true"`
	Open              bool               `json:"open" required:"true"`
	Coverage          float64            `json:"coverage" required:"true"`
	CoverageThreshold float64            `json:"coverage_threshold" required:"true"`
	Expected          int64              `json:"expected_boundaries" required:"true"`
	Observed          int64              `json:"observed_boundaries" required:"true"`
	Gaps              int64              `json:"capture_gaps" required:"true"`
	Risk              state.Risk         `json:"risk" required:"true"`
	Constraints       []state.Constraint `json:"constraints" required:"true"`
	Capabilities      []state.Capability `json:"capabilities" required:"true"`
	Obligations       []state.Obligation `json:"obligations" required:"true"`
	Approvals         []state.Approval   `json:"approvals" required:"true"`
	PolicyVersion     int64              `json:"policy_version" required:"true"`
	Model             string             `json:"model" required:"true"`
}

type TrajectoryEventView struct {
	ID         string             `json:"id" required:"true"`
	Seq        int64              `json:"seq" required:"true"`
	Kind       string             `json:"kind" required:"true"`
	Boundary   string             `json:"boundary" required:"true"`
	Actor      string             `json:"actor" required:"true"`
	Principal  string             `json:"principal" required:"true"`
	CallID     string             `json:"call_id" required:"true"`
	ActionID   string             `json:"action_id" required:"true"`
	OccurredAt string             `json:"occurred_at" required:"true"`
	Inputs     []string           `json:"inputs" required:"true"`
	Outputs    []string           `json:"outputs" required:"true"`
	Checks     []trajectory.Check `json:"checks" required:"true"`
	Payload    json.RawMessage    `json:"payload,omitempty"`
	PrevHash   string             `json:"prev_hash" required:"true"`
	Hash       string             `json:"hash" required:"true"`
}

type TrajectoryFindingView struct {
	FindingView
}

type TrajectorySegmentView struct {
	ID              string `json:"id" required:"true"`
	MediaType       string `json:"media_type" required:"true"`
	Size            int64  `json:"size" required:"true"`
	Integrity       string `json:"integrity" required:"true"`
	Confidentiality string `json:"confidentiality" required:"true"`
	SourceEvent     string `json:"source_event" required:"true"`
}

type TrajectorySegmentContentView struct {
	ID              string `json:"id" required:"true"`
	MediaType       string `json:"media_type" required:"true"`
	Integrity       string `json:"integrity" required:"true"`
	Confidentiality string `json:"confidentiality" required:"true"`
	Content         string `json:"content" required:"true"`
}

type TrajectoryEdgeView struct {
	From       string  `json:"src" required:"true"`
	To         string  `json:"dst" required:"true"`
	Relation   string  `json:"relation" required:"true"`
	EventID    string  `json:"event_id" required:"true"`
	Confidence float64 `json:"confidence" required:"true"`
}

type TrajectoryVerdictView struct {
	Value      string  `json:"value" required:"true" enum:"safe,unsafe,unknown"`
	Confidence float64 `json:"confidence" required:"true"`
	Reason     string  `json:"reason" required:"true"`
}

type TrajectoryDetailView struct {
	Summary   TrajectorySummaryView   `json:"summary" required:"true"`
	Verdict   TrajectoryVerdictView   `json:"verdict" required:"true"`
	State     TrajectoryStateView     `json:"state" required:"true"`
	Calls     []CallView              `json:"calls" required:"true"`
	Events    []TrajectoryEventView   `json:"events" required:"true"`
	Findings  []FindingView           `json:"findings" required:"true"`
	Segments  []TrajectorySegmentView `json:"segments" required:"true"`
	Edges     []TrajectoryEdgeView    `json:"edges" required:"true"`
	Integrity IntegrityView           `json:"integrity" required:"true"`
}

type trajectoryListInput struct {
	projectPath
	State       string `query:"state" json:"-"`
	AgentKeyID  string `query:"agent_key_id" json:"-"`
	SessionID   string `query:"session_id" json:"-"`
	MinSeverity string `query:"min_severity" json:"-"`
	Before      string `query:"before" json:"-"`
	BeforeID    string `query:"before_id" json:"-"`
	Limit       int    `query:"limit" json:"-"`
}

type trajectoryPath struct {
	ProjectID    string `path:"project_id" json:"-" required:"true"`
	TrajectoryID string `path:"trajectory_id" json:"-" required:"true"`
}

type trajectoryEventPath struct {
	trajectoryPath
	EventID string `path:"event_id" json:"-" required:"true"`
}

type trajectorySegmentPath struct {
	trajectoryPath
	SegmentID string `path:"segment_id" json:"-" required:"true"`
}

type approvalPath struct {
	trajectoryPath
	ApprovalID string `path:"approval_id" json:"-" required:"true"`
}

type denyApprovalInput struct {
	Reason string `json:"reason"`
}

type edgePage struct {
	Items []TrajectoryEdgeView `json:"items" required:"true"`
}

func trajectorySummaryView(value entity.TrajectorySummary) TrajectorySummaryView {
	return TrajectorySummaryView{
		ID: value.ID, ProjectID: value.ProjectID, AgentKeyID: value.AgentKeyID, SessionID: value.SessionID,
		Goal: value.Goal, State: value.State, Coverage: value.Coverage, Risk: value.Risk,
		MaxSeverity: value.MaxSeverity, EventCount: value.EventCount, CallCount: value.CallCount,
		FindingCount: value.FindingCount, FirstEventAt: stamp(value.FirstEventAt),
		LastEventAt: stamp(value.LastEventAt), EndedAt: stampOptional(value.EndedAt),
	}
}

func trajectoryStateView(value *state.State) TrajectoryStateView {
	return TrajectoryStateView{
		TrajectoryID: value.TrajectoryID, Version: value.Version, HeadEvent: value.HeadEvent,
		Goal: value.Goal, Open: value.Open, Coverage: value.Coverage,
		CoverageThreshold: value.CoverageThreshold, Expected: value.Expected, Observed: value.Observed,
		Gaps: value.Gaps, Risk: value.Risk, Constraints: nonNil(value.Constraints),
		Capabilities: nonNil(value.Capabilities), Obligations: nonNil(value.Obligations), Approvals: approvalValues(value.Approvals),
		PolicyVersion: value.PolicyVersion, Model: value.Model,
	}
}

func trajectoryEventView(event trajectory.Event, administrator bool) TrajectoryEventView {
	view := TrajectoryEventView{
		ID: event.ID, Seq: event.Seq, Kind: string(event.Kind), Boundary: string(event.Boundary),
		Actor: string(event.Actor), Principal: event.Principal, CallID: event.CallID, ActionID: event.ActionID,
		OccurredAt: stamp(event.At), Inputs: refIDs(event.Inputs), Outputs: refIDs(event.Outputs),
		Checks: nonNil(event.Checks), PrevHash: event.PrevHash, Hash: event.Hash,
	}
	if administrator {
		view.Payload = event.Payload
	}
	return view
}

func (console *Console) registerTrajectories() {
	console.add("GET", "/projects/{project_id}/trajectories", "listTrajectories", trajectoryListInput{}, TrajectoryPage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		limit := 50
		if raw := ctx.Query("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				return nil, errInvalidRequest
			}
			limit = parsed
		}
		filter := service.TrajectoryFilter{
			State: ctx.Query("state"), AgentKeyID: ctx.Query("agent_key_id"), SessionID: ctx.Query("session_id"),
			MinSeverity: ctx.Query("min_severity"), BeforeID: ctx.Query("before_id"), Limit: limit,
		}
		if raw := ctx.Query("before"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return nil, errInvalidRequest
			}
			filter.Before = parsed
		}
		items, err := console.Trajectory.List(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), filter)
		if err != nil {
			return nil, err
		}
		return TrajectoryPage{Items: viewAll(items, func(item *entity.TrajectorySummary) TrajectorySummaryView { return trajectorySummaryView(*item) })}, nil
	})
	console.add("GET", "/projects/{project_id}/trajectories/{trajectory_id}", "getTrajectory", trajectoryPath{}, TrajectoryDetailView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		detail, err := console.Trajectory.Get(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id"))
		if err != nil {
			return nil, err
		}
		return trajectoryDetailView(detail, identity.User.Administrator), nil
	})
	console.add("GET", "/projects/{project_id}/trajectories/{trajectory_id}/timeline", "getTrajectoryTimeline", trajectoryPath{}, []TrajectoryEventView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		detail, err := console.Trajectory.Get(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id"))
		if err != nil {
			return nil, err
		}
		return viewAll(detail.Events, func(event *trajectory.Event) TrajectoryEventView {
			return trajectoryEventView(*event, identity.User.Administrator)
		}), nil
	})
	console.add("GET", "/projects/{project_id}/trajectories/{trajectory_id}/coverage", "getTrajectoryCoverage", trajectoryPath{}, TrajectoryStateView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		current, err := console.Trajectory.Coverage(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id"))
		if err != nil {
			return nil, err
		}
		return trajectoryStateView(current), nil
	})
	console.add("GET", "/projects/{project_id}/trajectories/{trajectory_id}/why/{event_id}", "getTrajectoryWhy", trajectoryEventPath{}, edgePage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		edges, err := console.Trajectory.Why(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id"), ctx.Param("event_id"))
		if err != nil {
			return nil, err
		}
		return edgePage{Items: viewAll(edges, func(edge *trajectory.Edge) TrajectoryEdgeView { return trajectoryEdgeView(*edge) })}, nil
	})
	console.add("GET", "/projects/{project_id}/trajectories/{trajectory_id}/blast/{segment_id}", "getTrajectoryBlast", trajectorySegmentPath{}, edgePage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		edges, err := console.Trajectory.Blast(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id"), ctx.Param("segment_id"))
		if err != nil {
			return nil, err
		}
		return edgePage{Items: viewAll(edges, func(edge *trajectory.Edge) TrajectoryEdgeView { return trajectoryEdgeView(*edge) })}, nil
	})
	console.add("GET", "/projects/{project_id}/trajectories/{trajectory_id}/segments/{segment_id}", "getTrajectorySegment", trajectorySegmentPath{}, TrajectorySegmentContentView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		segment, content, err := console.Trajectory.Segment(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id"), ctx.Param("segment_id"))
		if err != nil {
			return nil, err
		}
		console.recordOperation(ctx, "readTrajectorySegment", identity)
		return TrajectorySegmentContentView{
			ID: segment.ID, MediaType: segment.MediaType, Integrity: segment.Integrity,
			Confidentiality: segment.Confidentiality, Content: string(content),
		}, nil
	})
	console.add("GET", "/projects/{project_id}/trajectories/{trajectory_id}/bundle", "getTrajectoryBundle", trajectoryPath{}, evidence.Bundle{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		return console.Trajectory.Bundle(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id"))
	})
	console.add("POST", "/projects/{project_id}/trajectories/{trajectory_id}/approvals/{approval_id}/grant", "grantApproval", approvalPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		if err := console.Trajectory.Approve(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id"), ctx.Param("approval_id")); err != nil {
			return nil, err
		}
		return nil, nil
	})
	console.add("POST", "/projects/{project_id}/trajectories/{trajectory_id}/approvals/{approval_id}/deny", "denyApproval", approvalPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		reason := ctx.Query("reason")
		if ctx.Request.ContentLength > 0 {
			var input denyApprovalInput
			if err := decode(ctx, &input); err != nil {
				return nil, err
			}
			reason = input.Reason
		}
		if err := console.Trajectory.Deny(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id"), ctx.Param("approval_id"), reason); err != nil {
			return nil, err
		}
		return nil, nil
	})
	console.add("POST", "/projects/{project_id}/trajectories/{trajectory_id}/rebuild", "rebuildTrajectory", trajectoryPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		if err := console.Trajectory.Rebuild(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("trajectory_id")); err != nil {
			return nil, err
		}
		return nil, nil
	})
}

func trajectoryDetailView(detail *service.TrajectoryDetail, administrator bool) TrajectoryDetailView {
	return TrajectoryDetailView{
		Summary: trajectorySummaryView(*detail.Summary), State: trajectoryStateView(detail.State),
		Verdict: TrajectoryVerdictView{Value: detail.Verdict.Value, Confidence: detail.Verdict.Confidence, Reason: detail.Verdict.Reason},
		Calls:   viewAll(detail.Calls, func(call *entity.Call) CallView { return callView(*call) }), Findings: viewAll(detail.Findings, func(finding *entity.Finding) FindingView { return storedFindingView(*finding) }),
		Events: viewAll(detail.Events, func(event *trajectory.Event) TrajectoryEventView { return trajectoryEventView(*event, administrator) }),
		Segments: viewAll(detail.Segments, func(segment *entity.Segment) TrajectorySegmentView {
			return TrajectorySegmentView{ID: segment.ID, MediaType: segment.MediaType, Size: segment.Size, Integrity: segment.Integrity, Confidentiality: segment.Confidentiality, SourceEvent: segment.SourceEvent}
		}),
		Edges:     viewAll(detail.Edges, func(edge *trajectory.Edge) TrajectoryEdgeView { return trajectoryEdgeView(*edge) }),
		Integrity: IntegrityView{Status: detail.Integrity.Status, Events: detail.Integrity.Events, Head: detail.Integrity.Head, BrokenAt: detail.Integrity.BrokenAt, Error: detail.Integrity.Error},
	}
}

func trajectoryEdgeView(edge trajectory.Edge) TrajectoryEdgeView {
	return TrajectoryEdgeView{From: edge.From, To: edge.To, Relation: edge.Relation, EventID: edge.EventID, Confidence: edge.Confidence}
}

func approvalValues(values map[string]state.Approval) []state.Approval {
	result := make([]state.Approval, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ID < result[right].ID })
	return result
}
