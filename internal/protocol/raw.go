package protocol

import "encoding/json"

// Raw keeps the wire fields a codec does not model at one node. It is replayed only towards the
// format that produced it; every other target records the dropped keys in the ledger. Because
// Fields contains only unmodelled keys it can never overwrite a modelled value.
type Raw struct {
	Format Format                     `json:"format,omitempty"`
	Fields map[string]json.RawMessage `json:"fields,omitempty"`
}

func (raw *Raw) Clone() *Raw {
	if raw == nil {
		return nil
	}
	clone := &Raw{Format: raw.Format}
	if raw.Fields != nil {
		clone.Fields = make(map[string]json.RawMessage, len(raw.Fields))
		for key, value := range raw.Fields {
			clone.Fields[key] = append(json.RawMessage(nil), value...)
		}
	}
	return clone
}

func (raw *Raw) IsEmpty() bool { return raw == nil || len(raw.Fields) == 0 }

func (raw *Raw) For(format Format) map[string]json.RawMessage {
	if raw == nil || raw.Format != format {
		return nil
	}
	return raw.Fields
}
