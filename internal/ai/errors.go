package ai

import "errors"

type Error struct{ Code string }

func (e *Error) Error() string { return safeCode(e.Code) }
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && safeCode(e.Code) == safeCode(t.Code)
}
func safeCode(code string) string {
	switch code {
	case "INVALID_REQUEST", "UNAUTHENTICATED", "NOT_FOUND", "FORBIDDEN", "AI_CONFLICT", "AI_EXPIRED", "AI_BUSY", "RATE_LIMITED", "RATE_LIMIT_UNAVAILABLE", "AI_LIMIT", "MODEL_UNAVAILABLE", "MODEL_INVALID_RESPONSE", "MODEL_TIMEOUT", "INTERNAL_ERROR":
		return code
	}
	return "INTERNAL_ERROR"
}
func NewError(code string) error { return &Error{Code: safeCode(code)} }
func Code(err error) string {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return safeCode(e.Code)
	}
	return "INTERNAL_ERROR"
}
