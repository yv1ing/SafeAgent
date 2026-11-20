package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// CountTokens returns the input token count from an upstream counting endpoint when one is
// configured, and a local estimate otherwise. It is rate limited like any other call, and the
// request is redacted first because the upstream sees it.
func (pipeline *Pipeline) CountTokens(ctx context.Context, input Request) (count int64, estimated bool, denied *protocol.Error) {
	identity, denied := pipeline.Authenticate(ctx, BearerToken(input.Header, input.QueryKey))
	if denied != nil {
		return 0, false, denied
	}
	release, err := pipeline.limiter.Acquire(
		limit.Scoped{Scope: "project:" + identity.Project.ID, Limit: limit.Limit{RequestsPerMinute: identity.Project.RequestsPerMinute, Concurrency: identity.Project.Concurrency}},
		limit.Scoped{Scope: "key:" + identity.AgentKey.ID, Limit: limit.Limit{RequestsPerMinute: identity.AgentKey.RequestsPerMinute, Concurrency: identity.AgentKey.Concurrency}},
	)
	if err != nil {
		return 0, false, failure("rate_limited", protocol.ClassRateLimit, "rate_limited", "The request rate or concurrency limit has been exceeded.", http.StatusTooManyRequests)
	}
	defer release()
	codec, found := pipeline.codecs.Get(input.Kind)
	if !found {
		return 0, false, internal("unsupported_format", "The protocol is not supported.")
	}
	peer := format.Peer{Side: format.Client, Vendor: input.Kind.DefaultVendor(), Envelope: Envelope{Secret: pipeline.secret}}
	request, _, err := codec.DecodeRequest(input.Body, input.Header, peer)
	if err != nil {
		return 0, false, invalid("invalid_request", "The request is invalid: "+sentence(err.Error()))
	}
	unsealRequest(peer, request)
	if input.Model != "" {
		request.Model = input.Model
	}
	if request.Model == "" {
		return 0, false, invalid("model_required", "The model field is required.")
	}
	engine, _, err := pipeline.redactionEngine(ctx, identity.Project)
	if err != nil {
		return 0, false, internal("rules_unavailable", "The redaction rules are unavailable.")
	}
	result, err := engine.NewScope().ApplyRequest(request)
	if err != nil {
		return 0, false, blocked("redaction_immutable", "Sensitive content appears in a position that cannot be redacted safely.", http.StatusUnprocessableEntity)
	}
	if result.Blocked {
		return 0, false, blocked("sensitive_content_blocked", "The request matches a blocking redaction rule.", http.StatusForbidden)
	}
	request.Stream = false
	targets, available, err := pipeline.resolve(ctx, identity.Project.ID, request.Model)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return 0, false, failure("invalid_request", protocol.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
		}
		return 0, false, internal("store_failure", "The model routes could not be loaded.")
	}
	if len(targets) == 0 {
		return 0, false, failure("invalid_request", protocol.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
	}
	for _, candidate := range pipeline.selector.Plan(targets, available.keys, "") {
		channel := available.byID[candidate.Target.ChannelID]
		if channel == nil || channel.CountTokensEndpoint == "" {
			continue
		}
		if counted, ok := pipeline.forwardCount(ctx, request, channel, candidate); ok {
			return counted, false, nil
		}
	}
	return int64(len(protocol.Marshal(request)) / 4), true, nil
}

func (pipeline *Pipeline) forwardCount(ctx context.Context, request *protocol.Request, channel *entity.Channel, candidate routing.Candidate) (int64, bool) {
	kind := protocol.Format(channel.Format)
	codec, found := pipeline.codecs.Get(kind)
	if !found || len(candidate.Keys) == 0 {
		return 0, false
	}
	peer := format.Peer{Side: format.Upstream, Vendor: protocol.Vendor(channel.Vendor), Model: candidate.Target.UpstreamModel}
	body, header, _, err := codec.EncodeRequest(protocol.CloneRequest(request), peer)
	if err != nil {
		return 0, false
	}
	endpoint := strings.ReplaceAll(channel.CountTokensEndpoint, "{model}", candidate.Target.UpstreamModel)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, false
	}
	applyHeaders(httpRequest, channel, header, false)
	httpRequest.Header.Set("Accept", "application/json")
	setAuth(httpRequest, kind, candidate.Keys[0].Secret)
	response, err := pipeline.clients.get(channel).Do(httpRequest)
	if err != nil {
		return 0, false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, false
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, pipeline.settings.MaxResponseBytes))
	if err != nil {
		return 0, false
	}
	count, err := parseCount(payload)
	return count, err == nil
}

func parseCount(payload []byte) (int64, error) {
	root, err := protocol.DecodeObject(payload)
	if err != nil {
		return 0, err
	}
	for _, key := range []string{"input_tokens", "totalTokens", "total_tokens", "count", "prompt_tokens"} {
		if value, ok := root[key].(json.Number); ok {
			if parsed, err := value.Int64(); err == nil {
				return parsed, nil
			}
		}
	}
	return 0, errors.New("count endpoint returned no token count")
}
