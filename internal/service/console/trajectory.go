package console

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	diagnosis "github.com/yv1ing/SafeAgent/internal/service/diagnosis"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	// MaximumTrajectoryCalls bounds how many calls one session trajectory reads.
	MaximumTrajectoryCalls = 200
	// MaximumTrajectoryBytes bounds the decoded request/response payload a trajectory may hold.
	MaximumTrajectoryBytes = 32 << 20
	// trajectoryCacheEntries is how many sessions the in-process projection cache keeps.
	trajectoryCacheEntries = 8
	itemPreviewLength      = 160
)

// NodeKind names one kind of trajectory node. The graph is a projection: every node is derived from
// calls, audit events, findings or redaction hits, never from a second source of truth.
type NodeKind string

const (
	NodeSession    NodeKind = "session"
	NodeCall       NodeKind = "call"
	NodeTurn       NodeKind = "turn"
	NodeRequest    NodeKind = "request"
	NodeResponse   NodeKind = "response"
	NodeToolCall   NodeKind = "tool_call"
	NodeToolResult NodeKind = "tool_result"
	NodeAttempt    NodeKind = "attempt"
	NodeDecision   NodeKind = "decision"
	NodeFinding    NodeKind = "finding"
	NodeRedaction  NodeKind = "redaction"
)

// EdgeKind names one relation between two nodes. Inferred marks a relation the reader had to guess,
// such as a continuation whose parent call is outside the retained window.
type EdgeKind string

const (
	EdgeContains  EdgeKind = "contains"
	EdgeContinues EdgeKind = "continues"
	EdgeProduces  EdgeKind = "produces"
	EdgeConsumes  EdgeKind = "consumes"
	EdgeBlocks    EdgeKind = "blocks"
	EdgeFlows     EdgeKind = "flows"
	EdgeRetried   EdgeKind = "retried"
)

// Trajectory is the session-level projection the console renders as a graph, a flat list and a
// replay. Steps are the audit events themselves; the builder never invents one.
type Trajectory struct {
	SessionID string           `json:"session_id"`
	ProjectID string           `json:"project_id"`
	Nodes     []TrajectoryNode `json:"nodes"`
	Edges     []TrajectoryEdge `json:"edges"`
	Steps     []TrajectoryStep `json:"steps"`
	Stats     TrajectoryStats  `json:"stats"`
	Truncated bool             `json:"truncated"`
	Warnings  []string         `json:"warnings"`
}

type TrajectoryNode struct {
	ID         string                    `json:"id"`
	Kind       NodeKind                  `json:"kind"`
	CallID     string                    `json:"call_id,omitempty"`
	Session    *TrajectorySessionNode    `json:"session,omitempty"`
	Call       *TrajectoryCallNode       `json:"call,omitempty"`
	Turn       *TrajectoryTurnNode       `json:"turn,omitempty"`
	Item       *TrajectoryItemNode       `json:"item,omitempty"`
	ToolCall   *TrajectoryToolCallNode   `json:"tool_call,omitempty"`
	ToolResult *TrajectoryToolResultNode `json:"tool_result,omitempty"`
	Attempt    *TrajectoryAttemptNode    `json:"attempt,omitempty"`
	Decision   *TrajectoryDecisionNode   `json:"decision,omitempty"`
	Finding    *TrajectoryFindingNode    `json:"finding,omitempty"`
	Redaction  *TrajectoryRedactionNode  `json:"redaction,omitempty"`
}

type TrajectoryEdge struct {
	ID       string   `json:"id"`
	Kind     EdgeKind `json:"kind"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Inferred bool     `json:"inferred"`
}

type TrajectoryStep struct {
	Sequence int64     `json:"sequence"`
	CallID   string    `json:"call_id"`
	Kind     string    `json:"kind"`
	Category string    `json:"category"`
	Label    string    `json:"label"`
	Status   string    `json:"status"`
	Code     string    `json:"code"`
	NodeIDs  []string  `json:"node_ids"`
	At       time.Time `json:"at"`
}

type TrajectoryStats struct {
	Calls                int    `json:"calls"`
	Turns                int    `json:"turns"`
	Items                int    `json:"items"`
	ToolCalls            int    `json:"tool_calls"`
	Attempts             int    `json:"attempts"`
	Findings             int    `json:"findings"`
	Redactions           int    `json:"redactions"`
	InputTokens          int64  `json:"input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	CostMicros           *int64 `json:"cost_micros,omitempty"`
	Currency             string `json:"currency"`
	DurationMilliseconds int64  `json:"duration_ms"`
}

type TrajectorySessionNode struct {
	SessionID string `json:"session_id"`
	ProjectID string `json:"project_id"`
}

type TrajectoryCallNode struct {
	CallID               string                 `json:"call_id"`
	Model                string                 `json:"model"`
	UpstreamModel        string                 `json:"upstream_model"`
	ChannelID            string                 `json:"channel_id"`
	Lane                 classify.Lane          `json:"lane"`
	Operation            classify.Operation     `json:"operation"`
	Purpose              classify.Purpose       `json:"purpose"`
	PurposeSource        classify.PurposeSource `json:"purpose_source"`
	Resolution           string                 `json:"resolution"`
	State                string                 `json:"state"`
	Outcome              classify.Outcome       `json:"outcome"`
	ErrorCode            string                 `json:"error_code"`
	InputTokens          int64                  `json:"input_tokens"`
	OutputTokens         int64                  `json:"output_tokens"`
	CacheReadTokens      int64                  `json:"cache_read_tokens"`
	CacheWriteTokens     int64                  `json:"cache_write_tokens"`
	ReasoningTokens      int64                  `json:"reasoning_tokens"`
	CostMicros           *int64                 `json:"cost_micros,omitempty"`
	Currency             string                 `json:"currency"`
	DurationMilliseconds int                    `json:"duration_ms"`
	Attempts             int                    `json:"attempts"`
	FindingCount         int                    `json:"finding_count"`
	RedactionCount       int                    `json:"redaction_count"`
	CreatedAt            time.Time              `json:"created_at"`
}

type TrajectoryTurnNode struct {
	Index  int              `json:"index"`
	Role   observation.Role `json:"role"`
	CallID string           `json:"call_id"`
}

type TrajectoryItemNode struct {
	Locator         observation.Locator     `json:"locator"`
	Kind            observation.ItemKind    `json:"kind"`
	Origin          observation.Origin      `json:"origin"`
	Trust           observation.Trust       `json:"trust"`
	Preview         string                  `json:"preview"`
	RawPreview      string                  `json:"-"`
	ShownRedactions []observation.ShownSpan `json:"shown_redactions"`
	RedactionLabels []string                `json:"redaction_labels"`
}

type TrajectoryToolCallNode struct {
	ToolCallID string             `json:"tool_call_id"`
	CallID     string             `json:"call_id"`
	Name       string             `json:"name"`
	Kind       string             `json:"kind"`
	Sink       observation.Sink   `json:"sink"`
	Target     observation.Target `json:"target"`
	Preview    string             `json:"preview"`
	RawPreview string             `json:"-"`
}

type TrajectoryToolResultNode struct {
	ToolCallID string `json:"tool_call_id"`
	CallID     string `json:"call_id"`
	Name       string `json:"name"`
	IsError    bool   `json:"is_error"`
	Preview    string `json:"preview"`
	RawPreview string `json:"-"`
}

type TrajectoryAttemptNode struct {
	CallAttempt
	CallID string `json:"call_id"`
}

type TrajectoryDecisionNode struct {
	DecisionID    string    `json:"decision_id"`
	CallID        string    `json:"call_id"`
	Phase         string    `json:"phase"`
	Action        string    `json:"action"`
	Blocked       bool      `json:"blocked"`
	RuleID        string    `json:"rule_id"`
	Code          string    `json:"code"`
	Severity      string    `json:"severity"`
	Reason        string    `json:"reason"`
	PolicyVersion int64     `json:"policy_version"`
	CreatedAt     time.Time `json:"created_at"`
}

type TrajectoryFindingNode struct {
	FindingID      string                `json:"finding_id"`
	Fingerprint    string                `json:"fingerprint"`
	CallID         string                `json:"call_id"`
	Code           string                `json:"code"`
	Severity       string                `json:"severity"`
	Confidence     float64               `json:"confidence"`
	Origin         observation.Origin    `json:"origin"`
	Trust          observation.Trust     `json:"trust"`
	Sink           observation.Sink      `json:"sink"`
	Evidence       []observation.Locator `json:"evidence"`
	SinkToolCallID string                `json:"sink_tool_call_id"`
	Relation       string                `json:"relation"`
}

type TrajectoryRedactionNode struct {
	ValueID     string `json:"value_id"`
	Label       string `json:"label"`
	Action      string `json:"action"`
	Detector    string `json:"detector,omitempty"`
	Secret      bool   `json:"secret"`
	Occurrences int    `json:"occurrences"`
	FirstCallID string `json:"first_call_id"`
	LastCallID  string `json:"last_call_id"`
}

// CallNodeID is the stable trajectory identity of one call. The transport uses it to anchor risk
// and monitor rows to the graph node the inspector opens.
func CallNodeID(callID string) string { return callNodeID(callID) }

func sessionNodeID() string                      { return "session" }
func callNodeID(callID string) string            { return "call:" + callID }
func turnNodeID(callID string, index int) string { return fmt.Sprintf("turn:%s:%d", callID, index) }
func itemNodeID(locator observation.Locator) string {
	section := locator.Section
	if section == "" {
		section = observation.SectionRequest
	}
	return fmt.Sprintf("item:%s:%s:%d", locator.CallID, section, locator.Ordinal)
}
func toolNodeID(callID, toolCallID string) string { return "tool:" + callID + ":" + toolCallID }
func toolResultNodeID(callID, toolCallID string) string {
	return "tool-result:" + callID + ":" + toolCallID
}
func attemptNodeID(callID string, index int) string {
	return fmt.Sprintf("attempt:%s:%d", callID, index)
}
func decisionNodeID(decisionID string) string { return "decision:" + decisionID }
func findingNodeID(findingID string) string   { return "finding:" + findingID }
func redactionNodeID(valueID string) string   { return "redaction:" + valueID }
func edgeIdentity(kind EdgeKind, from, to string) string {
	return string(kind) + "\x00" + from + "\x00" + to
}

// sessionProjection is one loaded session: calls, their rebuilt traces, their timeline and the
// session-wide findings and decisions. It is the shared input of the trajectory and redaction views
// and the unit the cache stores.
type sessionProjection struct {
	Conversation *entity.Conversation
	Calls        []entity.Call
	Rebuilds     map[string]diagnosis.Rebuild
	Entries      map[string][]auditevent.Entry
	Findings     map[string][]entity.AnalysisFinding
	Decisions    map[string][]entity.Decision
	Warnings     []string
	Truncated    bool
}

// TrajectoryService builds the session trajectory and owns the session cache the redaction index
// shares, so one console request never decodes the same session twice.
type TrajectoryService struct {
	store    *repository.Store
	analyzer *diagnosis.Service
	cache    *sessionCache
}

func NewTrajectoryService(store *repository.Store, analyzer *diagnosis.Service) *TrajectoryService {
	return &TrajectoryService{store: store, analyzer: analyzer, cache: newSessionCache(trajectoryCacheEntries)}
}

// Build returns the projection of one conversation after admitting the actor to the project.
func (service *TrajectoryService) Build(ctx context.Context, actor access.Actor, projectID, sessionID string) (*Trajectory, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	projection, err := service.load(ctx, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	return BuildTrajectory(projection), nil
}

func (service *TrajectoryService) load(ctx context.Context, projectID, sessionID string) (*sessionProjection, error) {
	conversation, err := service.store.Conversation(ctx, projectID, sessionID)
	if err != nil {
		return nil, access.Translate(err)
	}
	stored, err := service.store.SessionCalls(ctx, projectID, sessionID, MaximumTrajectoryCalls+1)
	if err != nil {
		return nil, err
	}
	warnings := []string{}
	truncated := false
	if len(stored) > MaximumTrajectoryCalls {
		stored = stored[:MaximumTrajectoryCalls]
		truncated = true
		warnings = append(warnings, "only the oldest calls of this conversation are shown")
	}
	calls := make([]entity.Call, 0, len(stored))
	for _, call := range stored {
		// The repository excludes auxiliary calls; re-check so a future query change cannot leak
		// protocol traffic into the conversation graph.
		if call.Lane.Conversational() {
			calls = append(calls, call)
		}
	}
	projection := &sessionProjection{
		Conversation: conversation, Calls: calls,
		Rebuilds: map[string]diagnosis.Rebuild{}, Entries: map[string][]auditevent.Entry{},
		Findings: map[string][]entity.AnalysisFinding{}, Decisions: map[string][]entity.Decision{},
		Warnings: warnings, Truncated: truncated,
	}
	if len(calls) == 0 {
		return projection, nil
	}
	ids := make([]string, 0, len(calls))
	for _, call := range calls {
		ids = append(ids, call.ID)
	}
	sequence, err := service.store.SessionEventStamp(ctx, ids)
	if err != nil {
		return nil, err
	}
	key := sessionCacheKey(projectID, sessionID, len(calls), maxCallUpdate(calls), maxSequence(sequence))
	if cached := service.cache.get(key); cached != nil {
		return cached, nil
	}
	events, err := service.store.EventsOfCalls(ctx, ids)
	if err != nil {
		return nil, err
	}
	byCall := make(map[string][]entity.Event, len(calls))
	for _, event := range events {
		byCall[event.CallID] = append(byCall[event.CallID], event)
	}
	cutoff := len(calls)
	totalBytes := 0
	for index, call := range calls {
		size := 0
		for _, event := range byCall[call.ID] {
			if event.Kind == auditevent.KindRequest || event.Kind == auditevent.KindResponse {
				size += len(event.Payload)
			}
		}
		if index > 0 && totalBytes+size > MaximumTrajectoryBytes {
			cutoff = index
			truncated = true
			warnings = append(warnings, "the conversation is larger than the trajectory payload budget; the newest calls are omitted")
			break
		}
		totalBytes += size
	}
	calls = calls[:cutoff]
	ids = ids[:cutoff]
	projection.Calls = calls
	projection.Truncated = truncated
	projection.Warnings = warnings
	for _, call := range calls {
		records := make([]auditevent.Record, 0, len(byCall[call.ID]))
		for _, event := range byCall[call.ID] {
			records = append(records, auditevent.Record{
				CallID: event.CallID, Sequence: event.Sequence, ProjectID: event.ProjectID,
				Kind: event.Kind, SchemaVersion: event.SchemaVersion, Payload: event.Payload,
				CreatedAt: event.CreatedAt, PrevHash: event.PrevHash, Hash: event.Hash,
			})
		}
		entries, err := auditevent.BuildTimeline(records)
		if err != nil {
			return nil, err
		}
		projection.Entries[call.ID] = entries
	}
	for index := range calls {
		call := &calls[index]
		if call.State == "running" {
			projection.Warnings = append(projection.Warnings, fmt.Sprintf("call %s is still running", call.ID))
			continue
		}
		rebuild, err := service.analyzer.BuildTrace(ctx, call)
		if err != nil {
			projection.Warnings = append(projection.Warnings, fmt.Sprintf("call %s: trace unavailable", call.ID))
			continue
		}
		projection.Rebuilds[call.ID] = rebuild
	}
	findings, err := service.store.FindingsOfCalls(ctx, projectID, ids)
	if err != nil {
		return nil, err
	}
	latest := make(map[string]string, len(calls))
	for _, call := range calls {
		latest[call.ID] = call.LatestAnalysisID
	}
	for index := range findings {
		finding := &findings[index]
		if latest[finding.CallID] == "" || finding.AnalysisID != latest[finding.CallID] {
			continue
		}
		projection.Findings[finding.CallID] = append(projection.Findings[finding.CallID], *finding)
	}
	decisions, err := service.store.DecisionsOfCalls(ctx, ids)
	if err != nil {
		return nil, err
	}
	for index := range decisions {
		decision := &decisions[index]
		projection.Decisions[decision.CallID] = append(projection.Decisions[decision.CallID], *decision)
	}
	service.cache.put(key, projection)
	return projection, nil
}

func maxCallUpdate(calls []entity.Call) time.Time {
	latest := time.Time{}
	for _, call := range calls {
		if call.UpdatedAt.After(latest) {
			latest = call.UpdatedAt
		}
	}
	return latest
}

func maxSequence(sequence map[string]int64) int64 {
	var maximum int64
	for _, value := range sequence {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func sessionCacheKey(projectID, sessionID string, calls int, updated time.Time, sequence int64) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d", projectID, sessionID, calls, updated.UnixNano(), sequence)
}

type sessionCacheEntry struct {
	key        string
	projection *sessionProjection
}

type sessionCache struct {
	mutex    sync.Mutex
	capacity int
	order    *list.List
	entries  map[string]*list.Element
}

func newSessionCache(capacity int) *sessionCache {
	return &sessionCache{capacity: capacity, order: list.New(), entries: map[string]*list.Element{}}
}

func (cache *sessionCache) get(key string) *sessionProjection {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	element, found := cache.entries[key]
	if !found {
		return nil
	}
	cache.order.MoveToFront(element)
	return element.Value.(*sessionCacheEntry).projection
}

func (cache *sessionCache) put(key string, projection *sessionProjection) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if cache.capacity <= 0 {
		return
	}
	if element, found := cache.entries[key]; found {
		cache.order.MoveToFront(element)
		element.Value.(*sessionCacheEntry).projection = projection
		return
	}
	element := cache.order.PushFront(&sessionCacheEntry{key: key, projection: projection})
	cache.entries[key] = element
	for cache.order.Len() > cache.capacity {
		oldest := cache.order.Back()
		if oldest == nil {
			break
		}
		cache.order.Remove(oldest)
		delete(cache.entries, oldest.Value.(*sessionCacheEntry).key)
	}
}

// BuildTrajectory projects loaded session data. It performs no I/O, so fixtures can exercise node
// and edge identity without a database.
func BuildTrajectory(projection *sessionProjection) *Trajectory {
	builder := &trajectoryBuilder{
		trajectory: &Trajectory{
			SessionID: projection.Conversation.ID, ProjectID: projection.Conversation.ProjectID,
			Nodes: []TrajectoryNode{}, Edges: []TrajectoryEdge{}, Steps: []TrajectoryStep{},
			Truncated: projection.Truncated, Warnings: append([]string{}, projection.Warnings...),
		},
		nodeIndex:     map[string]int{},
		edges:         map[string]bool{},
		warnings:      map[string]bool{},
		toolCalls:     map[string]string{},
		toolResults:   map[string]string{},
		resultsSeen:   map[string]bool{},
		perCallTools:  map[string]map[string]string{},
		responseItems: map[string][]string{},
		attemptNodes:  map[string]map[int64]string{},
		callIndex:     map[string]int{},
	}
	for index, call := range projection.Calls {
		builder.callIndex[call.ID] = index
	}
	builder.addSession()
	for index := range projection.Calls {
		builder.addCall(projection, &projection.Calls[index])
	}
	for index := range projection.Calls {
		call := &projection.Calls[index]
		rebuild, rebuilt := projection.Rebuilds[call.ID]
		builder.addAttempts(call, projection.Entries[call.ID])
		if rebuilt {
			builder.addTrace(call, rebuild)
		}
		builder.addRedactions(call, rebuild, rebuilt)
		builder.addDecisions(call, projection.Decisions[call.ID])
		builder.addFindings(call, rebuild, rebuilt, projection.Findings[call.ID])
	}
	builder.addContinuations(projection.Calls)
	builder.addSteps(projection)
	builder.collectStats(projection.Calls)
	builder.trajectory.Warnings = builder.sortedWarnings()
	return builder.trajectory
}

type trajectoryBuilder struct {
	trajectory    *Trajectory
	nodeIndex     map[string]int
	edges         map[string]bool
	warnings      map[string]bool
	toolCalls     map[string]string
	toolResults   map[string]string
	resultsSeen   map[string]bool
	perCallTools  map[string]map[string]string
	responseItems map[string][]string
	attemptNodes  map[string]map[int64]string
	callIndex     map[string]int
}

func (builder *trajectoryBuilder) warn(format string, arguments ...any) {
	message := fmt.Sprintf(format, arguments...)
	if builder.warnings[message] {
		return
	}
	builder.warnings[message] = true
	builder.trajectory.Warnings = append(builder.trajectory.Warnings, message)
}

func (builder *trajectoryBuilder) sortedWarnings() []string {
	warnings := builder.trajectory.Warnings
	seen := map[string]bool{}
	deduped := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if seen[warning] {
			continue
		}
		seen[warning] = true
		deduped = append(deduped, warning)
	}
	builder.trajectory.Warnings = deduped
	return deduped
}

func (builder *trajectoryBuilder) addNode(node TrajectoryNode) string {
	if index, found := builder.nodeIndex[node.ID]; found {
		return builder.trajectory.Nodes[index].ID
	}
	builder.nodeIndex[node.ID] = len(builder.trajectory.Nodes)
	builder.trajectory.Nodes = append(builder.trajectory.Nodes, node)
	return node.ID
}

func (builder *trajectoryBuilder) addEdge(kind EdgeKind, from, to string, inferred bool) {
	if from == "" || to == "" {
		return
	}
	if _, fromFound := builder.nodeIndex[from]; !fromFound {
		return
	}
	if _, toFound := builder.nodeIndex[to]; !toFound {
		return
	}
	identity := edgeIdentity(kind, from, to)
	if builder.edges[identity] {
		return
	}
	builder.edges[identity] = true
	builder.trajectory.Edges = append(builder.trajectory.Edges, TrajectoryEdge{ID: identity, Kind: kind, From: from, To: to, Inferred: inferred})
}

func (builder *trajectoryBuilder) addSession() {
	trajectory := builder.trajectory
	builder.addNode(TrajectoryNode{
		ID: sessionNodeID(), Kind: NodeSession,
		Session: &TrajectorySessionNode{SessionID: trajectory.SessionID, ProjectID: trajectory.ProjectID},
	})
}

func (builder *trajectoryBuilder) addCall(projection *sessionProjection, call *entity.Call) {
	rebuild, rebuilt := projection.Rebuilds[call.ID]
	redactions := call.RedactionValues
	if rebuilt {
		redactions = distinctHits(rebuild.Hits)
	}
	builder.addNode(TrajectoryNode{
		ID: callNodeID(call.ID), Kind: NodeCall, CallID: call.ID,
		Call: &TrajectoryCallNode{
			CallID: call.ID, Model: call.PublicModel, UpstreamModel: call.UpstreamModel, ChannelID: call.ChannelID,
			Lane: call.Lane, Operation: call.Operation, Purpose: call.Purpose, PurposeSource: call.PurposeSource,
			Resolution: call.Resolution, State: call.State, Outcome: call.Outcome, ErrorCode: call.ErrorCode,
			InputTokens: call.InputTokens, OutputTokens: call.OutputTokens,
			CacheReadTokens: call.CacheReadTokens, CacheWriteTokens: call.CacheWriteTokens, ReasoningTokens: call.ReasoningTokens,
			CostMicros: call.CostMicros, Currency: call.Currency,
			DurationMilliseconds: call.DurationMilliseconds, Attempts: call.Attempts,
			FindingCount: len(projection.Findings[call.ID]), RedactionCount: redactions, CreatedAt: call.CreatedAt,
		},
	})
	builder.addEdge(EdgeContains, sessionNodeID(), callNodeID(call.ID), false)
}

func (builder *trajectoryBuilder) addAttempts(call *entity.Call, entries []auditevent.Entry) {
	attempts, bySequence := BuildAttempts(call, entries)
	builder.attemptNodes[call.ID] = map[int64]string{}
	for index := range attempts {
		attempt := attempts[index]
		nodeID := attemptNodeID(call.ID, attempt.Index)
		builder.addNode(TrajectoryNode{ID: nodeID, Kind: NodeAttempt, CallID: call.ID, Attempt: &TrajectoryAttemptNode{CallAttempt: attempt, CallID: call.ID}})
		builder.addEdge(EdgeContains, callNodeID(call.ID), nodeID, false)
		if index > 0 && attempts[index-1].Result == attemptFailed {
			builder.addEdge(EdgeRetried, attemptNodeID(call.ID, attempts[index-1].Index), nodeID, false)
		}
	}
	for sequence, index := range bySequence {
		builder.attemptNodes[call.ID][sequence] = attemptNodeID(call.ID, index)
	}
}

func (builder *trajectoryBuilder) addTrace(call *entity.Call, rebuild diagnosis.Rebuild) {
	if rebuild.Own == nil {
		return
	}
	if builder.perCallTools[call.ID] == nil {
		builder.perCallTools[call.ID] = map[string]string{}
	}
	for _, turn := range rebuild.Own.Turns {
		turnID := turnNodeID(call.ID, turn.Index)
		builder.addNode(TrajectoryNode{ID: turnID, Kind: NodeTurn, CallID: call.ID, Turn: &TrajectoryTurnNode{Index: turn.Index, Role: turn.Role, CallID: call.ID}})
		builder.addEdge(EdgeContains, callNodeID(call.ID), turnID, false)
		for _, item := range turn.Items {
			// Own traces only carry this call's items; a foreign locator means an expanded trace
			// slipped in and would duplicate the ancestor's nodes.
			if item.Locator.CallID != "" && item.Locator.CallID != call.ID {
				continue
			}
			builder.addItem(call, turnID, item)
		}
	}
}

func (builder *trajectoryBuilder) addItem(call *entity.Call, turnID string, item observation.Item) {
	locator := item.Locator
	if locator.CallID == "" {
		locator.CallID = call.ID
	}
	kind := NodeRequest
	if locator.Section == observation.SectionResponse {
		kind = NodeResponse
	}
	node := TrajectoryItemNode{
		Locator: locator, Kind: item.Kind, Origin: item.Origin, Trust: item.Trust,
		ShownRedactions: []observation.ShownSpan{}, RedactionLabels: []string{},
	}
	node.Preview, node.RawPreview = itemPreviews(item)
	if len(item.Text.ShownRedactions) > 0 {
		node.ShownRedactions = item.Text.ShownRedactions
	} else if item.ToolCall != nil {
		node.ShownRedactions = item.ToolCall.Arguments.ShownRedactions
	}
	node.RedactionLabels = labelSet(node.ShownRedactions)
	nodeID := itemNodeID(locator)
	builder.addNode(TrajectoryNode{ID: nodeID, Kind: kind, CallID: call.ID, Item: &node})
	builder.addEdge(EdgeContains, turnID, nodeID, false)
	if kind == NodeResponse {
		builder.addEdge(EdgeProduces, callNodeID(call.ID), nodeID, false)
		builder.responseItems[call.ID] = append(builder.responseItems[call.ID], nodeID)
	}
	if item.ToolCall != nil {
		builder.addToolCall(call, item.ToolCall)
	}
	if item.ToolResult != nil {
		builder.addToolResult(call, item.ToolResult)
	}
}

// addToolCall keeps one node per logical tool call. Later calls carry earlier tool calls in their
// request history, so without this a long conversation would repeat the same call at every turn.
func (builder *trajectoryBuilder) addToolCall(call *entity.Call, tool *observation.ToolCall) {
	if existing := builder.toolCalls[tool.ID]; existing != "" {
		if builder.perCallTools[call.ID] == nil {
			builder.perCallTools[call.ID] = map[string]string{}
		}
		if tool.Name != "" {
			builder.perCallTools[call.ID][tool.Name] = existing
		}
		return
	}
	nodeID := toolNodeID(call.ID, tool.ID)
	builder.addNode(TrajectoryNode{
		ID: nodeID, Kind: NodeToolCall, CallID: call.ID,
		ToolCall: &TrajectoryToolCallNode{
			ToolCallID: tool.ID, CallID: call.ID, Name: tool.Name, Kind: tool.Kind, Sink: tool.Sink,
			Target: tool.Target, Preview: previewText(tool.Arguments.Shown), RawPreview: previewText(tool.Arguments.Raw),
		},
	})
	builder.addEdge(EdgeProduces, callNodeID(call.ID), nodeID, false)
	builder.toolCalls[tool.ID] = nodeID
	if builder.perCallTools[call.ID] == nil {
		builder.perCallTools[call.ID] = map[string]string{}
	}
	if tool.Name != "" {
		builder.perCallTools[call.ID][tool.Name] = nodeID
	}
}

func (builder *trajectoryBuilder) addToolResult(call *entity.Call, result *observation.ToolResult) {
	if result.CallID == "" {
		builder.warn("call %s has a tool result without a tool call id", call.ID)
		return
	}
	nodeID := toolResultNodeID(call.ID, result.CallID)
	if builder.resultsSeen[result.CallID] {
		return
	}
	builder.resultsSeen[result.CallID] = true
	builder.toolResults[call.ID+"\x00"+result.CallID] = nodeID
	builder.addNode(TrajectoryNode{
		ID: nodeID, Kind: NodeToolResult, CallID: call.ID,
		ToolResult: &TrajectoryToolResultNode{
			ToolCallID: result.CallID, CallID: call.ID, Name: result.Name, IsError: result.IsError,
			Preview: previewText(toolResultText(result, true)), RawPreview: previewText(toolResultText(result, false)),
		},
	})
	if targetID, found := builder.toolCalls[result.CallID]; found {
		builder.addEdge(EdgeConsumes, nodeID, targetID, false)
	} else {
		builder.warn("call %s has a tool result for unknown tool call %s", call.ID, result.CallID)
	}
}

func (builder *trajectoryBuilder) addRedactions(call *entity.Call, rebuild diagnosis.Rebuild, rebuilt bool) {
	if !rebuilt {
		return
	}
	for _, hit := range rebuild.Hits {
		if hit.ValueID == "" {
			continue
		}
		nodeID := redactionNodeID(hit.ValueID)
		if index, found := builder.nodeIndex[nodeID]; found {
			node := builder.trajectory.Nodes[index].Redaction
			node.Occurrences++
			node.LastCallID = call.ID
		} else {
			builder.addNode(TrajectoryNode{
				ID: nodeID, Kind: NodeRedaction, CallID: call.ID,
				Redaction: &TrajectoryRedactionNode{
					ValueID: hit.ValueID, Label: hit.Label, Action: string(hit.Action), Secret: hit.Secret,
					Detector: detectorHint(hit.Label), Occurrences: 1, FirstCallID: call.ID, LastCallID: call.ID,
				},
			})
		}
		locator := observation.Locator{CallID: call.ID, Section: observation.Section(hit.Section), Ordinal: hit.Ordinal, Path: hit.Path}
		anchor := itemNodeID(locator)
		if _, found := builder.nodeIndex[anchor]; !found {
			anchor = callNodeID(call.ID)
		}
		builder.addEdge(EdgeContains, anchor, nodeID, false)
	}
}

func (builder *trajectoryBuilder) addDecisions(call *entity.Call, decisions []entity.Decision) {
	for index := range decisions {
		decision := &decisions[index]
		if decision.ID == "" {
			continue
		}
		nodeID := decisionNodeID(decision.ID)
		builder.addNode(TrajectoryNode{
			ID: nodeID, Kind: NodeDecision, CallID: call.ID,
			Decision: &TrajectoryDecisionNode{
				DecisionID: decision.ID, CallID: call.ID, Phase: decision.Phase, Action: decision.Action,
				Blocked: decision.Blocked, RuleID: decision.RuleID, Code: decision.Code, Severity: decision.Severity,
				Reason: decision.Reason, PolicyVersion: decision.PolicyVersion, CreatedAt: decision.CreatedAt,
			},
		})
		builder.addEdge(EdgeContains, callNodeID(call.ID), nodeID, false)
		if decision.Blocked {
			builder.addEdge(EdgeBlocks, nodeID, builder.blockedNode(call.ID, decision), false)
		}
	}
}

func (builder *trajectoryBuilder) blockedNode(callID string, decision *entity.Decision) string {
	if decision.Phase == "tool_call" || decision.Phase == "tool_result" {
		if tools := builder.perCallTools[callID]; decision.Code != "" {
			if nodeID, found := tools[decision.Code]; found {
				return nodeID
			}
		}
	}
	return callNodeID(callID)
}

func (builder *trajectoryBuilder) addFindings(call *entity.Call, rebuild diagnosis.Rebuild, rebuilt bool, findings []entity.AnalysisFinding) {
	for index := range findings {
		finding := &findings[index]
		findingID := finding.ID
		if findingID == "" {
			findingID = finding.Fingerprint
		}
		if findingID == "" {
			continue
		}
		locators := findingLocators(rebuild, rebuilt, finding)
		nodeID := findingNodeID(findingID)
		builder.addNode(TrajectoryNode{
			ID: nodeID, Kind: NodeFinding, CallID: call.ID,
			Finding: &TrajectoryFindingNode{
				FindingID: findingID, Fingerprint: finding.Fingerprint, CallID: finding.CallID, Code: finding.Code,
				Severity: finding.Severity, Confidence: finding.Confidence,
				Origin: observation.Origin(finding.Origin), Trust: observation.Trust(finding.Trust), Sink: observation.Sink(finding.Sink),
				Evidence: locators, SinkToolCallID: finding.FlowSinkToolCallID, Relation: finding.FlowRelation,
			},
		})
		for _, locator := range locators {
			builder.addEdge(EdgeContains, itemNodeID(locator), nodeID, false)
		}
		if finding.FlowSourceCallID != "" && finding.FlowSinkToolCallID != "" {
			source := itemNodeID(observation.Locator{
				CallID: finding.FlowSourceCallID, Section: observation.Section(finding.FlowSourceSection),
				Ordinal: finding.FlowSourceOrdinal, Path: finding.FlowSourcePath,
			})
			sink := builder.toolCalls[finding.FlowSinkToolCallID]
			builder.addEdge(EdgeFlows, source, sink, false)
		}
	}
}

func findingLocators(rebuild diagnosis.Rebuild, rebuilt bool, finding *entity.AnalysisFinding) []observation.Locator {
	if len(finding.Evidence) == 0 {
		return []observation.Locator{}
	}
	var evidence []analysis.Evidence
	if err := json.Unmarshal(finding.Evidence, &evidence); err != nil {
		return []observation.Locator{}
	}
	locators := make([]observation.Locator, 0, len(evidence))
	for _, item := range evidence {
		if item.Locator.CallID != "" {
			locators = append(locators, item.Locator)
			continue
		}
		if !rebuilt || rebuild.Trace == nil {
			continue
		}
		if locator, found := traceLocator(rebuild.Trace, item.Turn, item.Item); found {
			locators = append(locators, locator)
		}
	}
	return locators
}

func traceLocator(trace *observation.Trace, turn, item int) (observation.Locator, bool) {
	if turn < 0 || turn >= len(trace.Turns) {
		return observation.Locator{}, false
	}
	items := trace.Turns[turn].Items
	if item < 0 || item >= len(items) {
		return observation.Locator{}, false
	}
	locator := items[item].Locator
	if locator.CallID == "" {
		return observation.Locator{}, false
	}
	return locator, true
}

func (builder *trajectoryBuilder) addContinuations(calls []entity.Call) {
	for index := 1; index < len(calls); index++ {
		call := calls[index]
		previous := calls[index-1]
		if call.ParentCallID == "" {
			builder.addEdge(EdgeContinues, callNodeID(previous.ID), callNodeID(call.ID), true)
			builder.warn("call %s has no recorded parent; the continuation is inferred", call.ID)
			continue
		}
		if parentIndex, found := builder.callIndex[call.ParentCallID]; found && parentIndex < index {
			builder.addEdge(EdgeContinues, callNodeID(call.ParentCallID), callNodeID(call.ID), false)
			continue
		}
		builder.addEdge(EdgeContinues, callNodeID(previous.ID), callNodeID(call.ID), true)
		builder.warn("call %s names missing ancestor %s; the continuation is inferred", call.ID, call.ParentCallID)
	}
}

func (builder *trajectoryBuilder) addSteps(projection *sessionProjection) {
	for index := range projection.Calls {
		call := &projection.Calls[index]
		for _, entry := range projection.Entries[call.ID] {
			builder.trajectory.Steps = append(builder.trajectory.Steps, TrajectoryStep{
				Sequence: entry.Sequence, CallID: call.ID, Kind: entry.Kind, Category: entry.Category,
				Label: entry.Label, Status: entry.Status, Code: entry.Code,
				NodeIDs: builder.stepNodes(call, entry), At: entry.At,
			})
		}
	}
}

func (builder *trajectoryBuilder) stepNodes(call *entity.Call, entry auditevent.Entry) []string {
	callNode := callNodeID(call.ID)
	nodes := []string{callNode}
	switch entry.Kind {
	case auditevent.KindRequest:
		if entry.Decoded.Request != nil {
			for _, hit := range entry.Decoded.Request.Hits {
				if hit.ValueID == "" {
					continue
				}
				if _, found := builder.nodeIndex[redactionNodeID(hit.ValueID)]; found {
					nodes = append(nodes, redactionNodeID(hit.ValueID))
				}
			}
		}
	case auditevent.KindAttempt, auditevent.KindAttemptFailed, auditevent.KindFirstToken:
		if attempt, found := builder.attemptNodes[call.ID][entry.Sequence]; found {
			nodes = []string{attempt}
		}
	case auditevent.KindToolCall:
		if entry.Decoded.ToolCall != nil {
			if node, found := builder.toolCalls[entry.Decoded.ToolCall.ID]; found {
				nodes = []string{node}
			}
		}
	case auditevent.KindToolResult:
		if entry.Decoded.ToolResult != nil {
			if node, found := builder.toolResults[call.ID+"\x00"+entry.Decoded.ToolResult.CallID]; found {
				nodes = []string{node}
			}
		}
	case auditevent.KindDecision:
		if entry.Decoded.Decision != nil && entry.Decoded.Decision.ID != "" {
			if _, found := builder.nodeIndex[decisionNodeID(entry.Decoded.Decision.ID)]; found {
				nodes = append(nodes, decisionNodeID(entry.Decoded.Decision.ID))
			}
		}
	case auditevent.KindResponse:
		nodes = append(nodes, builder.responseItems[call.ID]...)
	}
	return dedupeStrings(nodes)
}

func (builder *trajectoryBuilder) collectStats(calls []entity.Call) {
	stats := TrajectoryStats{Calls: len(calls)}
	var cost int64
	var costKnown bool
	currencies := map[string]bool{}
	for index := range builder.trajectory.Nodes {
		switch builder.trajectory.Nodes[index].Kind {
		case NodeTurn:
			stats.Turns++
		case NodeRequest, NodeResponse:
			stats.Items++
		case NodeToolCall:
			stats.ToolCalls++
		case NodeAttempt:
			stats.Attempts++
		case NodeFinding:
			stats.Findings++
		case NodeRedaction:
			stats.Redactions++
		}
	}
	for _, call := range calls {
		stats.InputTokens += call.InputTokens
		stats.OutputTokens += call.OutputTokens
		stats.DurationMilliseconds += int64(call.DurationMilliseconds)
		if call.CostMicros != nil {
			cost += *call.CostMicros
			costKnown = true
			currencies[call.Currency] = true
		}
	}
	if costKnown {
		stats.CostMicros = &cost
	}
	if len(currencies) == 1 {
		for currency := range currencies {
			stats.Currency = currency
		}
	}
	builder.trajectory.Stats = stats
}

func distinctHits(hits []auditevent.Hit) int {
	seen := map[string]struct{}{}
	for _, hit := range hits {
		if hit.ValueID != "" {
			seen[hit.ValueID] = struct{}{}
		}
	}
	return len(seen)
}

func itemPreviews(item observation.Item) (string, string) {
	switch {
	case item.ToolCall != nil:
		return previewText(item.ToolCall.Arguments.Shown), previewText(item.ToolCall.Arguments.Raw)
	case item.ToolResult != nil:
		return previewText(toolResultText(item.ToolResult, true)), previewText(toolResultText(item.ToolResult, false))
	default:
		return previewText(item.Text.Shown), previewText(item.Text.Raw)
	}
}

func toolResultText(result *observation.ToolResult, shown bool) string {
	parts := make([]string, 0, len(result.Content))
	for _, item := range result.Content {
		text := item.Text.Raw
		if shown {
			text = item.Text.Shown
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func previewText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= itemPreviewLength {
		return text
	}
	return string(runes[:itemPreviewLength]) + "…"
}

func labelSet(spans []observation.ShownSpan) []string {
	labels := []string{}
	seen := map[string]bool{}
	for _, span := range spans {
		if span.Label == "" || seen[span.Label] {
			continue
		}
		seen[span.Label] = true
		labels = append(labels, span.Label)
	}
	return labels
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	deduped := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		deduped = append(deduped, value)
	}
	return deduped
}
