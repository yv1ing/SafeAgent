package pipeline

import (
	"context"
	"errors"
	"net/http"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// ModelHeader names the route a call wants independently of the model field in its body. Agents
// use it for background calls that would otherwise name a model the project never configured.
const ModelHeader = "X-SafeAgent-Model"

// routePlan is the resolved routing decision of one call: the route, the usable candidates and the
// rule that matched.
type routePlan struct {
	resolution repository.RouteResolution
	rule       string
	targets    []routing.Target
	endpoints  map[string]endpoint
	available  channels
}

// resolvePublicModel maps the client-facing model name onto a route. Explicit header, exact name
// and alias win in that order; only side and auxiliary lanes may fall back to the project default,
// so a main call still fails when its model is not configured.
func (pipeline *Pipeline) resolvePublicModel(ctx context.Context, projectID string, lane classify.Lane, header http.Header, publicModel string) (repository.RouteResolution, string, error) {
	if explicit := header.Get(ModelHeader); explicit != "" {
		resolved, err := pipeline.repository.ResolveRoute(ctx, projectID, explicit)
		switch {
		case err == nil:
			return resolved, repository.ResolutionExplicit, nil
		case !errors.Is(err, repository.ErrNotFound):
			return repository.RouteResolution{}, "", err
		}
	}
	resolved, err := pipeline.repository.ResolveRoute(ctx, projectID, publicModel)
	if err == nil {
		return resolved, resolved.Resolution, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return repository.RouteResolution{}, "", err
	}
	if lane == classify.LaneMain {
		return repository.RouteResolution{}, "", repository.ErrNotFound
	}
	resolved, err = pipeline.repository.DefaultRoute(ctx, projectID)
	if err != nil {
		return repository.RouteResolution{}, "", err
	}
	return resolved, repository.ResolutionFallback, nil
}

// routeFor resolves a model name and builds the executable candidates. It is the single routing
// entry point of the pipeline; generation and auxiliary endpoints differ only in the kind of model
// they accept and in which refusal a missing model produces.
func (pipeline *Pipeline) routeFor(ctx context.Context, projectID, publicModel string, lane classify.Lane, header http.Header, clientFormat model.Format, kind entity.ModelKind) (routePlan, *model.Error) {
	resolved, resolution, err := pipeline.resolvePublicModel(ctx, projectID, lane, header, publicModel)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return routePlan{}, failure("invalid_request", model.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
		}
		pipeline.logger.Error("model routes could not be resolved", "error", err)
		return routePlan{}, internal("store_failure", "The model routes could not be loaded.")
	}
	plan, mismatch, err := pipeline.buildRoutePlan(ctx, resolved, resolution, clientFormat, kind)
	if err != nil {
		pipeline.logger.Error("channel keys could not be loaded", "error", err)
		return routePlan{}, internal("store_failure", "The channel credentials could not be loaded.")
	}
	// The route resolved, so the caller still emits the routing event when the route cannot serve
	// this endpoint: a replay should show which route matched before the refusal.
	plan.rule = resolution
	if mismatch {
		return plan, failure("invalid_request", model.ClassInvalid, "model_kind_mismatch", "The model is not a model of the kind this endpoint requires.", http.StatusUnprocessableEntity)
	}
	if len(plan.targets) == 0 {
		return plan, failure("invalid_request", model.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
	}
	return plan, nil
}

// buildRoutePlan turns the repository candidates into selector targets and endpoint descriptions.
// A candidate whose model has another kind is not a format problem: it tells the caller that the
// route exists but does not serve this endpoint at all.
func (pipeline *Pipeline) buildRoutePlan(ctx context.Context, resolved repository.RouteResolution, resolution string, clientFormat model.Format, kind entity.ModelKind) (routePlan, bool, error) {
	plan := routePlan{
		resolution: resolved,
		endpoints:  map[string]endpoint{},
		available:  channels{byID: map[string]*entity.Channel{}, keys: map[string][]routing.Key{}},
	}
	channelIDs := make([]string, 0, len(resolved.Targets))
	seenChannel := map[string]bool{}
	for _, candidate := range resolved.Targets {
		if !seenChannel[candidate.Channel.ID] {
			seenChannel[candidate.Channel.ID] = true
			channelIDs = append(channelIDs, candidate.Channel.ID)
		}
	}
	overrides := pipeline.capabilityOverrides(ctx, channelIDs)
	matchedKind := len(resolved.Targets) == 0
	for _, resolvedTarget := range resolved.Targets {
		target := resolvedTarget
		if target.Model.Kind != kind {
			continue
		}
		matchedKind = true
		spec, found := pipeline.providerSpec(target.Provider)
		if !found {
			continue
		}
		channel, modelEntry := target.Channel, target.Model
		format, endpointSpec, ok := selectTargetFormat(spec, modelEntry, target.Target.Format, clientFormat)
		if !ok {
			continue
		}
		endpointSpec = channelEndpoint(endpointSpec, channel)
		applyCapabilityOverrides(&endpointSpec, overrides, modelEntry.ID, string(format))
		upstreamModel := endpointSpec.ApplyAlias(modelEntry.UpstreamName)
		key := endpointKey(channel.ID, modelEntry.ID, format)
		plan.endpoints[key] = endpoint{channel: &channel, model: &modelEntry, endpoint: endpointSpec, format: format, upstreamModel: upstreamModel}
		if plan.available.byID[channel.ID] == nil {
			plan.available.byID[channel.ID] = &channel
		}
		plan.targets = append(plan.targets, routing.Target{
			ChannelID: channel.ID, ModelID: modelEntry.ID, UpstreamModel: upstreamModel, Format: string(format),
			Priority: target.Target.Priority, Weight: target.Target.Weight, MaxOutputTokens: target.Target.MaxOutputTokens,
		})
	}
	if len(plan.targets) == 0 {
		return plan, !matchedKind, nil
	}
	if err := pipeline.attachKeys(ctx, &plan); err != nil {
		return routePlan{}, false, err
	}
	return plan, false, nil
}

// attachKeys loads the decrypted upstream credentials of every channel the plan may reach. A key
// that cannot be opened is logged and skipped so one broken key does not hide the healthy ones; a
// failed read fails the call instead of pretending no channel is available.
func (pipeline *Pipeline) attachKeys(ctx context.Context, plan *routePlan) error {
	secrets, err := pipeline.repository.EnabledChannelKeys(ctx)
	if err != nil {
		return err
	}
	for _, secret := range secrets {
		if plan.available.byID[secret.ChannelID] == nil {
			continue
		}
		plain, err := secure.Open(pipeline.secret, secret.Secret)
		if err != nil {
			pipeline.logger.Error("upstream key could not be opened", "key", secret.ID, "error", err)
			continue
		}
		plan.available.keys[secret.ChannelID] = append(plan.available.keys[secret.ChannelID], routing.Key{ID: secret.ID, Secret: string(plain)})
	}
	return nil
}

// recordRouting writes the one routing event of the call and the columns the call row summarizes.
// It runs before any attempt event, so a replay can explain a fallback the row only names.
func (pipeline *Pipeline) recordRouting(recorder *Recorder, plan routePlan, publicModel string) {
	record := recorder.Call()
	record.RouteID = plan.resolution.Route.ID
	record.Resolution = plan.rule
	record.PublicModel = publicModel
	recorder.ModelChange(plan.resolution.Route.ID, plan.resolution.Route.Name, plan.rule, publicModel, "", "", "", "")
}

// capabilityOverrides is the only access to the runtime capability rejections. Keeping it in one
// function isolates the storage contract from the routing code.
func (pipeline *Pipeline) capabilityOverrides(ctx context.Context, channelIDs []string) []entity.CapabilityOverride {
	if len(channelIDs) == 0 {
		return nil
	}
	overrides, err := pipeline.repository.CapabilityOverrides(ctx, channelIDs)
	if err != nil {
		pipeline.logger.Warn("capability overrides could not be read", "error", err)
		return nil
	}
	return overrides
}

func applyCapabilityOverrides(endpoint *provider.EndpointSpec, overrides []entity.CapabilityOverride, modelID, format string) {
	for _, override := range overrides {
		if override.ModelID == modelID && override.Format == format {
			applyCapabilityOverride(endpoint, override.Field)
		}
	}
}

// selectTargetFormat picks exactly one format for a candidate: its declared format when set,
// otherwise the client format when the provider exposes it for this model kind, otherwise the
// first usable format in provider order.
func selectTargetFormat(spec provider.ProviderSpec, modelEntry entity.Model, declared string, clientFormat model.Format) (model.Format, provider.EndpointSpec, bool) {
	if declared != "" {
		format := model.Format(declared)
		endpoint, found := spec.Endpoint(format)
		if !found || !formatServesKind(endpoint, modelEntry.Kind) {
			return "", provider.EndpointSpec{}, false
		}
		return format, endpoint, true
	}
	if endpoint, found := spec.Endpoint(clientFormat); clientFormat != "" && found && formatServesKind(endpoint, modelEntry.Kind) {
		return clientFormat, endpoint, true
	}
	for _, format := range spec.Formats() {
		endpoint, found := spec.Endpoint(format)
		if found && formatServesKind(endpoint, modelEntry.Kind) {
			return format, endpoint, true
		}
	}
	return "", provider.EndpointSpec{}, false
}

func formatServesKind(endpoint provider.EndpointSpec, kind entity.ModelKind) bool {
	if kind == entity.ModelGeneration {
		return true
	}
	wanted := provider.AuxiliaryEmbeddings
	if kind == entity.ModelModeration {
		wanted = provider.AuxiliaryModerations
	}
	for _, auxiliary := range endpoint.Auxiliary {
		if auxiliary.Kind == wanted {
			return true
		}
	}
	return false
}

// channelEndpoint points one provider endpoint at the channel. A configured channel base URL wins
// over the provider default, which is what lets a channel reach a proxy or a self-hosted gateway;
// an empty channel base URL keeps the provider host, so multi-host providers work unchanged.
func channelEndpoint(endpoint provider.EndpointSpec, channel entity.Channel) provider.EndpointSpec {
	if channel.BaseURL != "" {
		endpoint.BaseURL = channel.BaseURL
	}
	return endpoint
}

// providerSpec rebuilds the runtime catalog from the provider row attached to a route target, so a
// custom provider works without any in-memory registry refresh.
func (pipeline *Pipeline) providerSpec(stored entity.Provider) (provider.ProviderSpec, bool) {
	if !stored.Enabled {
		return provider.ProviderSpec{}, false
	}
	endpoints, err := provider.UnmarshalEndpoints(stored.Endpoints)
	if err != nil {
		pipeline.logger.Error("provider endpoints are unreadable", "provider", stored.ID, "error", err)
		return provider.ProviderSpec{}, false
	}
	return provider.ProviderSpec{ID: stored.ID, Label: stored.Label, Custom: stored.Source == entity.ProviderCustom, Endpoints: endpoints}, true
}

// endpointKey identifies one format of one catalog model on one channel.
func endpointKey(channelID, modelID string, format model.Format) string {
	return channelID + "\x00" + modelID + "\x00" + string(format)
}

// channels is the resolved view of what a route can reach: the channels of its candidates and the
// live keys of each channel.
type channels struct {
	byID map[string]*entity.Channel
	keys map[string][]routing.Key
}
