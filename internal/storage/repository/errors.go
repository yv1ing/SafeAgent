package repository

import (
	"errors"
)

var ErrVersionConflict = errors.New("record was modified by another session")

var ErrNotFound = errors.New("record not found")

var ErrLeaseLost = errors.New("the job lease was lost")

var ErrRuleLimit = errors.New("the rule limit was reached")
