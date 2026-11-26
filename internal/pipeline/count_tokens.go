package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// CountTokens returns the input token count from an upstream counting endpoint when one is
// configured, and a local estimate otherwise.
func (pipeline *Pipeline) CountTokens(ctx context.Context, input Request) (int64, bool, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, input.Header, input.QueryKey)
	if denied != nil {
		return 0, false, denied
	}
	defer release()
	operation := classify.CountTokens
	coder, found := pipeline.codecs.Get(input.Kind)
	if !found {
		return 0, false, internal("unsupported_format", "The protocol is not supported.")
	}
	peer := codec.Peer{Side: codec.Client, Vendor: input.Kind.DefaultVendor(), Envelope: Envelope{Secret: pipeline.secret}}
	request, _, err := coder.DecodeRequest(input.Body, input.Header, peer)
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
	request.Stream = false
	original := model.CloneRequest(request)
	auxiliary, auditFailure := pipeline.beginAuxiliary(ctx, identity, operation, input, request.Model)
	if auditFailure != nil {
		return 0, false, auditFailure
	}
	result, err := auxiliary.scope.ApplyRequest(request)
	hits := relocateHits(original, 0, auxiliary.recorder.Call().ID, result)
	applyRedactionSummary(auxiliary.recorder.Call(), hits)
	params := *original
	params.Items, params.Tools = nil, nil
	auxiliary.recordRequest(original.Items, params, nil, hits)
	if err != nil {
		_ = pipeline.endAuxiliary(ctx, auxiliary, blocked("redaction_immutable", "Sensitive content appears in a position that cannot be redacted safely.", http.StatusUnprocessableEntity))
		return 0, false, blocked("redaction_immutable", "Sensitive content appears in a position that cannot be redacted safely.", http.StatusUnprocessableEntity)
	}
	if result.Blocked {
		refusal := blocked("sensitive_content_blocked", "The request matches a blocking redaction rule.", http.StatusForbidden)
		_ = pipeline.endAuxiliary(ctx, auxiliary, refusal)
		return 0, false, refusal
	}
	plan, failure := pipeline.routeFor(ctx, identity.Project.ID, request.Model, classify.LaneAuxiliary, input.Header, input.Kind, entity.ModelGeneration)
	if plan.resolution.Route.ID != "" {
		pipeline.recordRouting(auxiliary.recorder, plan, request.Model)
	}
	if failure != nil {
		return 0, false, pipeline.endAuxiliary(ctx, auxiliary, failure)
	}
	for _, candidate := range pipeline.selector.Plan(plan.targets, plan.available.keys, "", string(input.Kind)) {
		endpoint, found := plan.endpoints[endpointKey(candidate.Target.ChannelID, candidate.Target.ModelID, model.Format(candidate.Target.Format))]
		if !found || endpoint.endpoint.CountTokensPath == "" || !endpoint.endpoint.Capability.NativeCount || len(candidate.Keys) == 0 {
			continue
		}
		recordAuxiliaryTarget(auxiliary.recorder, candidate)
		if counted, ok := pipeline.forwardCount(ctx, request, endpoint, candidate); ok {
			_ = pipeline.endAuxiliary(ctx, auxiliary, nil)
			return counted, false, nil
		}
	}
	if failure := pipeline.endAuxiliary(ctx, auxiliary, nil); failure != nil {
		return 0, false, failure
	}
	return int64(len(model.Marshal(request)) / 4), true, nil
}

// forwardCount asks one candidate's native counting endpoint for the input token count.
func (pipeline *Pipeline) forwardCount(ctx context.Context, request *model.Request, endpoint endpoint, candidate routing.Candidate) (int64, bool) {
	peer := codec.Peer{Side: codec.Upstream, Vendor: endpoint.format.DefaultVendor(), Model: candidate.Target.UpstreamModel, Profile: model.Profile{Capability: endpoint.endpoint.Capability, Compat: endpoint.endpoint.Compat}, IncludeUsage: false}
	body, header, _, err := coderOf(pipeline, endpoint.format).EncodeRequest(model.CloneRequest(request), peer)
	if err != nil {
		return 0, false
	}
	key := candidate.Keys[0]
	url := endpoint.endpoint.CountTokensURL(endpoint.endpoint.BaseURL, candidate.Target.UpstreamModel)
	url = endpoint.endpoint.ApplyQuery(url, key.Secret)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, false
	}
	credentials := map[string]string{}
	endpoint.endpoint.ApplyAuth(credentials, key.Secret)
	for name, value := range credentials {
		httpRequest.Header.Set(name, value)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	applyHeaders(httpRequest, endpoint.channel, header)
	response, err := pipeline.clients.get(endpoint.channel).Do(httpRequest)
	if err != nil {
		return 0, false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, false
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, pipeline.settings.MaxResponseBytes+1))
	if err != nil || int64(len(payload)) > pipeline.settings.MaxResponseBytes {
		return 0, false
	}
	count, err := parseCount(payload)
	return count, err == nil
}

func parseCount(payload []byte) (int64, error) {
	root, err := model.DecodeObject(payload)
	if err != nil {
		return 0, err
	}
	for _, key := range []string{"input_tokens", "totalTokens", "total_tokens", "count", "prompt_tokens"} {
		if number, ok := model.Integer(root[key]); ok {
			return number, nil
		}
	}
	return 0, errors.New("count endpoint returned no token count")
}
