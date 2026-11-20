package testkit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Frame is one server-sent event the fake upstream emits; Wait pauses before it is written.
type Frame struct {
	Event string
	Data  string
	Wait  time.Duration
}

// Upstream is a scripted upstream. Script decides, per request, the status and frames to send.
type Upstream struct {
	*httptest.Server
	mu       sync.Mutex
	requests []Received
	Script   func(request Received) Reply
}

type Received struct {
	Path   string
	Header http.Header
	Body   string
}

type Reply struct {
	Status int
	Frames []Frame
	// Body is written verbatim for non-stream replies (error bodies).
	Body string
	// CutAfter closes the connection after that many frames without finishing the stream.
	CutAfter int
}

// NewUpstream starts the server and registers cleanup. A nil Script answers with an empty 200 stream.
func NewUpstream(t *testing.T, script func(Received) Reply) *Upstream {
	t.Helper()
	upstream := &Upstream{Script: script}
	upstream.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, _ := io.ReadAll(request.Body)
		received := Received{Path: request.URL.Path, Header: request.Header.Clone(), Body: string(payload)}
		upstream.mu.Lock()
		upstream.requests = append(upstream.requests, received)
		upstream.mu.Unlock()
		reply := Reply{Status: 200}
		if upstream.Script != nil {
			reply = upstream.Script(received)
		}
		if reply.Status == 0 {
			reply.Status = 200
		}
		if reply.Frames == nil {
			writer.WriteHeader(reply.Status)
			_, _ = writer.Write([]byte(reply.Body))
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(reply.Status)
		flusher, _ := writer.(http.Flusher)
		for index, frame := range reply.Frames {
			if reply.CutAfter > 0 && index >= reply.CutAfter {
				return
			}
			time.Sleep(frame.Wait)
			if frame.Event != "" {
				_, _ = writer.Write([]byte("event: " + frame.Event + "\n"))
			}
			_, _ = writer.Write([]byte("data: " + frame.Data + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func (upstream *Upstream) Requests() []Received {
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	return append([]Received(nil), upstream.requests...)
}

// ChatText scripts an OpenAI Chat Completions stream that answers with the given text pieces.
func ChatText(pieces ...string) []Frame {
	frames := make([]Frame, 0, len(pieces)+2)
	for _, piece := range pieces {
		frames = append(frames, Frame{Data: `{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":` + quote(piece) + `}}]}`})
	}
	frames = append(frames,
		Frame{Data: `{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`},
		Frame{Data: "[DONE]"},
	)
	return frames
}

// ChatToolCall scripts a Chat stream that issues one function call whose arguments arrive in pieces.
func ChatToolCall(id, name string, argumentPieces ...string) []Frame {
	frames := []Frame{{Data: `{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":` + quote(id) + `,"type":"function","function":{"name":` + quote(name) + `,"arguments":""}}]}}]}`}}
	for _, piece := range argumentPieces {
		frames = append(frames, Frame{Data: `{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":` + quote(piece) + `}}]}}]}`})
	}
	frames = append(frames,
		Frame{Data: `{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`},
		Frame{Data: "[DONE]"},
	)
	return frames
}

func quote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// Between returns the text between the first prefix and the next suffix after it, or "".
func Between(value, prefix, suffix string) string {
	start := strings.Index(value, prefix)
	if start < 0 {
		return ""
	}
	start += len(prefix)
	end := strings.Index(value[start:], suffix)
	if end < 0 {
		return ""
	}
	return value[start : start+end]
}
