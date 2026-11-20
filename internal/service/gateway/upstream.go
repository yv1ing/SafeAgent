package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// upstreamCall is one open attempt: the decoded stream of a single key on a single candidate.
type upstreamCall struct {
	channel   *entity.Channel
	candidate routing.Candidate
	key       routing.Key
	reader    *format.Reader
	decoder   format.StreamDecoder
	losses    []protocol.Loss
	repairs   []protocol.Repair
	body      io.Closer
}

func (call *upstreamCall) close() {
	if call.body != nil {
		_ = call.body.Close()
	}
}

func (pipeline *Pipeline) openUpstream(ctx context.Context, state *call, channel *entity.Channel, candidate routing.Candidate, key routing.Key) (*upstreamCall, *protocol.Error) {
	kind := protocol.Format(channel.Format)
	codec, found := pipeline.codecs.Get(kind)
	if !found {
		return nil, internal("unsupported_channel_format", "The channel format is not supported.")
	}
	adapted := protocol.CloneRequest(state.request)
	adapted.Source = state.kind
	adapted.Stream = true
	losses := protocol.AdaptReasoning(adapted, protocol.Vendor(channel.Vendor), kind)
	missingReasoning := protocol.MissingToolReasoning(adapted)
	if missingReasoning {
		adapted.Reasoning = &protocol.ReasoningConfig{Effort: "none"}
		losses = append(losses, protocol.Loss{Path: "reasoning", Code: protocol.LossReasoningNotReplayable, Class: protocol.LossDegraded, Detail: "the client history has a tool call without reasoning; target thinking was disabled", From: state.kind, To: kind})
	}
	repairs := protocol.RepairRequest(adapted, kind)
	if loss, applied := resolveMaxOutputTokens(adapted, kind, candidate.Target.MaxOutputTokens, channel, state.identity.Project); applied {
		losses = append(losses, loss)
	}
	capability := codec.Capability()
	capabilityLosses := protocol.ValidateRequestCapability(adapted, capability)
	peer := format.Peer{Side: format.Upstream, Vendor: protocol.Vendor(channel.Vendor), Model: candidate.Target.UpstreamModel, Salt: state.salt}
	header, headerLosses := format.ForwardHeaders(state.clientHeader, kind, channel.ForwardHeaders)
	raw := state.kind == kind && len(state.hits) == 0 && !state.historyExpanded && !state.unsealed && !missingReasoning && len(repairs) == 0 && rawSafeTarget(state.request, kind)
	state.failureLedger.Repairs = append(state.failureLedger.Repairs, repairs...)
	for _, loss := range capabilityLosses {
		if loss.Fatal() {
			state.failureLedger.Losses = append(state.failureLedger.Losses, losses...)
			state.failureLedger.Losses = append(state.failureLedger.Losses, capabilityLosses...)
			state.failureLedger.Losses = append(state.failureLedger.Losses, headerLosses...)
			return nil, failure("invalid_request", protocol.ClassInvalid, "conversion_failed", sentence("The request cannot be represented in the channel protocol: "+loss.Detail), http.StatusUnprocessableEntity)
		}
	}
	if !raw {
		losses = append(losses, capabilityLosses...)
	}
	losses = append(losses, headerLosses...)
	state.failureLedger.Losses = append(state.failureLedger.Losses, losses...)
	var body []byte
	if raw {
		var err error
		body, err = patchRawBody(state.body, kind, candidate.Target.UpstreamModel, true)
		if err != nil {
			return nil, internal("invalid_request_body", "The request body could not be prepared.")
		}
	} else {
		encoded, encodedHeader, encodeLosses, err := codec.EncodeRequest(adapted, peer)
		if err != nil {
			return nil, failure("invalid_request", protocol.ClassInvalid, "conversion_failed", sentence("The request cannot be represented in the channel protocol: "+err.Error()), http.StatusUnprocessableEntity)
		}
		body = encoded
		losses = append(losses, encodeLosses...)
		mergeHeaders(header, encodedHeader)
	}
	endpoint := upstreamEndpoint(channel, candidate.Target.UpstreamModel, true)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, internal("invalid_endpoint", "The channel endpoint is invalid.")
	}
	applyHeaders(httpRequest, channel, header, true)
	setAuth(httpRequest, kind, key.Secret)
	response, err := pipeline.clients.get(channel).Do(httpRequest)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, timedOut()
		}
		if ctx.Err() != nil {
			return nil, cancelled()
		}
		return nil, transient("upstream_unreachable", "The upstream could not be reached.")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		return nil, pipeline.upstreamFailure(response, channel, key)
	}
	call := &upstreamCall{channel: channel, candidate: candidate, key: key, losses: losses, repairs: repairs}
	stream := newIdleReader(response.Body, pipeline.settings.StreamIdle)
	reader, err := format.NewReader(stream, pipeline.settings.MaxStreamBytes)
	if err != nil {
		stream.Close()
		return nil, transient("upstream_stream_failed", "The upstream stream could not be read.")
	}
	call.reader, call.decoder, call.body = reader, codec.NewStreamDecoder(peer), stream
	return call, nil
}

func rawSafeTarget(request *protocol.Request, target protocol.Format) bool {
	if target == protocol.Messages && request.MaxOutputTokens == nil {
		return false
	}
	if target == protocol.Responses {
		for _, include := range request.Include {
			if include == "reasoning.encrypted_content" {
				return true
			}
		}
		return false
	}
	return true
}

func resolveMaxOutputTokens(request *protocol.Request, target protocol.Format, routeDefault int, channel *entity.Channel, project *entity.Project) (protocol.Loss, bool) {
	if target != protocol.Messages || request.MaxOutputTokens != nil {
		return protocol.Loss{}, false
	}
	value := int64(0)
	source := ""
	switch {
	case routeDefault > 0:
		value, source = int64(routeDefault), "route target default"
	case channel != nil && channel.DefaultMaxOutputTokens > 0:
		value, source = int64(channel.DefaultMaxOutputTokens), "channel default"
	case project != nil && project.DefaultMaxOutputTokens > 0:
		value, source = int64(project.DefaultMaxOutputTokens), "project default"
	default:
		value, source = 8192, "gateway fallback"
	}
	request.MaxOutputTokens = &value
	return protocol.Loss{Path: "max_output_tokens", Code: protocol.LossDefaultTokenLimit, Class: protocol.LossDegraded, Detail: "Messages requires a token limit; " + source + " was applied", To: target}, true
}

func upstreamDecodeFailure(err error) *protocol.Error {
	var streamFailure *format.StreamError
	if errors.As(err, &streamFailure) {
		status := streamFailure.Status
		if status < 400 || status >= 600 {
			status = http.StatusBadGateway
		}
		return failure("upstream_failure", protocol.ClassTransient, firstNonEmpty(streamFailure.Code, "upstream_rejected"), firstNonEmpty(streamFailure.Message, "The upstream rejected the response."), status)
	}
	var failure *protocol.Error
	if errors.As(err, &failure) {
		return failure
	}
	return transient("invalid_upstream_response", "The upstream response could not be decoded.")
}

func patchRawBody(body []byte, kind protocol.Format, model string, stream bool) ([]byte, error) {
	if kind == protocol.Gemini {
		return body, nil
	}
	root, err := protocol.DecodeObject(body)
	if err != nil {
		return nil, err
	}
	root["model"] = model
	root["stream"] = stream
	if kind == protocol.Chat {
		options := protocol.Object(root["stream_options"])
		if options == nil {
			options = map[string]any{}
			root["stream_options"] = options
		}
		options["include_usage"] = true
	}
	return protocol.Marshal(root), nil
}

func mergeHeaders(target, source http.Header) {
	for name, values := range source {
		for _, value := range values {
			if name == "Anthropic-Beta" && target.Get(name) != "" {
				target.Set(name, target.Get(name)+","+value)
				continue
			}
			target.Set(name, value)
		}
	}
}

func isReservedHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Authorization", "X-Api-Key", "X-Goog-Api-Key", "Host", "Content-Length", "Transfer-Encoding", "Connection", "Cookie":
		return true
	}
	return false
}

func applyHeaders(request *http.Request, channel *entity.Channel, forwarded http.Header, stream bool) {
	request.Header.Set("Content-Type", "application/json")
	if stream {
		request.Header.Set("Accept", "text/event-stream")
	} else {
		request.Header.Set("Accept", "application/json")
	}
	for name, value := range channel.Headers {
		if !isReservedHeader(name) {
			request.Header.Set(name, value)
		}
	}
	for name, values := range forwarded {
		for _, value := range values {
			if name == "Anthropic-Beta" && request.Header.Get(name) != "" {
				request.Header.Set(name, request.Header.Get(name)+","+value)
				continue
			}
			request.Header.Set(name, value)
		}
	}
}

func setAuth(request *http.Request, kind protocol.Format, secret string) {
	switch kind {
	case protocol.Messages:
		request.Header.Set("x-api-key", secret)
		if request.Header.Get("anthropic-version") == "" {
			request.Header.Set("anthropic-version", "2023-06-01")
		}
	case protocol.Gemini:
		request.Header.Set("x-goog-api-key", secret)
	default:
		request.Header.Set("Authorization", "Bearer "+secret)
	}
}

// upstreamEndpoint expands {model}; a Gemini endpoint without it is a base URL to which the model
// and the streaming or plain action are appended.
func upstreamEndpoint(channel *entity.Channel, upstreamModel string, stream bool) string {
	endpoint := channel.Endpoint
	if strings.Contains(endpoint, "{model}") {
		endpoint = strings.ReplaceAll(endpoint, "{model}", upstreamModel)
	} else if channel.Format == string(protocol.Gemini) {
		action := ":generateContent"
		if stream {
			action = ":streamGenerateContent"
		}
		endpoint = strings.TrimRight(endpoint, "/") + "/" + upstreamModel + action
	} else {
		return endpoint
	}
	if channel.Format == string(protocol.Gemini) && stream && !strings.Contains(endpoint, "alt=sse") {
		separator := "?"
		if strings.Contains(endpoint, "?") {
			separator = "&"
		}
		endpoint += separator + "alt=sse"
	}
	return endpoint
}

// idleReader enforces a per-read idle deadline with a single timer instead of a goroutine per read:
// the timer is re-armed before each read and closes the body when the upstream stalls.
type idleReader struct {
	body  io.ReadCloser
	idle  time.Duration
	timer *time.Timer
	mu    sync.Mutex
	stall bool
}

func newIdleReader(body io.ReadCloser, idle time.Duration) *idleReader {
	reader := &idleReader{body: body, idle: idle}
	if idle > 0 {
		reader.timer = time.AfterFunc(idle, reader.expire)
	}
	return reader
}

func (reader *idleReader) expire() {
	reader.mu.Lock()
	reader.stall = true
	reader.mu.Unlock()
	_ = reader.body.Close()
}

func (reader *idleReader) Read(buffer []byte) (int, error) {
	if reader.timer != nil {
		reader.timer.Reset(reader.idle)
	}
	count, err := reader.body.Read(buffer)
	if err != nil {
		reader.mu.Lock()
		stalled := reader.stall
		reader.mu.Unlock()
		if stalled {
			return count, errors.New("the upstream stream stalled")
		}
	}
	return count, err
}

func (reader *idleReader) Close() error {
	if reader.timer != nil {
		reader.timer.Stop()
	}
	return reader.body.Close()
}

// upstreamClients keeps one HTTP client per channel version, so editing a channel replaces its
// transport and the old idle connections are released.
type upstreamClients struct {
	mu      sync.Mutex
	clients map[string]*http.Client
	current map[string]string
}

func newUpstreamClients() *upstreamClients {
	return &upstreamClients{clients: map[string]*http.Client{}, current: map[string]string{}}
}

func (clients *upstreamClients) get(channel *entity.Channel) *http.Client {
	version := strconv.FormatInt(channel.Version, 10)
	clients.mu.Lock()
	defer clients.mu.Unlock()
	key := channel.ID + ":" + version
	if client, found := clients.clients[key]; found {
		return client
	}
	if previous, found := clients.current[channel.ID]; found {
		if old := clients.clients[previous]; old != nil {
			old.CloseIdleConnections()
		}
		delete(clients.clients, previous)
	}
	client := newUpstreamClient(channel)
	clients.clients[key] = client
	clients.current[channel.ID] = key
	return client
}

func newUpstreamClient(channel *entity.Channel) *http.Client {
	firstByte := time.Duration(channel.FirstByteTimeoutSeconds) * time.Second
	if firstByte <= 0 {
		firstByte = 60 * time.Second
	}
	allowPrivate := channel.AllowPrivateNetwork
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ResponseHeaderTimeout: firstByte,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(addresses) == 0 {
			return nil, errors.New("the upstream host has no addresses")
		}
		if !allowPrivate {
			for _, candidate := range addresses {
				if restrictedAddress(candidate.Unmap()) {
					return nil, errors.New("the upstream resolves to a restricted address")
				}
			}
		}
		var lastErr error
		for _, candidate := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.Unmap().String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// restrictedAddress rejects anything that is not a public unicast address. IPv4-compatible IPv6
// (::/96) is listed explicitly: netip does not decode it, but a dialer may still route it to the
// embedded IPv4 address.
func restrictedAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() {
		return true
	}
	for _, prefix := range restrictedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

var restrictedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}
