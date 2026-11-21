package model

// StreamError is an in-band protocol error frame reported by a decoder or a stream contract
// violation.
type StreamError struct {
	Code    string
	Type    string
	Message string
	Status  int
}

func (value *StreamError) Error() string { return value.Message }

// ErrTruncated is returned when a stream stopped before its terminal frame and the assembled
// response is not complete enough to synthesize one.
var ErrTruncated = &StreamError{Code: "stream_truncated", Message: "The stream ended before its terminal frame."}
