package redact

import (
	"strconv"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func collisionPair(t *testing.T, engine *Engine) (string, string) {
	t.Helper()
	seen := make(map[string]string, 1<<17)
	for index := 0; index < 1<<20; index++ {
		value := "value-" + strconv.Itoa(index)
		short := engine.digest("TOKEN", value)[:shortDigestLength]
		if previous, found := seen[short]; found {
			return previous, value
		}
		seen[short] = value
	}
	t.Fatal("no short-digest collision was found")
	return "", ""
}

func TestReplaceIssuesTheSamePlaceholdersAsApplyRequest(t *testing.T) {
	engine, err := NewEngine([]Rule{{
		Kind: KindRegex, Pattern: `value-[0-9]+`, Label: "TOKEN", Action: ActionReplace, Enabled: true,
	}}, []byte("secret"), "project")
	if err != nil {
		t.Fatal(err)
	}
	first, second := collisionPair(t, engine)
	text := "a " + first + " b " + second

	request := &model.Request{User: text}
	if _, err := engine.NewScope().ApplyRequest(request); err != nil {
		t.Fatal(err)
	}
	replayed, result := engine.NewScope().Replace(text, "user")
	if replayed != request.User {
		t.Fatalf("a single text must redact like a whole request:\nreplace:      %q\napply:        %q", replayed, request.User)
	}
	longLife := 1 + len("TOKEN") + 1 + longDigestLength + 1
	for _, mark := range result.Marks {
		if len(mark.Placeholder) != longLife {
			t.Fatalf("a colliding digest must take the long placeholder form: %+v", mark)
		}
	}
}
