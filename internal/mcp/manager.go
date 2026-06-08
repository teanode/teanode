package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/tools"
)

// discoveryTtl bounds how long a server's discovered tool list is reused before
// the manager re-queries the server. It keeps run startup cheap without letting
// the tool list go stale for long.
const discoveryTtl = 5 * time.Minute

// discoveryAttempts is how many times discovery of a single server is attempted
// before giving up. Discovery (initialize + tools/list) is idempotent, so it is
// safe to retry transient failures; tool invocation is never retried.
const discoveryAttempts = 3

// discoveryBackoffBase is the base delay between discovery retries. The delay
// grows exponentially per attempt (base, 2*base, ...).
const discoveryBackoffBase = 250 * time.Millisecond

// Manager discovers tools from remote MCP servers and registers them into a
// tool registry. Discovered tool lists are cached per server for discoveryTtl
// so that successive runs do not re-query every server. Tool invocation itself
// is not cached: each call opens a fresh session (see toolAdapter).
type Manager struct {
	mutex       sync.Mutex
	ttl         time.Duration
	cache       map[string]cacheEntry
	attempts    int
	backoffBase time.Duration
	// now is injectable for tests; defaults to time.Now.
	now func() time.Time
	// sleep is injectable for tests so retry backoff does not slow them down. It
	// returns false when the wait was cut short by context cancellation.
	sleep func(ctx context.Context, duration time.Duration) bool
}

type cacheEntry struct {
	tools     []RemoteTool
	expiresAt time.Time
}

// ServerDiscovery is the outcome of attempting to discover one server's tools.
// It lets callers reflect discovery results back onto per-user connection state.
type ServerDiscovery struct {
	Server    ServerConfiguration
	ToolCount int
	// Cached is true when the result came from the discovery cache rather than a
	// fresh network probe. Callers skip status writes for cached results so run
	// startup stays cheap when nothing was actually queried.
	Cached bool
	Err    error
}

// NewManager creates a Manager with the default discovery TTL.
func NewManager() *Manager {
	return &Manager{
		ttl:         discoveryTtl,
		cache:       make(map[string]cacheEntry),
		attempts:    discoveryAttempts,
		backoffBase: discoveryBackoffBase,
		now:         time.Now,
		sleep:       sleepWithContext,
	}
}

// sleepWithContext waits for duration unless ctx is cancelled first, returning
// false when interrupted.
func sleepWithContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// defaultManager backs the package-level RegisterConfiguredTools convenience.
var defaultManager = NewManager()

// RegisterTools connects to each server, discovers its tools, and registers
// namespaced adapters into the registry. It is best-effort: a server that fails
// to connect or list tools is logged and skipped so a broken MCP server never
// blocks a run or breaks existing tools. The returned outcomes let the caller
// update per-user connection status from the discovery result.
func (self *Manager) RegisterTools(ctx context.Context, registry *tools.ToolRegistry, servers []ServerConfiguration) []ServerDiscovery {
	if registry == nil {
		return nil
	}
	outcomes := make([]ServerDiscovery, 0, len(servers))
	for _, server := range servers {
		remoteTools, cached, err := self.discover(ctx, server)
		outcome := ServerDiscovery{Server: server, Cached: cached, Err: err}
		if err != nil {
			log.Warningf("mcp: skipping server %q: %v", server.Name, err)
			outcomes = append(outcomes, outcome)
			continue
		}
		registered := 0
		for _, remote := range remoteTools {
			if strings.TrimSpace(remote.Name) == "" {
				continue
			}
			registry.Register(newToolAdapter(server, remote))
			registered++
		}
		outcome.ToolCount = registered
		outcomes = append(outcomes, outcome)
		source := "discovered"
		if cached {
			source = "cached"
		}
		log.Infof("mcp: registered %d tools from server %q (%s)", registered, server.Name, source)
	}
	return outcomes
}

// discover returns the server's tools, using the cache when a fresh entry
// exists and otherwise probing the server (with bounded retries) and refreshing
// the cache. The second return value reports whether the result was served from
// the cache.
func (self *Manager) discover(ctx context.Context, server ServerConfiguration) ([]RemoteTool, bool, error) {
	signature := serverSignature(server)

	self.mutex.Lock()
	if entry, ok := self.cache[signature]; ok && self.now().Before(entry.expiresAt) {
		self.mutex.Unlock()
		return entry.tools, true, nil
	}
	self.mutex.Unlock()

	remoteTools, err := self.probe(ctx, server)
	if err != nil {
		return nil, false, err
	}

	self.mutex.Lock()
	self.cache[signature] = cacheEntry{tools: remoteTools, expiresAt: self.now().Add(self.ttl)}
	self.mutex.Unlock()
	return remoteTools, false, nil
}

// probe performs a single discovery attempt against the server, retrying
// transient failures with exponential backoff up to self.attempts times. Each
// attempt is bounded by the server's configured timeout.
func (self *Manager) probe(ctx context.Context, server ServerConfiguration) ([]RemoteTool, error) {
	var lastError error
	for attempt := 0; attempt < self.attempts; attempt++ {
		if attempt > 0 {
			backoff := self.backoffBase << (attempt - 1)
			if !self.sleep(ctx, backoff) {
				return nil, ctx.Err()
			}
			log.Infof("mcp: retrying discovery of %q (attempt %d/%d)", server.Name, attempt+1, self.attempts)
		}

		remoteTools, attemptError := self.probeOnce(ctx, server)
		if attemptError == nil {
			return remoteTools, nil
		}
		lastError = attemptError

		// Stop immediately if the caller's context is done, or if the failure is
		// not the kind that retrying could fix (e.g. an auth rejection).
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !isRetryableError(attemptError) {
			return nil, attemptError
		}
	}
	return nil, lastError
}

// probeOnce runs one connect + tools/list against the server, bounded by the
// server timeout.
func (self *Manager) probeOnce(ctx context.Context, server ServerConfiguration) ([]RemoteTool, error) {
	discoveryContext, cancel := context.WithTimeout(ctx, server.Timeout)
	defer cancel()

	client := NewClient(server)
	if connectError := client.Connect(discoveryContext); connectError != nil {
		return nil, connectError
	}
	return client.ListTools(discoveryContext)
}

// isRetryableError reports whether a discovery failure is transient and worth
// retrying. Server-side HTTP errors are retried only for status codes that
// indicate a transient or overloaded condition; client errors such as 401/403
// (bad or missing credential) are not retried because the credential will not
// change between attempts. Transport-level errors (refused connections, resets,
// timeouts) are always retried.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var statusError *httpStatusError
	if errors.As(err, &statusError) {
		switch statusError.StatusCode {
		case 408, 425, 429, 500, 502, 503, 504:
			return true
		default:
			return false
		}
	}
	// A JSON-RPC error reported by the server is an application-level response,
	// not a transport failure: retrying will produce the same result.
	var rpcErr *jsonrpcError
	if errors.As(err, &rpcErr) {
		return false
	}
	// Treat remaining transport-level errors (dial/reset/EOF) as retryable.
	var netError net.Error
	if errors.As(err, &netError) {
		return true
	}
	// Decoding failures and other unclassified errors are conservatively treated
	// as transient: a single extra attempt is cheap and bounded.
	return true
}

// serverSignature identifies a server configuration for cache keying. It
// includes the auth value so a credential change invalidates the cache. The
// per-user ConnectionID is intentionally excluded: it does not affect the tool
// list a server returns.
func serverSignature(server ServerConfiguration) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d", server.Name, server.URL, server.Authorization, server.Timeout)
}
