package console

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/classify"
)

// TestSpecificationRegistersTrajectoryEndpoints proves the new endpoint groups are declared through
// add() and therefore reach both the router and the OpenAPI document.
func TestSpecificationRegistersTrajectoryEndpoints(t *testing.T) {
	payload, err := Specification()
	if err != nil {
		t.Fatalf("specification: %v", err)
	}
	var document struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatalf("specification is not JSON: %v", err)
	}
	for _, path := range []string{
		"/console/api/projects/{project_id}/conversations/{session_id}/trajectory",
		"/console/api/projects/{project_id}/conversations/{session_id}/redactions",
	} {
		if _, found := document.Paths[path]; !found {
			t.Fatalf("path %q is missing from the contract", path)
		}
	}
	if _, found := document.Paths["/console/api/projects/{project_id}/conversations/{session_id}"]; found {
		t.Fatal("the removed conversation tree endpoint is still declared")
	}
}

// TestCallFilterEnumsTrackClassify keeps the OpenAPI enum tags honest: every advertised value must be
// accepted by the classifier, and every classifier value must be advertised.
func TestCallFilterEnumsTrackClassify(t *testing.T) {
	assertEnum(t, "Lane", []string{"main", "side", "auxiliary"}, func(value string) bool { return classify.Lane(value).Valid() })
	assertEnum(t, "Operation", []string{
		"generation", "count_tokens", "models", "model", "responses_retrieve",
		"responses_input_items", "responses_cancel", "responses_delete", "embeddings", "moderations",
	}, func(value string) bool { return classify.Operation(value).Valid() })
	assertEnum(t, "Purpose", []string{
		"chat", "subagent", "compaction", "title", "description", "review", "memory", "suggestion",
		"probe", "count_tokens", "model_list", "embedding", "moderation", "unknown",
	}, func(value string) bool { return classify.Purpose(value).Valid() })
	assertEnum(t, "Outcome", []string{
		"ok", "blocked", "rejected", "client_error", "upstream_error", "unsupported", "not_found",
		"rate_limited", "cancelled",
	}, func(value string) bool { return classify.Outcome(value).Valid() })
}

func assertEnum(t *testing.T, field string, expected []string, valid func(string) bool) {
	t.Helper()
	structField, found := reflect.TypeOf(listCallsInput{}).FieldByName(field)
	if !found {
		t.Fatalf("listCallsInput has no %s field", field)
	}
	tag := structField.Tag.Get("enum")
	values := strings.Split(tag, ",")
	if len(values) != len(expected) {
		t.Fatalf("%s enum = %q, want %q", field, tag, expected)
	}
	seen := map[string]bool{}
	for _, value := range values {
		if !valid(value) {
			t.Fatalf("%s advertises a value classify rejects: %q", field, value)
		}
		seen[value] = true
	}
	for _, value := range expected {
		if !seen[value] {
			t.Fatalf("%s is missing %q", field, value)
		}
	}
}
