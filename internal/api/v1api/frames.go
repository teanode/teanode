package v1api

import "encoding/json"

// requestFrame is a client-to-server RPC request.
type requestFrame struct {
	Type   string          `json:"type"` // "req"
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// responseFrame is a server-to-client RPC response.
type responseFrame struct {
	Type    string      `json:"type"` // "res"
	ID      string      `json:"id"`
	OK      bool        `json:"ok"`
	Payload interface{} `json:"payload,omitempty"`
	Error   *apiError   `json:"error,omitempty"`
}

// eventFrame is a server-to-client push event.
type eventFrame struct {
	Type    string      `json:"type"` // "event"
	Event   string      `json:"event"`
	Payload interface{} `json:"payload,omitempty"`
}

// apiError describes an error in an RPC response.
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
