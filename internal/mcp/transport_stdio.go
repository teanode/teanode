package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/util/deferutil"
)

// stdioShutdownGrace bounds how long close waits for a subprocess to exit after
// its stdin is closed before the process is killed. It is kept short so tearing
// down a hung server does not add much beyond the request that already failed: a
// well-behaved server exits near-instantly on stdin EOF.
const stdioShutdownGrace = 500 * time.Millisecond

// stdioTransport speaks the MCP stdio transport: newline-delimited JSON-RPC
// messages over a child process's stdin/stdout. The process is started lazily on
// first use and torn down by close. Like the HTTP transport it assumes a single
// in-flight request at a time (the Client drives one operation per session), so
// it forwards response-shaped messages in arrival order and drops
// server-initiated requests and notifications.
type stdioTransport struct {
	server ServerConfiguration

	mutex    sync.Mutex
	started  bool
	startErr error
	closed   bool
	command  *exec.Cmd
	stdin    io.WriteCloser

	// responses carries decoded response-shaped messages from the reader
	// goroutine; it is closed when stdout reaches EOF.
	responses chan *jsonrpcResponse
	// done is closed by close to release a reader blocked sending a response.
	done chan struct{}
	// readDone is closed by the reader goroutine when it returns, so close can
	// wait for stdout draining to finish before reaping the process.
	readDone chan struct{}

	readMutex sync.Mutex
	readErr   error
}

func newStdioTransport(server ServerConfiguration) *stdioTransport {
	return &stdioTransport{
		server:    server,
		responses: make(chan *jsonrpcResponse),
		done:      make(chan struct{}),
		readDone:  make(chan struct{}),
	}
}

// observeProtocolVersion is a no-op for stdio: the framing carries no per-message
// protocol version.
func (self *stdioTransport) observeProtocolVersion(string) {}

func (self *stdioTransport) roundTrip(ctx context.Context, payload []byte) (*jsonrpcResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Bound each request by the server's configured timeout even when the caller
	// passes a deadline-free context, matching the per-request timeout the HTTP
	// transport gets from http.Client so a hung subprocess cannot block forever.
	if self.server.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, self.server.Timeout)
		defer cancel()
	}
	if err := self.ensureStarted(); err != nil {
		return nil, err
	}
	if err := self.write(payload); err != nil {
		return nil, err
	}
	select {
	case response, ok := <-self.responses:
		if !ok {
			return nil, self.exitError()
		}
		return response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (self *stdioTransport) notify(ctx context.Context, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := self.ensureStarted(); err != nil {
		return err
	}
	return self.write(payload)
}

// ensureStarted launches the subprocess on first use, recording any failure so
// later calls return the same error rather than re-spawning.
func (self *stdioTransport) ensureStarted() error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.started {
		return self.startErr
	}
	self.started = true
	if strings.TrimSpace(self.server.Command) == "" {
		self.startErr = fmt.Errorf("mcp: stdio server %q has no command", self.server.Name)
		return self.startErr
	}

	command := exec.Command(self.server.Command, self.server.Arguments...)
	command.Dir = self.server.WorkingDir
	command.Env = stdioEnvironment(self.server.Environment)
	command.Stderr = newStdioLogWriter(self.server.Name)

	stdin, stdinError := command.StdinPipe()
	if stdinError != nil {
		self.startErr = fmt.Errorf("mcp: stdio server %q: stdin pipe: %w", self.server.Name, stdinError)
		close(self.readDone)
		return self.startErr
	}
	stdout, stdoutError := command.StdoutPipe()
	if stdoutError != nil {
		self.startErr = fmt.Errorf("mcp: stdio server %q: stdout pipe: %w", self.server.Name, stdoutError)
		close(self.readDone)
		return self.startErr
	}
	if startError := command.Start(); startError != nil {
		self.startErr = fmt.Errorf("mcp: stdio server %q: starting %q: %w", self.server.Name, self.server.Command, startError)
		close(self.readDone)
		return self.startErr
	}

	self.command = command
	self.stdin = stdin
	go self.readLoop(stdout)
	return nil
}

// write frames a payload as a single newline-terminated line on stdin. JSON-RPC
// messages are compact JSON, so they never contain an embedded newline.
func (self *stdioTransport) write(payload []byte) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.closed {
		return fmt.Errorf("mcp: stdio server %q: transport closed", self.server.Name)
	}
	if self.stdin == nil {
		return fmt.Errorf("mcp: stdio server %q: not started", self.server.Name)
	}
	line := make([]byte, 0, len(payload)+1)
	line = append(line, payload...)
	line = append(line, '\n')
	if _, err := self.stdin.Write(line); err != nil {
		return fmt.Errorf("mcp: stdio server %q: writing request: %w", self.server.Name, err)
	}
	return nil
}

// readLoop reads newline-delimited JSON-RPC messages from stdout and forwards
// response-shaped ones, dropping server-initiated requests and notifications. It
// uses a bufio.Reader rather than a bufio.Scanner so a large response line (for
// example a big tools/list schema or tool result) is not rejected by a fixed
// token-size cap — matching the unbounded HTTP JSON path.
func (self *stdioTransport) readLoop(stdout io.ReadCloser) {
	defer deferutil.Recover()
	defer close(self.readDone)
	defer func() { _ = stdout.Close() }()

	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		// ReadBytes returns a freshly allocated slice each call, so a decoded
		// json.RawMessage may safely reference it without an extra copy.
		line, readError := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			response := &jsonrpcResponse{}
			// Only replies to our requests (carrying a result or error) are
			// forwarded; server-initiated requests and notifications are dropped.
			if json.Unmarshal(trimmed, response) == nil && (response.Result != nil || response.Error != nil) {
				select {
				case self.responses <- response:
				case <-self.done:
					return
				}
			}
		}
		if readError != nil {
			self.readMutex.Lock()
			if readError != io.EOF {
				self.readErr = readError
			}
			self.readMutex.Unlock()
			close(self.responses)
			return
		}
	}
}

// exitError describes why the response channel closed without delivering a
// reply (the subprocess exited or its stdout failed).
func (self *stdioTransport) exitError() error {
	self.readMutex.Lock()
	readErr := self.readErr
	self.readMutex.Unlock()
	if readErr != nil {
		return fmt.Errorf("mcp: stdio server %q: reading response: %w", self.server.Name, readErr)
	}
	return fmt.Errorf("mcp: stdio server %q: process exited without a response", self.server.Name)
}

// close terminates the subprocess. It closes stdin to ask a well-behaved server
// to exit, waits for the reader to finish draining stdout, and kills the process
// if it overstays the shutdown grace period.
func (self *stdioTransport) close() error {
	self.mutex.Lock()
	if self.closed {
		self.mutex.Unlock()
		return nil
	}
	self.closed = true
	close(self.done)
	command := self.command
	stdin := self.stdin
	started := self.started
	self.mutex.Unlock()

	if !started || command == nil {
		return nil
	}
	if stdin != nil {
		_ = stdin.Close()
	}
	// First wait for the reader to drain stdout, so command.Wait does not close
	// the pipe out from under an in-progress read. Kill if it overstays.
	select {
	case <-self.readDone:
	case <-time.After(stdioShutdownGrace):
		_ = command.Process.Kill()
		<-self.readDone
	}
	// Then reap the process, killing it if it stopped producing output but has
	// not actually exited (a misbehaving server that closes stdout yet lingers),
	// so Close never blocks indefinitely.
	waited := make(chan error, 1)
	go func() {
		defer deferutil.Recover()
		waited <- command.Wait()
	}()
	select {
	case <-waited:
	case <-time.After(stdioShutdownGrace):
		_ = command.Process.Kill()
		<-waited
	}
	return nil
}

// stdioEnvironment builds the subprocess environment. With no extra entries it
// returns nil so the child inherits TeaNode's environment; otherwise the extra
// "KEY=VALUE" entries are appended to (and so override) the inherited set.
func stdioEnvironment(extra []string) []string {
	if len(extra) == 0 {
		return nil
	}
	environment := os.Environ()
	environment = append(environment, extra...)
	return environment
}

// stdioLogWriter forwards a subprocess's stderr to the package logger one line
// at a time so server diagnostics are visible without polluting stdout.
type stdioLogWriter struct {
	serverName string
	buffer     []byte
}

func newStdioLogWriter(serverName string) *stdioLogWriter {
	return &stdioLogWriter{serverName: serverName}
}

func (self *stdioLogWriter) Write(payload []byte) (int, error) {
	self.buffer = append(self.buffer, payload...)
	for {
		index := bytes.IndexByte(self.buffer, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimSpace(string(self.buffer[:index]))
		self.buffer = self.buffer[index+1:]
		if line != "" {
			log.Infof("mcp: stdio server %q: %s", self.serverName, line)
		}
	}
	return len(payload), nil
}
