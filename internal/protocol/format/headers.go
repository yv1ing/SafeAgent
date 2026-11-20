package format

import (
	"net/http"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

// protocolHeaders are the client headers a target protocol can consume. Everything else protocol
// specific is dropped and reported; credentials, cookies and transport headers are never candidates.
var protocolHeaders = map[protocol.Format][]string{
	protocol.Chat:      {"openai-beta", "openai-organization", "openai-project"},
	protocol.Responses: {"openai-beta", "openai-organization", "openai-project"},
	protocol.Messages:  {"anthropic-version", "anthropic-beta"},
	protocol.Gemini:    {"x-goog-api-client"},
}

var reservedHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "cookie": true, "set-cookie": true,
	"host": true, "content-length": true, "transfer-encoding": true, "connection": true,
	"keep-alive": true, "proxy-connection": true, "te": true, "trailer": true, "upgrade": true,
	"x-api-key": true, "x-goog-api-key": true, "forwarded": true, "via": true, "user-agent": true,
	"accept": true, "accept-encoding": true, "content-type": true,
}

// ForwardHeaders copies the client headers the target can understand. extra lists additional names
// configured on the channel. A protocol header the target cannot consume is reported.
func ForwardHeaders(source http.Header, target protocol.Format, extra []string) (http.Header, []protocol.Loss) {
	forwarded := http.Header{}
	if source == nil {
		return forwarded, nil
	}
	allowed := map[string]bool{}
	for _, name := range protocolHeaders[target] {
		allowed[name] = true
	}
	for _, name := range extra {
		allowed[strings.ToLower(strings.TrimSpace(name))] = true
	}
	sourceAllowed := map[string]bool{}
	for format := range protocolHeaders {
		for _, name := range protocolHeaders[format] {
			sourceAllowed[name] = true
		}
	}
	var losses []protocol.Loss
	for name, values := range source {
		lower := strings.ToLower(name)
		if reservedHeaders[lower] || len(values) == 0 {
			continue
		}
		if allowed[lower] {
			if lower == "anthropic-beta" {
				forwarded.Set(name, strings.Join(values, ","))
			} else {
				forwarded.Set(name, values[0])
			}
			continue
		}
		if sourceAllowed[lower] {
			losses = append(losses, protocol.Loss{
				Path: "header." + lower, Code: protocol.LossFieldDropped, Class: protocol.LossDegraded,
				Detail: "the target protocol does not accept this client header",
			})
		}
	}
	return forwarded, losses
}
