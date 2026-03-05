// Package ptrto provides pointers to constants.
package ptrto

import (
	"time"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("ptrto") //nolint:unused

func TimeNow() *time.Time {
	now := time.Now()
	return &now
}

func TimeNowInLocal() *time.Time {
	now := time.Now().In(time.Local)
	return &now
}

func Value[Type any](value Type) *Type {
	return &value
}
