// Package codec implements codecs for the four supported wire protocols and the shared helpers
// used to convert between them: sampling parameters, tools, media, reasoning, raw overlays and SSE.
package codec

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

type Side string

const (
	Client   Side = "client"
	Upstream Side = "upstream"
)

type Envelope interface {
	Seal(signature model.Signature) (string, error)
	Open(encoded string) (model.Signature, bool)
}

// Peer describes the party on the other end of a codec call. Salt is optional per-call entropy for
// identifiers a codec has to invent; an empty Salt keeps them deterministic.
type Peer struct {
	Side         Side
	Vendor       model.Vendor
	Model        string
	Profile      model.Profile
	Envelope     Envelope
	Salt         string
	IncludeUsage bool
}

// WireSignature returns the signature as it must appear on the wire: sealed towards clients, the
// raw provider blob towards upstreams. Towards clients the envelope records the digest of the text
// the client is shown, so a later echo can be checked against what it was issued for.
func (peer Peer) WireSignature(signature *model.Signature, shownText string) string {
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
		sealed.TextDigest = model.TextDigest(shownText)
		sealed.Text = shownText
	}
	encoded, err := peer.Envelope.Seal(sealed)
	if err != nil {
		return ""
	}
	return encoded
}

// WireToolSignature seals a tool-call signature towards a client, bound to the call name and
// arguments that the signature was issued for. The bound values are not stored in the envelope.
func (peer Peer) WireToolSignature(signature *model.Signature, name, arguments string) string {
	if signature == nil || signature.Blob == "" || peer.Side == Upstream {
		return peer.WireSignature(signature, "")
	}
	if peer.Envelope == nil {
		return ""
	}
	sealed := *signature
	sealed.TextDigest = model.TextDigest(toolSignatureBinding(name, arguments))
	sealed.Text = ""
	encoded, err := peer.Envelope.Seal(sealed)
	if err != nil {
		return ""
	}
	return encoded
}

func toolSignatureBinding(name, arguments string) string {
	return name + "\x00" + arguments
}

type StreamError = model.StreamError

var ErrTruncated = model.ErrTruncated

// Continuable is implemented by encoders that can resume a stream after some items were already
// forwarded verbatim. priorItems and priorParts seed the output order and wire indexes.
type Continuable interface {
	Resume(priorItems, priorParts int)
}

type StreamDecoder interface {
	Feed(frame Frame) ([]model.Event, []model.Loss, error)
	End() ([]model.Event, []model.Loss, error)
}

type StreamEncoder interface {
	Encode(event model.Event) ([]Frame, []model.Loss, error)
	Finish() ([]Frame, []model.Loss, error)
	Fail(failure *model.Error) ([]Frame, []model.Loss, error)
}

type Codec interface {
	Format() model.Format
	BaseCapability() model.Capability
	DecodeRequest(body []byte, header http.Header, peer Peer) (*model.Request, []model.Loss, error)
	EncodeRequest(request *model.Request, peer Peer) ([]byte, http.Header, []model.Loss, error)
	DecodeResponse(body []byte, peer Peer) (*model.Response, []model.Loss, error)
	EncodeResponse(response *model.Response, peer Peer) ([]byte, []model.Loss, error)
	NewStreamDecoder(peer Peer) StreamDecoder
	NewStreamEncoder(meta model.ResponseMeta, peer Peer) StreamEncoder
	EncodeError(failure *model.Error, streaming bool) (int, []byte)
}

type Registry struct {
	codecs map[model.Format]Codec
}

func NewRegistry(codecs ...Codec) Registry {
	registry := Registry{codecs: make(map[model.Format]Codec, len(codecs))}
	for _, codec := range codecs {
		registry.codecs[codec.Format()] = codec
	}
	return registry
}

func (registry Registry) Get(kind model.Format) (Codec, bool) {
	codec, found := registry.codecs[kind]
	return codec, found
}

// CallIDs names tool calls that arrive without an identifier. With an empty Peer.Salt the name is a
// pure function of the call, so a replayed history matches the original response; a per-call salt
// keeps identical calls of different turns apart. Repeats within one stream get a suffix.
type CallIDs struct {
	salt string
	used map[string]int
}

func NewCallIDs(peer Peer) *CallIDs { return &CallIDs{salt: peer.Salt, used: map[string]int{}} }

// AssignCallIDs names calls a provider left anonymous. Results are paired by name and order in
// Gemini histories, so a deterministic identifier keeps the pairing stable.
func AssignCallIDs(items []model.Item, ids *CallIDs) {
	for index := range items {
		call := items[index].ToolCall
		if call == nil || call.ID != "" {
			continue
		}
		call.ID = ids.Assign(call.Name, call.Arguments)
	}
}

// PairAnonymousResults assigns a missing tool result call id to the oldest unanswered call, which
// is how clients that omit both ids expect their results to line up.
func PairAnonymousResults(items []model.Item) {
	var pending []string
	for index := range items {
		item := &items[index]
		switch {
		case item.Kind == model.ItemToolCall && item.ToolCall != nil:
			pending = append(pending, item.ToolCall.ID)
		case item.Kind == model.ItemToolResult:
			if item.CallID == "" && len(pending) > 0 {
				item.CallID = pending[0]
				pending = pending[1:]
				continue
			}
			for position, id := range pending {
				if id == item.CallID {
					pending = append(pending[:position], pending[position+1:]...)
					break
				}
			}
		}
	}
}

func (ids *CallIDs) Assign(name, arguments string) string {
	identifier := model.CallID("", ids.salt, name, arguments)
	ids.used[identifier]++
	if count := ids.used[identifier]; count > 1 {
		identifier += "_" + strconv.Itoa(count)
	}
	return identifier
}

// RawFields applies a node's unmodelled fields to the encoded object. Same-format nodes replay
// them; a cross-format node loses them and says so. Fields never overwrite modelled values because
// a codec captures every key it recognizes.
func RawFields(object map[string]any, raw *model.Raw, target model.Format, path string) []model.Loss {
	if raw.IsEmpty() {
		return nil
	}
	if fields := raw.For(target); fields != nil {
		var losses []model.Loss
		for name, value := range fields {
			decoded, err := model.DecodeJSON(value)
			if err != nil {
				continue
			}
			if existing, present := object[name]; present {
				nested, nestedOK := existing.(map[string]any)
				extra, extraOK := decoded.(map[string]any)
				if nestedOK && extraOK {
					for child, childValue := range extra {
						if _, modelled := nested[child]; !modelled {
							nested[child] = childValue
						}
					}
				} else {
					losses = append(losses, model.Loss{Path: path + "." + name, Code: model.LossExtrasDropped, Class: model.LossDegraded, Detail: "unmodelled field conflicts with a modelled value"})
				}
				continue
			}
			object[name] = decoded
		}
		return losses
	}
	keys := make([]string, 0, len(raw.Fields))
	for name := range raw.Fields {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	losses := make([]model.Loss, 0, len(keys))
	for _, name := range keys {
		losses = append(losses, model.Loss{Path: path + "." + name, Code: model.LossExtrasDropped, Class: model.LossDegraded, Detail: "unmodelled field from another protocol", From: raw.Format, To: target})
	}
	return losses
}

// CaptureRaw records every field a codec does not model. A dotted name such as "function.name"
// marks one child of a nested object as modelled; unmodelled children are kept under the parent.
func CaptureRaw(kind model.Format, object map[string]any, modeled ...string) *model.Raw {
	if object == nil {
		return nil
	}
	exact := make(map[string]bool, len(modeled))
	nested := map[string]map[string]bool{}
	for _, name := range modeled {
		parent, child, dotted := strings.Cut(name, ".")
		if !dotted {
			exact[name] = true
			continue
		}
		if nested[parent] == nil {
			nested[parent] = map[string]bool{}
		}
		nested[parent][child] = true
	}
	fields := map[string]json.RawMessage{}
	for name, value := range object {
		if name == "" || exact[name] {
			continue
		}
		if children, partial := nested[name]; partial {
			remaining := map[string]any{}
			for childName, childValue := range model.Object(value) {
				if !children[childName] {
					remaining[childName] = childValue
				}
			}
			if len(remaining) == 0 {
				continue
			}
			value = remaining
		}
		fields[name] = model.Marshal(value)
	}
	if len(fields) == 0 {
		return nil
	}
	return &model.Raw{Format: kind, Fields: fields}
}
