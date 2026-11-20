package console

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	temporaryPasswordBytes = 18
	minimumPasswordBytes   = 14
	maximumPasswordBytes   = 72
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type AccountService struct {
	store *repository.Store
}

func NewAccountService(store *repository.Store) *AccountService { return &AccountService{store: store} }

func issuePassword() (string, string, error) {
	password := secure.Random(temporaryPasswordBytes)
	hash, err := secure.HashPassword(password)
	return password, hash, err
}

// Login reports bad credentials as unauthenticated; every other failure is an internal error.
func (service *AccountService) Login(ctx context.Context, username, password string) (*repository.Identity, error) {
	identity, err := service.store.Authenticate(ctx, username, password)
	if errors.Is(err, repository.ErrInvalidCredentials) {
		return nil, Unauthenticated("invalid_credentials")
	}
	return identity, err
}

// Session resolves a console cookie to the account it belongs to.
func (service *AccountService) Session(ctx context.Context, token string) (*entity.User, error) {
	identity, err := service.store.Session(ctx, token)
	if errors.Is(err, repository.ErrInvalidCredentials) {
		return nil, Unauthenticated("unauthenticated")
	}
	if err != nil {
		return nil, err
	}
	return identity.User, nil
}

func (service *AccountService) Logout(ctx context.Context, token string) error {
	return service.store.Logout(ctx, token)
}

func (service *AccountService) ChangePassword(ctx context.Context, userID, token, current, next string) error {
	if !ValidPassword(next) {
		return Invalid("weak_password")
	}
	err := service.store.ChangePassword(ctx, userID, token, current, next)
	if errors.Is(err, repository.ErrInvalidCredentials) {
		return Denied("invalid_credentials")
	}
	return err
}

// ValidPassword mirrors secure.HashPassword's bounds, which count bytes, so a rejected password is
// reported as weak rather than as a credential failure.
func ValidPassword(value string) bool {
	if len(value) < minimumPasswordBytes || len(value) > maximumPasswordBytes {
		return false
	}
	var lower, upper, digit bool
	for _, character := range value {
		switch {
		case unicode.IsLower(character):
			lower = true
		case unicode.IsUpper(character):
			upper = true
		case unicode.IsDigit(character):
			digit = true
		}
	}
	return lower && upper && digit
}

// validUsername trims value and accepts 1..64 characters from letters, digits, dot, underscore and hyphen.
func validUsername(value string) (string, error) {
	name := strings.TrimSpace(value)
	if !usernamePattern.MatchString(name) {
		return "", Invalid("invalid_username")
	}
	return name, nil
}

func (service *AccountService) Users(ctx context.Context, actor Actor) ([]entity.User, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	return service.store.Users(ctx)
}

type UserResult struct {
	User     *entity.User
	Password string
}

func (service *AccountService) Create(ctx context.Context, actor Actor, username string, administrator bool) (*UserResult, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	name, err := validUsername(username)
	if err != nil {
		return nil, err
	}
	password, hash, err := issuePassword()
	if err != nil {
		return nil, err
	}
	user := &entity.User{ID: secure.Random(12), Username: name, PasswordHash: hash, Administrator: administrator, MustChangePassword: true, CreatedAt: time.Now().UTC()}
	if err := service.store.CreateUser(ctx, user); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, Conflict("username_taken")
		}
		return nil, err
	}
	return &UserResult{User: user, Password: password}, nil
}

func (service *AccountService) Update(ctx context.Context, actor Actor, userID string, disabled, administrator bool) (*entity.User, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if userID == actor.UserID && (disabled || !administrator) {
		return nil, Invalid("cannot_demote_self")
	}
	if err := service.store.UpdateUser(ctx, userID, disabled, administrator); err != nil {
		return nil, Translate(err)
	}
	user, err := service.store.UserByID(ctx, userID)
	return user, Translate(err)
}

func (service *AccountService) ResetPassword(ctx context.Context, actor Actor, userID string) (*UserResult, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	password, hash, err := issuePassword()
	if err != nil {
		return nil, err
	}
	if err := service.store.ResetPassword(ctx, userID, hash); err != nil {
		return nil, Translate(err)
	}
	user, err := service.store.UserByID(ctx, userID)
	if err != nil {
		return nil, Translate(err)
	}
	return &UserResult{User: user, Password: password}, nil
}

// Members lists the accounts of a project; any member may read it, only administrators change it.
func (service *AccountService) Members(ctx context.Context, actor Actor, projectID string) ([]entity.User, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	return service.store.Members(ctx, projectID)
}

func (service *AccountService) AddMember(ctx context.Context, actor Actor, projectID, userID string) (*entity.User, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if _, err := service.store.Project(ctx, projectID); err != nil {
		return nil, Translate(err)
	}
	user, err := service.store.UserByID(ctx, userID)
	if err != nil {
		return nil, Translate(err)
	}
	if err := service.store.AddMember(ctx, projectID, userID); err != nil {
		return nil, err
	}
	return user, nil
}

func (service *AccountService) RemoveMember(ctx context.Context, actor Actor, projectID, userID string) (*entity.User, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	user, err := service.store.UserByID(ctx, userID)
	if err != nil {
		return nil, Translate(err)
	}
	if err := service.store.RemoveMember(ctx, projectID, userID); err != nil {
		return nil, Translate(err)
	}
	return user, nil
}
