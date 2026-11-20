package console

import (
	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

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

type ConversationTreeView struct {
	Conversation ConversationView `json:"conversation" required:"true"`
	Nodes        []TreeNodeView   `json:"nodes" required:"true"`
	Truncated    bool             `json:"truncated" required:"true"`
}

type TreeNodeView struct {
	Call    CallView `json:"call" required:"true"`
	Summary string   `json:"summary" required:"true"`
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
	console.add("GET", "/projects/{project_id}/conversations/{session_id}", "getConversation", conversationPath{}, ConversationTreeView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		tree, err := console.Calls.Conversation(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("session_id"))
		if err != nil {
			return nil, err
		}
		nodes := make([]TreeNodeView, 0, len(tree.Nodes))
		for index := range tree.Nodes {
			nodes = append(nodes, TreeNodeView{Call: callView(&tree.Nodes[index].Call), Summary: tree.Nodes[index].Summary})
		}
		return ConversationTreeView{Conversation: conversationView(tree.Conversation), Nodes: nodes, Truncated: tree.Truncated}, nil
	})
}
