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
	body      io.Closer
}

func (call *upstreamCall) close() { _ = call.body.Close() }

func (pipeline *Pipeline) openUpstream(ctx context.Context, request *protocol.Request, salt string, channel *entity.Channel, candidate routing.Candidate, key routing.Key) (*upstreamCall, *protocol.Error) {
	kind := protocol.Format(channel.Format)
	codec, found := pipeline.codecs.Get(kind)
	if !found {
		return nil, internal("unsupported_channel_format", "The channel format is not supported.")
	}
	adapted := protocol.CloneRequest(request)
	losses := protocol.AdaptReasoning(adapted, protocol.Vendor(channel.Vendor))
	adapted.Stream = true
	peer := format.Peer{Side: format.Upstream, Vendor: protocol.Vendor(channel.Vendor), Model: candidate.Target.UpstreamModel, Salt: salt}
	body, header, encodeLosses, err := codec.EncodeRequest(adapted, peer)
	if err != nil {
		return nil, failure("invalid_request", protocol.ClassInvalid, "conversion_failed", sentence("The request cannot be represented in the channel protocol: "+err.Error()), http.StatusUnprocessableEntity)
	}
	losses = append(losses, encodeLosses...)
	endpoint := streamEndpoint(channel, candidate.Target.UpstreamModel)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, internal("invalid_endpoint", "The channel endpoint is invalid.")
	}
	applyHeaders(httpRequest, channel, header)
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
	stream := newIdleReader(response.Body, pipeline.settings.StreamIdle)
	reader, err := format.NewReader(stream, pipeline.settings.MaxStreamBytes)
	if err != nil {
		stream.Close()
		return nil, transient("upstream_stream_failed", "The upstream stream could not be read.")
	}
	return &upstreamCall{channel: channel, candidate: candidate, key: key, reader: reader, decoder: codec.NewStreamDecoder(peer), losses: losses, body: stream}, nil
}

func isReservedHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Authorization", "X-Api-Key", "X-Goog-Api-Key", "Host", "Content-Length", "Transfer-Encoding", "Connection":
		return true
	}
	return false
}

func applyHeaders(request *http.Request, channel *entity.Channel, encoded http.Header) {
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	for name, value := range channel.Headers {
		if !isReservedHeader(name) {
			request.Header.Set(name, value)
		}
	}
	for name, values := range encoded {
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

// streamEndpoint expands {model}; a Gemini endpoint without it is a base URL to which the model
// and the streaming action are appended.
func streamEndpoint(channel *entity.Channel, upstreamModel string) string {
	endpoint := channel.Endpoint
	if strings.Contains(endpoint, "{model}") {
		endpoint = strings.ReplaceAll(endpoint, "{model}", upstreamModel)
	} else if channel.Format == string(protocol.Gemini) {
		endpoint = strings.TrimRight(endpoint, "/") + "/" + upstreamModel + ":streamGenerateContent"
	} else {
		return endpoint
	}
	if channel.Format == string(protocol.Gemini) && !strings.Contains(endpoint, "alt=sse") {
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
