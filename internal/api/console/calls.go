package console

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/observation"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	diagnosis "github.com/yv1ing/SafeAgent/internal/service/diagnosis"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type CallView struct {
	ID                     string     `json:"id" required:"true"`
	AgentKeyID             string     `json:"agent_key_id" required:"true"`
	ClientFormat           string     `json:"client_format" required:"true"`
	PublicModel            string     `json:"public_model" required:"true"`
	UpstreamModel          string     `json:"upstream_model" required:"true"`
	ChannelID              string     `json:"channel_id" required:"true"`
	State                  string     `json:"state" required:"true" enum:"running,completed,failed,blocked,cancelled,interrupted,rejected"`
	ErrorCode              string     `json:"error_code" required:"true"`
	Stream                 bool       `json:"stream" required:"true"`
	FirstTokenMilliseconds int        `json:"first_token_ms" required:"true"`
	DurationMilliseconds   int        `json:"duration_ms" required:"true"`
	InputTokens            int64      `json:"input_tokens" required:"true"`
	OutputTokens           int64      `json:"output_tokens" required:"true"`
	CacheReadTokens        int64      `json:"cache_read_tokens" required:"true"`
	CacheWriteTokens       int64      `json:"cache_write_tokens" required:"true"`
	ReasoningTokens        int64      `json:"reasoning_tokens" required:"true"`
	UsageKnown             bool       `json:"usage_known" required:"true"`
	CostMicros             *int64     `json:"cost_micros" required:"true"`
	Currency               string     `json:"currency" required:"true"`
	RedactionHits          int        `json:"redaction_hits" required:"true"`
	AnalysisState          string     `json:"analysis_state" required:"true"`
	LatestAnalysisID       string     `json:"latest_analysis_id" required:"true"`
	FindingCount           int        `json:"finding_count" required:"true"`
	MaxSeverity            string     `json:"max_severity" required:"true"`
	Losses                 []LossView `json:"losses" required:"true"`
	Attempts               int        `json:"attempts" required:"true"`
	SessionID              string     `json:"session_id" required:"true"`
	ParentCallID           string     `json:"parent_call_id" required:"true"`
	CreatedAt              string     `json:"created_at" required:"true"`
	TerminatedAt           string     `json:"terminated_at" required:"true"`
}

type LossView struct {
	Path    string `json:"path" required:"true"`
	Code    string `json:"code" required:"true"`
	Class   string `json:"class,omitempty" description:"fatal or degraded"`
	Detail  string `json:"detail" required:"true"`
	From    string `json:"from" required:"true"`
	To      string `json:"to" required:"true"`
	ItemKey string `json:"item_key,omitempty"`
}

type CallPage struct {
	Items []CallView  `json:"items" required:"true"`
	Next  *CursorView `json:"next,omitempty"`
}

type TranscriptView struct {
	Turns     []TurnView `json:"turns" required:"true"`
	Tools     []ToolView `json:"tools" required:"true"`
	Truncated bool       `json:"truncated" required:"true"`
}

type TurnView struct {
	Index int        `json:"index" required:"true"`
	Role  string     `json:"role" required:"true"`
	Items []ItemView `json:"items" required:"true"`
}

type ItemView struct {
	Kind       string          `json:"kind" required:"true"`
	Origin     string          `json:"origin" required:"true"`
	Trust      string          `json:"trust" required:"true"`
	Text       string          `json:"text" required:"true"`
	ToolCall   *ToolCallView   `json:"tool_call,omitempty"`
	ToolResult *ToolResultView `json:"tool_result,omitempty"`
	Media      *MediaView      `json:"media,omitempty"`
	CallID     string          `json:"call_id" required:"true"`
	Path       string          `json:"path,omitempty"`
}

type ToolCallView struct {
	ID           string     `json:"id" required:"true"`
	Name         string     `json:"name" required:"true"`
	Kind         string     `json:"kind" required:"true"`
	Arguments    string     `json:"arguments" required:"true"`
	Target       TargetView `json:"target" required:"true"`
	Capabilities []string   `json:"capabilities" required:"true"`
	Sink         string     `json:"sink" required:"true"`
}

type TargetView struct {
	Kind    string `json:"kind" required:"true"`
	Value   string `json:"value" required:"true"`
	Host    string `json:"host" required:"true"`
	Path    string `json:"path" required:"true"`
	Command string `json:"command" required:"true"`
}

type ToolResultView struct {
	CallID  string     `json:"call_id" required:"true"`
	Name    string     `json:"name" required:"true"`
	IsError bool       `json:"is_error" required:"true"`
	Content []ItemView `json:"content" required:"true"`
}

type MediaView struct {
	Kind     string `json:"kind" required:"true"`
	MIMEType string `json:"mime_type" required:"true"`
	Filename string `json:"filename" required:"true"`
}

type ToolView struct {
	Name        string   `json:"name" required:"true"`
	Kind        string   `json:"kind" required:"true"`
	Description string   `json:"description" required:"true"`
	Boundary    string   `json:"boundary" required:"true"`
	Sensitivity string   `json:"sensitivity" required:"true"`
	Integrity   string   `json:"integrity" required:"true"`
	Tags        []string `json:"tags" required:"true"`
}

type EvidenceView struct {
	Turn    int    `json:"turn" required:"true"`
	Item    int    `json:"item" required:"true"`
	Section string `json:"section" required:"true"`
	Path    string `json:"path" required:"true"`
	View    string `json:"view" required:"true"`
	Start   int    `json:"start" required:"true"`
	End     int    `json:"end" required:"true"`
	Quote   string `json:"quote" required:"true"`
}

type AttributionView struct {
	Turn       int    `json:"turn" required:"true"`
	Item       int    `json:"item" required:"true"`
	ToolCallID string `json:"tool_call_id" required:"true"`
	Source     string `json:"source" required:"true"`
}

type AxesView struct {
	Source string `json:"source" required:"true"`
	Mode   string `json:"mode" required:"true"`
	Harm   string `json:"harm" required:"true"`
}

type FlowView struct {
	SourceTurn     int     `json:"source_turn" required:"true"`
	SourceItem     int     `json:"source_item" required:"true"`
	SourceOrigin   string  `json:"source_origin" required:"true"`
	SourcePath     string  `json:"source_path" required:"true"`
	SinkToolCallID string  `json:"sink_tool_call_id" required:"true"`
	Sink           string  `json:"sink" required:"true"`
	Relation       string  `json:"relation" required:"true"`
	ContentHash    string  `json:"content_hash" required:"true"`
	Confidence     float64 `json:"confidence" required:"true"`
}

type FindingView struct {
	Fingerprint     string            `json:"fingerprint" required:"true"`
	Code            string            `json:"code" required:"true"`
	Origin          string            `json:"origin" required:"true"`
	Trust           string            `json:"trust" required:"true"`
	Sink            string            `json:"sink" required:"true"`
	Axes            AxesView          `json:"axes" required:"true"`
	Severity        string            `json:"severity" required:"true"`
	Confidence      float64           `json:"confidence" required:"true"`
	DetectorID      string            `json:"detector_id" required:"true"`
	DetectorVersion string            `json:"detector_version" required:"true"`
	Evidence        []EvidenceView    `json:"evidence" required:"true"`
	Attribution     []AttributionView `json:"attribution" required:"true"`
	Flow            *FlowView         `json:"flow,omitempty"`
}

type VersionsView struct {
	Adapter       string `json:"adapter" required:"true"`
	Detectors     string `json:"detectors" required:"true"`
	Catalogue     string `json:"catalogue" required:"true"`
	Taxonomy      string `json:"taxonomy" required:"true"`
	DecisionModel string `json:"decision_model" required:"true"`
}

type VerdictView struct {
	Value      string  `json:"value" required:"true" enum:"safe,unsafe,unknown"`
	Confidence float64 `json:"confidence" required:"true"`
	Reason     string  `json:"reason" required:"true"`
}

type CoverageView struct {
	Evaluated []string `json:"evaluated" required:"true"`
	Skipped   []string `json:"skipped" required:"true"`
	Errors    []string `json:"errors" required:"true"`
	Complete  bool     `json:"complete" required:"true"`
}

type ModelView struct {
	Unsafe          bool    `json:"unsafe" required:"true"`
	Probability     float64 `json:"probability" required:"true"`
	Confidence      float64 `json:"confidence" required:"true"`
	ConfidenceKnown bool    `json:"confidence_known" required:"true"`
	Source          string  `json:"source" required:"true"`
	Mode            string  `json:"mode" required:"true"`
	Harm            string  `json:"harm" required:"true"`
	Severity        string  `json:"severity" required:"true"`
	Model           string  `json:"model" required:"true"`
	AskedAt         string  `json:"asked_at" required:"true"`
}

type AnalysisView struct {
	ID         string        `json:"id" required:"true"`
	State      string        `json:"state" required:"true" enum:"running,completed,failed"`
	InputHash  string        `json:"input_hash" required:"true"`
	Versions   VersionsView  `json:"versions" required:"true"`
	Verdict    VerdictView   `json:"verdict" required:"true"`
	Coverage   CoverageView  `json:"coverage" required:"true"`
	Model      *ModelView    `json:"model,omitempty"`
	ModelState string        `json:"model_state" required:"true"`
	Error      string        `json:"error" required:"true"`
	StartedAt  string        `json:"started_at" required:"true"`
	FinishedAt string        `json:"finished_at" required:"true"`
	Findings   []FindingView `json:"findings" required:"true"`
	Flows      []FlowView    `json:"flows" required:"true"`
}

type DecisionView struct {
	ID            string `json:"id" required:"true"`
	Phase         string `json:"phase" required:"true" enum:"request,tool_call"`
	Action        string `json:"action" required:"true" enum:"block,record"`
	Blocked       bool   `json:"blocked" required:"true"`
	RuleID        string `json:"rule_id" required:"true"`
	Code          string `json:"code" required:"true"`
	Severity      string `json:"severity" required:"true"`
	Reason        string `json:"reason" required:"true"`
	PolicyVersion int64  `json:"policy_version" required:"true"`
	CreatedAt     string `json:"created_at" required:"true"`
}

type CallDetailView struct {
	Call       CallView       `json:"call" required:"true"`
	Transcript TranscriptView `json:"transcript" required:"true"`
	Decisions  []DecisionView `json:"decisions" required:"true"`
	Analyses   []AnalysisView `json:"analyses" required:"true"`
	Attempts   []AttemptView  `json:"attempts" required:"true"`
	Events     []EventView    `json:"events" required:"true" description:"Only returned to administrators"`
	Hits       []HitView      `json:"hits" required:"true"`
}

type AttemptView struct {
	ChannelID        string `json:"channel_id" required:"true"`
	ChannelName      string `json:"channel_name" required:"true"`
	UpstreamModel    string `json:"upstream_model" required:"true"`
	KeyID            string `json:"key_id" required:"true"`
	StartOffset      int64  `json:"start_ms" required:"true"`
	EndOffset        int64  `json:"end_ms" required:"true"`
	FirstTokenOffset int64  `json:"first_token_ms" required:"true"`
	Result           string `json:"result" required:"true"`
	Code             string `json:"code" required:"true"`
	Class            string `json:"class" required:"true"`
}

type EventView struct {
	Sequence      int64           `json:"sequence" required:"true"`
	Kind          string          `json:"kind" required:"true"`
	SchemaVersion int             `json:"schema_version" required:"true"`
	Payload       json.RawMessage `json:"payload" required:"true"`
	CreatedAt     string          `json:"created_at" required:"true"`
}

type listCallsInput struct {
	projectPath
	pageQuery
	State       string `query:"state" json:"-" enum:"running,completed,failed,blocked,cancelled,interrupted,rejected"`
	MinSeverity string `query:"min_severity" json:"-" enum:"info,medium,high"`
	Model       string `query:"model" json:"-"`
	AgentKeyID  string `query:"agent_key_id" json:"-"`
	SessionID   string `query:"session_id" json:"-"`
}

type callPath struct {
	ProjectID string `path:"project_id" json:"-" required:"true"`
	CallID    string `path:"call_id" json:"-" required:"true"`
}

type analysesOfCallInput struct {
	callPath
}

type diffInput struct {
	projectPath
	Before string `query:"before" json:"-" required:"true"`
	After  string `query:"after" json:"-" required:"true"`
}

type AnalysisPage struct {
	Items []AnalysisView `json:"items" required:"true"`
}

type DiffView struct {
	BeforeID    string              `json:"before_id" required:"true"`
	AfterID     string              `json:"after_id" required:"true"`
	Comparable  bool                `json:"comparable" required:"true"`
	Reason      string              `json:"reason" required:"true"`
	Added       []FindingView       `json:"added" required:"true"`
	Removed     []FindingView       `json:"removed" required:"true"`
	Changed     []FindingChangeView `json:"changed" required:"true"`
	BeforeCount int                 `json:"before_count" required:"true"`
	AfterCount  int                 `json:"after_count" required:"true"`
}

type FindingChangeView struct {
	Fingerprint string      `json:"fingerprint" required:"true"`
	Code        string      `json:"code" required:"true"`
	Before      FindingView `json:"before" required:"true"`
	After       FindingView `json:"after" required:"true"`
}

func callView(value *entity.Call) CallView {
	losses := make([]LossView, 0, len(value.Losses))
	for _, loss := range value.Losses {
		losses = append(losses, LossView{Path: loss.Path, Code: loss.Code, Class: loss.Class, Detail: loss.Detail, From: loss.From, To: loss.To, ItemKey: loss.ItemKey})
	}
	return CallView{
		ID: value.ID, AgentKeyID: value.AgentKeyID, ClientFormat: value.ClientFormat, PublicModel: value.PublicModel,
		UpstreamModel: value.UpstreamModel, ChannelID: value.ChannelID, State: value.State, ErrorCode: value.ErrorCode,
		Stream: value.Stream, FirstTokenMilliseconds: value.FirstTokenMilliseconds, DurationMilliseconds: value.DurationMilliseconds,
		InputTokens: value.InputTokens, OutputTokens: value.OutputTokens, CacheReadTokens: value.CacheReadTokens,
		CacheWriteTokens: value.CacheWriteTokens, ReasoningTokens: value.ReasoningTokens, UsageKnown: value.UsageKnown,
		CostMicros: value.CostMicros, Currency: value.Currency, RedactionHits: value.RedactionHits,
		AnalysisState: value.AnalysisState, LatestAnalysisID: value.LatestAnalysisID, FindingCount: value.FindingCount,
		MaxSeverity: value.MaxSeverity, Losses: losses, Attempts: value.Attempts, SessionID: value.SessionID,
		ParentCallID: value.ParentCallID, CreatedAt: stamp(value.CreatedAt), TerminatedAt: stampOptional(value.TerminatedAt),
	}
}

func eventView(value *entity.Event) EventView {
	return EventView{Sequence: value.Sequence, Kind: value.Kind, SchemaVersion: value.SchemaVersion, Payload: value.Payload, CreatedAt: stamp(value.CreatedAt)}
}

func attemptView(value *service.CallAttempt) AttemptView {
	return AttemptView{ChannelID: value.ChannelID, ChannelName: value.ChannelName, UpstreamModel: value.UpstreamModel, KeyID: value.KeyID, StartOffset: value.StartOffset, EndOffset: value.EndOffset, FirstTokenOffset: value.FirstTokenOffset, Result: value.Result, Code: value.Code, Class: value.Class}
}

func itemView(value observation.Item, administrator bool) ItemView {
	view := ItemView{Kind: string(value.Kind), Origin: string(value.Origin), Trust: string(value.Trust), CallID: value.Locator.CallID, Path: value.Locator.Path}
	view.Text = textOf(value.Text, administrator)
	if value.ToolCall != nil {
		target := value.ToolCall.Target
		if !administrator && value.ToolCall.TargetShown.Kind != "" {
			target = value.ToolCall.TargetShown
		}
		view.ToolCall = &ToolCallView{
			ID: value.ToolCall.ID, Name: value.ToolCall.Name, Kind: value.ToolCall.Kind,
			Arguments:    textOf(value.ToolCall.Arguments, administrator),
			Target:       TargetView{Kind: target.Kind, Value: target.Value, Host: target.Host, Path: target.Path, Command: target.Command},
			Capabilities: nonNil(value.ToolCall.Capabilities), Sink: string(value.ToolCall.Sink),
		}
	}
	if value.ToolResult != nil {
		content := make([]ItemView, 0, len(value.ToolResult.Content))
		for _, item := range value.ToolResult.Content {
			content = append(content, itemView(item, administrator))
		}
		view.ToolResult = &ToolResultView{CallID: value.ToolResult.CallID, Name: value.ToolResult.Name, IsError: value.ToolResult.IsError, Content: content}
	}
	if value.Media != nil {
		view.Media = &MediaView{Kind: value.Media.Kind, MIMEType: value.Media.MIMEType, Filename: value.Media.Filename}
	}
	return view
}

func textOf(value observation.Value, administrator bool) string {
	if administrator {
		return value.Raw
	}
	return value.Shown
}

func transcriptView(value *service.Transcript, administrator bool) TranscriptView {
	turns := make([]TurnView, 0, len(value.Trace.Turns))
	for _, turn := range value.Trace.Turns {
		items := make([]ItemView, 0, len(turn.Items))
		for _, item := range turn.Items {
			items = append(items, itemView(item, administrator))
		}
		turns = append(turns, TurnView{Index: turn.Index, Role: string(turn.Role), Items: items})
	}
	tools := make([]ToolView, 0, len(value.Trace.Tools))
	for _, tool := range value.Trace.Tools {
		description := tool.Description
		if !administrator {
			description = tool.DescriptionShown
		}
		tools = append(tools, ToolView{Name: tool.Name, Kind: tool.Kind, Description: description, Boundary: tool.Tags.Boundary, Sensitivity: tool.Tags.Sensitivity, Integrity: tool.Tags.Integrity, Tags: nonNil(tool.Tags.Tags)})
	}
	return TranscriptView{Turns: turns, Tools: tools, Truncated: value.Truncated}
}

func flowView(value *analysis.Flow) FlowView {
	return FlowView{
		SourceTurn: value.SourceTurn, SourceItem: value.SourceItem, SourceOrigin: string(value.SourceOrigin),
		SourcePath: value.SourcePath, SinkToolCallID: value.SinkToolCallID, Sink: string(value.Sink),
		Relation: value.Relation, ContentHash: value.ContentHash, Confidence: value.Confidence,
	}
}

func findingView(value *analysis.Finding) FindingView {
	evidence := make([]EvidenceView, 0, len(value.Evidence))
	for _, item := range value.Evidence {
		evidence = append(evidence, EvidenceView{Turn: item.Turn, Item: item.Item, Section: item.Section, Path: item.Path, View: item.View, Start: item.Start, End: item.End, Quote: item.Quote})
	}
	attribution := make([]AttributionView, 0, len(value.Attribution))
	for _, item := range value.Attribution {
		attribution = append(attribution, AttributionView{Turn: item.Turn, Item: item.Item, ToolCallID: item.ToolCallID, Source: item.Source})
	}
	view := FindingView{
		Fingerprint: value.Fingerprint, Code: value.Code, Origin: string(value.Origin), Trust: string(value.Trust),
		Sink: string(value.Sink), Axes: AxesView{Source: string(value.Axes.Source), Mode: string(value.Axes.Mode), Harm: string(value.Axes.Harm)},
		Severity: value.Severity, Confidence: value.Confidence, DetectorID: value.Detector.ID,
		DetectorVersion: value.Detector.Version, Evidence: evidence, Attribution: attribution,
	}
	if value.Flow != nil {
		flow := flowView(value.Flow)
		view.Flow = &flow
	}
	return view
}

func analysisView(value *diagnosis.AnalysisView) AnalysisView {
	record := value.Analysis
	var model *ModelView
	if record.Model != nil {
		model = &ModelView{
			Unsafe: record.Model.Unsafe, Probability: record.Model.Probability, Confidence: record.Model.Confidence,
			ConfidenceKnown: record.Model.ConfidenceKnown,
			Source:          string(record.Model.Source), Mode: string(record.Model.Mode), Harm: string(record.Model.Harm),
			Severity: record.Model.Severity, Model: record.Model.Model, AskedAt: stamp(record.Model.AskedAt),
		}
	}
	findings := make([]FindingView, 0, len(record.Findings))
	for index := range record.Findings {
		findings = append(findings, findingView(&record.Findings[index]))
	}
	flows := make([]FlowView, 0, len(record.Flows))
	for index := range record.Flows {
		flows = append(flows, flowView(&record.Flows[index]))
	}
	return AnalysisView{
		ID: record.ID, State: record.State, InputHash: record.InputHash,
		Versions: VersionsView{
			Adapter: record.Versions.Adapter, Detectors: record.Versions.Detectors,
			Catalogue: record.Versions.Catalogue, Taxonomy: record.Versions.Taxonomy,
			DecisionModel: record.Versions.DecisionModel,
		},
		Verdict: VerdictView{Value: record.Verdict.Value, Confidence: record.Verdict.Confidence, Reason: record.Verdict.Reason},
		Coverage: CoverageView{
			Evaluated: nonNil(record.Coverage.Evaluated), Skipped: nonNil(record.Coverage.Skipped),
			Errors: nonNil(record.Coverage.Errors), Complete: record.Coverage.Complete,
		},
		Model: model, ModelState: record.ModelState,
		Error: record.Error, StartedAt: stamp(record.StartedAt), FinishedAt: stampOptional(record.FinishedAt),
		Findings: findings, Flows: flows,
	}
}

func decisionView(value *entity.Decision) DecisionView {
	return DecisionView{
		ID: value.ID, Phase: value.Phase, Action: value.Action, Blocked: value.Blocked,
		RuleID: value.RuleID, Code: value.Code, Severity: value.Severity, Reason: value.Reason,
		PolicyVersion: value.PolicyVersion, CreatedAt: stamp(value.CreatedAt),
	}
}

func callDetailView(detail *service.CallDetail, administrator bool) CallDetailView {
	events := []EventView{}
	if administrator {
		events = viewAll(detail.Events, eventView)
	}
	hits := make([]HitView, 0, len(detail.Transcript.Hits))
	for index := range detail.Transcript.Hits {
		hits = append(hits, hitView(&detail.Transcript.Hits[index]))
	}
	analyses := make([]AnalysisView, 0, len(detail.Analyses))
	for _, analysis := range detail.Analyses {
		analyses = append(analyses, analysisView(analysis))
	}
	return CallDetailView{
		Call: callView(detail.Call), Transcript: transcriptView(&detail.Transcript, administrator),
		Decisions: viewAll(detail.Decisions, decisionView), Analyses: analyses,
		Attempts: viewAll(detail.Attempts, attemptView), Events: events, Hits: hits,
	}
}

func (console *Console) registerCalls() {
	console.add("GET", "/projects/{project_id}/calls", "listCalls", listCallsInput{}, CallPage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		position, err := cursor(ctx)
		if err != nil {
			return nil, err
		}
		filter := service.CallFilter{State: ctx.Query("state"), MinSeverity: ctx.Query("min_severity"), Model: ctx.Query("model"), AgentKeyID: ctx.Query("agent_key_id"), SessionID: ctx.Query("session_id")}
		page, err := console.Calls.List(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), filter, position)
		if err != nil {
			return nil, err
		}
		return CallPage{Items: viewAll(page.Items, callView), Next: cursorView(page.Next)}, nil
	})
	console.add("GET", "/projects/{project_id}/calls/{call_id}", "getCall", callPath{}, CallDetailView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		detail, err := console.Calls.Detail(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("call_id"))
		if err != nil {
			return nil, err
		}
		if identity.User.Administrator && len(detail.Events) > 0 {
			console.recordOperation(ctx, "readCallRaw", identity)
		}
		return callDetailView(detail, identity.User.Administrator), nil
	})
	console.add("GET", "/projects/{project_id}/calls/{call_id}/analyses", "listCallAnalyses", analysesOfCallInput{}, AnalysisPage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		if err := console.Project.Require(ctx.Request.Context(), identity.actor(), ctx.Param("project_id")); err != nil {
			return nil, err
		}
		views, err := console.Analysis.AnalysesOfCall(ctx.Request.Context(), ctx.Param("project_id"), ctx.Param("call_id"))
		if err != nil {
			return nil, analysisError(err)
		}
		items := make([]AnalysisView, 0, len(views))
		for _, view := range views {
			items = append(items, analysisView(view))
		}
		return AnalysisPage{Items: items}, nil
	})
	console.add("POST", "/projects/{project_id}/calls/{call_id}/analyses", "reanalyzeCall", analysesOfCallInput{}, AnalysisView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		if err := console.Project.Require(ctx.Request.Context(), identity.actor(), ctx.Param("project_id")); err != nil {
			return nil, err
		}
		record, err := console.Analysis.AnalyzeCall(ctx.Request.Context(), ctx.Param("project_id"), ctx.Param("call_id"), "")
		if err != nil {
			return nil, analysisError(err)
		}
		view, err := console.Analysis.AnalysisView(ctx.Request.Context(), ctx.Param("project_id"), record.ID)
		if err != nil {
			return nil, analysisError(err)
		}
		return analysisView(view), nil
	})
	console.add("GET", "/projects/{project_id}/analyses/diff", "diffAnalyses", diffInput{}, DiffView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		if err := console.Project.Require(ctx.Request.Context(), identity.actor(), ctx.Param("project_id")); err != nil {
			return nil, err
		}
		if ctx.Query("before") == "" || ctx.Query("after") == "" {
			return nil, errInvalidRequest
		}
		diff, err := console.Analysis.Diff(ctx.Request.Context(), ctx.Param("project_id"), ctx.Query("before"), ctx.Query("after"))
		if err != nil {
			return nil, analysisError(err)
		}
		added := make([]FindingView, 0, len(diff.Added))
		for index := range diff.Added {
			added = append(added, findingView(&diff.Added[index]))
		}
		removed := make([]FindingView, 0, len(diff.Removed))
		for index := range diff.Removed {
			removed = append(removed, findingView(&diff.Removed[index]))
		}
		changed := make([]FindingChangeView, 0, len(diff.Changed))
		for _, item := range diff.Changed {
			changed = append(changed, FindingChangeView{Fingerprint: item.Fingerprint, Code: item.Code, Before: findingView(item.Before), After: findingView(item.After)})
		}
		return DiffView{BeforeID: diff.BeforeID, AfterID: diff.AfterID, Comparable: diff.Comparable, Reason: diff.Reason, Added: added, Removed: removed, Changed: changed, BeforeCount: diff.BeforeCount, AfterCount: diff.AfterCount}, nil
	})
}
