package console

import (
	"context"

	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// OperationService serves the administration log and project-scoped audit trail.
type OperationService struct {
	store *repository.Store
}

func NewOperationService(store *repository.Store) *OperationService {
	return &OperationService{store: store}
}

// Operations lists the administration log newest first.
func (service *OperationService) Operations(ctx context.Context, actor access.Actor, cursor Cursor) (Page[entity.OperationLog], error) {
	if err := actor.RequireAdmin(); err != nil {
		return Page[entity.OperationLog]{}, err
	}
	logs, err := service.store.OperationLogs(ctx, cursor.Before, cursor.BeforeID, cursor.Fetch())
	if err != nil {
		return Page[entity.OperationLog]{}, err
	}
	return Paginate(logs, cursor, func(log entity.OperationLog) Position {
		return Position{At: log.CreatedAt, ID: log.ID}
	}), nil
}

// ProjectOperations lists operations for a specific project, newest first.
func (service *OperationService) ProjectOperations(ctx context.Context, actor access.Actor, projectID string, cursor Cursor) (Page[entity.OperationLog], error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return Page[entity.OperationLog]{}, err
	}
	logs, err := service.store.ProjectOperationLogs(ctx, projectID, cursor.Before, cursor.BeforeID, cursor.Fetch())
	if err != nil {
		return Page[entity.OperationLog]{}, err
	}
	return Paginate(logs, cursor, func(log entity.OperationLog) Position {
		return Position{At: log.CreatedAt, ID: log.ID}
	}), nil
}

// RecordOperation stores one successful console action. Callers log a failure instead of failing
// the request, so the log never claims something that did not happen.
func (service *OperationService) RecordOperation(ctx context.Context, entry *entity.OperationLog) error {
	return service.store.RecordOperation(ctx, entry)
}
