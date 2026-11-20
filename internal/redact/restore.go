package redact

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Restorer rewrites issued placeholders back to their original values in a single pass, so text that
// an original value happens to contain is never replaced a second time. It is the only restoration
// path: Scope.Restore is a one-shot use of the same state machine.
//
// In JSON mode placeholders are only recognised inside string literals and the restored value is
// escaped for that context. Truncated JSON is passed through untouched.
type Restorer struct {
	scope    *Scope
	json     bool
	pending  []byte
	inString bool
	escaped  bool
}

func (scope *Scope) TextRestorer() *Restorer {
	return &Restorer{scope: scope}
}

func (scope *Scope) JSONRestorer() *Restorer {
	return &Restorer{scope: scope, json: true}
}

func (scope *Scope) Restore(text string) string {
	restorer := scope.TextRestorer()
	return restorer.Push(text) + restorer.Flush()
}

// Push returns the restorable prefix of the stream so far. A trailing fragment that may still grow
// into a placeholder is held back until the next Push or Flush.
func (restorer *Restorer) Push(fragment string) string {
	data := append(restorer.pending, fragment...)
	restorer.pending = nil
	var output strings.Builder
	for index := 0; index < len(data); {
		character := data[index]
		if restorer.json && restorer.advanceJSON(character) {
			output.WriteByte(character)
			index++
			continue
		}
		if character != '[' {
			output.WriteByte(character)
			index++
			continue
		}
		window := data[index:min(len(data), index+maximumPlaceholderBytes)]
		if end := bytes.IndexByte(window, ']'); end >= 0 {
			if entry, issued := restorer.scope.byToken[string(window[:end+1])]; issued {
				output.WriteString(restorer.encode(entry.value))
				index += end + 1
				continue
			}
		} else if len(data)-index < maximumPlaceholderBytes && placeholderPrefix(data[index:]) {
			restorer.pending = bytes.Clone(data[index:])
			break
		}
		output.WriteByte('[')
		index++
	}
	return output.String()
}

func (restorer *Restorer) Flush() string {
	pending := string(restorer.pending)
	restorer.pending = nil
	return pending
}

// advanceJSON tracks string state and reports whether the byte is JSON structure or string syntax
// that must be copied verbatim rather than scanned for a placeholder.
func (restorer *Restorer) advanceJSON(character byte) bool {
	switch {
	case !restorer.inString:
		restorer.inString = character == '"'
		restorer.escaped = false
		return true
	case restorer.escaped:
		restorer.escaped = false
		return true
	case character == '\\':
		restorer.escaped = true
		return true
	case character == '"':
		restorer.inString = false
		return true
	}
	return false
}

func (restorer *Restorer) encode(value string) string {
	if !restorer.json {
		return value
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return ""
	}
	literal := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
	return string(literal[1 : len(literal)-1])
}

func placeholderPrefix(data []byte) bool {
	if len(data) > 1 && (data[1] < 'A' || data[1] > 'Z') {
		return false
	}
	for _, character := range data[1:] {
		if !(character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
