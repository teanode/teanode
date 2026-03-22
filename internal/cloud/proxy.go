package cloud

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
)

// HandleProxyStream routes an incoming cloud proxy stream based on its metadata.
// The ctx should carry the authenticated proxy user context. WebSocket-type
// streams are handled by the websocketHandler callback using StreamTransport.
// HTTP-type streams receive an HTTP/1.1 request/response exchange served by the
// given http.Handler with the user context attached.
func HandleProxyStream(ctx context.Context, metadata *StreamMetadata, stream io.ReadWriteCloser, httpHandler http.Handler, websocketHandler func()) {
	// Only allow /api/ paths.
	if !strings.HasPrefix(metadata.Path, "/api/") {
		log.Warningf("cloud proxy: rejected non-api path %q", metadata.Path)
		return
	}

	switch metadata.Type {
	case StreamTypeWebSocket:
		websocketHandler()
	case StreamTypeHTTP:
		handleHttpStream(ctx, stream, httpHandler)
	default:
		log.Warningf("cloud proxy: unknown stream type %q", metadata.Type)
	}
}

// handleHttpStream reads an HTTP/1.1 request from the stream, serves it through
// the given handler, and writes the response back to the stream. The ctx carries
// the authenticated proxy user so that downstream handlers see the correct identity.
func handleHttpStream(ctx context.Context, stream io.ReadWriteCloser, handler http.Handler) {
	reader := bufio.NewReader(stream)

	request, err := http.ReadRequest(reader)
	if err != nil {
		log.Warningf("cloud proxy: failed to read HTTP request from stream: %v", err)
		return
	}
	defer func() { _ = request.Body.Close() }()

	// Attach the proxy user context to the request.
	request = request.WithContext(ctx)

	// Serve the request through the handler and capture the response.
	responseWriter := newStreamResponseWriter()
	handler.ServeHTTP(responseWriter, request)

	// Write the HTTP response back to the stream.
	response := responseWriter.toResponse(request)
	if err := response.Write(stream); err != nil {
		log.Warningf("cloud proxy: failed to write HTTP response to stream: %v", err)
	}
}

// streamResponseWriter captures an HTTP response in memory so it can be
// serialized back over the stream as HTTP/1.1.
type streamResponseWriter struct {
	statusCode int
	header     http.Header
	body       []byte
}

func newStreamResponseWriter() *streamResponseWriter {
	return &streamResponseWriter{
		statusCode: http.StatusOK,
		header:     make(http.Header),
	}
}

func (self *streamResponseWriter) Header() http.Header {
	return self.header
}

func (self *streamResponseWriter) Write(data []byte) (int, error) {
	self.body = append(self.body, data...)
	return len(data), nil
}

func (self *streamResponseWriter) WriteHeader(statusCode int) {
	self.statusCode = statusCode
}

func (self *streamResponseWriter) toResponse(request *http.Request) *http.Response {
	return &http.Response{
		StatusCode:    self.statusCode,
		Status:        http.StatusText(self.statusCode),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        self.header,
		Body:          io.NopCloser(bytes.NewReader(self.body)),
		ContentLength: int64(len(self.body)),
		Request:       request,
	}
}
