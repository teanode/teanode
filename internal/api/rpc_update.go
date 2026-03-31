package api

import (
	"github.com/teanode/teanode/internal/lifecycle"
	"github.com/teanode/teanode/internal/updater"
)

// handleUpdateStatus returns the current updater status without triggering a check.
func (self *webSocketConnection) handleUpdateStatus(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}

	updateManager := updater.UpdaterFromContext(self.ctx)
	if updateManager == nil {
		return map[string]interface{}{
			"enabled": false,
		}, nil
	}

	status := updateManager.Status()
	return map[string]interface{}{
		"enabled": true,
		"status":  status,
	}, nil
}

// handleUpdateCheck triggers an immediate update check and returns the result.
func (self *webSocketConnection) handleUpdateCheck(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}

	updateManager := updater.UpdaterFromContext(self.ctx)
	if updateManager == nil {
		return nil, rpcError(503, "updater not available")
	}

	status := updateManager.Check(self.ctx)
	return map[string]interface{}{
		"status": status,
	}, nil
}

// handleUpdateApply downloads, verifies, and applies the latest update, then
// requests a restart.
func (self *webSocketConnection) handleUpdateApply(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}

	updateManager := updater.UpdaterFromContext(self.ctx)
	if updateManager == nil {
		return nil, rpcError(503, "updater not available")
	}

	if err := updateManager.Apply(self.ctx); err != nil {
		return nil, rpcError(500, "update failed: "+err.Error())
	}

	// RPC apply has no coordinator defer, fire the scheduled restart immediately.
	if lifecycleManager := lifecycle.LifecycleFromContext(self.ctx); lifecycleManager != nil {
		lifecycleManager.FirePendingLifecycle()
	}

	return map[string]interface{}{
		"ok":      true,
		"message": "update applied, restarting",
	}, nil
}
