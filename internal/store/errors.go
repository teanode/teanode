package store

import "errors"

var (
	ErrNotFound       = errors.New("not found")
	ErrAlreadyExists  = errors.New("already exists")
	ErrInvalidOptions = errors.New("invalid options")
	ErrNotImplemented = errors.New("not implemented")
)
