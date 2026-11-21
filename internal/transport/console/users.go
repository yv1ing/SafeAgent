package console

import (
	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type UserView struct {
	ID                 string `json:"id" required:"true"`
	Username           string `json:"username" required:"true"`
	Administrator      bool   `json:"administrator" required:"true"`
	MustChangePassword bool   `json:"must_change_password" required:"true"`
	Disabled           bool   `json:"disabled" required:"true"`
	CreatedAt          string `json:"created_at" required:"true"`
	InitialPassword    string `json:"initial_password,omitempty" description:"Returned once, when the account is created or its password is reset"`
}

type UserInput struct {
	Username      string `json:"username" required:"true"`
	Administrator bool   `json:"administrator"`
}

type UserUpdate struct {
	Disabled      bool `json:"disabled"`
	Administrator bool `json:"administrator"`
}

type userPath struct {
	UserID string `path:"user_id" json:"-" required:"true"`
}

type updateUserInput struct {
	userPath
	UserUpdate
}

func userView(user *entity.User) UserView {
	return UserView{ID: user.ID, Username: user.Username, Administrator: user.Administrator, MustChangePassword: user.MustChangePassword, Disabled: user.Disabled, CreatedAt: stamp(user.CreatedAt)}
}

func issuedUserView(result *service.UserResult) UserView {
	view := userView(result.User)
	view.InitialPassword = result.Password
	return view
}

func (console *Console) registerUsers() {
	console.add("GET", "/users", "listUsers", nil, []UserView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		users, err := console.Account.Users(ctx.Request.Context(), identity.actor())
		if err != nil {
			return nil, err
		}
		return viewAll(users, userView), nil
	})
	console.add("POST", "/users", "createUser", UserInput{}, UserView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input UserInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		created, err := console.Account.Create(ctx.Request.Context(), identity.actor(), input.Username, input.Administrator)
		if err != nil {
			return nil, err
		}
		return issuedUserView(created), nil
	})
	console.add("PUT", "/users/{user_id}", "updateUser", updateUserInput{}, UserView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input UserUpdate
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Account.Update(ctx.Request.Context(), identity.actor(), ctx.Param("user_id"), input.Disabled, input.Administrator)
		if err != nil {
			return nil, err
		}
		return userView(updated), nil
	})
	console.add("POST", "/users/{user_id}/password", "resetPassword", userPath{}, UserView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		result, err := console.Account.ResetPassword(ctx.Request.Context(), identity.actor(), ctx.Param("user_id"))
		if err != nil {
			return nil, err
		}
		return issuedUserView(result), nil
	})
}
