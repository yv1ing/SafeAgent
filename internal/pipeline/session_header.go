package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const SessionHeader = "X-SafeAgent-Session"

// namespacedSession turns an optional client grouping value into a project-key scoped identifier.
// The value is never an identity: authentication remains the agent key.
func namespacedSession(agentKeyID, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 128 {
		return "", raw == ""
	}
	for _, character := range raw {
		if character < 0x20 || character == 0x7f {
			return "", false
		}
	}
	sum := sha256.Sum256([]byte("safeagent.session.v1\x00" + agentKeyID + "\x00" + raw))
	return "sess_" + hex.EncodeToString(sum[:16]), true
}
