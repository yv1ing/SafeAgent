package console

import (
	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type MemberView struct {
	UserID        string `json:"user_id" required:"true"`
	Username      string `json:"username" required:"true"`
	Administrator bool   `json:"administrator" required:"true"`
	Disabled      bool   `json:"disabled" required:"true"`
}

type MemberInput struct {
	UserID string `json:"user_id" required:"true"`
}

type addMemberInput struct {
	projectPath
	MemberInput
}

type memberPath struct {
	ProjectID string `path:"project_id" json:"-" required:"true"`
	UserID    string `path:"user_id" json:"-" required:"true"`
}

func memberView(user *entity.User) MemberView {
	return MemberView{UserID: user.ID, Username: user.Username, Administrator: user.Administrator, Disabled: user.Disabled}
}

func (console *Console) registerMembers() {
	console.add("GET", "/projects/{project_id}/members", "listMembers", projectPath{}, []MemberView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		members, err := console.Account.Members(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
		if err != nil {
			return nil, err
		}
		return viewAll(members, memberView), nil
	})
	console.add("POST", "/projects/{project_id}/members", "addMember", addMemberInput{}, MemberView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input MemberInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		user, err := console.Account.AddMember(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), input.UserID)
		if err != nil {
			return nil, err
		}
		return memberView(user), nil
	})
	console.add("DELETE", "/projects/{project_id}/members/{user_id}", "removeMember", memberPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		_, err := console.Account.RemoveMember(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("user_id"))
		return nil, err
	})
}
