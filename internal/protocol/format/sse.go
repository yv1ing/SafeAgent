package format

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type Frame struct {
	Event   string
	Data    []byte
	Comment string
}

type Reader struct {
	scanner   *bufio.Scanner
	limited   *io.LimitedReader
	maxBytes  int64
	firstLine bool
	event     string
	payload   strings.Builder
}

func NewReader(source io.Reader, maxBytes int64) (*Reader, error) {
	if maxBytes < 1 || maxBytes > 128<<20 {
		return nil, errors.New("invalid stream size limit")
	}
	limited := &io.LimitedReader{R: source, N: maxBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), int(maxBytes)+1)
	scanner.Split(splitLine)
	return &Reader{scanner: scanner, limited: limited, maxBytes: maxBytes, firstLine: true}, nil
}

func (reader *Reader) Next() (Frame, error) {
	for reader.scanner.Scan() {
		line := reader.scanner.Text()
		if reader.firstLine {
			line = strings.TrimPrefix(line, "\ufeff")
			reader.firstLine = false
		}
		if reader.limited.N == 0 {
			return Frame{}, errors.New("stream size limit exceeded")
		}
		if line == "" {
			if frame, found := reader.flush(); found {
				return frame, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		if found {
			value = strings.TrimPrefix(value, " ")
		} else {
			name, value = line, ""
		}
		switch name {
		case "event":
			reader.event = value
		case "data":
			if int64(reader.payload.Len()+len(value)) > reader.maxBytes {
				return Frame{}, errors.New("SSE event exceeds size limit")
			}
			reader.payload.WriteString(value)
			reader.payload.WriteByte('\n')
		}
	}
	if err := reader.scanner.Err(); err != nil {
		return Frame{}, err
	}
	if reader.limited.N == 0 {
		return Frame{}, errors.New("stream size limit exceeded")
	}
	if frame, found := reader.flush(); found {
		return frame, nil
	}
	return Frame{}, io.EOF
}

func (reader *Reader) flush() (Frame, bool) {
	if reader.payload.Len() == 0 {
		reader.event = ""
		return Frame{}, false
	}
	data := strings.TrimSuffix(reader.payload.String(), "\n")
	event := reader.event
	reader.event = ""
	reader.payload.Reset()
	if data == "" {
		return Frame{}, false
	}
	return Frame{Event: event, Data: []byte(data)}, true
}

func splitLine(payload []byte, atEOF bool) (int, []byte, error) {
	if index := bytes.IndexAny(payload, "\r\n"); index >= 0 {
		if payload[index] == '\r' {
			if index+1 == len(payload) && !atEOF {
				return 0, nil, nil
			}
			if index+1 < len(payload) && payload[index+1] == '\n' {
				return index + 2, payload[:index], nil
			}
		}
		return index + 1, payload[:index], nil
	}
	if atEOF && len(payload) > 0 {
		return len(payload), payload, nil
	}
	return 0, nil, nil
}

func (frame Frame) Encode() ([]byte, error) {
	if frame.Comment != "" && len(frame.Data) == 0 {
		return []byte(": " + frame.Comment + "\n\n"), nil
	}
	return EncodeFrame(frame.Event, string(frame.Data))
}

func EncodeFrame(event string, value any) ([]byte, error) {
	var payload []byte
	if text, ok := value.(string); ok {
		payload = []byte(text)
	} else {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		payload = encoded
	}
	if event != "" {
		return []byte("event: " + event + "\ndata: " + string(payload) + "\n\n"), nil
	}
	return []byte("data: " + string(payload) + "\n\n"), nil
}
