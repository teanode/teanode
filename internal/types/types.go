package types

import "encoding/json"

// RequestFrame is a client-to-server RPC request.
type RequestFrame struct {
	Type   string          `json:"type"`   // "req"
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// ResponseFrame is a server-to-client RPC response.
type ResponseFrame struct {
	Type    string      `json:"type"` // "res"
	ID      string      `json:"id"`
	OK      bool        `json:"ok"`
	Payload interface{} `json:"payload,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// EventFrame is a server-to-client push event.
type EventFrame struct {
	Type    string      `json:"type"`  // "event"
	Event   string      `json:"event"`
	Payload interface{} `json:"payload,omitempty"`
}

// RPCError describes an error in an RPC response.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
