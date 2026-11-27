package trajectory

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
	chainKeyLabel  = "SafeAgent/trajectory-chain/key/v1"
)

var ErrInvalidPayload = errors.New("trajectory: invalid event payload")

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

func Marshal(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}

func SanitizePayload(payload []byte) []byte {
	const nul = `\u0000`
	const replacement = `\ufffd`
	result := payload
	copied := false
	for index := 0; index+len(nul) <= len(payload); index++ {
		if string(payload[index:index+len(nul)]) != nul {
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
		copy(result[index:], replacement)
	}
	return result
}

func chainKey(secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(chainKeyLabel))
	return mac.Sum(nil)
}

func Sign(event *Event, secret []byte) error {
	payload, err := Canonicalize(SanitizePayload(event.Payload))
	if err != nil {
		return fmt.Errorf("trajectory: event %s: %w", event.ID, err)
	}
	event.Payload = payload
	event.At = event.At.UTC().Truncate(time.Microsecond)
	event.Inputs = nonNilRefs(event.Inputs)
	event.Outputs = nonNilRefs(event.Outputs)
	event.Checks = nonNilChecks(event.Checks)
	event.Obligations = nonNilObligations(event.Obligations)
	material, err := marshalCanonical(hashMaterial{
		Algorithm: chainAlgorithm, Version: chainVersion,
		ID: event.ID, Trajectory: event.TrajectoryID, ProjectID: event.ProjectID,
		Seq: event.Seq, Kind: event.Kind, Boundary: event.Boundary, Actor: event.Actor,
		Principal: event.Principal, CallID: event.CallID, ActionID: event.ActionID,
		AtMicros: event.At.UnixMicro(), Inputs: event.Inputs, Outputs: event.Outputs,
		Auth: event.Auth, Checks: event.Checks, Obligations: event.Obligations,
		Snapshot: event.Snapshot, Capture: event.Capture, Payload: event.Payload,
		PrevHash: event.PrevHash,
	})
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, chainKey(secret))
	mac.Write(material)
	event.Hash = hex.EncodeToString(mac.Sum(nil))
	return nil
}

type hashMaterial struct {
	Algorithm   string          `json:"alg"`
	Version     int             `json:"v"`
	ID          string          `json:"id"`
	Trajectory  string          `json:"trajectory_id"`
	ProjectID   string          `json:"project_id"`
	Seq         int64           `json:"seq"`
	Kind        Kind            `json:"kind"`
	Boundary    Boundary        `json:"boundary"`
	Actor       Actor           `json:"actor"`
	Principal   string          `json:"principal"`
	CallID      string          `json:"call_id,omitempty"`
	ActionID    string          `json:"action_id,omitempty"`
	AtMicros    int64           `json:"occurred_at_micros"`
	Inputs      []Ref           `json:"inputs"`
	Outputs     []Ref           `json:"outputs"`
	Auth        *Auth           `json:"auth,omitempty"`
	Checks      []Check         `json:"checks"`
	Obligations []Obligation    `json:"obligations"`
	Snapshot    *Snapshot       `json:"snapshot,omitempty"`
	Capture     Capture         `json:"capture"`
	Payload     json.RawMessage `json:"payload"`
	PrevHash    string          `json:"prev_hash"`
}

func VerifyChain(events []Event, secret []byte) ChainReport {
	report := ChainReport{Status: StatusVerified}
	previous := ""
	for index := range events {
		event := events[index]
		sequence := int64(index + 1)
		if event.Seq != sequence {
			return brokenReport(event.Seq, "sequence_gap")
		}
		if event.PrevHash != previous {
			return brokenReport(event.Seq, "previous_hash_mismatch")
		}
		expected := event
		expected.Hash = ""
		if err := Sign(&expected, secret); err != nil {
			return brokenReport(event.Seq, "invalid_payload")
		}
		if !hmac.Equal([]byte(expected.Hash), []byte(event.Hash)) {
			return brokenReport(event.Seq, "hash_mismatch")
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

func nonNilRefs(values []Ref) []Ref {
	if values == nil {
		return []Ref{}
	}
	return values
}

func nonNilChecks(values []Check) []Check {
	if values == nil {
		return []Check{}
	}
	return values
}

func nonNilObligations(values []Obligation) []Obligation {
	if values == nil {
		return []Obligation{}
	}
	return values
}
