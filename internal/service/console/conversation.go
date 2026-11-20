package console

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/observe"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

type ConversationNode struct {
	Call    entity.Call
	Summary string
}

type ConversationTree struct {
	Conversation *entity.Conversation
	Nodes        []ConversationNode
	Truncated    bool
}

// Conversation returns the call tree of one conversation.
func (service *CallService) Conversation(ctx context.Context, actor Actor, projectID, sessionID string) (*ConversationTree, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	conversation, err := service.store.Conversation(ctx, projectID, sessionID)
	if err != nil {
		return nil, Translate(err)
	}
	rows, err := service.store.SessionCalls(ctx, projectID, sessionID, repository.MaximumSessionCalls)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	// Summaries need only recent user text. Bound the payloads separately from the call list so a
	// long session cannot make one console read materialize every request body.
	truncated := len(rows) == repository.MaximumSessionCalls
	summaryIDs := ids
	if len(summaryIDs) > 50 {
		summaryIDs = summaryIDs[len(summaryIDs)-50:]
		truncated = true
	}
	events, err := service.store.RequestMessages(ctx, summaryIDs)
	if err != nil {
		return nil, err
	}
	project, err := service.store.Project(ctx, projectID)
	if err != nil {
		return nil, Translate(err)
	}
	summaries := make(map[string]string, len(events))
	for _, event := range events {
		redactor, _, err := service.analyzer.RedactorForCall(ctx, project, event.CallID)
		if err != nil {
			return nil, err
		}
		summaries[event.CallID] = summaryFromPayload(event.Payload, redactor)
	}
	tree := &ConversationTree{Conversation: conversation, Nodes: make([]ConversationNode, 0, len(rows)), Truncated: truncated}
	for _, row := range rows {
		tree.Nodes = append(tree.Nodes, ConversationNode{Call: row, Summary: summaries[row.ID]})
	}
	return tree, nil
}

func truncateSummary(text string, length int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= length {
		return text
	}
	return string(runes[:length])
}

// summaryFromPayload reads only the user text of the stored message list; it deliberately does not
// depend on the protocol model so the console read path stays free of wire types.
func summaryFromPayload(payload json.RawMessage, redactor observe.Redactor) string {
	var record struct {
		Items []struct {
			Kind  string `json:"kind"`
			Role  string `json:"role"`
			Parts []struct {
				Kind string `json:"kind"`
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"items"`
	}
	if err := json.Unmarshal(payload, &record); err != nil {
		return ""
	}
	messages := record.Items
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "user" || messages[index].Kind != "message" {
			continue
		}
		for position := len(messages[index].Parts) - 1; position >= 0; position-- {
			if text := messages[index].Parts[position].Text; text != "" {
				if redactor != nil {
					text, _ = redactor(text, "summary")
				}
				return truncateSummary(text, 120)
			}
		}
	}
	return ""
}

func (service *CallService) Conversations(ctx context.Context, actor Actor, projectID string, cursor Cursor) (Page[entity.Conversation], error) {
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
