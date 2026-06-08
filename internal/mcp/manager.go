package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/tools"
)

// discoveryTtl bounds how long a server's discovered tool list is reused before
// the manager re-queries the server. It keeps run startup cheap without letting
// the tool list go stale for long.
const discoveryTtl = 5 * time.Minute

// Manager discovers tools from remote MCP servers and registers them into a
// tool registry. Discovered tool lists are cached per server for discoveryTtl
// so that successive runs do not re-query every server. Tool invocation itself
// is not cached: each call opens a fresh session (see toolAdapter).
type Manager struct {
	mutex sync.Mutex
	ttl   time.Duration
	cache map[string]cacheEntry
	// now is injectable for tests; defaults to time.Now.
	now func() time.Time
}

type cacheEntry struct {
	tools     []RemoteTool
	expiresAt time.Time
}

// NewManager creates a Manager with the default discovery TTL.
func NewManager() *Manager {
	return &Manager{
		ttl:   discoveryTtl,
		cache: make(map[string]cacheEntry),
		now:   time.Now,
	}
}

// defaultManager backs the package-level RegisterConfiguredTools convenience.
var defaultManager = NewManager()

// RegisterTools connects to each server, discovers its tools, and registers
// namespaced adapters into the registry. It is best-effort: a server that fails
// to connect or list tools is logged and skipped so a broken MCP server never
// blocks a run or breaks existing tools.
func (self *Manager) RegisterTools(ctx context.Context, registry *tools.ToolRegistry, servers []ServerConfiguration) {
	if registry == nil {
		return
	}
	for _, server := range servers {
		remoteTools, err := self.discover(ctx, server)
		if err != nil {
			log.Warningf("mcp: skipping server %q: %v", server.Name, err)
			continue
		}
		for _, remote := range remoteTools {
			if strings.TrimSpace(remote.Name) == "" {
				continue
			}
			registry.Register(newToolAdapter(server, remote))
		}
		log.Infof("mcp: registered %d tools from server %q", len(remoteTools), server.Name)
	}
}

// discover returns the server's tools, using the cache when a fresh entry
// exists and otherwise querying the server and refreshing the cache.
func (self *Manager) discover(ctx context.Context, server ServerConfiguration) ([]RemoteTool, error) {
	signature := serverSignature(server)

	self.mutex.Lock()
	if entry, ok := self.cache[signature]; ok && self.now().Before(entry.expiresAt) {
		self.mutex.Unlock()
		return entry.tools, nil
	}
	self.mutex.Unlock()

	// Bound discovery so a slow server does not stall run startup beyond its
	// configured timeout.
	discoveryContext, cancel := context.WithTimeout(ctx, server.Timeout)
	defer cancel()

	client := NewClient(server)
	if connectError := client.Connect(discoveryContext); connectError != nil {
		return nil, connectError
	}
	remoteTools, listError := client.ListTools(discoveryContext)
	if listError != nil {
		return nil, listError
	}

	self.mutex.Lock()
	self.cache[signature] = cacheEntry{tools: remoteTools, expiresAt: self.now().Add(self.ttl)}
	self.mutex.Unlock()
	return remoteTools, nil
}

// serverSignature identifies a server configuration for cache keying. It
// includes the auth value so a credential change invalidates the cache.
func serverSignature(server ServerConfiguration) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d", server.Name, server.URL, server.Authorization, server.Timeout)
}
