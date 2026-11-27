package pipeline

import (
	"context"
	"errors"
	"net/http"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// Model returns one configured public model. Resolution follows the routing table, so a disabled
// route is not advertised and an alias answers the name the project configured for it.
func (pipeline *Pipeline) Model(ctx context.Context, key, name string) (*entity.Route, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, nil, key)
	if denied != nil {
		return nil, denied
	}
	defer release()
	input := Request{Kind: model.Chat, Operation: classify.Model}
	resolved, err := pipeline.repository.ResolveRoute(ctx, identity.Project.ID, name)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, pipeline.recordAuxiliary(ctx, identity, classify.Model, input, nil, "", nil, internal("store_failure", "The model list could not be loaded."))
	}
	if err != nil {
		refusal := failure("invalid_request", model.ClassInvalid, "model_not_found", "The model is not configured for this project.", http.StatusNotFound)
		if auditFailure := pipeline.recordAuxiliary(ctx, identity, classify.Model, input, nil, "", nil, refusal); auditFailure != nil {
			return nil, auditFailure
		}
		return nil, refusal
	}
	route := resolved.Route
	if auditFailure := pipeline.recordAuxiliary(ctx, identity, classify.Model, input, nil, "", nil, nil); auditFailure != nil {
		return nil, auditFailure
	}
	return &route, nil
}

// Models lists the model names a project may request: every enabled route plus its aliases, so a
// client that knows an old name still discovers it.
func (pipeline *Pipeline) Models(ctx context.Context, key string) ([]string, *model.Error) {
	identity, release, denied := pipeline.authorize(ctx, nil, key)
	if denied != nil {
		return nil, denied
	}
	defer release()
	records, err := pipeline.repository.Routes(ctx, identity.Project.ID, false)
	if err != nil {
		return nil, pipeline.recordAuxiliary(ctx, identity, classify.Models, Request{Kind: model.Chat, Operation: classify.Models}, nil, "", nil, internal("store_failure", "The model list could not be loaded."))
	}
	names := make([]string, 0, len(records))
	seen := map[string]bool{}
	for _, record := range records {
		if !record.Route.Enabled {
			continue
		}
		for _, name := range append([]string{record.Route.Name}, aliasNames(record.Aliases)...) {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	if failure := pipeline.recordAuxiliary(ctx, identity, classify.Models, Request{Kind: model.Chat, Operation: classify.Models}, nil, "", nil, nil); failure != nil {
		return nil, failure
	}
	return names, nil
}

func aliasNames(aliases []entity.RouteAlias) []string {
	names := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		names = append(names, alias.Alias)
	}
	return names
}
