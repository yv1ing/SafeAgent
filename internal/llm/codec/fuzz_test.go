package codec_test

import (
	"io"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

var fuzzFormats = []model.Format{model.Chat, model.Responses, model.Messages, model.Gemini}

var fuzzSeeds = []string{
	`{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
	`{"model":"m","input":"hi"}`,
	`{"model":"m","max_tokens":8,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"c","content":[]}]}]}`,
	`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
	`{"stream":true,"messages":[]}`,
	`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{"}}]}}]}`,
	`{"type":"response.output_item.added","item":{"type":"function_call","arguments":"{"}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","input":null}}`,
	`{"candidates":[{"index":0,"content":{"parts":[{"functionCall":{"args":null}}]}}]}`,
}

func FuzzDecodeRequest(f *testing.F) {
	for _, kind := range fuzzFormats {
		for _, seed := range fuzzSeeds {
			f.Add(string(kind), seed)
		}
	}
	f.Fuzz(func(t *testing.T, name, body string) {
		kind := model.Format(name)
		if !kind.Valid() {
			return
		}
		coder, found := codecs.Get(kind)
		if !found {
			return
		}
		client, _ := peers(kind)
		_, _, _ = coder.DecodeRequest([]byte(body), nil, client)
	})
}

func FuzzStreamDecoder(f *testing.F) {
	for _, kind := range fuzzFormats {
		for _, seed := range fuzzSeeds {
			f.Add(string(kind), seed)
		}
	}
	f.Fuzz(func(t *testing.T, name, payload string) {
		kind := model.Format(name)
		if !kind.Valid() {
			return
		}
		coder, found := codecs.Get(kind)
		if !found {
			return
		}
		_, upstream := peers(kind)
		decoder := coder.NewStreamDecoder(upstream)
		_, _, _ = decoder.Feed(codec.Frame{Data: []byte(payload)})
		_, _, _ = decoder.End()
	})
}

func FuzzSSEReader(f *testing.F) {
	for _, seed := range fuzzSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, payload string) {
		reader, err := codec.NewReader(&stringReader{value: payload}, 1<<20)
		if err != nil {
			return
		}
		for range 8 {
			if _, err := reader.Next(); err != nil {
				return
			}
		}
	})
}

type stringReader struct {
	value string
	index int
}

func (reader *stringReader) Read(buffer []byte) (int, error) {
	if reader.index >= len(reader.value) {
		return 0, io.EOF
	}
	count := copy(buffer, reader.value[reader.index:])
	reader.index += count
	return count, nil
}
