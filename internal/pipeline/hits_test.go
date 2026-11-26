package pipeline

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/redact"
)

func TestSettlementMapping(t *testing.T) {
	failureOf := func(class model.FailureClass, code string) *model.Error {
		return &model.Error{Class: class, Code: code}
	}
	cases := []struct {
		name    string
		failure *model.Error
		lane    classify.Lane
		state   string
		outcome classify.Outcome
	}{
		{"success", nil, classify.LaneMain, "completed", classify.OutcomeOK},
		{"policy refusal completes the exchange", failureOf(model.ClassPolicy, "request_blocked"), classify.LaneMain, "completed", classify.OutcomeBlocked},
		{"cancellation", failureOf(model.ClassCancelled, "client_cancelled"), classify.LaneMain, "cancelled", classify.OutcomeCancelled},
		{"client error", failureOf(model.ClassInvalid, "invalid_request"), classify.LaneMain, "failed", classify.OutcomeClientError},
		{"upstream transient", failureOf(model.ClassTransient, "upstream_unreachable"), classify.LaneMain, "failed", classify.OutcomeUpstreamError},
		{"upstream auth", failureOf(model.ClassAuth, "upstream_auth_failed"), classify.LaneMain, "failed", classify.OutcomeUpstreamError},
		{"rate limited", failureOf(model.ClassRateLimit, "rate_limited"), classify.LaneMain, "failed", classify.OutcomeRateLimited},
		{"upstream rejected the request", failureOf(model.ClassRejected, "upstream_rejected"), classify.LaneMain, "failed", classify.OutcomeRejected},
		{"internal", failureOf(model.ClassInternal, "audit_unavailable"), classify.LaneMain, "failed", classify.OutcomeUpstreamError},
		{"side model miss is a failure", failureOf(model.ClassInvalid, "model_not_found"), classify.LaneSide, "failed", classify.OutcomeClientError},
		{"auxiliary unsupported", failureOf(model.ClassInvalid, "auxiliary_unsupported"), classify.LaneAuxiliary, "completed", classify.OutcomeUnsupported},
		{"auxiliary model kind mismatch", failureOf(model.ClassInvalid, "model_kind_mismatch"), classify.LaneAuxiliary, "completed", classify.OutcomeUnsupported},
		{"auxiliary response not found", failureOf(model.ClassInvalid, "response_not_found"), classify.LaneAuxiliary, "completed", classify.OutcomeNotFound},
		{"auxiliary invalid body is still a failure", failureOf(model.ClassInvalid, "invalid_request"), classify.LaneAuxiliary, "failed", classify.OutcomeClientError},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			settled := settlementFor(test.failure, test.lane)
			if settled.state != test.state || settled.outcome != test.outcome {
				t.Fatalf("settlement = %s/%s, want %s/%s", settled.state, settled.outcome, test.state, test.outcome)
			}
		})
	}
}

func TestRelocateHitsDropsAncestorItemsAndFillsShownRanges(t *testing.T) {
	request := &model.Request{Items: []model.Item{{Ordinal: 0}, {Ordinal: 1}, {Ordinal: 2}}}
	result := &redact.Result{
		Hits: []redact.Hit{
			{Path: "items[0].parts[0].text", Start: 0, End: 4, Label: "EMAIL", Action: redact.ActionReplace, ValueID: "v0"},
			{Path: "items[1].parts[0].text", Start: 0, End: 4, Label: "EMAIL", Action: redact.ActionReplace, ValueID: "v1"},
			{Path: "items[2].parts[0].text", Start: 4, End: 9, Label: "TOKEN", Action: redact.ActionReplace, ValueID: "v2"},
			{Path: "instructions", Start: 0, End: 3, Label: "EMAIL", Action: redact.ActionReplace, ValueID: "v2"},
			{Path: "tools[1].description", Start: 0, End: 5, Label: "TOKEN", Action: redact.ActionReplace, ValueID: "v3"},
			{Path: "metadata[0].value", Start: 1, End: 4, Label: "EMAIL", Action: redact.ActionAlert, ValueID: "v4"},
		},
		Marks: []redact.Mark{
			{Start: 0, End: 4, ShownStart: 0, ShownEnd: 11, Label: "EMAIL", Action: redact.ActionReplace, ValueID: "v0"},
			{Start: 0, End: 4, ShownStart: 0, ShownEnd: 11, Label: "EMAIL", Action: redact.ActionReplace, ValueID: "v1"},
			{Start: 4, End: 9, ShownStart: 4, ShownEnd: 20, Label: "TOKEN", Action: redact.ActionReplace, ValueID: "v2"},
			{Start: 0, End: 3, ShownStart: 0, ShownEnd: 9, Label: "EMAIL", Action: redact.ActionReplace, ValueID: "v2"},
			{Start: 0, End: 5, ShownStart: 0, ShownEnd: 13, Label: "TOKEN", Action: redact.ActionReplace, ValueID: "v3"},
			{Start: 1, End: 4, ShownStart: 1, ShownEnd: 4, Label: "EMAIL", Action: redact.ActionAlert, ValueID: "v4"},
		},
	}
	hits := relocateHits(request, 2, "call_2", result)
	if len(hits) != 4 {
		t.Fatalf("ancestor hits must be dropped, got %+v", hits)
	}
	first := hits[0]
	if first.Section != "request" || first.Ordinal != 2 || first.Path != "parts[0].text" || first.CallID != "call_2" {
		t.Fatalf("item hit relocation is wrong: %+v", first)
	}
	if first.Start != 4 || first.End != 9 {
		t.Fatalf("raw range changed: %+v", first)
	}
	instructions := hits[1]
	if instructions.Section != "instructions" || instructions.Ordinal != 0 || instructions.Path != "" {
		t.Fatalf("instruction hit relocation is wrong: %+v", instructions)
	}
	if instructions.ShownStart != 0 || instructions.ShownEnd != 9 {
		t.Fatalf("shown range must come from the matching mark: %+v", instructions)
	}
	tool := hits[2]
	if tool.Section != "tools" || tool.Ordinal != 1 || tool.Path != "description" || tool.ShownStart != 0 || tool.ShownEnd != 13 {
		t.Fatalf("tool hit relocation is wrong: %+v", tool)
	}
	params := hits[3]
	if params.Section != "params" || params.Ordinal != 0 || params.Path != "metadata[0].value" || params.ShownStart != 0 || params.ShownEnd != 0 {
		t.Fatalf("parameter hit relocation is wrong: %+v", params)
	}
}

func TestRelocateHitsDropsShownRangesWhenTheRequestIsBlocked(t *testing.T) {
	request := &model.Request{Items: []model.Item{{Ordinal: 0}}}
	result := &redact.Result{
		Blocked: true,
		Hits: []redact.Hit{
			{Path: "items[0].parts[0].text", Start: 0, End: 4, Label: "SECRET", Action: redact.ActionBlock, ValueID: "v0"},
		},
		Marks: []redact.Mark{{Start: 0, End: 4, ShownStart: 0, ShownEnd: 8, Label: "SECRET", Action: redact.ActionReplace, ValueID: "v0"}},
	}
	hits := relocateHits(request, 0, "call_1", result)
	if len(hits) != 1 || hits[0].ShownStart != 0 || hits[0].ShownEnd != 0 {
		t.Fatalf("a blocked text has no shown range: %+v", hits)
	}
	if hits[0].Section != "request" || hits[0].Ordinal != 0 {
		t.Fatalf("a blocked hit still names its item: %+v", hits[0])
	}
}

func TestShownRangesAssignAMergedPlaceholderToEveryCoveredHit(t *testing.T) {
	request := &model.Request{Items: []model.Item{{Ordinal: 0}, {Ordinal: 1}}}
	result := &redact.Result{
		Hits: []redact.Hit{
			{Path: "items[0].parts[0].text", Start: 0, End: 4, Label: "URL", Action: redact.ActionReplace, ValueID: "v0"},
			{Path: "items[0].parts[0].text", Start: 5, End: 9, Label: "TOKEN", Action: redact.ActionReplace, ValueID: "v1"},
			{Path: "items[1].parts[0].text", Start: 0, End: 3, Label: "EMAIL", Action: redact.ActionReplace, ValueID: "v2"},
		},
		Marks: []redact.Mark{
			{Start: 0, End: 9, ShownStart: 0, ShownEnd: 9, Label: "URL", Action: redact.ActionReplace, ValueID: "v0"},
			{Start: 0, End: 3, ShownStart: 0, ShownEnd: 8, Label: "EMAIL", Action: redact.ActionReplace, ValueID: "v2"},
		},
	}
	hits := relocateHits(request, 0, "call_1", result)
	if len(hits) != 3 {
		t.Fatalf("three hits expected: %+v", hits)
	}
	if hits[0].ShownStart != 0 || hits[0].ShownEnd != 9 || hits[1].ShownStart != 0 || hits[1].ShownEnd != 9 {
		t.Fatalf("both merged occurrences share the placeholder range: %+v", hits[:2])
	}
	if hits[2].ShownStart != 0 || hits[2].ShownEnd != 8 || hits[2].Ordinal != 1 {
		t.Fatalf("the next text keeps its own mark: %+v", hits[2])
	}
}
