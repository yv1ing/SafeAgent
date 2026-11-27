package threat

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func joinVersion(parts []string) string {
	if len(parts) == 0 {
		return "empty"
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:8])
}
