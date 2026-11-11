package knowledge

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidRequest  = errors.New("invalid request")
	ErrUnauthenticated = errors.New("authentication required")
	ErrForbidden       = errors.New("permission denied")
	ErrNotFound        = errors.New("resource not found")
	ErrConflict        = errors.New("document state conflict")
	ErrUnsupported     = errors.New("unsupported document type")
	ErrTooLarge        = errors.New("document exceeds size limit")
	ErrStorage         = errors.New("document operation failed")
	ErrSourceChanged   = fmt.Errorf("source changed: %w", ErrStorage)
	ErrUncertain       = errors.New("metadata commit outcome unknown")
)
