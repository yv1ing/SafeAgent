package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/platform/netguard"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// upstreamCall is one open attempt: the decoded stream or body of a single key on a single
// candidate.
type upstreamCall struct {
	endpoint endpoint
	reader   *codec.Reader
	decoder  codec.StreamDecoder
	response *model.Response
	losses   []model.Loss
	body     io.Closer
}

func (call *upstreamCall) close() {
	if call.body != nil {
		_ = call.body.Close()
	}
}

func (state *call) openUpstream(ctx context.Context, prepared *prepared, key routing.Key) (*upstreamCall, *model.Error, string) {
	endpoint := prepared.endpoint
	endpointURL := endpoint.endpoint.URL(endpoint.endpoint.BaseURL, prepared.upstreamModel, prepared.stream)
	endpointURL = endpoint.endpoint.ApplyQuery(endpointURL, key.Secret)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(prepared.body))
	if err != nil {
		return nil, internal("invalid_endpoint", "The channel endpoint is invalid."), ""
	}
	headers := map[string]string{}
	endpoint.endpoint.ApplyAuth(headers, key.Secret)
	for name, value := range headers {
		httpRequest.Header.Set(name, value)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if prepared.stream {
		httpRequest.Header.Set("Accept", "text/event-stream")
	} else {
		httpRequest.Header.Set("Accept", "application/json")
	}
	applyHeaders(httpRequest, endpoint.channel, prepared.header)
	response, err := state.pipeline.clients.get(endpoint.channel).Do(httpRequest)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, timedOut(), ""
		}
		if ctx.Err() != nil {
			return nil, cancelled(), ""
		}
		return nil, transient("upstream_unreachable", "The upstream could not be reached."), ""
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		failure, field := state.pipeline.upstreamFailure(response, endpoint.channel, key)
		return nil, failure, field
	}
	call := &upstreamCall{endpoint: endpoint}
	if prepared.stream || strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		stream := newIdleReader(response.Body, state.pipeline.settings.StreamIdle)
		reader, err := codec.NewReader(stream, state.pipeline.settings.MaxStreamBytes)
		if err != nil {
			stream.Close()
			return nil, transient("upstream_stream_failed", "The upstream stream could not be read."), ""
		}
		call.reader, call.decoder, call.body = reader, prepared.coder.NewStreamDecoder(prepared.peer), stream
		return call, nil, ""
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, state.pipeline.settings.MaxResponseBytes+1))
	response.Body.Close()
	if err != nil {
		return nil, transient("upstream_response_failed", "The upstream response could not be read."), ""
	}
	if int64(len(payload)) > state.pipeline.settings.MaxResponseBytes {
		return nil, failure("upstream_failure", model.ClassTransient, "response_too_large", "The upstream response exceeded the size limit.", http.StatusBadGateway), ""
	}
	if endpoint.endpoint.Compat.HTTP200FailedStatus {
		if failedStatus(payload) {
			return nil, failure("upstream_failure", model.ClassTransient, "upstream_failed_status", "The upstream reported a failed response.", http.StatusBadGateway), ""
		}
	}
	decoded, losses, err := prepared.coder.DecodeResponse(payload, prepared.peer)
	if err != nil {
		return nil, upstreamDecodeFailure(err), ""
	}
	call.response = decoded
	call.losses = append(call.losses, losses...)
	return call, nil, ""
}

func failedStatus(payload []byte) bool {
	root, err := model.DecodeObject(payload)
	if err != nil {
		return false
	}
	return model.String(root["status"]) == "failed"
}

func upstreamDecodeFailure(err error) *model.Error {
	var streamFailure *codec.StreamError
	if errors.As(err, &streamFailure) {
		status := streamFailure.Status
		if status < 400 || status >= 600 {
			status = http.StatusBadGateway
		}
		return failure("upstream_failure", model.ClassTransient, firstNonEmpty(streamFailure.Code, "upstream_rejected"), firstNonEmpty(streamFailure.Message, "The upstream rejected the response."), status)
	}
	var failure *model.Error
	if errors.As(err, &failure) {
		return failure
	}
	return transient("invalid_upstream_response", "The upstream response could not be decoded.")
}

func patchRawBody(body []byte, kind model.Format, upstreamModel string, stream, includeUsage bool) ([]byte, error) {
	if kind == model.Gemini {
		return body, nil
	}
	root, err := applyUpstreamModel(body, upstreamModel)
	if err != nil {
		return nil, err
	}
	root["stream"] = stream
	if kind == model.Chat {
		if stream && includeUsage {
			options := model.Object(root["stream_options"])
			if options == nil {
				options = map[string]any{}
				root["stream_options"] = options
			}
			options["include_usage"] = true
		} else if !stream {
			delete(root, "stream_options")
		}
	}
	return model.Marshal(root), nil
}

// applyUpstreamModel decodes a JSON request body and names the route's upstream model.
func applyUpstreamModel(body []byte, upstreamModel string) (map[string]any, error) {
	root, err := model.DecodeObject(body)
	if err != nil {
		return nil, err
	}
	root["model"] = upstreamModel
	return root, nil
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
	if strings.HasPrefix(strings.ToLower(name), "x-safeagent-") {
		return true
	}
	switch http.CanonicalHeaderKey(name) {
	case "Authorization", "X-Api-Key", "X-Goog-Api-Key", "Host", "Content-Length", "Transfer-Encoding", "Connection", "Cookie":
		return true
	}
	return false
}

func applyHeaders(request *http.Request, channel *entity.Channel, forwarded http.Header) {
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
// transport and the old idle connections are released. The cache is bounded: a channel that has
// not served a request while many others have is evicted and rebuilt on demand, so deleted
// channels cannot accumulate transports for the life of the process.
type upstreamClients struct {
	mu      sync.Mutex
	clients map[string]*http.Client
	current map[string]string
	used    map[string]uint64
	clock   uint64
}

const maximumUpstreamClients = 256

func newUpstreamClients() *upstreamClients {
	return &upstreamClients{
		clients: map[string]*http.Client{},
		current: map[string]string{},
		used:    map[string]uint64{},
	}
}

func (clients *upstreamClients) get(channel *entity.Channel) *http.Client {
	clients.mu.Lock()
	defer clients.mu.Unlock()
	clients.clock++
	clients.used[channel.ID] = clients.clock
	key := channel.ID + ":" + strconv.FormatInt(channel.Version, 10)
	if client, found := clients.clients[key]; found {
		return client
	}
	clients.drop(channel.ID)
	if len(clients.current) >= maximumUpstreamClients {
		clients.evict()
	}
	client := newUpstreamClient(channel)
	clients.clients[key] = client
	clients.current[channel.ID] = key
	return client
}

// drop releases the transport of a channel that is about to receive a new version.
func (clients *upstreamClients) drop(channelID string) {
	key, found := clients.current[channelID]
	if !found {
		return
	}
	if client := clients.clients[key]; client != nil {
		client.CloseIdleConnections()
	}
	delete(clients.clients, key)
	delete(clients.current, channelID)
}

// evict releases the channel that has gone longest without a request.
func (clients *upstreamClients) evict() {
	oldest := ""
	for channelID := range clients.current {
		if oldest == "" || clients.used[channelID] < clients.used[oldest] {
			oldest = channelID
		}
	}
	if oldest == "" {
		return
	}
	clients.drop(oldest)
	delete(clients.used, oldest)
}

func newUpstreamClient(channel *entity.Channel) *http.Client {
	firstByte := time.Duration(channel.FirstByteTimeoutSeconds) * time.Second
	if firstByte <= 0 {
		firstByte = 60 * time.Second
	}
	return &http.Client{
		Transport: netguard.NewTransport(netguard.DialOptions{
			AllowPrivate: channel.AllowPrivateNetwork,
			FirstByte:    firstByte,
		}),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
