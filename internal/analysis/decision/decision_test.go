package decision_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/analysis/decision"
)

func TestInterpretReadsTheTypedAnswers(t *testing.T) {
	labels, err := decision.Interpret(decision.Answers{
		decision.QuestionUnsafe:   {Probability: 0.91, Confidence: 0.82},
		decision.QuestionSource:   {Choice: string(analysis.SourceIndirectPromptInjection), Confidence: 0.7},
		decision.QuestionMode:     {Choice: string(analysis.ModeInsecureInteraction)},
		decision.QuestionHarm:     {Choice: string(analysis.HarmSystemIntegrity)},
		decision.QuestionSeverity: {Score: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !labels.Unsafe || labels.Probability != 0.91 || labels.Source != analysis.SourceIndirectPromptInjection ||
		labels.Mode != analysis.ModeInsecureInteraction || labels.Harm != analysis.HarmSystemIntegrity ||
		labels.Severity != analysis.SeverityHigh {
		t.Fatalf("unexpected labels: %+v", labels)
	}
	if _, err := decision.Interpret(decision.Answers{decision.QuestionUnsafe: {Probability: 0.1}}); err != nil {
		t.Fatalf("a safe answer with no axes is valid: %v", err)
	}
	if _, err := decision.Interpret(decision.Answers{
		decision.QuestionUnsafe: {Probability: 0.9},
		decision.QuestionSource: {Choice: "invented_source"},
	}); err == nil {
		t.Fatal("an unknown option must be rejected")
	}
}

func TestTypeSafeAdapterSendsSystemOneQuestions(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode: %v", err)
		}
		_, _ = writer.Write([]byte(`{"model":"jev-1.13.0","answers":{"unsafe":{"type":"noul","noul":0.2},"source":{"type":"choice","choice":"none","confidence":0.8,"probabilities":{"none":0.8}},"mode":{"type":"choice","choice":"none","confidence":0.8,"probabilities":{"none":0.8}},"harm":{"type":"choice","choice":"none","confidence":0.8,"probabilities":{"none":0.8}},"severity":{"type":"score","score":2,"confidence":0.9,"legend":{"0":"info","1":"medium","2":"high"}}},"usage":{"input_tokens":12,"output_tokens":4}}`))
	}))
	defer server.Close()
	client, err := decision.NewClient(decision.Config{Kind: decision.KindTypeSafe, Endpoint: server.URL, Model: "jev-test", AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	answers, usage, err := client.Ask(context.Background(), decision.State{CallID: "call_1", Turns: []decision.Turn{}}, decision.FixedQuestions())
	if err != nil {
		t.Fatal(err)
	}
	if usage.Total() != 16 || answers[decision.QuestionUnsafe].Probability != 0.2 {
		t.Fatalf("unexpected answers: %+v %+v", answers, usage)
	}
	if answers[decision.QuestionUnsafe].ConfidenceKnown {
		t.Fatal("a Noul answer has no separate confidence")
	}
	labels, err := decision.Interpret(answers)
	if err != nil {
		t.Fatal(err)
	}
	if labels.Severity != analysis.SeverityHigh {
		t.Fatalf("a TypeSafe score level must be normalized: %+v", labels)
	}
	if received["model"] != "jev-test" {
		t.Fatalf("the pinned model was not sent: %+v", received)
	}
	questions, ok := received["questions"].(map[string]any)
	if !ok || len(questions) != 5 {
		t.Fatalf("the fixed question set must be sent as a map: %+v", received["questions"])
	}
	severity, _ := questions[decision.QuestionSeverity].(map[string]any)
	if severity["type"] != "score" || len(severity["criteria"].([]any)) != 3 {
		t.Fatalf("the score question is not in System One form: %+v", severity)
	}
}

func TestNoulProbabilityCarriesNoConfidence(t *testing.T) {
	labels, err := decision.Interpret(decision.Answers{
		decision.QuestionUnsafe: {Probability: 0.9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !labels.Unsafe || labels.ConfidenceKnown {
		t.Fatalf("a Noul answer must keep its probability without inventing confidence: %+v", labels)
	}
}

func TestOpenAIAdapterReadsTheJSONAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		content, _ := json.Marshal(map[string]any{
			"answers": map[string]any{"authorized": map[string]any{"probability": 0.05, "confidence": 0.9}},
		})
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":` + string(mustJSON(string(content))) + `}}],"usage":{"prompt_tokens":20,"completion_tokens":5}}`))
	}))
	defer server.Close()
	client, err := decision.NewClient(decision.Config{Kind: decision.KindOpenAI, Endpoint: server.URL, Model: "m", AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	answers, usage, err := client.Ask(context.Background(), decision.State{Turns: []decision.Turn{}}, []decision.Question{decision.AuthorizationQuestion()})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := decision.InterpretAuthorization(answers)
	if err != nil || answer.Probability != 0.05 {
		t.Fatalf("unexpected authorization: %+v %v", answer, err)
	}
	if usage.Total() != 25 {
		t.Fatalf("provider usage was not read: %+v", usage)
	}
}

func TestPrivateEndpointRequiresExplicitPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	if _, err := decision.NewClient(decision.Config{Kind: decision.KindTypeSafe, Endpoint: server.URL, Model: "m"}); err == nil {
		t.Fatal("a loopback endpoint must be rejected by default")
	}
	if _, err := decision.NewClient(decision.Config{Kind: decision.KindTypeSafe, Endpoint: server.URL, Model: "m", AllowPrivate: true}); err != nil {
		t.Fatalf("an explicitly allowed private endpoint must be accepted: %v", err)
	}
}

func mustJSON(value string) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}
