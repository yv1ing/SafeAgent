// Package classify names the axes that describe why a gateway request exists: the operation its
// endpoint performs, the purpose its client declared or implied, the lane it belongs to and the
// outcome it reached. The gateway writes these values, the console filters by them and the metric
// queries count by them, so the vocabulary lives in one leaf package instead of in any of the
// layers that consume it.
package classify

import (
	"net/http"
	"strings"
)

// Operation is the endpoint family a request addressed. It replaces the overloaded kind column that
// used to hold a wire format for generations and an operation name for everything else.
type Operation string

const (
	Generation          Operation = "generation"
	CountTokens         Operation = "count_tokens"
	Models              Operation = "models"
	Model               Operation = "model"
	ResponsesRetrieve   Operation = "responses_retrieve"
	ResponsesInputItems Operation = "responses_input_items"
	ResponsesCancel     Operation = "responses_cancel"
	ResponsesDelete     Operation = "responses_delete"
	Embeddings          Operation = "embeddings"
	Moderations         Operation = "moderations"
)

var operations = []Operation{
	Generation, CountTokens, Models, Model,
	ResponsesRetrieve, ResponsesInputItems, ResponsesCancel, ResponsesDelete,
	Embeddings, Moderations,
}

func (operation Operation) Valid() bool {
	for _, candidate := range operations {
		if operation == candidate {
			return true
		}
	}
	return false
}

// Purpose says why the client issued the request. It only matters for generation calls; auxiliary
// operations carry a purpose derived from the operation itself.
type Purpose string

const (
	PurposeChat        Purpose = "chat"
	PurposeSubagent    Purpose = "subagent"
	PurposeCompaction  Purpose = "compaction"
	PurposeTitle       Purpose = "title"
	PurposeDescription Purpose = "description"
	PurposeReview      Purpose = "review"
	PurposeMemory      Purpose = "memory"
	PurposeSuggestion  Purpose = "suggestion"
	PurposeProbe       Purpose = "probe"
	PurposeCountTokens Purpose = "count_tokens"
	PurposeModelList   Purpose = "model_list"
	PurposeEmbedding   Purpose = "embedding"
	PurposeModeration  Purpose = "moderation"
	PurposeUnknown     Purpose = "unknown"
)

var purposes = []Purpose{
	PurposeChat, PurposeSubagent, PurposeCompaction, PurposeTitle, PurposeDescription,
	PurposeReview, PurposeMemory, PurposeSuggestion, PurposeProbe, PurposeCountTokens,
	PurposeModelList, PurposeEmbedding, PurposeModeration, PurposeUnknown,
}

func (purpose Purpose) Valid() bool {
	for _, candidate := range purposes {
		if purpose == candidate {
			return true
		}
	}
	return false
}

// Side reports whether a purpose belongs to the background work an agent performs alongside its
// main conversation: subagents, compaction, title generation and the like.
func (purpose Purpose) Side() bool {
	switch purpose {
	case PurposeSubagent, PurposeCompaction, PurposeTitle, PurposeDescription,
		PurposeReview, PurposeMemory, PurposeSuggestion:
		return true
	}
	return false
}

// PurposeSource records how a purpose was established, so the console can flag an inferred one.
type PurposeSource string

const (
	SourceDeclared  PurposeSource = "declared"
	SourceHeuristic PurposeSource = "heuristic"
	SourceDefault   PurposeSource = "default"
)

// Lane groups calls by the role they play in a conversation. The lane, not the endpoint, decides
// whether a call joins a conversation, is analyzed, and is counted in traffic metrics.
type Lane string

const (
	LaneMain      Lane = "main"
	LaneSide      Lane = "side"
	LaneAuxiliary Lane = "auxiliary"
)

func (lane Lane) Valid() bool {
	return lane == LaneMain || lane == LaneSide || lane == LaneAuxiliary
}

// Conversational reports whether a call belongs to a conversation and therefore stores a session.
func (lane Lane) Conversational() bool {
	return lane == LaneMain || lane == LaneSide
}

// Analyzed reports whether a call is subject to asynchronous diagnosis.
func (lane Lane) Analyzed() bool {
	return lane == LaneMain
}

// Counted reports whether a call contributes to traffic counters and failure rates.
func (lane Lane) Counted() bool {
	return lane == LaneMain
}

// Outcome is the terminal reason of a call, independent of whether the HTTP lifecycle completed.
// A protocol-level "not found" for an auxiliary probe is a completed call with a not_found outcome,
// while an upstream timeout is a failed call with an upstream_error outcome.
type Outcome string

const (
	OutcomeOK            Outcome = "ok"
	OutcomeBlocked       Outcome = "blocked"
	OutcomeRejected      Outcome = "rejected"
	OutcomeClientError   Outcome = "client_error"
	OutcomeUpstreamError Outcome = "upstream_error"
	OutcomeUnsupported   Outcome = "unsupported"
	OutcomeNotFound      Outcome = "not_found"
	OutcomeRateLimited   Outcome = "rate_limited"
	OutcomeCancelled     Outcome = "cancelled"
)

var outcomes = []Outcome{
	OutcomeOK, OutcomeBlocked, OutcomeRejected, OutcomeClientError, OutcomeUpstreamError,
	OutcomeUnsupported, OutcomeNotFound, OutcomeRateLimited, OutcomeCancelled,
}

func (outcome Outcome) Valid() bool {
	for _, candidate := range outcomes {
		if outcome == candidate {
			return true
		}
	}
	return false
}

// Failed reports whether the outcome is a failure the operator must see: an upstream or client
// error, as opposed to a deliberate refusal or a successful protocol round trip.
func (outcome Outcome) Failed() bool {
	return outcome == OutcomeUpstreamError || outcome == OutcomeClientError
}

// Hints are the request facts the classifier may read. They are gathered once by the gateway so the
// classifier never parses a request body a second time.
type Hints struct {
	Header          http.Header
	ToolCount       int
	MaxOutputTokens *int64
	// Prompt is the leading text of the request, lowercased and truncated, used only by the title
	// heuristic. Empty when the gateway has no cheap candidate.
	Prompt string
}

// Classification is the decision for one request.
type Classification struct {
	Operation     Operation
	Purpose       Purpose
	PurposeSource PurposeSource
	Lane          Lane
}

var purposeHeaders = []struct {
	header  string
	purpose Purpose
}{
	{"X-Openai-Subagent", PurposeSubagent},
	{"X-Claude-Code-Agent-Id", PurposeSubagent},
	{"X-Deepseek-Harness-Compact", PurposeCompaction},
	{"X-Openai-Memgen-Request", PurposeMemory},
}

var titleMarkers = []string{
	"generate a title",
	"generate a short title",
	"conversation title",
	"summarize the conversation into a title",
	"为这段对话生成标题",
	"生成对话标题",
}

// Classify decides the axes of one request. Declared purposes always win; the only inferred purpose
// is a title request, which is recognisable by having no tools, a small output budget and an
// explicit title instruction. Everything else is a plain chat call, so a misclassification can only
// move a call into a background lane, never out of one.
func Classify(operation Operation, hints Hints) Classification {
	if !operation.Valid() {
		operation = Generation
	}
	if operation != Generation {
		return Classification{
			Operation:     operation,
			Purpose:       auxiliaryPurpose(operation),
			PurposeSource: SourceDefault,
			Lane:          LaneAuxiliary,
		}
	}
	for _, declared := range purposeHeaders {
		if hints.Header.Get(declared.header) != "" {
			return sideOrMain(operation, declared.purpose, SourceDeclared)
		}
	}
	if metadata := hints.Header.Get("X-Codex-Turn-Metadata"); strings.Contains(metadata, `"thread_source":"subagent"`) {
		return sideOrMain(operation, PurposeSubagent, SourceDeclared)
	}
	if isTitleRequest(hints) {
		return sideOrMain(operation, PurposeTitle, SourceHeuristic)
	}
	return Classification{Operation: operation, Purpose: PurposeChat, PurposeSource: SourceDefault, Lane: LaneMain}
}

func sideOrMain(operation Operation, purpose Purpose, source PurposeSource) Classification {
	lane := LaneMain
	if purpose.Side() {
		lane = LaneSide
	}
	return Classification{Operation: operation, Purpose: purpose, PurposeSource: source, Lane: lane}
}

func auxiliaryPurpose(operation Operation) Purpose {
	switch operation {
	case CountTokens:
		return PurposeCountTokens
	case Models, Model:
		return PurposeModelList
	case Embeddings:
		return PurposeEmbedding
	case Moderations:
		return PurposeModeration
	}
	return PurposeUnknown
}

func isTitleRequest(hints Hints) bool {
	if hints.ToolCount > 0 || len(hints.Prompt) == 0 {
		return false
	}
	if hints.MaxOutputTokens == nil || *hints.MaxOutputTokens > 512 {
		return false
	}
	probe := strings.ToLower(hints.Prompt)
	for _, marker := range titleMarkers {
		if strings.Contains(probe, marker) {
			return true
		}
	}
	return false
}
