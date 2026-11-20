// Package format implements codecs for the four supported wire protocols and the shared helpers
// used to convert between them: sampling parameters, tools, media, reasoning and SSE.
package format

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

type Side string

const (
	Client   Side = "client"
	Upstream Side = "upstream"
)

type Envelope interface {
	Seal(signature protocol.Signature) (string, error)
	Open(encoded string) (protocol.Signature, bool)
}

// Peer describes the party on the other end of a codec call. Salt is optional per-call entropy for
// identifiers a codec has to invent (Gemini tool calls carry none); an empty Salt keeps them
// deterministic.
type Peer struct {
	Side     Side
	Vendor   protocol.Vendor
	Model    string
	Envelope Envelope
	Salt     string
}

// WireSignature returns the signature as it must appear on the wire: sealed towards clients, the
// raw provider blob towards upstreams. Towards clients the envelope records the digest of the text
// the client is shown, so a later echo can be checked against what it was issued for.
func (peer Peer) WireSignature(signature *protocol.Signature, shownText string) string {
	if signature == nil || signature.Blob == "" {
		return ""
	}
	if peer.Side != Client {
		if peer.Vendor != "" && signature.Vendor != "" && signature.Vendor != peer.Vendor {
			return ""
		}
		return signature.Blob
	}
	if peer.Envelope == nil {
		return ""
	}
	sealed := *signature
	if shownText != "" {
		sealed.TextDigest = protocol.TextDigest(shownText)
	}
	encoded, err := peer.Envelope.Seal(sealed)
	if err != nil {
		return ""
	}
	return encoded
}

// StreamError is an in-band protocol error frame.
type StreamError struct {
	Code    string
	Message string
}

func (value *StreamError) Error() string { return value.Message }

// ErrTruncated is returned by StreamDecoder.End when the stream stopped before its terminal frame.
var ErrTruncated = &StreamError{Code: "stream_truncated", Message: "The stream ended before its terminal frame."}

// StreamDecoder turns provider frames into canonical events. Blocks are strictly sequential: a block
// is stopped exactly once, before the next one starts. Losses found while decoding are attached to
// the EventMessageDelta. End fails with ErrTruncated when the provider's terminal frame never came.
type StreamDecoder interface {
	Feed(frame Frame) ([]protocol.Event, error)
	End() ([]protocol.Event, error)
}

// StreamEncoder turns canonical events into provider frames. Frames returned for an event belong to
// that event's block, and every block's frames are self-contained, so a caller may hold a block back
// and release it later as long as frames are written in the order they were returned.
type StreamEncoder interface {
	Encode(event protocol.Event) ([]Frame, []protocol.Loss, error)
	Finish() ([]Frame, []protocol.Loss, error)
}

type Codec interface {
	Format() protocol.Format
	DecodeRequest(body []byte, header http.Header, peer Peer) (*protocol.Request, error)
	EncodeRequest(request *protocol.Request, peer Peer) ([]byte, http.Header, []protocol.Loss, error)
	DecodeResponse(body []byte, peer Peer) (*protocol.Response, []protocol.Loss, error)
	EncodeResponse(response *protocol.Response, peer Peer) ([]byte, []protocol.Loss, error)
	NewStreamDecoder(peer Peer) StreamDecoder
	NewStreamEncoder(meta protocol.ResponseMeta, peer Peer) StreamEncoder
	EncodeError(failure *protocol.Error, streaming bool) (int, []byte)
}

type Registry struct {
	codecs map[protocol.Format]Codec
}

func NewRegistry(codecs ...Codec) Registry {
	registry := Registry{codecs: make(map[protocol.Format]Codec, len(codecs))}
	for _, codec := range codecs {
		registry.codecs[codec.Format()] = codec
	}
	return registry
}

func (registry Registry) Get(kind protocol.Format) (Codec, bool) {
	codec, found := registry.codecs[kind]
	return codec, found
}

// BlockStream numbers the blocks of one decoded stream and guarantees the event contract: blocks
// never overlap, each one is stopped exactly once, and streamed content travels only in deltas.
type BlockStream struct {
	blocks   []*protocol.Block
	builders []*strings.Builder
	stopped  []bool
	open     int
}

func NewBlockStream() *BlockStream { return &BlockStream{open: -1} }

// Start stops the open block, if any, and opens a new one. Content already present in block is
// emitted as a delta right after the start event.
func (stream *BlockStream) Start(block protocol.Block) (int, []protocol.Event) {
	events := stream.stopOpen()
	index := len(stream.blocks)
	stored := protocol.WithStreamedContent(block, "")
	stream.blocks = append(stream.blocks, &stored)
	stream.builders = append(stream.builders, &strings.Builder{})
	stream.stopped = append(stream.stopped, false)
	stream.open = index
	started := protocol.CloneBlock(stored)
	events = append(events, protocol.Event{Kind: protocol.EventBlockStart, Index: index, Block: &started})
	return index, append(events, stream.emit(index, protocol.StreamedContent(block))...)
}

// Delta appends text to a block's streamed content.
func (stream *BlockStream) Delta(index int, text string) ([]protocol.Event, error) {
	if stream.Block(index) == nil || stream.stopped[index] {
		return nil, &StreamError{Code: "block_closed", Message: "The stream contains a delta for a block that is not open."}
	}
	return stream.emit(index, text), nil
}

func (stream *BlockStream) emit(index int, text string) []protocol.Event {
	if text == "" {
		return nil
	}
	stream.builders[index].WriteString(text)
	return []protocol.Event{{Kind: protocol.EventBlockDelta, Index: index, Text: text}}
}

// Streamed returns the content accumulated so far without materializing the final block snapshot.
func (stream *BlockStream) Streamed(index int) string {
	if index < 0 || index >= len(stream.builders) {
		return ""
	}
	return stream.builders[index].String()
}

// Stop is idempotent; the second stop of a block produces nothing. When a final snapshot merged
// through Update extends the streamed content, the missing suffix is emitted as a delta first; a
// snapshot that contradicts what was already streamed loses to the streamed content.
func (stream *BlockStream) Stop(index int) []protocol.Event {
	block := stream.Block(index)
	if block == nil || stream.stopped[index] {
		return nil
	}
	var events []protocol.Event
	final, streamed := protocol.StreamedContent(*block), stream.builders[index].String()
	if final != "" && strings.HasPrefix(final, streamed) {
		events = stream.emit(index, final[len(streamed):])
	} else {
		*block = protocol.WithStreamedContent(*block, streamed)
	}
	stream.stopped[index] = true
	if stream.open == index {
		stream.open = -1
	}
	if block.Reasoning != nil && block.Reasoning.Signature != nil {
		block.Reasoning.Signature.TextDigest = protocol.TextDigest(block.Text)
	}
	snapshot := protocol.CloneBlock(*block)
	return append(events, protocol.Event{Kind: protocol.EventBlockStop, Index: index, Block: &snapshot})
}

func (stream *BlockStream) stopOpen() []protocol.Event {
	if stream.open < 0 {
		return nil
	}
	return stream.Stop(stream.open)
}

// StopAll stops every block that is still open, in order.
func (stream *BlockStream) StopAll() []protocol.Event {
	var events []protocol.Event
	for index := range stream.blocks {
		events = append(events, stream.Stop(index)...)
	}
	return events
}

// Update lets a decoder amend a block's live state, for example to merge the final snapshot a
// provider sends when it closes an item.
func (stream *BlockStream) Update(index int, change func(*protocol.Block)) {
	if block := stream.Block(index); block != nil && !stream.stopped[index] {
		change(block)
	}
}

// Block returns the live state of a block so a decoder can attach signatures and citations.
func (stream *BlockStream) Block(index int) *protocol.Block {
	if index < 0 || index >= len(stream.blocks) {
		return nil
	}
	return stream.blocks[index]
}

// Open returns the index of the block that has started and not yet stopped.
func (stream *BlockStream) Open() (int, bool) { return stream.open, stream.open >= 0 }

func (stream *BlockStream) HasToolCall() bool {
	for _, block := range stream.blocks {
		if block.Kind == protocol.KindToolCall && block.ToolCall != nil && block.ToolCall.Kind != protocol.ToolServer {
			return true
		}
	}
	return false
}

// CallIDs names Gemini function calls, which carry no identifier. With an empty Peer.Salt the name
// is a pure function of the call, so a replayed history matches the original response; a per-call
// salt keeps identical calls of different turns apart. Repeats within one message get a suffix.
type CallIDs struct {
	salt string
	used map[string]int
}

func NewCallIDs(peer Peer) *CallIDs { return &CallIDs{salt: peer.Salt, used: map[string]int{}} }

func (ids *CallIDs) Assign(name, arguments string) string {
	material := name + "\x00" + arguments
	if ids.salt != "" {
		material = ids.salt + "\x00" + material
	}
	sum := sha256.Sum256([]byte(material))
	identifier := "call_" + hex.EncodeToString(sum[:8])
	ids.used[identifier]++
	if count := ids.used[identifier]; count > 1 {
		identifier += "_" + strconv.Itoa(count)
	}
	return identifier
}

// CleanEOF reports whether a stream that ended without its terminal frame still carries a complete,
// usable response: at least one content block and every tool call with complete JSON arguments.
// A block whose arguments are still growing, or a stream that produced nothing, is a truncation.
func CleanEOF(stream *BlockStream) bool {
	content := false
	for index, block := range stream.blocks {
		switch block.Kind {
		case protocol.KindText, protocol.KindReasoning, protocol.KindRefusal:
			if block.Text != "" || stream.Streamed(index) != "" {
				content = true
			}
		case protocol.KindToolCall:
			if block.ToolCall == nil {
				continue
			}
			arguments := block.ToolCall.Arguments
			if arguments == "" {
				arguments = stream.Streamed(index)
			}
			if block.ToolCall.Kind != protocol.ToolCustom && arguments != "" && !json.Valid([]byte(arguments)) {
				return false
			}
			content = true
		}
	}
	return content
}
