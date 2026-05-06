// Package ptrto provides pointers to constants.
package ptrto

import (
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/util/timeutil"
)

var log = logging.MustGetLogger("ptrto") //nolint:unused

func TimeNow() *time.Time {
	now := time.Now()
	return &now
}

func TimeNowInLocal() *time.Time {
	now := time.Now().In(timeutil.LocalLocation())
	return &now
}

func Value[Type any](value Type) *Type {
	return &value
}
