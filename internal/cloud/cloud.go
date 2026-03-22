// Package cloud manages the WebSocket connection to a TeaNode cloud server.
package cloud

import (
	"io"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("cloud")

// StreamHandler is called for each incoming yamux stream from the cloud server.
// The metadata describes the stream type and target path.
type StreamHandler func(metadata *StreamMetadata, stream io.ReadWriteCloser)
