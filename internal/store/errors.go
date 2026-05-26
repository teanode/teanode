package store

import "errors"

var (
	ErrNotFound       = errors.New("store: not found")
	ErrAlreadyExists  = errors.New("store: already exists")
	ErrInvalidOptions = errors.New("store: invalid options")
	ErrNotImplemented = errors.New("store: not implemented")
)
