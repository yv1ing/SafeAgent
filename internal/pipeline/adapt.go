package pipeline

import (
	"net/http"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// prepared is one upstream request ready to send: the bytes, the headers, the peer profile and
// whether the request is the client's own body with only the transport fields patched.
type prepared struct {
	endpoint      endpoint
	coder         codec.Codec
	peer          codec.Peer
	body          []byte
	header        http.Header
	losses        []model.Loss
	repairs       []model.Repair
	raw           bool
	stream        bool
	upstreamModel string
}

func (state *call) prepare(candidate routing.Candidate) (*prepared, *model.Error) {
	target := candidate.Target
	endpoint, found := state.endpoints[endpointKey(target.ChannelID, target.UpstreamModel, model.Format(target.Format))]
	if !found {
		return nil, failure("invalid_request", model.ClassInvalid, "unsupported_channel_format", "The channel does not expose this format.", http.StatusUnprocessableEntity)
	}
	adapted := model.CloneRequest(state.request)
	adapted.Source = state.kind
	adapted.Stream = state.stream
	losses := model.AdaptReasoning(adapted, endpoint.format.DefaultVendor(), endpoint.format)
	missingReasoning := model.MissingToolReasoning(adapted)
	if missingReasoning {
		adapted.Reasoning = &model.ReasoningConfig{Effort: "none"}
		losses = append(losses, model.Loss{Path: "reasoning", Code: model.LossReasoningNotReplayable, Class: model.LossDegraded, Detail: "the client history has a tool call without reasoning; target thinking was disabled", From: state.kind, To: endpoint.format})
	}
	repairs := model.RepairRequest(adapted, endpoint.format)
	if endpoint.endpoint.Compat.NoReasoning && adapted.Reasoning != nil {
		adapted.Reasoning = nil
		losses = append(losses, model.Loss{Path: "reasoning", Code: model.LossFieldDropped, Class: model.LossDegraded, Detail: "the upstream rejected reasoning for this channel; it is now omitted", To: endpoint.format})
	}
	if endpoint.endpoint.Compat.NoTools && len(adapted.Tools) > 0 {
		adapted.Tools, adapted.ToolChoice, adapted.ParallelToolCalls = nil, nil, nil
		losses = append(losses, model.Loss{Path: "tools", Code: model.LossFieldDropped, Class: model.LossDegraded, Detail: "the upstream rejected tools for this channel; they are now omitted", To: endpoint.format})
	}
	raw := state.kind == endpoint.format && len(state.hits) == 0 && !state.historyExpanded && !state.unsealed && len(repairs) == 0 && !missingReasoning && rawSafe(adapted, endpoint.format)
	if loss, applied := resolveMaxOutputTokens(adapted, endpoint, target.MaxOutputTokens, state.identity.Project); applied {
		losses = append(losses, loss)
	}
	if endpoint.endpoint.Compat.NoCustomToolType {
		losses = append(losses, downgradeCustomTools(adapted)...)
	}
	capabilityLosses := model.ValidateRequestCapability(adapted, endpoint.endpoint.Capability)
	for _, loss := range capabilityLosses {
		if loss.Fatal() {
			state.failureLedger.Losses = append(state.failureLedger.Losses, losses...)
			state.failureLedger.Losses = append(state.failureLedger.Losses, capabilityLosses...)
			return nil, failure("invalid_request", model.ClassInvalid, "conversion_failed", sentence("The request cannot be represented in the channel protocol: "+loss.Detail), http.StatusUnprocessableEntity)
		}
	}
	stream := upstreamStream(state.stream, endpoint.endpoint.Transport)
	includeUsage := endpoint.endpoint.Capability.StreamUsage
	if !includeUsage && adapted.StreamOptions != nil && adapted.StreamOptions.IncludeUsage {
		losses = append(losses, model.Loss{Path: "stream_options.include_usage", Code: model.LossFieldDropped, Class: model.LossDegraded, Detail: "the target does not report usage in a stream", To: endpoint.format})
	}
	peer := codec.Peer{Side: codec.Upstream, Vendor: endpoint.format.DefaultVendor(), Model: target.UpstreamModel, Profile: model.Profile{Capability: endpoint.endpoint.Capability, Compat: endpoint.endpoint.Compat}, Salt: state.salt, IncludeUsage: includeUsage}
	header, headerLosses := codec.ForwardHeaders(state.clientHeader, endpoint.format, endpoint.channel.ForwardHeaders)
	raw = raw && len(capabilityLosses) == 0
	if adapted.ChoiceCount > 1 && (!endpoint.endpoint.Capability.MultiChoice || !raw) {
		return nil, model.UnsupportedFeature("unsupported_multi_choice", "Multiple candidates are available only through a same-format streaming pass-through.")
	}
	state.failureLedger.Repairs = append(state.failureLedger.Repairs, repairs...)
	if !raw {
		losses = append(losses, capabilityLosses...)
	}
	losses = append(losses, headerLosses...)
	var body []byte
	if raw {
		var err error
		body, err = patchRawBody(state.body, endpoint.format, target.UpstreamModel, stream)
		if err != nil {
			state.failureLedger.Losses = append(state.failureLedger.Losses, losses...)
			return nil, internal("invalid_request_body", "The request body could not be prepared.")
		}
	} else {
		encoded, encodedHeader, encodeLosses, err := coderOf(state.pipeline, endpoint.format).EncodeRequest(adapted, peer)
		if err != nil {
			state.failureLedger.Losses = append(state.failureLedger.Losses, losses...)
			return nil, failure("invalid_request", model.ClassInvalid, "conversion_failed", sentence("The request cannot be represented in the channel protocol: "+err.Error()), http.StatusUnprocessableEntity)
		}
		body = encoded
		losses = append(losses, encodeLosses...)
		mergeHeaders(header, encodedHeader)
	}
	state.failureLedger.Losses = append(state.failureLedger.Losses, losses...)
	return &prepared{
		endpoint: endpoint, coder: coderOf(state.pipeline, endpoint.format), peer: peer, body: body, header: header, losses: losses, repairs: repairs,
		raw: raw, stream: stream, upstreamModel: target.UpstreamModel,
	}, nil
}

func upstreamStream(clientStream bool, mode provider.TransportMode) bool {
	switch mode {
	case provider.TransportStream:
		return true
	case provider.TransportJSON:
		return false
	}
	return clientStream
}

func rawSafe(request *model.Request, target model.Format) bool {
	if request.ChoiceCount > 1 && target == model.Chat {
		return true
	}
	switch target {
	case model.Messages:
		return request.MaxOutputTokens != nil
	case model.Responses:
		if request.PreviousResponseID != "" {
			return false
		}
		return request.Store == nil || !*request.Store
	}
	return true
}

func downgradeCustomTools(request *model.Request) []model.Loss {
	var losses []model.Loss
	for index := range request.Tools {
		tool := &request.Tools[index]
		if tool.Kind != model.ToolCustom {
			continue
		}
		tool.Kind = model.ToolFunction
		tool.Parameters = customToolSchema()
		tool.Format = nil
		losses = append(losses, model.Loss{Path: "tools[" + strconv.Itoa(index) + "]", Code: model.LossToolKindMapped, Class: model.LossDegraded, Detail: "the endpoint has no custom tool type; the tool became a function taking one string input", To: request.Source})
	}
	return losses
}

func customToolSchema() []byte {
	return []byte(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`)
}

func resolveMaxOutputTokens(request *model.Request, endpoint endpoint, routeDefault int, project *entity.Project) (model.Loss, bool) {
	if !endpoint.endpoint.Compat.RequireMaxTokens || request.MaxOutputTokens != nil {
		return model.Loss{}, false
	}
	value, source := int64(0), ""
	switch {
	case routeDefault > 0:
		value, source = int64(routeDefault), "route target default"
	case endpoint.channel != nil && endpoint.channel.DefaultMaxOutputTokens > 0:
		value, source = int64(endpoint.channel.DefaultMaxOutputTokens), "channel default"
	case project != nil && project.DefaultMaxOutputTokens > 0:
		value, source = int64(project.DefaultMaxOutputTokens), "project default"
	case endpoint.endpoint.Compat.DefaultMaxOutput > 0:
		value, source = endpoint.endpoint.Compat.DefaultMaxOutput, "provider default"
	default:
		value, source = 8192, "gateway fallback"
	}
	request.MaxOutputTokens = &value
	return model.Loss{Path: "max_output_tokens", Code: model.LossDefaultTokenLimit, Class: model.LossDegraded, Detail: "the endpoint requires a token limit; " + source + " was applied", To: endpoint.format}, true
}
