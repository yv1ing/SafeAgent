package auditevent

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

const (
	chainAlgorithm = "HMAC-SHA256"
	chainVersion   = 1
)

var chainKeyLabel = []byte("SafeAgent/audit-chain/key/v1")

const (
	StatusVerified    = "verified"
	StatusInProgress  = "in_progress"
	StatusIncomplete  = "incomplete"
	StatusBroken      = "broken"
	StatusUnavailable = "unavailable"
)

type ChainEvent struct {
	CallID        string          `json:"call_id"`
	Sequence      int64           `json:"sequence"`
	ProjectID     string          `json:"project_id"`
	Kind          string          `json:"kind"`
	SchemaVersion int             `json:"schema_version"`
	Payload       json.RawMessage `json:"payload"`
	CreatedAt     time.Time       `json:"created_at"`
	PrevHash      string          `json:"prev_hash"`
	Hash          string          `json:"hash"`
}

type ChainReport struct {
	Status   string `json:"status"`
	Events   int64  `json:"events"`
	Head     string `json:"head"`
	BrokenAt int64  `json:"broken_at,omitempty"`
	Error    string `json:"error,omitempty"`
}

func AuditKey(secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(chainKeyLabel)
	return mac.Sum(nil)
}

// SanitizePayload replaces the NUL escape PostgreSQL jsonb rejects. It runs before signing so the
// stored bytes and the verified bytes are identical.
func SanitizePayload(payload []byte) []byte {
	const nul = `\u0000`
	result := payload
	copied := false
	for index := 0; index+len(nul) <= len(payload); index++ {
		if payload[index] != '\\' || string(payload[index:index+len(nul)]) != nul {
			continue
		}
		backslashes := 0
		for before := index - 1; before >= 0 && payload[before] == '\\'; before-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			continue
		}
		if !copied {
			result = append([]byte(nil), payload...)
			copied = true
		}
		copy(result[index:], `\ufffd`)
	}
	return result
}

func Canonicalize(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("the JSON value has trailing data")
		}
		return nil, err
	}
	normalized, err := normalize(value)
	if err != nil {
		return nil, err
	}
	return marshalCanonical(normalized)
}

func normalize(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		normalized, err := normalizeNumber(typed.String())
		if err != nil {
			return nil, err
		}
		return json.Number(normalized), nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, entry := range typed {
			normalized, err := normalize(entry)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, entry := range typed {
			normalized, err := normalize(entry)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
}

func normalizeNumber(raw string) (string, error) {
	if !strings.ContainsAny(raw, ".eE") {
		integer := new(big.Int)
		if _, ok := integer.SetString(raw, 10); ok {
			return integer.String(), nil
		}
		return raw, nil
	}
	number, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return "", fmt.Errorf("invalid JSON number %q", raw)
	}
	if number == 0 {
		return "0", nil
	}
	return strconv.FormatFloat(number, 'g', -1, 64), nil
}

func marshalCanonical(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func Sign(event *ChainEvent, secret []byte) error {
	payload, err := Canonicalize(SanitizePayload(event.Payload))
	if err != nil {
		return err
	}
	event.Payload = payload
	event.CreatedAt = event.CreatedAt.UTC().Truncate(time.Microsecond)
	material, err := marshalCanonical(chainMaterial{
		Algorithm:       chainAlgorithm,
		Version:         chainVersion,
		PrevHash:        event.PrevHash,
		CallID:          event.CallID,
		Sequence:        event.Sequence,
		ProjectID:       event.ProjectID,
		Kind:            event.Kind,
		SchemaVersion:   event.SchemaVersion,
		CreatedAtMicros: event.CreatedAt.UnixMicro(),
		Payload:         event.Payload,
	})
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, AuditKey(secret))
	mac.Write(material)
	event.Hash = hex.EncodeToString(mac.Sum(nil))
	return nil
}

type chainMaterial struct {
	Algorithm       string          `json:"alg"`
	Version         int             `json:"v"`
	PrevHash        string          `json:"prev_hash"`
	CallID          string          `json:"call_id"`
	Sequence        int64           `json:"sequence"`
	ProjectID       string          `json:"project_id"`
	Kind            string          `json:"kind"`
	SchemaVersion   int             `json:"schema_version"`
	CreatedAtMicros int64           `json:"created_at_micros"`
	Payload         json.RawMessage `json:"payload"`
}

// VerifyChain replays one call's chain. It reports where the chain breaks instead of returning an
// error so a caller can show a partial audit record.
func VerifyChain(events []ChainEvent, secret []byte) ChainReport {
	report := ChainReport{Status: StatusVerified}
	previous := ""
	for index := range events {
		event := events[index]
		sequence := int64(index + 1)
		if event.SchemaVersion != SchemaVersion {
			return brokenReport(event.Sequence, "unsupported_schema_version")
		}
		if event.Sequence != sequence {
			return brokenReport(event.Sequence, "sequence_gap")
		}
		if event.PrevHash != previous {
			return brokenReport(event.Sequence, "previous_hash_mismatch")
		}
		expected := event
		expected.PrevHash = previous
		expected.Hash = ""
		if err := Sign(&expected, secret); err != nil {
			return brokenReport(event.Sequence, "invalid_payload")
		}
		if !hmac.Equal([]byte(expected.Hash), []byte(event.Hash)) {
			return brokenReport(event.Sequence, "hash_mismatch")
		}
		previous = event.Hash
		report.Events++
	}
	report.Head = previous
	return report
}

func brokenReport(sequence int64, reason string) ChainReport {
	return ChainReport{Status: StatusBroken, BrokenAt: sequence, Error: reason}
}
