package pipeline

import (
	"context"
	"net/http"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// Request is one client call, already read off the wire by the HTTP layer.
type Request struct {
	Kind        model.Format
	Header      http.Header
	QueryKey    string
	Body        []byte
	Model       string
	ForceStream bool
}

// Writer is the transport the pipeline streams into; the HTTP layer adapts a response writer to it.
type Writer interface {
	Header() http.Header
	WriteHeader(status int)
	Write(data []byte) (int, error)
	Flush()
}

// Serve runs one client call end to end and writes the protocol-shaped response or error. Every
// path writes something: an admitted call ends in settle, a refused one in writeError.
func (pipeline *Pipeline) Serve(ctx context.Context, input Request, writer Writer) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	state, refusal := pipeline.admit(ctx, input, writer)
	if refusal != nil {
		pipeline.refuse(state, input.Kind, writer, refusal)
		return
	}
	state.registerInflight(cancel)
	defer pipeline.inflight.remove(state.recorder.Call().ID)
	defer state.release()
	state.run()
}
