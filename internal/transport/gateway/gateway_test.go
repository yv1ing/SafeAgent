package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/pipeline"
	"github.com/yv1ing/SafeAgent/internal/testkit"
	api "github.com/yv1ing/SafeAgent/internal/transport/gateway"
)

func newRouter(t *testing.T, upstreamURL, format string, maxRequest int64) (*gin.Engine, *testkit.Memory) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := testkit.NewMemory(upstreamURL, format)
	settings := pipeline.Settings{MaxRequestBytes: maxRequest, MaxResponseBytes: 1 << 20, MaxStreamBytes: 1 << 20}
	pipeline := pipeline.New(store, []byte(testkit.InstanceSecret), pipeline.Options{Settings: settings, Providers: store.Registry})
	router := gin.New()
	api.NewHandler(pipeline, settings).Register(router)
	return router, store
}

func call(router http.Handler, method, path, key string, headers map[string]string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestEndpointsAnswerInTheClientsProtocol(t *testing.T) {
	upstream := testkit.NewUpstream(t, func(testkit.Received) testkit.Reply { return testkit.Reply{Frames: testkit.ChatText("hello")} })
	router, _ := newRouter(t, upstream.URL, "openai_chat", 1<<20)
	cases := []struct {
		name    string
		path    string
		headers map[string]string
		body    string
		want    string
	}{
		{"chat", "/v1/chat/completions", nil, `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`, `"content":"hello"`},
		{"responses", "/v1/responses", nil, `{"model":"gpt-5","input":"hi"}`, `"output_text"`},
		{"messages", "/v1/messages", map[string]string{"x-api-key": "agent-key"}, `{"model":"gpt-5","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, `"text":"hello"`},
		{"gemini", "/v1beta/models/gpt-5:generateContent", nil, `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, `"text":"hello"`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			key := "agent-key"
			if testCase.headers != nil {
				key = ""
			}
			response := call(router, http.MethodPost, testCase.path, key, testCase.headers, testCase.body)
			if response.Code != 200 || !strings.Contains(response.Body.String(), testCase.want) {
				t.Fatalf("%d %s", response.Code, response.Body)
			}
		})
	}
}

func TestOversizedBodiesAreRefused(t *testing.T) {
	router, _ := newRouter(t, "http://127.0.0.1:1", "openai_chat", 1024)
	response := call(router, http.MethodPost, "/v1/chat/completions", "agent-key", nil, `{"model":"gpt-5","messages":[{"role":"user","content":"`+strings.Repeat("a", 4096)+`"}]}`)
	if response.Code != 413 {
		t.Fatalf("expected 413, got %d", response.Code)
	}
}

func TestGeminiActionsAreValidated(t *testing.T) {
	router, _ := newRouter(t, "http://127.0.0.1:1", "openai_chat", 1<<20)
	for _, path := range []string{"/v1beta/models/gpt-5", "/v1beta/models/gpt-5:embedContent"} {
		if response := call(router, http.MethodPost, path, "agent-key", nil, `{}`); response.Code != 400 {
			t.Fatalf("%s: expected 400, got %d", path, response.Code)
		}
	}
}

func TestModelListsFollowTheClientProtocol(t *testing.T) {
	router, _ := newRouter(t, "http://127.0.0.1:1", "openai_chat", 1<<20)
	cases := []struct {
		path    string
		headers map[string]string
		field   string
	}{
		{"/v1/models", nil, "object"},
		{"/v1/models", map[string]string{"anthropic-version": "2023-06-01"}, "has_more"},
		{"/v1beta/models", nil, "models"},
	}
	for _, testCase := range cases {
		response := call(router, http.MethodGet, testCase.path, "agent-key", testCase.headers, "")
		var payload map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || response.Code != 200 {
			t.Fatalf("%s: %d %s", testCase.path, response.Code, response.Body)
		}
		if _, found := payload[testCase.field]; !found {
			t.Fatalf("%s lacks %q: %v", testCase.path, testCase.field, payload)
		}
	}
	if response := call(router, http.MethodGet, "/v1/models", "wrong", nil, ""); response.Code != 401 {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestModelRetrieveAndCountEndpoints(t *testing.T) {
	upstream := testkit.NewUpstream(t, func(testkit.Received) testkit.Reply {
		return testkit.Reply{Status: 200, Body: `{"input_tokens":12}`}
	})
	router, _ := newRouter(t, upstream.URL, "anthropic_messages", 1<<20)
	response := call(router, http.MethodGet, "/v1/models/gpt-5", "agent-key", nil, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"id":"gpt-5"`) {
		t.Fatalf("%d %s", response.Code, response.Body)
	}
	if missing := call(router, http.MethodGet, "/v1/models/other", "agent-key", nil, ""); missing.Code != 404 {
		t.Fatalf("an unknown model must be 404: %d", missing.Code)
	}
	count := call(router, http.MethodPost, "/v1/messages/count_tokens", "agent-key", nil, `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`)
	if count.Code != 200 || !strings.Contains(count.Body.String(), `"input_tokens":12`) {
		t.Fatalf("%d %s", count.Code, count.Body)
	}
}

func TestResponsesSocketCreatesAndStreams(t *testing.T) {
	upstream := testkit.NewUpstream(t, func(testkit.Received) testkit.Reply { return testkit.Reply{Frames: testkit.ChatText("socket answer")} })
	router, _ := newRouter(t, upstream.URL, "openai_chat", 1<<20)
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer agent-key"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.create","model":"gpt-5","input":"hi"}`)); err != nil {
		t.Fatal(err)
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("socket ended before completion: %v", err)
		}
		if strings.Contains(string(data), `"response.completed"`) {
			return
		}
	}
}

func TestGeminiArrayFraming(t *testing.T) {
	upstream := testkit.NewUpstream(t, func(testkit.Received) testkit.Reply { return testkit.Reply{Frames: testkit.ChatText("hello")} })
	router, _ := newRouter(t, upstream.URL, "openai_chat", 1<<20)
	response := call(router, http.MethodPost, "/v1beta/models/gpt-5:streamGenerateContent", "agent-key", nil, `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	body := strings.TrimSpace(response.Body.String())
	if response.Code != 200 || !strings.HasPrefix(body, "[") || !strings.HasSuffix(body, "]") {
		t.Fatalf("%d %s", response.Code, response.Body)
	}
}
