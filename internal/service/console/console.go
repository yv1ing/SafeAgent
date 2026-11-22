// Package console holds the business rules of the management API: who may act, what counts as a
// valid input and which repository calls a use case makes. Transport concerns stay in api/console.
package console

import (
	"context"
	"errors"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const MaximumNameLength = 120

// configurationMu serializes channel, model and route graph edits. The gateway runs one process
// (the database lease enforces that), so a process mutex is the whole coordination surface.
var configurationMu sync.Mutex

type Actor struct {
	UserID        string
	Administrator bool
}

// Kind classifies an expected failure; the transport layer decides how to present each kind.
type Kind int

const (
	KindInvalid Kind = iota + 1
	KindUnauthenticated
	KindForbidden
	KindNotFound
	KindConflict
)

// Failure is an expected outcome of a use case. Code is a stable machine-readable reason.
type Failure struct {
	Kind Kind
	Code string
}

func (failure *Failure) Error() string { return failure.Code }

func Invalid(code string) error         { return &Failure{Kind: KindInvalid, Code: code} }
func Unauthenticated(code string) error { return &Failure{Kind: KindUnauthenticated, Code: code} }
func Denied(code string) error          { return &Failure{Kind: KindForbidden, Code: code} }
func Forbidden() error                  { return Denied("forbidden") }
func NotFound() error                   { return &Failure{Kind: KindNotFound, Code: "not_found"} }
func Conflict(code string) error        { return &Failure{Kind: KindConflict, Code: code} }

// Translate maps repository outcomes onto service outcomes; services call it once per repository
// call so transport code never needs the repository's vocabulary. Other errors pass through and
// surface as internal errors.
func Translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrNotFound):
		return NotFound()
	case errors.Is(err, repository.ErrVersionConflict):
		return Conflict("version_conflict")
	}
	return err
}

func (actor Actor) RequireAdmin() error {
	if !actor.Administrator {
		return Forbidden()
	}
	return nil
}

// ProjectAccess is what RequireProject needs from storage.
type ProjectAccess interface {
	Project(ctx context.Context, id string) (*entity.Project, error)
	Membership(ctx context.Context, projectID, userID string) (bool, error)
}

// RequireProject admits administrators and members of an existing project. Everyone else gets the
// same not-found answer as for a project that does not exist, so ids cannot be probed.
func (actor Actor) RequireProject(ctx context.Context, access ProjectAccess, projectID string) error {
	if actor.Administrator {
		_, err := access.Project(ctx, projectID)
		return Translate(err)
	}
	member, err := access.Membership(ctx, projectID, actor.UserID)
	if err != nil {
		return err
	}
	if !member {
		return NotFound()
	}
	return nil
}

// RequireText trims value and requires 1..maximum characters without control characters.
func RequireText(value string, maximum int, code string) (string, error) {
	text, err := OptionalText(value, maximum, code)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", Invalid(code)
	}
	return text, nil
}

func OptionalText(value string, maximum int, code string) (string, error) {
	text := strings.TrimSpace(value)
	if utf8.RuneCountInString(text) > maximum || strings.IndexFunc(text, unicode.IsControl) >= 0 {
		return "", Invalid(code)
	}
	return text, nil
}

func RequireName(value, code string) (string, error) {
	return RequireText(value, MaximumNameLength, code)
}

func RequireVersion(version int64) error {
	if version <= 0 {
		return Invalid("version_required")
	}
	return nil
}
