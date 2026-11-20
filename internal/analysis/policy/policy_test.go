package policy

import "testing"

func TestFlattenedArgumentsAreDeterministicOnDottedCollisions(t *testing.T) {
	input := map[string]any{"a.b": "top", "a": map[string]any{"b": "nested"}}
	first := flatArguments(input)["a.b"]
	for range 100 {
		if got := flatArguments(input)["a.b"]; got != first {
			t.Fatalf("a map with colliding dotted keys must flatten identically: %v then %v", first, got)
		}
	}
}
