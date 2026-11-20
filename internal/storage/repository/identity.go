package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

const (
	idleLifetime     = 2 * time.Hour
	absoluteLifetime = 7 * 24 * time.Hour
	touchInterval    = time.Minute
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type Identity struct {
	User    *entity.User
	Session *entity.Session
	Token   string
}

func (store *Store) Authenticate(ctx context.Context, username, password string) (*Identity, error) {
	user := new(entity.User)
	err := store.DB.NewSelect().Model(user).Where("username = ?", username).Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	known := err == nil
	hash := ""
	if known {
		hash = user.PasswordHash
	}
	if !secure.VerifyPassword(hash, password) || !known || user.Disabled {
		return nil, ErrInvalidCredentials
	}
	token := secure.Random(32)
	now := time.Now().UTC()
	session := &entity.Session{ID: secure.Digest(token), UserID: user.ID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(absoluteLifetime)}
	if _, err := store.DB.NewInsert().Model(session).Exec(ctx); err != nil {
		return nil, err
	}
	return &Identity{User: user, Session: session, Token: token}, nil
}

func (store *Store) Session(ctx context.Context, token string) (*Identity, error) {
	if token == "" {
		return nil, ErrInvalidCredentials
	}
	session := new(entity.Session)
	if err := store.DB.NewSelect().Model(session).Where("id = ?", secure.Digest(token)).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	now := time.Now().UTC()
	if now.After(session.ExpiresAt) || now.Sub(session.LastSeenAt) > idleLifetime {
		_, _ = store.DB.NewDelete().Model(session).WherePK().Exec(ctx)
		return nil, ErrInvalidCredentials
	}
	user := new(entity.User)
	if err := store.DB.NewSelect().Model(user).Where("id = ?", session.UserID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if user.Disabled {
		return nil, ErrInvalidCredentials
	}
	if now.Sub(session.LastSeenAt) > touchInterval {
		session.LastSeenAt = now
		_, _ = store.DB.NewUpdate().Model(session).Column("last_seen_at").WherePK().Exec(ctx)
	}
	return &Identity{User: user, Session: session, Token: token}, nil
}

func (store *Store) Logout(ctx context.Context, token string) error {
	_, err := store.DB.NewDelete().Model((*entity.Session)(nil)).Where("id = ?", secure.Digest(token)).Exec(ctx)
	return err
}

// ChangePassword replaces the password and ends every other session of the user; the session
// identified by keepToken stays signed in.
func (store *Store) ChangePassword(ctx context.Context, userID, keepToken, current, next string) error {
	user := new(entity.User)
	if err := store.DB.NewSelect().Model(user).Where("id = ?", userID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidCredentials
		}
		return err
	}
	if !secure.VerifyPassword(user.PasswordHash, current) {
		return ErrInvalidCredentials
	}
	hash, err := secure.HashPassword(next)
	if err != nil {
		return err
	}
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		if _, err := transaction.NewUpdate().Model((*entity.User)(nil)).Set("password_hash = ?, must_change_password = false", hash).Where("id = ?", userID).Exec(ctx); err != nil {
			return err
		}
		_, err := transaction.NewDelete().Model((*entity.Session)(nil)).Where("user_id = ? AND id <> ?", userID, secure.Digest(keepToken)).Exec(ctx)
		return err
	})
}
