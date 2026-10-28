package storage

import (
	"errors"

	"kept/server/internal/apperr"
)

func isKind(err error, k apperr.Kind) bool {
	var e *apperr.Error
	return errors.As(err, &e) && e.Kind == k
}
