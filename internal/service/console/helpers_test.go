package console

import "errors"

func isKind(err error, kind Kind) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Kind == kind
}
