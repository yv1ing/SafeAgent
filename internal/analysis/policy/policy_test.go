package policy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/observation"
)

func TestFlattenedArgumentsAreDeterministicOnDottedCollisions(t *testing.T) {
	input := map[string]any{"a.b": "top", "a": map[string]any{"b": "nested"}}
	first := flatArguments(input)["a.b"]
	for range 100 {
		if got := flatArguments(input)["a.b"]; got != first {
			t.Fatalf("a map with colliding dotted keys must flatten identically: %v then %v", first, got)
		}
	}
}

func TestNumericArgumentsAreNumbersInCEL(t *testing.T) {
	arguments := flatArguments(map[string]any{"limit": json.Number("200"), "ratio": json.Number("0.5")})
	if arguments["limit"] != int64(200) || arguments["ratio"] != 0.5 {
		t.Fatalf("numeric arguments must not remain strings: %#v", arguments)
	}
}

func TestNumericArgumentRuleUsesNumberSemantics(t *testing.T) {
	compiled, err := Compile(Document{Rules: []Rule{{
		ID: "limit", Enabled: true, Phase: PhaseToolCall, When: "args.limit > 100", Action: ActionRecord,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := compiled.Evaluate(context.Background(), Input{
		Phase: PhaseToolCall,
		Tool:  observation.ToolCall{Name: "run", Input: map[string]any{"limit": json.Number("200")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Decisions) != 1 {
		t.Fatalf("a numeric argument must satisfy a numeric rule: %+v", outcome)
	}
}
