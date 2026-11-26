package console

import (
	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// ConversationView is one row of the conversation listing. Its counts only include conversational
// lanes; the session page itself is the trajectory.
type ConversationView struct {
	ID          string `json:"id" required:"true"`
	FirstCallID string `json:"first_call_id" required:"true"`
	LastCallID  string `json:"last_call_id" required:"true"`
	Calls       int    `json:"calls" required:"true"`
	MaxSeverity string `json:"max_severity" required:"true"`
	CreatedAt   string `json:"created_at" required:"true"`
	LastAt      string `json:"last_at" required:"true"`
}

type ConversationPage struct {
	Items []ConversationView `json:"items" required:"true"`
	Next  *CursorView        `json:"next,omitempty"`
}

type listConversationsInput struct {
	projectPath
	pageQuery
}

type conversationPath struct {
	ProjectID string `path:"project_id" json:"-" required:"true"`
	SessionID string `path:"session_id" json:"-" required:"true"`
}

func conversationView(value *entity.Conversation) ConversationView {
	return ConversationView{ID: value.ID, FirstCallID: value.FirstCallID, LastCallID: value.LastCallID, Calls: value.Calls, MaxSeverity: value.MaxSeverity, CreatedAt: stamp(value.CreatedAt), LastAt: stamp(value.LastAt)}
}

func (console *Console) registerConversations() {
	console.add("GET", "/projects/{project_id}/conversations", "listConversations", listConversationsInput{}, ConversationPage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		position, err := cursor(ctx)
		if err != nil {
			return nil, err
		}
		page, err := console.Calls.Conversations(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), position)
		if err != nil {
			return nil, err
		}
		return ConversationPage{Items: viewAll(page.Items, conversationView), Next: cursorView(page.Next)}, nil
	})
}
