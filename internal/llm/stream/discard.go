package stream

import (
	"net/http"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// DiscardWriter is the sink for a collector that only needs the session's restoration, gating and
// journal state, not its encoded bytes.
type DiscardWriter struct {
	header http.Header
}

func NewDiscardWriter() *DiscardWriter { return &DiscardWriter{header: http.Header{}} }

func (writer *DiscardWriter) Header() http.Header { return writer.header }
func (writer *DiscardWriter) WriteHeader(int)     {}
func (writer *DiscardWriter) Write(data []byte) (int, error) {
	return len(data), nil
}
func (writer *DiscardWriter) Flush() {}

// NopEncoder satisfies codec.StreamEncoder for collectors.
type NopEncoder struct{}

func (NopEncoder) Encode(model.Event) ([]codec.Frame, []model.Loss, error) { return nil, nil, nil }
func (NopEncoder) Finish() ([]codec.Frame, []model.Loss, error)            { return nil, nil, nil }
func (NopEncoder) Fail(*model.Error) ([]codec.Frame, []model.Loss, error)  { return nil, nil, nil }
