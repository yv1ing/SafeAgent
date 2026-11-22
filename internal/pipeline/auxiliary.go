package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/responses"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

func (pipeline *Pipeline) authorize(ctx context.Context, header http.Header, queryKey string) (*Identity, func(), *model.Error) {
	identity, denied := pipeline.Authenticate(ctx, BearerToken(header, queryKey))
	if denied != nil {
		return nil, nil, denied
	}
	release, err := pipeline.limiter.Acquire(
		limit.Scoped{Scope: "project:" + identity.Project.ID, Limit: limit.Limit{RequestsPerMinute: identity.Project.RequestsPerMinute, Concurrency: identity.Project.Concurrency}},
		limit.Scoped{Scope: "key:" + identity.AgentKey.ID, Limit: limit.Limit{RequestsPerMinute: identity.AgentKey.RequestsPerMinute, Concurrency: identity.AgentKey.Concurrency}},
	)
	if err != nil {
		return nil, nil, failure("rate_limited", model.ClassRateLimit, "rate_limited", "The request rate or concurrency limit has been exceeded.", http.StatusTooManyRequests)
	}
	return identity, release, nil
}

// CountTokens returns the input token count from an upstream counting endpoint when one is
// configured, and a local estimate otherwise.
func (pipeline *Pipeline) CountTokens(ctx context.Context, input Request) (int64, bool, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, input.Header, input.QueryKey)
	if denied != nil {
		return 0, false, denied
	}
	defer release()
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
	recorder, auditFailure := pipeline.beginAuxiliary(ctx, identity, "count_tokens", input, request.Items, request.Model, nil)
	if auditFailure != nil {
		return 0, false, auditFailure
	}
	engine, _, err := pipeline.redactionEngine(ctx, identity.Project)
	if err != nil {
		if failure := pipeline.endAuxiliary(ctx, recorder, "failed", "rules_unavailable"); failure != nil {
			return 0, false, failure
		}
		return 0, false, internal("rules_unavailable", "The redaction rules are unavailable.")
	}
	scope := engine.NewScope()
	result, err := scope.ApplyRequest(request)
	if err != nil {
		if failure := pipeline.endAuxiliary(ctx, recorder, "blocked", "redaction_immutable"); failure != nil {
			return 0, false, failure
		}
		return 0, false, blocked("redaction_immutable", "Sensitive content appears in a position that cannot be redacted safely.", http.StatusUnprocessableEntity)
	}
	if result.Blocked {
		if failure := pipeline.endAuxiliary(ctx, recorder, "blocked", "sensitive_content_blocked"); failure != nil {
			return 0, false, failure
		}
		return 0, false, blocked("sensitive_content_blocked", "The request matches a blocking redaction rule.", http.StatusForbidden)
	}
	request.Stream = false
	targets, endpoints, available, routeKind, err := pipeline.resolve(ctx, identity.Project.ID, request.Model, input.Kind)
	if err != nil {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", "model_not_found")
		if errors.Is(err, repository.ErrNotFound) {
			return 0, false, failure("invalid_request", model.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
		}
		return 0, false, internal("store_failure", "The model routes could not be loaded.")
	}
	if len(targets) == 0 {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", "model_not_found")
		return 0, false, failure("invalid_request", model.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
	}
	if routeKind != string(entity.ModelGeneration) {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", "model_kind_mismatch")
		return 0, false, failure("invalid_request", model.ClassInvalid, "model_kind_mismatch", "The model is not a generation model.", http.StatusUnprocessableEntity)
	}
	for _, candidate := range pipeline.selector.Plan(targets, available.keys, "", string(input.Kind)) {
		endpoint, found := endpoints[endpointKey(candidate.Target.ChannelID, candidate.Target.ModelID, model.Format(candidate.Target.Format))]
		if !found || endpoint.endpoint.CountTokensPath == "" || !endpoint.endpoint.Capability.NativeCount || len(candidate.Keys) == 0 {
			continue
		}
		recordAuxiliaryTarget(recorder, candidate)
		if counted, ok := pipeline.forwardCount(ctx, request, endpoint, candidate); ok {
			if failure := pipeline.endAuxiliary(ctx, recorder, "completed", ""); failure != nil {
				return 0, false, failure
			}
			return counted, false, nil
		}
	}
	if failure := pipeline.endAuxiliary(ctx, recorder, "completed", ""); failure != nil {
		return 0, false, failure
	}
	return int64(len(model.Marshal(request)) / 4), true, nil
}

// beginAuxiliary opens the audit record before any upstream egress. A failure to record refuses
// the call, matching the generation path.
func (pipeline *Pipeline) beginAuxiliary(ctx context.Context, identity *Identity, kind string, input Request, items []model.Item, modelName string, body []byte) (*Recorder, *model.Error) {
	callID := pipeline.ids()
	record := &entity.Call{
		ID: callID, ProjectID: identity.Project.ID, AgentKeyID: identity.AgentKey.ID,
		ClientFormat: string(input.Kind), Kind: kind, ClientTransport: "json", PublicModel: modelName,
		State: "running", AnalysisState: "completed", SessionID: callID, CreatedAt: time.Now().UTC(),
	}
	recorder := NewRecorder(pipeline.repository, pipeline.logger, pipeline.secret, record)
	if err := recorder.Open(ctx); err != nil {
		pipeline.logger.Error("auxiliary call could not be recorded", "kind", kind, "error", err)
		return nil, internal("audit_unavailable", "The call could not be recorded.")
	}
	recorder.Event(auditevent.KindRequest, auditevent.Request{Items: items, Params: model.Request{Source: input.Kind, Model: modelName}, Body: body})
	return recorder, nil
}

func (pipeline *Pipeline) endAuxiliary(ctx context.Context, recorder *Recorder, stateName, code string) *model.Error {
	if recorder == nil {
		return nil
	}
	if err := recorder.Seal(ctx, stateName, code); err != nil {
		pipeline.logger.Error("auxiliary call could not be sealed", "call", recorder.Call().ID, "error", err)
		return internal("audit_unavailable", "The auxiliary call could not be recorded.")
	}
	return nil
}

func (pipeline *Pipeline) recordAuxiliary(ctx context.Context, identity *Identity, kind string, input Request, items []model.Item, modelName string, body []byte) *model.Error {
	recorder, failure := pipeline.beginAuxiliary(ctx, identity, kind, input, items, modelName, body)
	if failure != nil {
		return failure
	}
	return pipeline.endAuxiliary(ctx, recorder, "completed", "")
}

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

// Model returns one configured public model.
func (pipeline *Pipeline) Model(ctx context.Context, key, name string) (*entity.Route, *model.Error) {
	routes, denied := pipeline.Models(ctx, key)
	if denied != nil {
		return nil, denied
	}
	for index := range routes {
		if routes[index].Name == name {
			return &routes[index], nil
		}
	}
	return nil, failure("invalid_request", model.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
}

func (pipeline *Pipeline) storedResponse(ctx context.Context, identity *Identity, responseID string) (*model.Response, []model.Item, *model.Error) {
	call, err := pipeline.repository.CallByResponseID(ctx, identity.AgentKey.ID, responseID)
	if err != nil {
		return nil, nil, responseNotFound()
	}
	if !call.ResponseStore {
		return nil, nil, responseNotFound()
	}
	events, err := pipeline.repository.EventsOfKinds(ctx, []string{call.ID}, auditevent.KindRequest, auditevent.KindResponse)
	if err != nil {
		return nil, nil, internal("store_failure", "The response could not be loaded.")
	}
	var response *model.Response
	var input []model.Item
	for _, event := range events {
		decoded, err := auditevent.Decode(event.Kind, event.Payload)
		if err != nil {
			continue
		}
		switch event.Kind {
		case auditevent.KindRequest:
			if decoded.Request != nil {
				input = decoded.Request.Items
			}
		case auditevent.KindResponse:
			if decoded.Response != nil {
				stored := decoded.Response.Response
				response = &stored
			}
		}
	}
	if response == nil {
		return nil, nil, responseNotFound()
	}
	return response, input, nil
}

func responseNotFound() *model.Error {
	return failure("invalid_request", model.ClassInvalid, "response_not_found", "The response is unknown or has expired.", http.StatusNotFound)
}

func (pipeline *Pipeline) beginLifecycle(ctx context.Context, identity *Identity, kind, responseID string) (*Recorder, *model.Error) {
	body := model.Marshal(map[string]string{"response_id": responseID})
	return pipeline.beginAuxiliary(ctx, identity, kind, Request{Kind: model.Responses}, nil, "", body)
}

// RetrieveResponse returns a stored Responses response and audits the lifecycle call.
func (pipeline *Pipeline) RetrieveResponse(ctx context.Context, key, responseID string) ([]byte, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, nil, key)
	if denied != nil {
		return nil, denied
	}
	defer release()
	recorder, auditFailure := pipeline.beginLifecycle(ctx, identity, "responses_retrieve", responseID)
	if auditFailure != nil {
		return nil, auditFailure
	}
	response, _, failure := pipeline.storedResponse(ctx, identity, responseID)
	if failure != nil {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", failure.Code)
		return nil, failure
	}
	payload, refusal := pipeline.EncodeStoredResponse(response)
	if refusal != nil {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", refusal.Code)
		return nil, refusal
	}
	if failure := pipeline.endAuxiliary(ctx, recorder, "completed", ""); failure != nil {
		return nil, failure
	}
	return payload, nil
}

// InputItems returns the stored input items and the losses of the flat encoding.
func (pipeline *Pipeline) InputItems(ctx context.Context, key, responseID string) ([]any, []model.Loss, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, nil, key)
	if denied != nil {
		return nil, nil, denied
	}
	defer release()
	recorder, auditFailure := pipeline.beginLifecycle(ctx, identity, "responses_input_items", responseID)
	if auditFailure != nil {
		return nil, nil, auditFailure
	}
	_, items, failure := pipeline.storedResponse(ctx, identity, responseID)
	if failure != nil {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", failure.Code)
		return nil, nil, failure
	}
	encoded, losses, err := responses.EncodeInputItems(items)
	if err != nil {
		failure := internal("encode_failed", "The input items could not be encoded.")
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", failure.Code)
		return nil, nil, failure
	}
	if failure := pipeline.endAuxiliary(ctx, recorder, "completed", ""); failure != nil {
		return nil, nil, failure
	}
	return encoded, losses, nil
}

// DeleteResponse soft-deletes a stored response.
func (pipeline *Pipeline) DeleteResponse(ctx context.Context, key, responseID string) *model.Error {
	identity, release, denied := pipeline.authorize(ctx, nil, key)
	if denied != nil {
		return denied
	}
	defer release()
	recorder, auditFailure := pipeline.beginLifecycle(ctx, identity, "responses_delete", responseID)
	if auditFailure != nil {
		return auditFailure
	}
	if err := pipeline.repository.DeleteResponse(ctx, identity.AgentKey.ID, responseID); err != nil {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", "response_not_found")
		return responseNotFound()
	}
	return pipeline.endAuxiliary(ctx, recorder, "completed", "")
}

// CancelResponse aborts an in-flight response.
func (pipeline *Pipeline) CancelResponse(ctx context.Context, key, responseID string) (bool, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, nil, key)
	if denied != nil {
		return false, denied
	}
	defer release()
	recorder, auditFailure := pipeline.beginLifecycle(ctx, identity, "responses_cancel", responseID)
	if auditFailure != nil {
		return false, auditFailure
	}
	call, err := pipeline.repository.CallByResponseID(ctx, identity.AgentKey.ID, responseID)
	if err != nil {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", "response_not_found")
		return false, responseNotFound()
	}
	if call.State != "running" {
		if failure := pipeline.endAuxiliary(ctx, recorder, "completed", ""); failure != nil {
			return false, failure
		}
		return false, nil
	}
	ok := pipeline.inflight.Cancel(call.ID)
	if ok {
		pipeline.repository.MarkCallCancelled(ctx, call.ID)
	}
	if failure := pipeline.endAuxiliary(ctx, recorder, "completed", ""); failure != nil {
		return false, failure
	}
	return ok, nil
}

// registerInflight lets CancelResponse reach a running call.
func (state *call) registerInflight(cancel context.CancelFunc) {
	state.pipeline.inflight.register(state.recorder.Call().ID, cancel)
}

// Passthrough forwards an auxiliary request unchanged to a channel that declares the capability.
// It never converts between formats; an unsupported capability is a protocol-level refusal.
func (pipeline *Pipeline) Passthrough(ctx context.Context, input Request, kind provider.AuxiliaryKind) ([]byte, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, input.Header, input.QueryKey)
	if denied != nil {
		return nil, denied
	}
	defer release()
	root, err := model.DecodeObject(input.Body)
	if err != nil {
		return nil, invalid("invalid_request", "The request must be a JSON object.")
	}
	modelName := model.String(root["model"])
	if modelName == "" {
		return nil, invalid("model_required", "The model field is required.")
	}
	recorder, auditFailure := pipeline.beginAuxiliary(ctx, identity, string(kind), input, nil, modelName, input.Body)
	if auditFailure != nil {
		return nil, auditFailure
	}
	engine, _, err := pipeline.redactionEngine(ctx, identity.Project)
	if err != nil {
		if failure := pipeline.endAuxiliary(ctx, recorder, "failed", "rules_unavailable"); failure != nil {
			return nil, failure
		}
		return nil, internal("rules_unavailable", "The redaction rules are unavailable.")
	}
	scope := engine.NewScope()
	body, result, err := scope.ApplyAuxiliaryBody(input.Body)
	if err != nil {
		if failure := pipeline.endAuxiliary(ctx, recorder, "blocked", "redaction_immutable"); failure != nil {
			return nil, failure
		}
		return nil, blocked("redaction_immutable", "Sensitive content appears in a position that cannot be redacted safely.", http.StatusUnprocessableEntity)
	}
	if result.Blocked {
		if failure := pipeline.endAuxiliary(ctx, recorder, "blocked", "sensitive_content_blocked"); failure != nil {
			return nil, failure
		}
		return nil, blocked("sensitive_content_blocked", "The request matches a blocking redaction rule.", http.StatusForbidden)
	}
	targets, endpoints, available, routeKind, err := pipeline.resolve(ctx, identity.Project.ID, modelName, input.Kind)
	if err != nil {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", "model_not_found")
		if errors.Is(err, repository.ErrNotFound) {
			return nil, failure("invalid_request", model.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
		}
		return nil, internal("store_failure", "The model routes could not be loaded.")
	}
	if len(targets) == 0 {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", "model_not_found")
		return nil, failure("invalid_request", model.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
	}
	if routeKind != string(modelKindForAuxiliary(kind)) {
		_ = pipeline.endAuxiliary(ctx, recorder, "failed", "model_kind_mismatch")
		return nil, failure("invalid_request", model.ClassInvalid, "model_kind_mismatch", "The model has a different kind than this endpoint requires.", http.StatusUnprocessableEntity)
	}
	for _, candidate := range pipeline.selector.Plan(targets, available.keys, "", string(input.Kind)) {
		endpoint, found := endpoints[endpointKey(candidate.Target.ChannelID, candidate.Target.ModelID, model.Format(candidate.Target.Format))]
		if !found {
			continue
		}
		auxiliary, baseURL, ok := auxiliaryOf(endpoint, kind)
		if !ok || len(candidate.Keys) == 0 {
			continue
		}
		recordAuxiliaryTarget(recorder, candidate)
		forwarded, err := rewriteAuxiliaryModel(body, candidate.Target.UpstreamModel)
		if err != nil {
			_ = pipeline.endAuxiliary(ctx, recorder, "failed", "invalid_request_body")
			return nil, internal("invalid_request_body", "The auxiliary request could not be prepared.")
		}
		payload, failure := pipeline.forwardAuxiliary(ctx, forwarded, endpoint, auxiliary, baseURL, candidate)
		if failure != nil {
			_ = pipeline.endAuxiliary(ctx, recorder, "failed", failure.Code)
			return nil, failure
		}
		if failure := pipeline.endAuxiliary(ctx, recorder, "completed", ""); failure != nil {
			return nil, failure
		}
		return payload, nil
	}
	_ = pipeline.endAuxiliary(ctx, recorder, "failed", "auxiliary_unsupported")
	return nil, failure("unsupported", model.ClassInvalid, "auxiliary_unsupported", "No channel supports this auxiliary endpoint for the model.", http.StatusUnprocessableEntity)
}

func modelKindForAuxiliary(kind provider.AuxiliaryKind) entity.ModelKind {
	if kind == provider.AuxiliaryModerations {
		return entity.ModelModeration
	}
	return entity.ModelEmbedding
}

func auxiliaryOf(endpoint endpoint, kind provider.AuxiliaryKind) (provider.AuxiliarySpec, string, bool) {
	for _, auxiliary := range endpoint.endpoint.Auxiliary {
		if auxiliary.Kind == kind {
			return auxiliary, endpoint.endpoint.BaseURL, true
		}
	}
	return provider.AuxiliarySpec{}, "", false
}

// recordAuxiliaryTarget attributes a routed auxiliary call to the channel model that served it.
func recordAuxiliaryTarget(recorder *Recorder, candidate routing.Candidate) {
	record := recorder.Call()
	record.ChannelID = candidate.Target.ChannelID
	record.ModelID = candidate.Target.ModelID
	record.UpstreamModel = candidate.Target.UpstreamModel
	record.EndpointFormat = candidate.Target.Format
}

// rewriteAuxiliaryModel applies the route's upstream model name to a same-format passthrough body.
// The public model selected the route; the upstream receives the name its provider knows.
func rewriteAuxiliaryModel(body []byte, upstreamModel string) ([]byte, error) {
	root, err := applyUpstreamModel(body, upstreamModel)
	if err != nil {
		return nil, err
	}
	return model.Marshal(root), nil
}

func coderOf(pipeline *Pipeline, format model.Format) codec.Codec {
	coder, _ := pipeline.codecs.Get(format)
	return coder
}

func (pipeline *Pipeline) forwardAuxiliary(ctx context.Context, body []byte, endpoint endpoint, auxiliary provider.AuxiliarySpec, baseURL string, candidate routing.Candidate) ([]byte, *model.Error) {
	key := candidate.Keys[0]
	url := endpoint.endpoint.AuxiliaryURL(baseURL, auxiliary)
	url = endpoint.endpoint.ApplyQuery(url, key.Secret)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, internal("invalid_endpoint", "The channel endpoint is invalid.")
	}
	credentials := map[string]string{}
	endpoint.endpoint.ApplyAuth(credentials, key.Secret)
	for name, value := range credentials {
		httpRequest.Header.Set(name, value)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	applyHeaders(httpRequest, endpoint.channel, nil)
	response, err := pipeline.clients.get(endpoint.channel).Do(httpRequest)
	if err != nil {
		return nil, transient("upstream_unreachable", "The upstream could not be reached.")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure, _ := pipeline.upstreamFailure(response, endpoint.channel, key)
		return nil, failure
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, pipeline.settings.MaxResponseBytes+1))
	if err != nil {
		return nil, transient("upstream_response_failed", "The upstream response could not be read.")
	}
	if int64(len(payload)) > pipeline.settings.MaxResponseBytes {
		return nil, transient("response_too_large", "The upstream response exceeded the size limit.")
	}
	return payload, nil
}

// EncodeStoredResponse renders a stored response in the Responses wire shape.
func (pipeline *Pipeline) EncodeStoredResponse(response *model.Response) ([]byte, *model.Error) {
	coder, found := pipeline.codecs.Get(model.Responses)
	if !found {
		return nil, internal("unsupported_format", "The protocol is not supported.")
	}
	peer := codec.Peer{Side: codec.Client, Vendor: model.VendorOpenAI, Model: response.Meta.Model, Envelope: Envelope{Secret: pipeline.secret}}
	payload, _, err := coder.EncodeResponse(response, peer)
	if err != nil {
		return nil, internal("encode_failed", "The response could not be encoded.")
	}
	return payload, nil
}
