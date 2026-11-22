package pipeline

import (
	"net/http"
	"strconv"
	"strings"

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
	if model.MissingToolReasoning(adapted) {
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
	if !includeUsage && adapted.StreamOptions != nil {
		losses = append(losses, model.Loss{Path: "stream_options", Code: model.LossFieldDropped, Class: model.LossDegraded, Detail: "the target does not report usage in a stream", To: endpoint.format})
	}
	peer := codec.Peer{Side: codec.Upstream, Vendor: endpoint.format.DefaultVendor(), Model: target.UpstreamModel, Profile: model.Profile{Capability: endpoint.endpoint.Capability, Compat: endpoint.endpoint.Compat}, Salt: state.salt, IncludeUsage: includeUsage}
	header, headerLosses := codec.ForwardHeaders(state.clientHeader, endpoint.format, endpoint.channel.ForwardHeaders)
	raw := state.kind == endpoint.format && len(state.hits) == 0 && !state.historyExpanded && !state.unsealed &&
		len(losses) == 0 && len(repairs) == 0 && len(capabilityLosses) == 0 &&
		rawSafe(adapted, endpoint.format) && rawRequestSafe(adapted, endpoint) && rawBodySafe(state.body, endpoint.format)
	if adapted.ChoiceCount > 1 && (!endpoint.endpoint.Capability.MultiChoice || !raw) {
		return nil, model.UnsupportedFeature("unsupported_multi_choice", "Multiple candidates are available only through a same-format streaming pass-through.")
	}
	state.failureLedger.Repairs = append(state.failureLedger.Repairs, repairs...)
	losses = append(losses, capabilityLosses...)
	losses = append(losses, headerLosses...)
	var body []byte
	if raw {
		var err error
		body, err = patchRawBody(state.body, endpoint.format, target.UpstreamModel, stream, includeUsage)
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
		endpoint: endpoint, coder: coderOf(state.pipeline, endpoint.format), peer: peer, body: body, header: header, losses: losses,
		stream: stream, upstreamModel: target.UpstreamModel,
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

// rawSafe carries the format rules that need no endpoint capability. Responses always takes the
// canonical request path because the gateway owns store, include and previous_response_id.
func rawSafe(request *model.Request, target model.Format) bool {
	if target == model.Responses {
		return false
	}
	if request.ChoiceCount > 1 && target == model.Chat {
		return true
	}
	return target != model.Messages || request.MaxOutputTokens != nil
}

// rawRequestSafe rejects any modelled value the target codec would drop or rewrite. Raw bytes are
// only safe when canonical encoding would preserve the request unchanged.
func rawRequestSafe(request *model.Request, endpoint endpoint) bool {
	capability, compat := endpoint.endpoint.Capability, endpoint.endpoint.Compat
	if request.Store != nil {
		return false
	}
	if request.TopK != nil && (!capability.TopK || compat.IgnoreTopK) {
		return false
	}
	if (request.Logprobs != nil || request.TopLogprobs != nil) && !capability.Logprobs {
		return false
	}
	if request.TopLogprobs != nil && (request.Logprobs == nil || !*request.Logprobs) {
		return false
	}
	if request.ServiceTier != "" && compat.IgnoreServiceTier {
		return false
	}
	if request.Reasoning != nil && request.Reasoning.BudgetTokens != nil && compat.IgnoreBudgetTokens {
		return false
	}
	if request.StreamOptions != nil && !capability.StreamUsage {
		return false
	}
	for _, modality := range request.Modalities {
		if modality != "text" {
			return false
		}
	}
	if !rawReasoningSafe(request, endpoint.format) {
		return false
	}
	if !rawItemsSafe(request, endpoint.format) {
		return false
	}
	if endpoint.format == model.Gemini && !strings.Contains(endpoint.upstreamModel, "gemini-3") {
		for _, item := range request.Items {
			if item.ToolCall != nil && item.ToolCall.ID != "" || item.CallID != "" {
				return false
			}
		}
	}
	if !rawToolsSafe(request, endpoint) {
		return false
	}
	return rawResponseFormatSafe(request.ResponseFormat, endpoint.format)
}

// rawBodySafe catches wire-level shapes that the canonical model normalizes for the same format.
func rawBodySafe(body []byte, target model.Format) bool {
	if target != model.Chat && target != model.Messages && target != model.Gemini {
		return true
	}
	root, err := model.DecodeObject(body)
	if err != nil {
		return false
	}
	switch target {
	case model.Chat:
		if root["max_tokens"] != nil || root["functions"] != nil || root["thinking"] != nil {
			return false
		}
		if choice, ok := root["tool_choice"].(string); ok && choice == "any" {
			return false
		}
		for _, value := range model.Array(root["messages"]) {
			message := model.Object(value)
			if message["function_call"] != nil || message["reasoning"] != nil {
				return false
			}
		}
	case model.Messages:
		if root["parallel_tool_calls"] != nil {
			return false
		}
		if value, found := root["metadata"]; found && value != nil {
			metadata := model.Object(value)
			if metadata == nil {
				return false
			}
			for key, entry := range metadata {
				if key != "user_id" {
					return false
				}
				if _, ok := entry.(string); !ok {
					return false
				}
			}
		}
		for _, value := range model.Array(root["messages"]) {
			switch model.String(model.Object(value)["role"]) {
			case "system", "developer":
				return false
			}
		}
	case model.Gemini:
		if config := model.Object(root["generationConfig"]); config["responseSchema"] != nil {
			return false
		}
	}
	return true
}

func rawItemsSafe(request *model.Request, target model.Format) bool {
	if target != model.Messages {
		return true
	}
	for _, item := range request.Items {
		if item.Role == model.RoleDeveloper {
			return false
		}
		if item.Role != model.RoleSystem {
			continue
		}
		for _, part := range item.Parts {
			if part.Kind != model.PartText {
				return false
			}
		}
	}
	return true
}

func rawReasoningSafe(request *model.Request, target model.Format) bool {
	reasoning := request.Reasoning
	if reasoning == nil {
		return true
	}
	if reasoning.IncludeThoughts != nil && target != model.Gemini {
		return false
	}
	if reasoning.Summary != "" && target != model.Responses {
		return false
	}
	switch target {
	case model.Chat:
		return reasoning.BudgetTokens == nil && reasoning.Effort != "auto" && reasoning.Effort != "max"
	case model.Messages:
		if reasoning.Effort == "minimal" || reasoning.BudgetTokens != nil && reasoning.Effort != "" {
			return false
		}
		if reasoning.BudgetTokens == nil {
			return true
		}
		budget := *reasoning.BudgetTokens
		return budget <= 0 || budget >= 1024 && (request.MaxOutputTokens == nil || *request.MaxOutputTokens > budget)
	case model.Gemini:
		if reasoning.BudgetTokens != nil && reasoning.Effort != "" {
			return false
		}
		if reasoning.Effort == "" {
			return true
		}
		switch reasoning.Effort {
		case "minimal", "low", "medium", "high":
			return strings.HasPrefix(request.Model, "gemini-3")
		}
		return false
	}
	return true
}

func rawToolsSafe(request *model.Request, endpoint endpoint) bool {
	for _, tool := range request.Tools {
		if tool.Strict != nil && !endpoint.endpoint.Capability.Tools.Strict {
			return false
		}
	}
	if request.ParallelToolCalls != nil && len(request.Tools) == 0 {
		return false
	}
	if request.ToolChoice == nil {
		return true
	}
	if len(request.Tools) == 0 && request.ToolChoice.Mode != "auto" && request.ToolChoice.Mode != "none" {
		return false
	}
	return endpoint.format != model.Messages || request.ToolChoice.Mode != "allowed"
}

func rawResponseFormatSafe(responseFormat *model.ResponseFormat, target model.Format) bool {
	if responseFormat == nil {
		return true
	}
	extra := responseFormat.Name != "" || responseFormat.Description != "" || responseFormat.Strict != nil
	switch target {
	case model.Messages:
		return responseFormat.Kind != "json_object" && (responseFormat.Kind != "json_schema" || !extra)
	case model.Gemini:
		return responseFormat.Kind != "json_schema" || !extra
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
