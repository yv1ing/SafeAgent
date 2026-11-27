package builtin

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/threat"
)

var (
	injectionPhrases = []string{
		"ignore previous instructions", "ignore all previous", "ignore the previous", "ignore prior instructions",
		"disregard previous", "disregard the above", "ignore above", "forget your instructions", "your new role",
		"reveal your instructions", "reveal your system prompt", "print your instructions", "do not tell the user",
		"don't tell the user", "忽略之前的指令", "忽略以上", "忽略上述", "忽略前面的", "忘记你的指令", "你的新角色",
		"不要告诉用户", "输出你的指令", "泄露你的指令", "输出你的系统提示词",
	}
	urlExpression     = regexp.MustCompile(`(?i)\b(?:https?|wss?|ftp)://[^\s"'<>\\)]+`)
	imageExpression   = regexp.MustCompile(`(?i)!\[[^\]]*\]\(\s*<?([^\s)>]+)|<img\b[^>]*\bsrc\s*=\s*["']?([^"'\s>]+)`)
	credentialPattern = regexp.MustCompile(`(?i)\b(?:sk|pk|rk)-[A-Za-z0-9_-]{16,}|\bAKIA[0-9A-Z]{16}\b|\bgh[pousr]_[A-Za-z0-9]{20,}\b|\bxox[baprs]-[A-Za-z0-9-]{10,}`)
	commandSeparators = regexp.MustCompile(`[\s"'|;&<>()=,]+`)
	carrierNetworks   = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10")}
	metadataAddresses = []netip.Addr{
		netip.MustParseAddr("169.254.169.254"), netip.MustParseAddr("100.100.100.200"),
		netip.MustParseAddr("168.63.129.16"), netip.MustParseAddr("fd00:ec2::254"),
	}
	restrictedSuffixes = []string{".localhost", ".internal", ".local", ".lan", ".localdomain"}
	pathKeys           = map[string]bool{
		"path": true, "file": true, "filepath": true, "filename": true, "targetfile": true, "notebookpath": true,
		"directory": true, "dir": true, "cwd": true, "folder": true, "sourcepath": true, "targetpath": true,
		"logpath": true, "configpath": true, "keypath": true, "source": true, "destination": true,
	}
	commandKeys    = map[string]bool{"command": true, "cmd": true, "commandline": true, "shell": true, "script": true, "argv": true}
	hostKeys       = map[string]bool{"url": true, "uri": true, "endpoint": true, "baseurl": true, "host": true, "hostname": true}
	credentialKeys = map[string]bool{
		"password": true, "passwd": true, "secret": true, "apikey": true, "accesskey": true, "secretkey": true,
		"token": true, "accesstoken": true, "privatekey": true, "credential": true, "credentials": true, "authorization": true,
	}
	sensitivePaths = []string{
		"/etc/shadow", "/etc/passwd", "/etc/sudoers", "/etc/ssh/", "/proc/self/environ", "/proc/1/environ",
		"/var/run/secrets/", "/run/secrets/", "/root/.ssh/", "/root/.aws/",
		".ssh/id_rsa", ".ssh/id_ed25519", ".ssh/id_ecdsa", ".aws/credentials", ".config/gcloud/", ".kube/config",
		".docker/config.json", ".git-credentials", ".netrc", "/windows/system32/config/",
	}
	sensitiveNames   = []string{".env", "id_rsa", "id_ed25519", "id_ecdsa", ".npmrc", ".pypirc", ".pgpass"}
	templateSuffixes = []string{".example", ".sample", ".template", ".dist"}
	commandPatterns  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:curl|wget)\b[^\n|]*\|\s*(?:sudo\s+)?(?:ba|z|da|k)?sh\b`),
		regexp.MustCompile(`(?i)\b(?:ba|z)?sh\s+<\(\s*(?:curl|wget)\b`),
		regexp.MustCompile(`(?i)/dev/tcp/`),
		regexp.MustCompile(`(?i)\bn(?:c|cat|etcat)\b[^\n]*\s-[a-z]*[ec]\s`),
		regexp.MustCompile(`(?i)\brm\s+(?:-[a-z-]+\s+)+(?:/|~/?|\$HOME/?)\*?(?:\s|$|[;&|])`),
		regexp.MustCompile(`(?i)\b(?:base64|xxd)\b[^\n|]*\s(?:-d|--decode|-r)\b[^\n|]*\|\s*(?:sudo\s+)?(?:ba|z|da|k)?sh\b`),
		regexp.MustCompile(`(?i)\bchmod\s+(?:-R\s+)?[0-7]*777\s+/(?:\s|$|\*|etc\b|usr\b|bin\b|var\b|home\b|root\b)`),
		regexp.MustCompile(`(?i)\bdd\b[^\n]*\bof=/dev/(?:[hsv]d|nvme|mmcblk|disk|sd)`),
		regexp.MustCompile(`(?i)\b(?:cat|less|more|head|tail|strings|base64|xxd)\b[^\n|;&]*(?:/etc/shadow|\.ssh/id_(?:rsa|ed25519|ecdsa)(?:\s|$|['"])|\.aws/credentials|\.env(?:\s|$|['"]))`),
		regexp.MustCompile(`(?i)\b(?:env|printenv)\b[^\n|]*\|\s*(?:curl|wget|nc|ncat)\b`),
	}
)

var injectionMatchers = compileMatchers(injectionPhrases)

func compileMatchers(phrases []string) []*regexp.Regexp {
	matchers := make([]*regexp.Regexp, 0, len(phrases))
	for _, phrase := range phrases {
		matchers = append(matchers, regexp.MustCompile("(?i)"+regexp.QuoteMeta(phrase)))
	}
	return matchers
}

func evaluateContent(ctx context.Context, trace *observation.Trace) []threat.Finding {
	findings := []threat.Finding{}
	for _, ref := range trace.ItemRefs() {
		if ctx.Err() != nil {
			break
		}
		item := ref.Item
		switch item.Kind {
		case observation.ItemText, observation.ItemRefusal:
			findings = append(findings, inspectText(ref, item, item.Text)...)
		case observation.ItemToolCall:
			if item.ToolCall == nil || item.ToolCall.Kind == "server" {
				continue
			}
			findings = append(findings, inspectToolCall(ref, item)...)
		}
	}
	for index, tool := range trace.Tools {
		if tool.Description == "" {
			continue
		}
		ref := observation.ItemRef{Turn: -1, Position: index}
		description := observation.NewValue(tool.Description, tool.DescriptionShown, tool.DescriptionRedactions, nil)
		item := observation.Item{Kind: observation.ItemText, Origin: observation.OriginToolDescription, Trust: observation.Untrusted, Text: description}
		findings = append(findings, inspectText(ref, item, description)...)
	}
	return findings
}

// inspectText runs the checks that make sense for a text's origin. Commands, paths, private
// addresses and credentials are not searched in ordinary prose: they are judged where they can act,
// in tool arguments and tool output.
func inspectText(ref observation.ItemRef, item observation.Item, value observation.Value) []threat.Finding {
	text := value.Raw
	if text == "" {
		return nil
	}
	findings := []threat.Finding{}
	prose := item.Origin != observation.OriginAssistant
	if prose {
		for _, matcher := range injectionMatchers {
			if match := matcher.FindStringIndex(text); match != nil {
				findings = append(findings, makeFinding("content", threat.CodePromptInjection, ref, item.Origin, observation.SinkNone,
					"message", "text", value, match[0], match[1], 0))
				break
			}
		}
	}
	if start, end, found := hiddenUnicode(text); found {
		findings = append(findings, makeFinding("content", threat.CodeHiddenInstructions, ref, item.Origin, observation.SinkNone,
			"message", "text", value, start, end, 0))
	}
	if prose {
		findings = append(findings, inspectLinks(ref, item.Origin, "text", text, value)...)
	}
	if item.Origin == observation.OriginToolResult {
		findings = append(findings, inspectActionText(ref, item.Origin, observation.SinkNone, text, value)...)
	}
	return findings
}

// inspectActionText applies action-oriented checks to tool output or a free-form custom tool's
// arguments. Ordinary prose is not passed here: commands and private addresses matter where they
// can act.
func inspectActionText(ref observation.ItemRef, origin observation.Origin, sink observation.Sink, text string, value observation.Value) []threat.Finding {
	findings := []threat.Finding{}
	for _, pattern := range commandPatterns {
		if match := pattern.FindStringIndex(text); match != nil {
			findings = append(findings, makeFinding("content", threat.CodeDangerousCommand, ref, origin, sink,
				"message", "text", value, match[0], match[1], 0))
			break
		}
	}
	for index, candidate := range urlExpression.FindAllStringIndex(text, 32) {
		raw := text[candidate[0]:candidate[1]]
		if _, restricted := restrictedURL(raw); restricted {
			findings = append(findings, makeFinding("content", threat.CodePrivateNetworkTarget, ref, origin, sink,
				"message", "text.url", value, candidate[0], candidate[1], index))
		}
	}
	for index, match := range credentialPattern.FindAllStringIndex(text, 8) {
		findings = append(findings, makeFinding("content", threat.CodeCredentialExposure, ref, origin, sink,
			"message", "text", value, match[0], match[1], index))
	}
	for _, word := range commandSeparators.Split(text, -1) {
		if _, found := sensitivePath(word); found {
			start := strings.Index(text, word)
			if start < 0 {
				start = 0
			}
			findings = append(findings, makeFinding("content", threat.CodeSensitivePath, ref, origin, sink,
				"message", "text", value, start, start+len(word), 0))
			break
		}
	}
	return findings
}

func inspectLinks(ref observation.ItemRef, origin observation.Origin, field, text string, value observation.Value) []threat.Finding {
	findings := []threat.Finding{}
	for index, candidate := range urlExpression.FindAllStringIndex(text, 32) {
		raw := text[candidate[0]:candidate[1]]
		if parsed, err := url.Parse(raw); err == nil && (parsed.RawQuery != "" || parsed.User != nil) {
			findings = append(findings, makeFinding("content", threat.CodeExfiltrationLink, ref, origin, observation.SinkNone,
				"message", field+".url", value, candidate[0], candidate[1], index))
		}
	}
	if match := imageExpression.FindStringSubmatchIndex(text); match != nil {
		start, end := -1, -1
		if match[2] >= 0 {
			start, end = match[2], match[3]
		} else if match[4] >= 0 {
			start, end = match[4], match[5]
		}
		if start >= 0 {
			if parsed, err := url.Parse(text[start:end]); err == nil && parsed.Host != "" &&
				(parsed.RawQuery != "" || parsed.User != nil || restrictedHost(parsed.Hostname())) {
				findings = append(findings, makeFinding("content", threat.CodeExfiltrationLink, ref, origin, observation.SinkNone,
					"message", field+".image", value, start, end, 0))
			}
		}
	}
	return findings
}

func inspectToolCall(ref observation.ItemRef, item observation.Item) []threat.Finding {
	call := item.ToolCall
	findings := []threat.Finding{}
	origin := item.Origin
	if call.Kind == "custom" {
		findings = append(findings, inspectActionText(ref, origin, call.Sink, call.Arguments.Raw, call.Arguments)...)
		findings = append(findings, inspectLinks(ref, origin, "tool_call.arguments", call.Arguments.Raw, call.Arguments)...)
		if hasSecretPlaceholder(call.Arguments) {
			findings = append(findings, makeFinding("content", threat.CodeCredentialExposure, ref, origin, call.Sink,
				"message", "tool_call.arguments", call.Arguments, 0, len(call.Arguments.Raw), 0))
		}
		return findings
	}
	if call.Input != nil {
		shown, shownErr := decodeValue(call.Arguments.Shown)
		hide := shownErr != nil && call.Arguments.Shown != call.Arguments.Raw
		if shownErr != nil {
			shown = nil
		}
		findings = append(findings, inspectArgumentValue(ref, item, call.Input, shown, hide, "", 0, "tool_call.arguments")...)
	}
	if hasSecretPlaceholder(call.Arguments) {
		findings = append(findings, makeFinding("content", threat.CodeCredentialExposure, ref, origin, call.Sink,
			"message", "tool_call.arguments", call.Arguments, 0, len(call.Arguments.Raw), 0))
	}
	return findings
}

func inspectArgumentValue(ref observation.ItemRef, item observation.Item, value, shown any, hide bool, key string, depth int, valuePath string) []threat.Finding {
	if depth > 12 {
		return nil
	}
	call := item.ToolCall
	findings := []threat.Finding{}
	switch typed := value.(type) {
	case map[string]any:
		shownMap, _ := shown.(map[string]any)
		for _, name := range sortedKeys(typed) {
			childShown, known := shownMap[name]
			findings = append(findings, inspectArgumentValue(ref, item, typed[name], childShown, hide || !known, name, depth+1, valuePath+"."+name)...)
		}
	case []any:
		shownList, _ := shown.([]any)
		if joined, ok := joinedCommand(typed); ok && commandKeys[normalizeKey(key)] {
			shownText := joined
			if joinedShown, shownOK := joinedCommand(shownList); shownOK {
				shownText = joinedShown
			} else if hide || shown != nil {
				shownText = "[redacted]"
			}
			return inspectArgumentText(ref, item, joined, shownText, valuePath)
		}
		for index, entry := range typed {
			var childShown any
			childHide := hide
			if index < len(shownList) {
				childShown = shownList[index]
			} else {
				childHide = true
			}
			findings = append(findings, inspectArgumentValue(ref, item, entry, childShown, childHide, key, depth+1, valuePath+"["+strconv.Itoa(index)+"]")...)
		}
	case string:
		shownText := typed
		if text, ok := shown.(string); ok {
			shownText = text
		} else if hide {
			shownText = "[redacted]"
		}
		argument := argumentValue(typed, shownText)
		normalized := normalizeKey(key)
		if credentialKeys[normalized] && typed != "" {
			findings = append(findings, makeFinding("content", threat.CodeCredentialExposure, ref, item.Origin, call.Sink,
				"message", valuePath, argument, 0, len(typed), 0))
		}
		if commandKeys[normalized] {
			if match := commandMatch(typed); match != nil {
				findings = append(findings, makeFinding("content", threat.CodeDangerousCommand, ref, item.Origin, call.Sink,
					"message", valuePath, argument, match[0], match[1], 0))
			}
		}
		if pathKeys[normalized] {
			if sensitive, found := sensitivePath(typed); found {
				start := indexFold(typed, sensitive)
				if start < 0 {
					start = 0
				}
				findings = append(findings, makeFinding("content", threat.CodeSensitivePath, ref, item.Origin, call.Sink,
					"message", valuePath, argument, start, start+len(sensitive), 0))
			}
		}
		if hostKeys[normalized] {
			if _, restricted := restrictedURL(typed); restricted {
				findings = append(findings, makeFinding("content", threat.CodePrivateNetworkTarget, ref, item.Origin, call.Sink,
					"message", valuePath, argument, 0, len(typed), 0))
			} else if !strings.Contains(typed, "://") && restrictedHost(typed) {
				findings = append(findings, makeFinding("content", threat.CodePrivateNetworkTarget, ref, item.Origin, call.Sink,
					"message", valuePath, argument, 0, len(typed), 0))
			}
		}
		findings = append(findings, inspectLinks(ref, item.Origin, valuePath, typed, argument)...)
	}
	return findings
}

// argumentValue keeps a leaf's redacted form alongside its raw form, so evidence never quotes a
// value that the call view hides. Offsets refer to the raw leaf; a redacted leaf is marked as one
// span because the nested view has no character-level map back to the parent.
func argumentValue(raw, shown string) observation.Value {
	if shown == "" {
		shown = raw
	}
	value := observation.Value{Raw: raw, Shown: shown}
	if shown != raw {
		value.Redactions = []observation.Span{{Start: 0, End: len(raw), Label: "redacted"}}
	}
	return value
}

func inspectArgumentText(ref observation.ItemRef, item observation.Item, text, shown, valuePath string) []threat.Finding {
	findings := []threat.Finding{}
	value := argumentValue(text, shown)
	if match := commandMatch(text); match != nil {
		findings = append(findings, makeFinding("content", threat.CodeDangerousCommand, ref, item.Origin, item.ToolCall.Sink,
			"message", valuePath, value, match[0], match[1], 0))
	}
	for _, word := range commandSeparators.Split(text, -1) {
		if sensitive, found := sensitivePath(word); found {
			start := indexFold(text, sensitive)
			if start < 0 {
				start = 0
			}
			findings = append(findings, makeFinding("content", threat.CodeSensitivePath, ref, item.Origin, item.ToolCall.Sink,
				"message", valuePath, value, start, start+len(sensitive), 0))
			break
		}
	}
	findings = append(findings, inspectLinks(ref, item.Origin, valuePath, text, value)...)
	return findings
}

func commandMatch(text string) []int {
	for _, pattern := range commandPatterns {
		if match := pattern.FindStringIndex(text); match != nil {
			return match
		}
	}
	return nil
}

func joinedCommand(items []any) (string, bool) {
	words := make([]string, len(items))
	for index, item := range items {
		word, ok := item.(string)
		if !ok {
			return "", false
		}
		words[index] = word
	}
	return strings.Join(words, " "), true
}

func normalizeKey(key string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(key))
}

func sensitivePath(value string) (string, bool) {
	expanded := strings.ReplaceAll(value, "\\", "/")
	if strings.HasPrefix(expanded, "~/") {
		expanded = expanded[1:]
	}
	lower := strings.ToLower(path.Clean("/" + strings.TrimPrefix(expanded, "/")))
	for _, pattern := range sensitivePaths {
		if strings.HasSuffix(pattern, "/") {
			if strings.Contains(lower+"/", pattern) {
				return pattern, true
			}
		} else if strings.HasSuffix(lower, pattern) && (strings.HasPrefix(pattern, "/") || lower[len(lower)-len(pattern)-1] == '/') {
			return pattern, true
		}
	}
	base := path.Base(lower)
	for _, name := range sensitiveNames {
		if base == name || (name == ".env" && strings.HasPrefix(base, ".env.") && !hasAnySuffix(base, templateSuffixes)) {
			return name, true
		}
	}
	return "", false
}

// indexFold finds a case-insensitive match without changing byte offsets: lower-casing the whole
// value can change its length, so evidence would point at the wrong slice.
func indexFold(value, pattern string) int {
	expression := regexp.MustCompile("(?i)" + regexp.QuoteMeta(pattern))
	if match := expression.FindStringIndex(value); match != nil {
		return match[0]
	}
	return -1
}

func hasAnySuffix(value string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func restrictedURL(raw string) (string, bool) {
	address, err := url.Parse(raw)
	if err != nil || address.Hostname() == "" {
		return "", false
	}
	if restrictedHost(address.Hostname()) {
		return address.Hostname(), true
	}
	return "", false
}

func restrictedHost(host string) bool {
	normalized := strings.ToLower(strings.TrimSuffix(host, "."))
	if withoutPort, _, err := net.SplitHostPort(normalized); err == nil {
		normalized = strings.Trim(withoutPort, "[]")
	}
	normalized = strings.Trim(normalized, "[]")
	if normalized == "localhost" {
		return true
	}
	for _, suffix := range restrictedSuffixes {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	address, ok := parseLooseAddress(normalized)
	if !ok {
		return false
	}
	if metadataAddress(address) || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return true
	}
	for _, network := range carrierNetworks {
		if network.Contains(address) {
			return true
		}
	}
	return false
}

func parseLooseAddress(raw string) (netip.Addr, bool) {
	if parsed, err := netip.ParseAddr(raw); err == nil {
		return parsed.Unmap(), true
	}
	if strings.HasPrefix(strings.ToLower(raw), "0x") {
		value, err := strconv.ParseUint(raw[2:], 16, 32)
		return ipv4FromNumber(value), err == nil
	}
	number, err := strconv.ParseUint(raw, 10, 32)
	return ipv4FromNumber(number), err == nil && len(raw) == 10
}

func ipv4FromNumber(number uint64) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(number >> 24), byte(number >> 16), byte(number >> 8), byte(number)})
}

func metadataAddress(address netip.Addr) bool {
	for _, candidate := range metadataAddresses {
		if candidate == address {
			return true
		}
	}
	return false
}

func hiddenUnicode(text string) (int, int, bool) {
	runes := []rune(text)
	offset := 0
	for index, character := range runes {
		hidden := false
		switch {
		case character >= 0xE0000 && character <= 0xE007F:
			hidden = !followsFlag(runes, index)
		case character == 0x200B || character == 0x2060 || (character == 0xFEFF && index > 0):
			hidden = true
		case (character == 0x200C || character == 0x200D) && index > 0 && index+1 < len(runes) && asciiWord(runes[index-1]) && asciiWord(runes[index+1]):
			hidden = true
		case character >= 0x202A && character <= 0x202E, character >= 0x2066 && character <= 0x2069:
			hidden = true
		}
		if hidden {
			return offset, offset + len(string(character)), true
		}
		offset += len(string(character))
	}
	return 0, 0, false
}

func followsFlag(runes []rune, index int) bool {
	for index > 0 && runes[index-1] >= 0xE0000 && runes[index-1] <= 0xE007F {
		index--
	}
	return index > 0 && runes[index-1] == 0x1F3F4
}

func asciiWord(character rune) bool {
	return character < unicode.MaxASCII && (unicode.IsLetter(character) || unicode.IsDigit(character))
}
