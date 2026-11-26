package console

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/observation"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
)

// TrajectoryView is the session graph, flat list and replay in one payload. All three views read the
// same nodes and edges, so switching views never changes identities.
type TrajectoryView struct {
	SessionID string               `json:"session_id" required:"true"`
	ProjectID string               `json:"project_id" required:"true"`
	Nodes     []TrajectoryNodeView `json:"nodes" required:"true"`
	Edges     []TrajectoryEdgeView `json:"edges" required:"true"`
	Steps     []TrajectoryStepView `json:"steps" required:"true"`
	Stats     TrajectoryStatsView  `json:"stats" required:"true"`
	Truncated bool                 `json:"truncated" required:"true"`
	Warnings  []string             `json:"warnings" required:"true"`
}

type TrajectoryStatsView struct {
	Calls                int    `json:"calls" required:"true"`
	Turns                int    `json:"turns" required:"true"`
	Items                int    `json:"items" required:"true"`
	ToolCalls            int    `json:"tool_calls" required:"true"`
	Attempts             int    `json:"attempts" required:"true"`
	Findings             int    `json:"findings" required:"true"`
	Redactions           int    `json:"redactions" required:"true"`
	InputTokens          int64  `json:"input_tokens" required:"true"`
	OutputTokens         int64  `json:"output_tokens" required:"true"`
	CostMicros           *int64 `json:"cost_micros,omitempty"`
	Currency             string `json:"currency" required:"true"`
	DurationMilliseconds int64  `json:"duration_ms" required:"true"`
}

type TrajectoryNodeView struct {
	ID         string                    `json:"id" required:"true"`
	Kind       string                    `json:"kind" required:"true" enum:"session,call,turn,request,response,tool_call,tool_result,attempt,decision,finding,redaction"`
	CallID     string                    `json:"call_id,omitempty"`
	Session    *TrajectorySessionView    `json:"session,omitempty"`
	Call       *TrajectoryCallView       `json:"call,omitempty"`
	Turn       *TrajectoryTurnView       `json:"turn,omitempty"`
	Item       *TrajectoryItemView       `json:"item,omitempty"`
	ToolCall   *TrajectoryToolCallView   `json:"tool_call,omitempty"`
	ToolResult *TrajectoryToolResultView `json:"tool_result,omitempty"`
	Attempt    *TrajectoryAttemptView    `json:"attempt,omitempty"`
	Decision   *TrajectoryDecisionView   `json:"decision,omitempty"`
	Finding    *TrajectoryFindingView    `json:"finding,omitempty"`
	Redaction  *TrajectoryRedactionView  `json:"redaction,omitempty"`
}

type TrajectoryEdgeView struct {
	ID       string `json:"id" required:"true"`
	Kind     string `json:"kind" required:"true" enum:"contains,continues,produces,consumes,blocks,flows,retried"`
	From     string `json:"from" required:"true"`
	To       string `json:"to" required:"true"`
	Inferred bool   `json:"inferred" required:"true" description:"The reader had to infer this relation"`
}

type TrajectoryStepView struct {
	Sequence int64     `json:"sequence" required:"true"`
	CallID   string    `json:"call_id" required:"true"`
	Kind     string    `json:"kind" required:"true"`
	Category string    `json:"category" required:"true"`
	Label    string    `json:"label" required:"true"`
	Status   string    `json:"status" required:"true"`
	Code     string    `json:"code" required:"true"`
	NodeIDs  []string  `json:"node_ids" required:"true"`
	At       time.Time `json:"at" required:"true"`
}

type TrajectorySessionView struct {
	SessionID string `json:"session_id" required:"true"`
	ProjectID string `json:"project_id" required:"true"`
}

type TrajectoryCallView struct {
	CallID               string `json:"call_id" required:"true"`
	Model                string `json:"model" required:"true"`
	UpstreamModel        string `json:"upstream_model" required:"true"`
	ChannelID            string `json:"channel_id" required:"true"`
	Lane                 string `json:"lane" required:"true" enum:"main,side,auxiliary"`
	Operation            string `json:"operation" required:"true" enum:"generation,count_tokens,models,model,responses_retrieve,responses_input_items,responses_cancel,responses_delete,embeddings,moderations"`
	Purpose              string `json:"purpose" required:"true" enum:"chat,subagent,compaction,title,description,review,memory,suggestion,probe,count_tokens,model_list,embedding,moderation,unknown"`
	PurposeSource        string `json:"purpose_source" required:"true" enum:"declared,heuristic,default"`
	Resolution           string `json:"resolution" required:"true" enum:"exact,alias,explicit,fallback"`
	State                string `json:"state" required:"true" enum:"running,completed,failed,cancelled,interrupted"`
	Outcome              string `json:"outcome" required:"true" enum:"ok,blocked,rejected,client_error,upstream_error,unsupported,not_found,rate_limited,cancelled"`
	ErrorCode            string `json:"error_code" required:"true"`
	InputTokens          int64  `json:"input_tokens" required:"true"`
	OutputTokens         int64  `json:"output_tokens" required:"true"`
	CacheReadTokens      int64  `json:"cache_read_tokens" required:"true"`
	CacheWriteTokens     int64  `json:"cache_write_tokens" required:"true"`
	ReasoningTokens      int64  `json:"reasoning_tokens" required:"true"`
	CostMicros           *int64 `json:"cost_micros,omitempty"`
	Currency             string `json:"currency" required:"true"`
	DurationMilliseconds int    `json:"duration_ms" required:"true"`
	Attempts             int    `json:"attempts" required:"true"`
	FindingCount         int    `json:"finding_count" required:"true"`
	RedactionValues      int    `json:"redaction_values" required:"true"`
	CreatedAt            string `json:"created_at" required:"true"`
}

type TrajectoryTurnView struct {
	Index  int    `json:"index" required:"true"`
	Role   string `json:"role" required:"true" enum:"system,developer,user,assistant,tool"`
	CallID string `json:"call_id" required:"true"`
}

type TrajectoryLocatorView struct {
	CallID  string `json:"call_id" required:"true"`
	Section string `json:"section" required:"true" enum:"request,response,instructions,params,tools"`
	Ordinal int    `json:"ordinal" required:"true"`
	Path    string `json:"path" required:"true"`
	ToolID  string `json:"tool_id" required:"true"`
}

type TrajectoryItemView struct {
	Locator         TrajectoryLocatorView `json:"locator" required:"true"`
	Kind            string                `json:"kind" required:"true"`
	Origin          string                `json:"origin" required:"true"`
	Trust           string                `json:"trust" required:"true"`
	Preview         string                `json:"preview" required:"true"`
	ShownRedactions []ShownRedactionView  `json:"shown_redactions" required:"true"`
	RedactionLabels []string              `json:"redaction_labels" required:"true"`
}

type TrajectoryToolCallView struct {
	ToolCallID string     `json:"tool_call_id" required:"true"`
	CallID     string     `json:"call_id" required:"true"`
	Name       string     `json:"name" required:"true"`
	Kind       string     `json:"kind" required:"true"`
	Sink       string     `json:"sink" required:"true"`
	Target     TargetView `json:"target" required:"true"`
	Preview    string     `json:"preview" required:"true"`
}

type TrajectoryToolResultView struct {
	ToolCallID string `json:"tool_call_id" required:"true"`
	CallID     string `json:"call_id" required:"true"`
	Name       string `json:"name" required:"true"`
	IsError    bool   `json:"is_error" required:"true"`
	Preview    string `json:"preview" required:"true"`
}

type TrajectoryAttemptView struct {
	Index                int    `json:"index" required:"true"`
	ChannelID            string `json:"channel_id" required:"true"`
	ChannelName          string `json:"channel_name" required:"true"`
	ModelID              string `json:"model_id" required:"true"`
	UpstreamModel        string `json:"upstream_model" required:"true"`
	Format               string `json:"format" required:"true"`
	KeyID                string `json:"key_id" required:"true"`
	StartOffset          int64  `json:"start_ms" required:"true"`
	EndOffset            int64  `json:"end_ms" required:"true"`
	FirstTokenOffset     int64  `json:"first_token_ms" required:"true"`
	DurationMilliseconds int64  `json:"duration_ms" required:"true"`
	Result               string `json:"result" required:"true" enum:"running,succeeded,failed,cancelled"`
	Code                 string `json:"code" required:"true"`
	Class                string `json:"class" required:"true"`
	Recovered            bool   `json:"recovered" required:"true"`
}

type TrajectoryDecisionView struct {
	DecisionID    string `json:"decision_id" required:"true"`
	CallID        string `json:"call_id" required:"true"`
	Phase         string `json:"phase" required:"true" enum:"request,tool_call,tool_result,response"`
	Action        string `json:"action" required:"true" enum:"block,record"`
	Blocked       bool   `json:"blocked" required:"true"`
	RuleID        string `json:"rule_id" required:"true"`
	Code          string `json:"code" required:"true"`
	Severity      string `json:"severity" required:"true"`
	Reason        string `json:"reason" required:"true"`
	PolicyVersion int64  `json:"policy_version" required:"true"`
	CreatedAt     string `json:"created_at" required:"true"`
}

type TrajectoryFindingView struct {
	FindingID      string                  `json:"finding_id" required:"true"`
	Fingerprint    string                  `json:"fingerprint" required:"true"`
	CallID         string                  `json:"call_id" required:"true"`
	Code           string                  `json:"code" required:"true"`
	Severity       string                  `json:"severity" required:"true" enum:"info,medium,high"`
	Confidence     float64                 `json:"confidence" required:"true"`
	Origin         string                  `json:"origin" required:"true"`
	Trust          string                  `json:"trust" required:"true"`
	Sink           string                  `json:"sink" required:"true"`
	Evidence       []TrajectoryLocatorView `json:"evidence" required:"true"`
	SinkToolCallID string                  `json:"sink_tool_call_id" required:"true"`
	Relation       string                  `json:"relation" required:"true"`
}

type TrajectoryRedactionView struct {
	ValueID     string `json:"value_id" required:"true"`
	Label       string `json:"label" required:"true"`
	Action      string `json:"action" required:"true" enum:"replace,block,alert"`
	Detector    string `json:"detector" required:"true"`
	Secret      bool   `json:"secret" required:"true"`
	Occurrences int    `json:"occurrences" required:"true"`
	FirstCallID string `json:"first_call_id" required:"true"`
	LastCallID  string `json:"last_call_id" required:"true"`
}

func (console *Console) registerTrajectory() {
	console.add("GET", "/projects/{project_id}/conversations/{session_id}/trajectory", "getConversationTrajectory", conversationPath{}, TrajectoryView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		trajectory, err := console.Trajectory.Build(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("session_id"))
		if err != nil {
			return nil, err
		}
		return trajectoryView(trajectory, identity.User.Administrator), nil
	})
}

func trajectoryView(trajectory *service.Trajectory, administrator bool) TrajectoryView {
	view := TrajectoryView{
		SessionID: trajectory.SessionID, ProjectID: trajectory.ProjectID,
		Nodes: make([]TrajectoryNodeView, 0, len(trajectory.Nodes)),
		Edges: make([]TrajectoryEdgeView, 0, len(trajectory.Edges)),
		Steps: make([]TrajectoryStepView, 0, len(trajectory.Steps)),
		Stats: TrajectoryStatsView{
			Calls: trajectory.Stats.Calls, Turns: trajectory.Stats.Turns, Items: trajectory.Stats.Items,
			ToolCalls: trajectory.Stats.ToolCalls, Attempts: trajectory.Stats.Attempts,
			Findings: trajectory.Stats.Findings, Redactions: trajectory.Stats.Redactions,
			InputTokens: trajectory.Stats.InputTokens, OutputTokens: trajectory.Stats.OutputTokens,
			CostMicros: trajectory.Stats.CostMicros, Currency: trajectory.Stats.Currency,
			DurationMilliseconds: trajectory.Stats.DurationMilliseconds,
		},
		Truncated: trajectory.Truncated, Warnings: nonNil(trajectory.Warnings),
	}
	for index := range trajectory.Nodes {
		view.Nodes = append(view.Nodes, trajectoryNodeView(&trajectory.Nodes[index], administrator))
	}
	for index := range trajectory.Edges {
		edge := &trajectory.Edges[index]
		view.Edges = append(view.Edges, TrajectoryEdgeView{ID: edge.ID, Kind: string(edge.Kind), From: edge.From, To: edge.To, Inferred: edge.Inferred})
	}
	for index := range trajectory.Steps {
		step := &trajectory.Steps[index]
		view.Steps = append(view.Steps, TrajectoryStepView{
			Sequence: step.Sequence, CallID: step.CallID, Kind: step.Kind, Category: step.Category,
			Label: step.Label, Status: step.Status, Code: step.Code, NodeIDs: nonNil(step.NodeIDs), At: step.At,
		})
	}
	return view
}

func trajectoryNodeView(node *service.TrajectoryNode, administrator bool) TrajectoryNodeView {
	view := TrajectoryNodeView{ID: node.ID, Kind: string(node.Kind), CallID: node.CallID}
	if node.Session != nil {
		view.Session = &TrajectorySessionView{SessionID: node.Session.SessionID, ProjectID: node.Session.ProjectID}
	}
	if node.Call != nil {
		value := node.Call
		view.Call = &TrajectoryCallView{
			CallID: value.CallID, Model: value.Model, UpstreamModel: value.UpstreamModel, ChannelID: value.ChannelID,
			Lane: string(value.Lane), Operation: string(value.Operation), Purpose: string(value.Purpose),
			PurposeSource: string(value.PurposeSource), Resolution: value.Resolution, State: value.State,
			Outcome: string(value.Outcome), ErrorCode: value.ErrorCode,
			InputTokens: value.InputTokens, OutputTokens: value.OutputTokens,
			CacheReadTokens: value.CacheReadTokens, CacheWriteTokens: value.CacheWriteTokens, ReasoningTokens: value.ReasoningTokens,
			CostMicros: value.CostMicros, Currency: value.Currency, DurationMilliseconds: value.DurationMilliseconds,
			Attempts: value.Attempts, FindingCount: value.FindingCount, RedactionValues: value.RedactionCount,
			CreatedAt: stamp(value.CreatedAt),
		}
	}
	if node.Turn != nil {
		view.Turn = &TrajectoryTurnView{Index: node.Turn.Index, Role: string(node.Turn.Role), CallID: node.Turn.CallID}
	}
	if node.Item != nil {
		value := node.Item
		preview := value.Preview
		if administrator && value.RawPreview != "" {
			preview = value.RawPreview
		}
		view.Item = &TrajectoryItemView{
			Locator: locatorView(value.Locator), Kind: string(value.Kind), Origin: string(value.Origin),
			Trust: string(value.Trust), Preview: preview,
			ShownRedactions: viewAll(value.ShownRedactions, func(span *observation.ShownSpan) ShownRedactionView {
				return ShownRedactionView{Start: span.Start, End: span.End, Label: span.Label, Action: span.Action, ValueID: span.ValueID}
			}),
			RedactionLabels: nonNil(value.RedactionLabels),
		}
	}
	if node.ToolCall != nil {
		value := node.ToolCall
		preview := value.Preview
		if administrator && value.RawPreview != "" {
			preview = value.RawPreview
		}
		target := value.Target
		view.ToolCall = &TrajectoryToolCallView{
			ToolCallID: value.ToolCallID, CallID: value.CallID, Name: value.Name, Kind: value.Kind,
			Sink: string(value.Sink), Preview: preview,
			Target: TargetView{Kind: target.Kind, Value: target.Value, Host: target.Host, Path: target.Path, Command: target.Command},
		}
	}
	if node.ToolResult != nil {
		value := node.ToolResult
		preview := value.Preview
		if administrator && value.RawPreview != "" {
			preview = value.RawPreview
		}
		view.ToolResult = &TrajectoryToolResultView{ToolCallID: value.ToolCallID, CallID: value.CallID, Name: value.Name, IsError: value.IsError, Preview: preview}
	}
	if node.Attempt != nil {
		value := node.Attempt
		view.Attempt = &TrajectoryAttemptView{
			Index: value.Index, ChannelID: value.ChannelID, ChannelName: value.ChannelName,
			ModelID: value.ModelID, UpstreamModel: value.UpstreamModel, Format: value.Format, KeyID: value.KeyID,
			StartOffset: value.StartOffset, EndOffset: value.EndOffset, FirstTokenOffset: value.FirstTokenOffset,
			DurationMilliseconds: value.DurationMilliseconds, Result: value.Result, Code: value.Code,
			Class: value.Class, Recovered: value.Recovered,
		}
	}
	if node.Decision != nil {
		value := node.Decision
		view.Decision = &TrajectoryDecisionView{
			DecisionID: value.DecisionID, CallID: value.CallID, Phase: value.Phase, Action: value.Action,
			Blocked: value.Blocked, RuleID: value.RuleID, Code: value.Code, Severity: value.Severity,
			Reason: value.Reason, PolicyVersion: value.PolicyVersion, CreatedAt: stamp(value.CreatedAt),
		}
	}
	if node.Finding != nil {
		value := node.Finding
		view.Finding = &TrajectoryFindingView{
			FindingID: value.FindingID, Fingerprint: value.Fingerprint, CallID: value.CallID, Code: value.Code,
			Severity: value.Severity, Confidence: value.Confidence, Origin: string(value.Origin),
			Trust: string(value.Trust), Sink: string(value.Sink),
			Evidence: viewAll(value.Evidence, func(locator *observation.Locator) TrajectoryLocatorView {
				return locatorView(*locator)
			}),
			SinkToolCallID: value.SinkToolCallID, Relation: value.Relation,
		}
	}
	if node.Redaction != nil {
		value := node.Redaction
		view.Redaction = &TrajectoryRedactionView{
			ValueID: value.ValueID, Label: value.Label, Action: value.Action, Detector: value.Detector,
			Secret: value.Secret, Occurrences: value.Occurrences, FirstCallID: value.FirstCallID, LastCallID: value.LastCallID,
		}
	}
	return view
}

func locatorView(locator observation.Locator) TrajectoryLocatorView {
	return TrajectoryLocatorView{
		CallID: locator.CallID, Section: string(locator.Section), Ordinal: locator.Ordinal,
		Path: locator.Path, ToolID: locator.ToolID,
	}
}
