package console

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

type LoginInput struct {
	Username string `json:"username" required:"true"`
	Password string `json:"password" required:"true"`
}

type PasswordInput struct {
	Current string `json:"current" required:"true"`
	Next    string `json:"next" required:"true"`
}

type SessionView struct {
	User UserView `json:"user" required:"true"`
}

func (console *Console) registerSession() {
	console.addAnonymous("POST", "/session", "login", LoginInput{}, SessionView{}, func(ctx *gin.Context, _ *Identity) (any, error) {
		var input LoginInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		if len(input.Username) > 64 {
			return nil, errInvalidRequest
		}
		attempt := ctx.ClientIP() + "\x00" + input.Username
		if console.loginLimit.Blocked(attempt) {
			return nil, errRateLimited
		}
		identity, err := console.Account.Login(ctx.Request.Context(), input.Username, input.Password)
		if err != nil {
			console.loginLimit.Record(attempt)
			slog.Warn("console login failed", "username", input.Username, "ip", ctx.ClientIP())
			return nil, err
		}
		console.loginLimit.Reset(attempt)
		console.setSessionCookie(ctx, identity.Token)
		console.recordOperation(ctx, "login", &Identity{User: identity.User, Token: identity.Token})
		return SessionView{User: userView(identity.User)}, nil
	})
	console.add("GET", "/session", "me", nil, SessionView{}, func(_ *gin.Context, identity *Identity) (any, error) {
		return SessionView{User: userView(identity.User)}, nil
	})
	console.add("DELETE", "/session", "logout", nil, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		if err := console.Account.Logout(ctx.Request.Context(), identity.Token); err != nil {
			return nil, err
		}
		console.clearSessionCookie(ctx)
		return nil, nil
	})
	console.add("PUT", "/session/password", "changePassword", PasswordInput{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input PasswordInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		attempt := "password\x00" + identity.User.ID
		if console.loginLimit.Blocked(attempt) {
			return nil, errRateLimited
		}
		if err := console.Account.ChangePassword(ctx.Request.Context(), identity.User.ID, identity.Token, input.Current, input.Next); err != nil {
			console.loginLimit.Record(attempt)
			return nil, err
		}
		console.loginLimit.Reset(attempt)
		return nil, nil
	})
}
