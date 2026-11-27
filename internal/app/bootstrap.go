// Package app is the composition root: it wires the store, gateway, console services and HTTP
// transports, and owns process lifecycle and dependency boundaries.
package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// Bootstrap creates an administrator when none is enabled, which covers a fresh database and a
// first start that stopped before the account was written, and prints the password once. The
// password deliberately goes to stdout only: it is a one-time credential, not a log line.
func Bootstrap(ctx context.Context, database *repository.Store) error {
	exists, err := database.AdministratorExists(ctx)
	if err != nil || exists {
		return err
	}
	password := secure.Random(18)
	hash, err := secure.HashPassword(password)
	if err != nil {
		return err
	}
	user := &entity.User{ID: secure.Random(12), Username: bootstrapUsername(ctx, database), PasswordHash: hash, Administrator: true, MustChangePassword: true, CreatedAt: time.Now().UTC()}
	if err := database.CreateUser(ctx, user); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Initial administrator: %s / %s\n", user.Username, password)
	return nil
}

// bootstrapUsername is "admin" unless that name is taken by an account that is not an enabled
// administrator, in which case a suffixed name avoids clobbering it.
func bootstrapUsername(ctx context.Context, database *repository.Store) string {
	if taken, err := database.UsernameExists(ctx, "admin"); err == nil && !taken {
		return "admin"
	}
	return "admin-" + strings.ToLower(secure.Random(3))
}
