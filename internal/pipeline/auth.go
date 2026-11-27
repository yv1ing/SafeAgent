// Package pipeline is the online request pipeline: admission, redaction, policy checkpoints,
// routing, upstream attempts, streaming and the terminal call seal.
package pipeline

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/platform/limit"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
	_ "github.com/yv1ing/SafeAgent/internal/threat/builtin"
)

// Authenticate maps a presented agent key to its project.
func (pipeline *Pipeline) Authenticate(ctx context.Context, key string) (*Identity, *model.Error) {
	if key == "" {
		return nil, failure("unauthorized", model.ClassAuth, "missing_api_key", "An API key is required.", http.StatusUnauthorized)
	}
	agentKey, err := pipeline.repository.AgentKeyByHash(ctx, secure.Digest(key))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, failure("unauthorized", model.ClassAuth, "invalid_api_key", "The API key is invalid.", http.StatusUnauthorized)
		}
		pipeline.logger.Error("agent key lookup failed", "error", err)
		return nil, internal("store_failure", "The API key could not be checked.")
	}
	project, err := pipeline.repository.Project(ctx, agentKey.ProjectID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, failure("unauthorized", model.ClassAuth, "invalid_project", "The project is unavailable.", http.StatusUnauthorized)
		}
		pipeline.logger.Error("project lookup failed", "error", err)
		return nil, internal("store_failure", "The project could not be loaded.")
	}
	pipeline.repository.TouchAgentKey(ctx, agentKey.ID)
	return &Identity{Project: project, AgentKey: agentKey}, nil
}

// BearerToken extracts the agent key from the headers agents conventionally use, then from the
// ?key= value the transport passed for Gemini routes.
func BearerToken(header http.Header, queryKey string) string {
	if value := header.Get("Authorization"); value != "" {
		if len(value) > 7 && strings.EqualFold(value[:7], "bearer ") {
			return strings.TrimSpace(value[7:])
		}
		return ""
	}
	for _, name := range []string{"x-api-key", "x-goog-api-key"} {
		if value := header.Get(name); value != "" {
			return value
		}
	}
	return queryKey
}

// authorize authenticates the request and reserves its rate and concurrency slots. The returned
// release must run once the call settles.
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
