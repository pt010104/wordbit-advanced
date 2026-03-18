package domain

import "errors"

var (
	ErrNotFound             = errors.New("not found")
	ErrUnauthorized         = errors.New("unauthorized")
	ErrForbidden            = errors.New("forbidden")
	ErrValidation           = errors.New("validation error")
	ErrNoActionableCard     = errors.New("no actionable card")
	ErrDuplicateClientEvent = errors.New("duplicate client event")
)
