package repository

import (
	"errors"
	"fmt"
)

var ErrVersionConflict = errors.New("record was modified by another session")

var ErrNotFound = errors.New("record not found")

var ErrLeaseLost = errors.New("the job lease was lost")

var ErrRuleLimit = errors.New("the rule limit was reached")

var ErrBuiltinProvider = errors.New("built-in providers cannot be deleted")

var ErrNotBuiltinProvider = errors.New("only built-in providers can be reset")

// ModelNameConflictError says which upstream name the provider catalog already holds, so a batch
// import can report the offending row instead of the whole transaction.
type ModelNameConflictError struct {
	UpstreamName string
}

func (conflict *ModelNameConflictError) Error() string {
	return fmt.Sprintf("a model named %q already exists for the provider", conflict.UpstreamName)
}

// RouteNameConflictError covers both route names and route aliases: both are model names a client
// resolves in one project, so both report the same conflict.
type RouteNameConflictError struct {
	Name string
}

func (conflict *RouteNameConflictError) Error() string {
	return fmt.Sprintf("the model name %q is already taken in this project", conflict.Name)
}
