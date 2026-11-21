package observe

import (
	"net"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/yv1ing/SafeAgent/internal/observation"
)

var urlArguments = []string{"url", "uri", "endpoint", "href", "link"}
var pathArguments = []string{"path", "file", "filepath", "filename", "target_path", "dir", "directory"}
var commandArguments = []string{"command", "cmd", "script", "shell", "exec"}
var hostArguments = []string{"host", "hostname", "domain", "address"}

var commandName = regexp.MustCompile(`^[^\s;&|]+`)

var writeWords = []string{"write", "save", "append", "create", "delete", "remove", "move", "edit", "patch", "upload", "put"}
var sendWords = []string{"send", "post", "upload", "webhook", "request", "fetch", "http", "mail", "message", "publish"}
var execWords = []string{"shell", "exec", "command", "bash", "terminal", "run"}
var credentialWords = []string{"secret", "credential", "token", "password", "apikey"}
var paymentWords = []string{"pay", "transfer", "purchase", "checkout", "refund", "withdraw", "invoice"}

// TargetOf normalizes a tool call's destination into a stable key. Queries are dropped: a random
// nonce must not create a new graph node, while the host still tells the analyst where data went.
func TargetOf(name string, input any) observation.Target {
	object, _ := input.(map[string]any)
	if object == nil {
		return observation.Target{Kind: "tool", Value: name}
	}
	if value := firstString(object, urlArguments); value != "" {
		if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
			host := strings.ToLower(parsed.Hostname())
			cleanPath := cleanURLPath(parsed.Path)
			return observation.Target{Kind: "url", Value: parsed.Scheme + "://" + strings.ToLower(parsed.Host) + (&url.URL{Path: cleanPath}).EscapedPath(), Scheme: strings.ToLower(parsed.Scheme), Host: host, Path: cleanPath}
		}
		return observation.Target{Kind: "url", Value: value}
	}
	if value := firstString(object, hostArguments); value != "" {
		return observation.Target{Kind: "host", Value: strings.ToLower(value), Host: targetHost(value)}
	}
	if value := firstString(object, commandArguments); value != "" {
		executable := commandName.FindString(value)
		return observation.Target{Kind: "command", Value: executable, Command: filepath.Base(executable)}
	}
	if value := firstString(object, pathArguments); value != "" {
		cleaned := filepath.Clean(value)
		return observation.Target{Kind: "file", Value: cleaned, Path: cleaned}
	}
	return observation.Target{Kind: "tool", Value: name}
}

func cleanURLPath(value string) string {
	if value == "" {
		return "/"
	}
	return path.Clean(value)
}

func targetHost(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if host, _, err := net.SplitHostPort(value); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(value, "[]")
}

func firstString(object map[string]any, names []string) string {
	for _, name := range names {
		value, _ := object[name].(string)
		if value != "" {
			return value
		}
	}
	return ""
}

// Capabilities infers coarse ABAC capabilities from the tool name and its normalized target. The
// inference is deliberately conservative; a policy that needs a stronger guarantee must not rely
// on it alone.
func Capabilities(name string, target observation.Target) []string {
	value := name
	capabilities := []string{}
	add := func(capability string) {
		for _, existing := range capabilities {
			if existing == capability {
				return
			}
		}
		capabilities = append(capabilities, capability)
	}
	switch target.Kind {
	case "url", "host":
		add("network")
	case "file":
		if containsAny(value, writeWords...) {
			add("file_write")
		} else {
			add("file_read")
		}
	case "command":
		add("shell_exec")
	}
	if target.Kind != "file" && target.Kind != "command" && containsAny(value, sendWords...) {
		add("external_send")
	}
	if containsAny(value, execWords...) {
		add("shell_exec")
	}
	if containsAny(value, credentialWords...) {
		add("credential_access")
	}
	if containsAny(value, paymentWords...) {
		add("payment")
	}
	return capabilities
}

// SinkOf names what a tool call can do with the values it receives. When the arguments exist but
// nothing identifies a destination, the answer is Unknown rather than an implied internal call.
func SinkOf(name string, target observation.Target, input any) observation.Sink {
	capabilities := Capabilities(name, target)
	has := func(capability string) bool {
		for _, candidate := range capabilities {
			if candidate == capability {
				return true
			}
		}
		return false
	}
	switch {
	case target.Kind == "url" || target.Kind == "host" || has("external_send"):
		return observation.SinkExternalSend
	case target.Kind == "command" || has("shell_exec"):
		return observation.SinkShellExec
	case has("credential_access"):
		return observation.SinkCredentialAccess
	case has("payment"):
		return observation.SinkPayment
	case target.Kind == "file" && has("file_write"):
		return observation.SinkFileWrite
	case target.Kind == "file":
		return observation.SinkFileRead
	}
	if object, ok := input.(map[string]any); ok && len(object) > 0 {
		return observation.SinkUnknown
	}
	return observation.SinkNone
}

// containsAny matches whole name tokens, not substrings: "truncate" must not be read as "run" and
// "postgres" must not be read as a "post" tool. Camel case is split so "sendEmail" still yields
// "send" and "email".
func containsAny(value string, candidates ...string) bool {
	for _, token := range nameTokens(value) {
		for _, candidate := range candidates {
			if token == candidate {
				return true
			}
		}
	}
	return false
}

func nameTokens(value string) []string {
	tokens := []string{}
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		tokens = append(tokens, strings.ToLower(string(current)))
		current = current[:0]
	}
	for _, character := range value {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			flush()
			continue
		}
		if unicode.IsUpper(character) && len(current) > 0 && (unicode.IsLower(current[len(current)-1]) || unicode.IsDigit(current[len(current)-1])) {
			flush()
		}
		current = append(current, character)
	}
	flush()
	return tokens
}

// TagsForTool derives ABAC tags from the tool name and description. Unknown is the default: a tool
// whose boundary cannot be inferred must not be presented to a policy as internal and trusted.
func TagsForTool(name, description string, target observation.Target) observation.ToolTags {
	tags := observation.ToolTags{Boundary: "unknown", Sensitivity: "unknown", Integrity: "unknown"}
	value := name + " " + description
	switch target.Kind {
	case "url", "host":
		tags.Boundary, tags.Integrity = "external", "unfiltered"
	case "file":
		tags.Boundary = "internal"
	case "command":
		tags.Boundary = "privileged"
	}
	if containsAny(value, "read", "file", "secret", "credential", "database", "query", "admin", "shell", "exec") {
		tags.Sensitivity = "high"
	}
	if containsAny(value, "delete", "write", "sudo", "admin") {
		tags.Boundary = "privileged"
	}
	tags.Tags = append(tags.Tags, Capabilities(name, target)...)
	return tags
}
