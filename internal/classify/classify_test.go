package classify_test

import (
	"net/http"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/classify"
)

func header(name, value string) http.Header {
	values := http.Header{}
	values.Set(name, value)
	return values
}

func TestClassifyOperationDecidesLane(t *testing.T) {
	cases := []struct {
		name      string
		operation classify.Operation
		purpose   classify.Purpose
		lane      classify.Lane
	}{
		{"count tokens", classify.CountTokens, classify.PurposeCountTokens, classify.LaneAuxiliary},
		{"model list", classify.Models, classify.PurposeModelList, classify.LaneAuxiliary},
		{"single model", classify.Model, classify.PurposeModelList, classify.LaneAuxiliary},
		{"embeddings", classify.Embeddings, classify.PurposeEmbedding, classify.LaneAuxiliary},
		{"moderations", classify.Moderations, classify.PurposeModeration, classify.LaneAuxiliary},
		{"retrieve", classify.ResponsesRetrieve, classify.PurposeUnknown, classify.LaneAuxiliary},
		{"input items", classify.ResponsesInputItems, classify.PurposeUnknown, classify.LaneAuxiliary},
		{"cancel", classify.ResponsesCancel, classify.PurposeUnknown, classify.LaneAuxiliary},
		{"delete", classify.ResponsesDelete, classify.PurposeUnknown, classify.LaneAuxiliary},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := classify.Classify(testCase.operation, classify.Hints{})
			if got.Lane != testCase.lane || got.Purpose != testCase.purpose {
				t.Fatalf("got lane=%s purpose=%s, want lane=%s purpose=%s", got.Lane, got.Purpose, testCase.lane, testCase.purpose)
			}
			if got.PurposeSource != classify.SourceDefault {
				t.Fatalf("auxiliary purpose source = %s, want %s", got.PurposeSource, classify.SourceDefault)
			}
		})
	}
}

func TestDeclaredPurposesChooseTheSideLane(t *testing.T) {
	cases := []struct {
		header  string
		value   string
		purpose classify.Purpose
	}{
		{"X-Openai-Subagent", "1", classify.PurposeSubagent},
		{"X-Claude-Code-Agent-Id", "agent", classify.PurposeSubagent},
		{"X-Openai-Memgen-Request", "1", classify.PurposeMemory},
		{"X-Deepseek-Harness-Compact", "1", classify.PurposeCompaction},
	}
	for _, testCase := range cases {
		got := classify.Classify(classify.Generation, classify.Hints{Header: header(testCase.header, testCase.value)})
		if got.Purpose != testCase.purpose || got.Lane != classify.LaneSide || got.PurposeSource != classify.SourceDeclared {
			t.Fatalf("%s: got %+v, want purpose=%s lane=side source=declared", testCase.header, got, testCase.purpose)
		}
	}
}

func TestCodexMetadataDeclaresASubagent(t *testing.T) {
	got := classify.Classify(classify.Generation, classify.Hints{Header: header("X-Codex-Turn-Metadata", `{"thread_source":"subagent"}`)})
	if got.Purpose != classify.PurposeSubagent || got.Lane != classify.LaneSide {
		t.Fatalf("got %+v, want subagent side lane", got)
	}
}

func TestTitleHeuristicRequiresNoToolsAndASmallBudget(t *testing.T) {
	budget := int64(256)
	large := int64(4096)
	hints := func(tools int, max *int64, prompt string) classify.Hints {
		return classify.Hints{ToolCount: tools, MaxOutputTokens: max, Prompt: prompt}
	}
	if got := classify.Classify(classify.Generation, hints(0, &budget, "generate a title for this conversation")); got.Purpose != classify.PurposeTitle || got.Lane != classify.LaneSide || got.PurposeSource != classify.SourceHeuristic {
		t.Fatalf("title: got %+v", got)
	}
	if got := classify.Classify(classify.Generation, hints(2, &budget, "generate a title for this conversation")); got.Lane != classify.LaneMain {
		t.Fatalf("title with tools should stay main: got %+v", got)
	}
	if got := classify.Classify(classify.Generation, hints(0, &large, "generate a title for this conversation")); got.Lane != classify.LaneMain {
		t.Fatalf("title with a large budget should stay main: got %+v", got)
	}
	if got := classify.Classify(classify.Generation, hints(0, nil, "generate a title for this conversation")); got.Lane != classify.LaneMain {
		t.Fatalf("title without a budget should stay main: got %+v", got)
	}
	if got := classify.Classify(classify.Generation, hints(0, &budget, "summarise these documents")); got.Lane != classify.LaneMain {
		t.Fatalf("unrelated short prompt should stay main: got %+v", got)
	}
}

func TestPlainGenerationIsAMainChat(t *testing.T) {
	got := classify.Classify(classify.Generation, classify.Hints{Header: http.Header{}})
	if got != (classify.Classification{Operation: classify.Generation, Purpose: classify.PurposeChat, PurposeSource: classify.SourceDefault, Lane: classify.LaneMain}) {
		t.Fatalf("got %+v", got)
	}
}

func TestUnknownOperationFallsBackToGeneration(t *testing.T) {
	got := classify.Classify(classify.Operation("bogus"), classify.Hints{})
	if got.Operation != classify.Generation || got.Lane != classify.LaneMain {
		t.Fatalf("got %+v", got)
	}
}

func TestLanePredicates(t *testing.T) {
	if !classify.LaneMain.Conversational() {
		t.Fatal("main must be conversational")
	}
	if !classify.LaneSide.Conversational() {
		t.Fatal("side must be conversational")
	}
	if classify.LaneAuxiliary.Conversational() {
		t.Fatal("auxiliary must not be conversational")
	}
}

func TestVocabularyIsValidated(t *testing.T) {
	if classify.Operation("bogus").Valid() || classify.Purpose("bogus").Valid() || classify.Outcome("bogus").Valid() || classify.Lane("bogus").Valid() {
		t.Fatal("unknown values must not validate")
	}
	for _, operation := range []classify.Operation{classify.Generation, classify.CountTokens, classify.Embeddings} {
		if !operation.Valid() {
			t.Fatalf("%s should validate", operation)
		}
	}
}
