package console

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/observation"
	diagnosis "github.com/yv1ing/SafeAgent/internal/service/diagnosis"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func projectionCall(id, parent, state string) entity.Call {
	created := time.Unix(1700000000, 0).UTC()
	return entity.Call{
		ID: id, ProjectID: "prj", AgentKeyID: "key", Lane: classify.LaneMain,
		Operation: classify.Generation, Purpose: classify.PurposeChat, PurposeSource: classify.SourceDefault,
		State: state, Outcome: classify.OutcomeOK, SessionID: "session",
		ParentCallID: parent, PublicModel: "gpt", CreatedAt: created, UpdatedAt: created,
		InputTokens: 10, OutputTokens: 5, DurationMilliseconds: 100,
	}
}

func textItem(callID string, section observation.Section, ordinal int, text string) observation.Item {
	return observation.Item{
		Kind: observation.ItemText, Origin: observation.OriginUser, Trust: observation.Trusted,
		Text:    observation.NewValue(text, text, nil, nil),
		Locator: observation.Locator{CallID: callID, Section: section, Ordinal: ordinal, Path: fmt.Sprintf("items[%d].text", ordinal)},
	}
}

func requestTrace(callID string, items ...observation.Item) *observation.Trace {
	return &observation.Trace{ID: callID, Turns: []observation.Turn{
		{Index: 0, Role: observation.RoleUser, Items: items},
	}}
}

func toolCallItem(callID string, ordinal int, id, name string) observation.Item {
	return observation.Item{
		Kind: observation.ItemToolCall, Origin: observation.OriginAssistant, Trust: observation.Trusted,
		ToolCall: &observation.ToolCall{ID: id, Name: name, Kind: "function", Sink: observation.SinkShellExec},
		Locator:  observation.Locator{CallID: callID, Section: observation.SectionResponse, Ordinal: ordinal},
	}
}

func toolResultItem(callID string, ordinal int, toolCallID string) observation.Item {
	return observation.Item{
		Kind: observation.ItemToolResult, Origin: observation.OriginToolResult, Trust: observation.Untrusted,
		ToolResult: &observation.ToolResult{CallID: toolCallID, Name: "shell", Content: []observation.Item{
			textItem(callID, observation.SectionRequest, ordinal, "result"),
		}},
		Locator: observation.Locator{CallID: callID, Section: observation.SectionRequest, Ordinal: ordinal},
	}
}

func entry(t *testing.T, call *entity.Call, sequence int64, kind, payload string) auditevent.Entry {
	t.Helper()
	decoded, err := auditevent.Decode(kind, []byte(payload))
	if err != nil {
		t.Fatalf("%s payload: %v", kind, err)
	}
	return auditevent.Entry{Sequence: sequence, At: call.CreatedAt.Add(time.Duration(sequence) * time.Millisecond), Kind: kind, Decoded: decoded}
}

func findNode(t *testing.T, trajectory *Trajectory, id string) *TrajectoryNode {
	t.Helper()
	for index := range trajectory.Nodes {
		if trajectory.Nodes[index].ID == id {
			return &trajectory.Nodes[index]
		}
	}
	return nil
}

func hasEdge(trajectory *Trajectory, kind EdgeKind, from, to string) bool {
	for _, edge := range trajectory.Edges {
		if edge.Kind == kind && edge.From == from && edge.To == to {
			return true
		}
	}
	return false
}

func hasWarning(trajectory *Trajectory, contains string) bool {
	for _, warning := range trajectory.Warnings {
		if len(warning) >= len(contains) && (warning == contains || containsSubstring(warning, contains)) {
			return true
		}
	}
	return false
}

func containsSubstring(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}

func fixtureProjection(t *testing.T) *sessionProjection {
	t.Helper()
	first := projectionCall("c1", "", "completed")
	second := projectionCall("c2", "c1", "completed")
	side := projectionCall("cs", "c1", "completed")
	side.Lane = classify.LaneSide
	side.Purpose = classify.PurposeSubagent

	sideTrace := requestTrace("cs", textItem("cs", observation.SectionRequest, 0, "subagent work"))

	firstEntries := []auditevent.Entry{
		entry(t, &first, 1, auditevent.KindRequest, `{"items":[],"params":{},"hits":[{"label":"EMAIL","action":"replace","value_id":"v1","section":"request","ordinal":0,"path":"items[0].text","start":0,"end":5,"shown_start":0,"shown_end":12}]}`),
		entry(t, &first, 2, auditevent.KindAttempt, `{"channel_id":"ch1","channel_name":"primary","model_id":"m1","upstream_model":"gpt","format":"chat","key_id":"k1"}`),
		entry(t, &first, 3, auditevent.KindFirstToken, `{"milliseconds":12}`),
		entry(t, &first, 4, auditevent.KindResponse, `{"response":{"status":"completed","finish":{"kind":"stop"},"items":[]}}`),
	}
	secondEntries := []auditevent.Entry{
		entry(t, &second, 1, auditevent.KindRequest, `{"items":[],"params":{}}`),
		entry(t, &second, 2, auditevent.KindAttempt, `{"channel_id":"ch2","channel_name":"secondary","model_id":"m2","upstream_model":"gpt","format":"chat","key_id":"k2"}`),
		entry(t, &second, 3, auditevent.KindAttemptFailed, `{"code":"upstream_timeout","class":"transient","channel_id":"ch2","key_id":"k2"}`),
		entry(t, &second, 4, auditevent.KindAttempt, `{"channel_id":"ch1","channel_name":"primary","model_id":"m1","upstream_model":"gpt","format":"chat","key_id":"k1"}`),
	}
	evidence, err := json.Marshal([]analysis.Evidence{{
		Turn: 0, Item: 0, Locator: observation.Locator{CallID: "c1", Section: observation.SectionRequest, Ordinal: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	findings := map[string][]entity.AnalysisFinding{
		"c1": {{
			ID: "f1", AnalysisID: "a1", CallID: "c1", Fingerprint: "fp1", Code: "sensitive.value_to_tool",
			Severity: analysis.SeverityHigh, Confidence: 0.9, Origin: string(observation.OriginUser),
			Trust: string(observation.Trusted), Sink: string(observation.SinkShellExec), Evidence: evidence,
			FlowSourceCallID: "c1", FlowSourceSection: string(observation.SectionRequest), FlowSourceOrdinal: 0,
			FlowSinkToolCallID: "tc1", FlowRelation: analysis.RelationValueReuse,
		}},
	}
	first.LatestAnalysisID = "a1"
	first.FindingCount = 1
	decisions := map[string][]entity.Decision{
		"c1": {{ID: "d1", CallID: "c1", Phase: "request", Action: "block", Blocked: true, Code: "EMAIL", RuleID: "EMAIL", Severity: analysis.SeverityHigh}},
	}
	return &sessionProjection{
		Conversation: &entity.Conversation{ID: "session", ProjectID: "prj", Calls: 3},
		Calls:        []entity.Call{first, second, side},
		Rebuilds: map[string]diagnosis.Rebuild{
			"c1": {
				Own: &observation.Trace{ID: "c1", Turns: []observation.Turn{
					{Index: 0, Role: observation.RoleUser, Items: []observation.Item{textItem("c1", observation.SectionRequest, 0, "hello")}},
					{Index: 1, Role: observation.RoleAssistant, Items: []observation.Item{toolCallItem("c1", 0, "tc1", "shell")}},
				}},
				Hits: []auditevent.Hit{{
					Label: "EMAIL", Action: "replace", ValueID: "v1", CallID: "c1", Section: "request",
					Ordinal: 0, Path: "items[0].text", Start: 0, End: 5, ShownStart: 0, ShownEnd: 12,
				}},
			},
			"c2": {
				Own: &observation.Trace{ID: "c2", Turns: []observation.Turn{
					{Index: 0, Role: observation.RoleUser, Items: []observation.Item{toolResultItem("c2", 0, "tc1")}},
				}},
			},
			"cs": {Own: sideTrace},
		},
		Entries:   map[string][]auditevent.Entry{"c1": firstEntries, "c2": secondEntries, "cs": {}},
		Findings:  findings,
		Decisions: decisions,
	}
}

func TestTrajectoryNodeIdentityAndEdges(t *testing.T) {
	trajectory := BuildTrajectory(fixtureProjection(t))
	for _, id := range []string{
		"session", "call:c1", "call:c2", "call:cs",
		"turn:c1:0", "item:c1:request:0", "item:c1:response:0",
		"tool:c1:tc1", "tool-result:c2:tc1", "redaction:v1", "finding:f1", "decision:d1", "attempt:c1:1",
	} {
		if findNode(t, trajectory, id) == nil {
			t.Fatalf("node %q is missing from %v", id, nodeIDs(trajectory))
		}
	}
	for _, edge := range []struct {
		kind     EdgeKind
		from, to string
	}{
		{EdgeContains, "session", "call:c1"},
		{EdgeContinues, "call:c1", "call:c2"},
		{EdgeProduces, "call:c1", "tool:c1:tc1"},
		{EdgeConsumes, "tool-result:c2:tc1", "tool:c1:tc1"},
		{EdgeContains, "item:c1:request:0", "redaction:v1"},
		{EdgeContains, "item:c1:request:0", "finding:f1"},
		{EdgeFlows, "item:c1:request:0", "tool:c1:tc1"},
		{EdgeBlocks, "decision:d1", "call:c1"},
	} {
		if !hasEdge(trajectory, edge.kind, edge.from, edge.to) {
			t.Fatalf("edge %s %s -> %s is missing", edge.kind, edge.from, edge.to)
		}
	}
	item := findNode(t, trajectory, "item:c1:request:0")
	if item.Item.Locator.Ordinal != 0 || item.Item.Locator.Section != observation.SectionRequest {
		t.Fatalf("item locator = %+v", item.Item.Locator)
	}
	call := findNode(t, trajectory, "call:c2").Call
	if call.FindingCount != 0 || call.Outcome != classify.OutcomeOK || call.Lane != classify.LaneMain {
		t.Fatalf("call node = %+v", call)
	}
	if got := findNode(t, trajectory, "call:c1").Call.RedactionCount; got != 1 {
		t.Fatalf("redaction count = %d", got)
	}
	if got := findNode(t, trajectory, "redaction:v1").Redaction.Occurrences; got != 1 {
		t.Fatalf("redaction occurrences = %d", got)
	}
}

func nodeIDs(trajectory *Trajectory) []string {
	ids := make([]string, 0, len(trajectory.Nodes))
	for _, node := range trajectory.Nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

func TestTrajectoryReplayStepsAreAuditEvents(t *testing.T) {
	trajectory := BuildTrajectory(fixtureProjection(t))
	if len(trajectory.Steps) != 8 {
		t.Fatalf("steps = %d, want one per audit event: %+v", len(trajectory.Steps), trajectory.Steps)
	}
	request := trajectory.Steps[0]
	if request.Kind != auditevent.KindRequest || request.CallID != "c1" || request.Sequence != 1 {
		t.Fatalf("first step = %+v", request)
	}
	if len(request.NodeIDs) != 2 || request.NodeIDs[0] != "call:c1" || request.NodeIDs[1] != "redaction:v1" {
		t.Fatalf("request step nodes = %v", request.NodeIDs)
	}
	attempt := trajectory.Steps[1]
	if attempt.CallID != "c1" || len(attempt.NodeIDs) != 1 || attempt.NodeIDs[0] != "attempt:c1:1" {
		t.Fatalf("attempt step = %+v", attempt)
	}
	for _, step := range trajectory.Steps {
		if step.Sequence == 0 {
			t.Fatalf("step without an audit sequence: %+v", step)
		}
	}
}

func TestAttemptsRecoveredAndRetried(t *testing.T) {
	projection := fixtureProjection(t)
	trajectory := BuildTrajectory(projection)
	// c2 has one failed attempt and no successful one: not recovered.
	attempts := findNode(t, trajectory, "attempt:c2:1")
	if attempts == nil || attempts.Attempt == nil {
		t.Fatal("attempt:c2:1 missing")
	}
	if !hasEdge(trajectory, EdgeRetried, "attempt:c2:1", "attempt:c2:2") {
		t.Fatal("retried edge missing")
	}
	completed := projectionCall("c3", "c2", "completed")
	entries := []auditevent.Entry{
		entry(t, &completed, 1, auditevent.KindAttempt, `{"channel_id":"ch1","channel_name":"primary","model_id":"m1","upstream_model":"gpt","format":"chat","key_id":"k1"}`),
		entry(t, &completed, 2, auditevent.KindAttemptFailed, `{"code":"upstream_error","class":"transient","channel_id":"ch1","key_id":"k1"}`),
		entry(t, &completed, 3, auditevent.KindAttempt, `{"channel_id":"ch2","channel_name":"secondary","model_id":"m2","upstream_model":"gpt","format":"chat","key_id":"k2"}`),
	}
	attemptRecords, _ := BuildAttempts(&completed, entries)
	if len(attemptRecords) != 2 {
		t.Fatalf("attempts = %+v", attemptRecords)
	}
	if attemptRecords[0].Result != attemptFailed || !attemptRecords[0].Recovered {
		t.Fatalf("first attempt = %+v", attemptRecords[0])
	}
	if attemptRecords[1].Result != attemptSucceeded {
		t.Fatalf("second attempt = %+v", attemptRecords[1])
	}
}

func TestTrajectoryWarningsForMissingAncestorAndDanglingToolResult(t *testing.T) {
	projection := fixtureProjection(t)
	projection.Calls[1].ParentCallID = "gone"
	projection.Rebuilds["c2"] = diagnosis.Rebuild{
		Own: &observation.Trace{ID: "c2", Turns: []observation.Turn{
			{Index: 0, Role: observation.RoleUser, Items: []observation.Item{toolResultItem("c2", 0, "unknown-tool")}},
		}},
	}
	projection.Truncated = true
	trajectory := BuildTrajectory(projection)
	if !trajectory.Truncated {
		t.Fatal("truncation was not propagated")
	}
	if !hasWarning(trajectory, "missing ancestor gone") {
		t.Fatalf("missing-ancestor warning absent: %v", trajectory.Warnings)
	}
	if !hasEdge(trajectory, EdgeContinues, "call:c1", "call:c2") || !findInferred(trajectory, "call:c1", "call:c2") {
		t.Fatal("inferred continuation missing")
	}
	if !hasWarning(trajectory, "tool result for unknown tool call") {
		t.Fatalf("dangling tool result warning absent: %v", trajectory.Warnings)
	}
}

func findInferred(trajectory *Trajectory, from, to string) bool {
	for _, edge := range trajectory.Edges {
		if edge.Kind == EdgeContinues && edge.From == from && edge.To == to {
			return edge.Inferred
		}
	}
	return false
}

func TestTrajectoryStats(t *testing.T) {
	trajectory := BuildTrajectory(fixtureProjection(t))
	stats := trajectory.Stats
	if stats.Calls != 3 || stats.Findings != 1 || stats.Redactions != 1 || stats.ToolCalls != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.InputTokens != 30 || stats.OutputTokens != 15 || stats.DurationMilliseconds != 300 {
		t.Fatalf("token stats = %+v", stats)
	}
	if stats.Turns != 4 || stats.Items != 4 {
		t.Fatalf("turn/item stats = %+v", stats)
	}
}

func TestTrajectorySkipsItemsOfOtherCalls(t *testing.T) {
	projection := fixtureProjection(t)
	// Own traces must be per call; an item that still points at another call is not duplicated.
	projection.Rebuilds["c2"] = diagnosis.Rebuild{Own: &observation.Trace{ID: "c2", Turns: []observation.Turn{
		{Index: 0, Role: observation.RoleUser, Items: []observation.Item{textItem("c1", observation.SectionRequest, 3, "foreign")}},
	}}}
	trajectory := BuildTrajectory(projection)
	if node := findNode(t, trajectory, "item:c1:request:3"); node != nil {
		t.Fatal("an item of another call leaked into the call's turn")
	}
}

func TestTrajectoryDeduplicatesToolActivityEchoedInLaterRequests(t *testing.T) {
	first := projectionCall("c1", "", "completed")
	second := projectionCall("c2", "c1", "completed")
	third := projectionCall("c3", "c2", "completed")
	echoCall := func(callID string) observation.Item {
		return observation.Item{
			Kind: observation.ItemToolCall, Origin: observation.OriginAssistant, Trust: observation.Trusted,
			ToolCall: &observation.ToolCall{ID: "tc1", Name: "shell", Kind: "function", Sink: observation.SinkShellExec},
			Locator:  observation.Locator{CallID: callID, Section: observation.SectionRequest, Ordinal: 0},
		}
	}
	echoResult := func(callID string) observation.Item {
		return observation.Item{
			Kind: observation.ItemToolResult, Origin: observation.OriginToolResult, Trust: observation.Untrusted,
			ToolResult: &observation.ToolResult{CallID: "tc1", Name: "shell"},
			Locator:    observation.Locator{CallID: callID, Section: observation.SectionRequest, Ordinal: 1},
		}
	}
	projection := &sessionProjection{
		Conversation: &entity.Conversation{ID: "session", ProjectID: "prj", Calls: 3},
		Calls:        []entity.Call{first, second, third},
		Rebuilds: map[string]diagnosis.Rebuild{
			"c1": {Own: &observation.Trace{ID: "c1", Turns: []observation.Turn{
				{Index: 0, Role: observation.RoleAssistant, Items: []observation.Item{toolCallItem("c1", 0, "tc1", "shell")}},
			}}},
			"c2": {Own: &observation.Trace{ID: "c2", Turns: []observation.Turn{
				{Index: 0, Role: observation.RoleUser, Items: []observation.Item{echoCall("c2"), echoResult("c2")}},
			}}},
			"c3": {Own: &observation.Trace{ID: "c3", Turns: []observation.Turn{
				{Index: 0, Role: observation.RoleUser, Items: []observation.Item{echoCall("c3"), echoResult("c3")}},
			}}},
		},
		Entries: map[string][]auditevent.Entry{"c2": {}},
	}
	trajectory := BuildTrajectory(projection)
	calls, results := 0, 0
	for _, node := range trajectory.Nodes {
		if node.ToolCall != nil && node.ToolCall.ToolCallID == "tc1" {
			calls++
		}
		if node.ToolResult != nil && node.ToolResult.ToolCallID == "tc1" {
			results++
		}
	}
	if calls != 1 || results != 1 {
		t.Fatalf("echoed tool activity created %d call nodes and %d result nodes, want 1 and 1", calls, results)
	}
	if !hasEdge(trajectory, EdgeConsumes, "tool-result:c2:tc1", "tool:c1:tc1") {
		t.Fatalf("the tool result is not linked to the call that produced it")
	}
}
