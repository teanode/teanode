package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// HTTPError is an error with an associated HTTP status code.
type HTTPError struct {
	StatusCode int
	message    string
}

func (self *HTTPError) Error() string { return self.message }

// Predefined sentinel errors.
var (
	ErrBadRequest         = &HTTPError{StatusCode: 400, message: "bad request"}
	ErrUnauthorized       = &HTTPError{StatusCode: 401, message: "unauthorized"}
	ErrNotFound           = &HTTPError{StatusCode: 404, message: "not found"}
	ErrMethodNotAllowed   = &HTTPError{StatusCode: 405, message: "method not allowed"}
	ErrServiceUnavailable = &HTTPError{StatusCode: 503, message: "service unavailable"}
)

// Error creates an HTTPError with a custom message.
func Error(statusCode int, message string) *HTTPError {
	return &HTTPError{StatusCode: statusCode, message: message}
}

// Errorf creates an HTTPError with a formatted message.
func Errorf(statusCode int, format string, arguments ...interface{}) *HTTPError {
	return &HTTPError{StatusCode: statusCode, message: fmt.Sprintf(format, arguments...)}
}

// errorResponse is the JSON envelope written by WriteError.
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// WriteError writes a JSON error response. If err is an *HTTPError, uses its
// status code; otherwise defaults to 500.
func WriteError(writer http.ResponseWriter, err error) {
	statusCode := http.StatusInternalServerError
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		statusCode = httpError.StatusCode
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	json.NewEncoder(writer).Encode(errorResponse{
		Error: errorBody{
			Message: err.Error(),
			Code:    statusCode,
		},
	})
}

// HandlerFunc is like http.HandlerFunc but returns an error.
// Returned errors are automatically written as JSON error responses.
type HandlerFunc func(http.ResponseWriter, *http.Request) error

func (self HandlerFunc) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if err := self(writer, request); err != nil {
		WriteError(writer, err)
	}
}
