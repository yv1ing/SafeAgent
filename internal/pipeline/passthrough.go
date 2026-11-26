package pipeline

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

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
	operation := input.Operation
	if operation != classify.Embeddings && operation != classify.Moderations {
		operation = classify.Embeddings
		if kind == provider.AuxiliaryModerations {
			operation = classify.Moderations
		}
	}
	auxiliary, auditFailure := pipeline.beginAuxiliary(ctx, identity, operation, input, modelName)
	if auditFailure != nil {
		return nil, auditFailure
	}
	body, result, err := auxiliary.scope.ApplyAuxiliaryBody(input.Body)
	hits := relocateHits(&model.Request{}, 0, auxiliary.recorder.Call().ID, result)
	applyRedactionSummary(auxiliary.recorder.Call(), hits)
	auxiliary.recordRequest(nil, model.Request{Source: input.Kind, Model: modelName}, body, hits)
	if err != nil {
		return nil, pipeline.endAuxiliary(ctx, auxiliary, blocked("redaction_immutable", "Sensitive content appears in a position that cannot be redacted safely.", http.StatusUnprocessableEntity))
	}
	if result.Blocked {
		return nil, pipeline.endAuxiliary(ctx, auxiliary, blocked("sensitive_content_blocked", "The request matches a blocking redaction rule.", http.StatusForbidden))
	}
	plan, failure := pipeline.routeFor(ctx, identity.Project.ID, modelName, classify.LaneAuxiliary, input.Header, input.Kind, modelKindForAuxiliary(kind))
	if plan.resolution.Route.ID != "" {
		pipeline.recordRouting(auxiliary.recorder, plan, modelName)
	}
	if failure != nil {
		return nil, pipeline.endAuxiliary(ctx, auxiliary, failure)
	}
	for _, candidate := range pipeline.selector.Plan(plan.targets, plan.available.keys, "", string(input.Kind)) {
		endpoint, found := plan.endpoints[endpointKey(candidate.Target.ChannelID, candidate.Target.ModelID, model.Format(candidate.Target.Format))]
		if !found {
			continue
		}
		auxiliaryEndpoint, baseURL, ok := auxiliaryOf(endpoint, kind)
		if !ok || len(candidate.Keys) == 0 {
			continue
		}
		recordAuxiliaryTarget(auxiliary.recorder, candidate)
		forwarded, err := rewriteAuxiliaryModel(body, candidate.Target.UpstreamModel)
		if err != nil {
			return nil, pipeline.endAuxiliary(ctx, auxiliary, internal("invalid_request_body", "The auxiliary request could not be prepared."))
		}
		payload, failure := pipeline.forwardAuxiliary(ctx, forwarded, endpoint, auxiliaryEndpoint, baseURL, candidate)
		if failure != nil {
			return nil, pipeline.endAuxiliary(ctx, auxiliary, failure)
		}
		if refusal := pipeline.endAuxiliary(ctx, auxiliary, nil); refusal != nil {
			return nil, refusal
		}
		return payload, nil
	}
	refusal := rejectAuxiliary("auxiliary_unsupported", "No channel supports this auxiliary endpoint for the model.")
	return nil, pipeline.endAuxiliary(ctx, auxiliary, refusal)
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
	record.UpstreamFormat = candidate.Target.Format
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
