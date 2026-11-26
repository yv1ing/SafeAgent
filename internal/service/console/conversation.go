package console

import (
	"context"

	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// Conversations lists the conversations of one project. Only conversational lanes belong to a
// conversation, so protocol traffic never appears here.
func (service *CallService) Conversations(ctx context.Context, actor access.Actor, projectID string, cursor Cursor) (Page[entity.Conversation], error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return Page[entity.Conversation]{}, err
	}
	rows, err := service.store.Conversations(ctx, projectID, cursor.Before, cursor.BeforeID, cursor.Fetch())
	if err != nil {
		return Page[entity.Conversation]{}, err
	}
	return Paginate(rows, cursor, func(conversation entity.Conversation) Position {
		return Position{At: conversation.LastAt, ID: conversation.ID}
	}), nil
}
