package repository

import (
	"context"
	"errors"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

const uniqueViolation = "23505"

// IsUniqueViolation reports whether err is PostgreSQL rejecting a duplicate key.
func IsUniqueViolation(err error) bool {
	var failure pgdriver.Error
	return errors.As(err, &failure) && failure.Field('C') == uniqueViolation
}

func (store *Store) Users(ctx context.Context) ([]entity.User, error) {
	users := []entity.User{}
	if err := store.DB.NewSelect().Model(&users).Order("username").Scan(ctx); err != nil {
		return nil, err
	}
	return users, nil
}

func (store *Store) CreateUser(ctx context.Context, user *entity.User) error {
	_, err := store.DB.NewInsert().Model(user).Exec(ctx)
	return err
}

func (store *Store) UserByID(ctx context.Context, id string) (*entity.User, error) {
	user := new(entity.User)
	if err := store.DB.NewSelect().Model(user).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return user, nil
}

func (store *Store) UpdateUser(ctx context.Context, id string, disabled, administrator bool) error {
	result, err := store.DB.NewUpdate().Model((*entity.User)(nil)).
		Set("disabled = ?, administrator = ?", disabled, administrator).
		Where("id = ?", id).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetPassword stores a new hash that must be changed at the next login and ends every session.
func (store *Store) ResetPassword(ctx context.Context, id, hash string) error {
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		result, err := transaction.NewUpdate().Model((*entity.User)(nil)).
			Set("password_hash = ?, must_change_password = true", hash).
			Where("id = ?", id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrNotFound
		}
		_, err = transaction.NewDelete().Model((*entity.Session)(nil)).Where("user_id = ?", id).Exec(ctx)
		return err
	})
}

// AdministratorExists reports whether any enabled administrator account exists.
func (store *Store) AdministratorExists(ctx context.Context) (bool, error) {
	return store.DB.NewSelect().Model((*entity.User)(nil)).Where("administrator AND NOT disabled").Exists(ctx)
}

func (store *Store) UsernameExists(ctx context.Context, username string) (bool, error) {
	return store.DB.NewSelect().Model((*entity.User)(nil)).Where("username = ?", username).Exists(ctx)
}
