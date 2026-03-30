package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/lifecycle"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/version"
)

// Policy controls the auto-update behavior.
type Policy string

const (
	// PolicyDisabled disables update checking entirely.
	PolicyDisabled Policy = "disabled"
	// PolicyNotify checks for updates and notifies but does not apply them.
	PolicyNotify Policy = "notify"
	// PolicyAuto checks for updates, downloads, and applies them automatically.
	PolicyAuto Policy = "auto"
)

// DefaultCheckInterval is the default interval between periodic update checks.
const DefaultCheckInterval = 6 * time.Hour

// configRefreshInterval bounds how long runtime config changes take to be
// observed by the background updater loop.
const configRefreshInterval = time.Minute

// Status represents the current state of the updater.
type Status struct {
	// Available is the latest release info if an update is available, or nil.
	Available *ReleaseInfo `json:"available,omitempty"`
	// CurrentVersion is the running version.
	CurrentVersion string `json:"currentVersion"`
	// LatestVersion is the latest release version, or empty if unknown.
	LatestVersion string `json:"latestVersion,omitempty"`
	// UpdateAvailable is true when LatestVersion > CurrentVersion.
	UpdateAvailable bool `json:"updateAvailable"`
	// LastChecked is the time of the most recent check.
	LastChecked *time.Time `json:"lastChecked,omitempty"`
	// Error is the last error message, if any.
	Error string `json:"error,omitempty"`
	// IsContainer is true if the process appears to run in a container.
	IsContainer bool `json:"isContainer"`
	// Policy is the configured update policy.
	Policy Policy `json:"policy"`
}

// Updater manages periodic update checking and staged update application.
// It reads the current configuration from the store on each check cycle,
// so runtime config changes take effect without a restart.
type Updater struct {
	mutex       sync.RWMutex
	status      Status
	stopChannel chan struct{}
	isContainer bool
}

// New creates a new Updater. Configuration (policy, interval) is read from the
// store on each check cycle, so no policy or interval arguments are needed.
func New() *Updater {
	isContainer := IsContainerEnvironment()
	return &Updater{
		status: Status{
			CurrentVersion: version.Version(),
			IsContainer:    isContainer,
			Policy:         PolicyNotify, // default until first config read
		},
		stopChannel: make(chan struct{}),
		isContainer: isContainer,
	}
}

// Start begins periodic update checking in the background.
// The updater reads config from the store on each cycle to pick up runtime changes.
func (self *Updater) Start(ctx context.Context) {
	if self.isContainer {
		log.Info("updater: container environment detected, disabling self-update")
		return
	}

	go self.run(ctx)
	log.Info("updater: started background manager")
}

// Stop halts the periodic checker.
func (self *Updater) Stop() {
	select {
	case <-self.stopChannel:
	default:
		close(self.stopChannel)
	}
}

// Status returns the current updater status.
func (self *Updater) Status() Status {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.status
}

// Check performs an immediate update check and returns the status.
func (self *Updater) Check(ctx context.Context) Status {
	self.applyRuntimeConfig(ctx)
	self.check(ctx)
	return self.Status()
}

// Apply downloads, verifies, and applies the latest available update. Returns
// an error if no update is available or if the apply fails.
// After a successful apply, it requests a lifecycle restart.
func (self *Updater) Apply(ctx context.Context) error {
	if self.isContainer {
		return fmt.Errorf("self-update is not supported in container environments")
	}
	if err := ValidateApplyEnvironment(); err != nil {
		return err
	}

	self.mutex.RLock()
	available := self.status.Available
	self.mutex.RUnlock()

	if available == nil {
		// Run a fresh check.
		self.check(ctx)
		self.mutex.RLock()
		available = self.status.Available
		self.mutex.RUnlock()
	}

	if available == nil {
		return fmt.Errorf("no update available")
	}

	log.Infof("updater: downloading and verifying %s", available.Version())
	result, err := DownloadAndVerify(ctx, available)
	if err != nil {
		return fmt.Errorf("download/verify failed: %w", err)
	}

	// Clean up staging directory on failure.
	stageDirectory := filepath.Dir(result.StagedPath)
	defer func() { _ = os.RemoveAll(stageDirectory) }()

	log.Infof("updater: applying update (checksum=%s)", result.Checksum)
	if err := Apply(result.StagedPath); err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	log.Noticef("updater: update applied successfully, version %s → %s", version.Version(), available.Version())

	// Request restart via lifecycle manager.
	if lifecycleManager := lifecycle.LifecycleFromContext(ctx); lifecycleManager != nil {
		lifecycleManager.ScheduleLifecycle(lifecycle.Restart)
		lifecycleManager.FirePendingLifecycle()
	}

	return nil
}

// readConfig reads the current update configuration from the store.
// Returns the resolved policy and check interval. Falls back to safe defaults
// if the store is unavailable or the config section is absent.
func (self *Updater) readConfig(ctx context.Context) (Policy, time.Duration) {
	policy := PolicyNotify
	checkInterval := DefaultCheckInterval

	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return policy, checkInterval
	}

	var configuration *models.Configuration
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		loaded, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		configuration = loaded
		return nil
	})

	if configuration == nil || configuration.Updating == nil {
		return policy, checkInterval
	}

	updateConfig := configuration.Updating
	if configuredPolicy := updateConfig.GetPolicy(); configuredPolicy != "" {
		candidate := Policy(configuredPolicy)
		if IsValidPolicy(candidate) {
			policy = candidate
		}
	}
	if hours := updateConfig.GetCheckIntervalHours(); hours > 0 {
		checkInterval = time.Duration(hours) * time.Hour
	}

	return policy, checkInterval
}

// applyRuntimeConfig refreshes the updater status from the current config and
// returns the effective policy plus the configured interval.
func (self *Updater) applyRuntimeConfig(ctx context.Context) (Policy, time.Duration) {
	policy, checkInterval := self.readConfig(ctx)
	self.mutex.RLock()
	previousPolicy := self.status.Policy
	self.mutex.RUnlock()
	if runtime.GOOS == "windows" && policy == PolicyAuto {
		if previousPolicy != PolicyNotify {
			log.Warning("updater: auto-apply is not supported on Windows; falling back to notify-only")
		}
		policy = PolicyNotify
	}

	self.mutex.Lock()
	self.status.Policy = policy
	self.mutex.Unlock()

	return policy, checkInterval
}

func (self *Updater) shouldCheck(checkInterval time.Duration) bool {
	self.mutex.RLock()
	lastChecked := self.status.LastChecked
	self.mutex.RUnlock()

	if lastChecked == nil {
		return true
	}
	return time.Since(*lastChecked) >= checkInterval
}

// run is the periodic check loop. On each cycle it reads the current config
// from the store, so policy and interval changes take effect at runtime.
func (self *Updater) run(ctx context.Context) {
	// Initial check shortly after startup.
	initialDelay := 30 * time.Second
	select {
	case <-time.After(initialDelay):
	case <-self.stopChannel:
		return
	case <-ctx.Done():
		return
	}

	self.runCycle(ctx)

	ticker := time.NewTicker(configRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			self.runCycle(ctx)
		case <-self.stopChannel:
			return
		case <-ctx.Done():
			return
		}
	}
}

// runCycle performs a single check-and-maybe-apply cycle, reading fresh config.
func (self *Updater) runCycle(ctx context.Context) {
	policy, checkInterval := self.applyRuntimeConfig(ctx)

	if policy == PolicyDisabled {
		return
	}

	if !self.shouldCheck(checkInterval) {
		return
	}

	self.check(ctx)

	if policy == PolicyAuto {
		self.mutex.RLock()
		available := self.status.Available
		self.mutex.RUnlock()
		if available != nil {
			if err := self.Apply(ctx); err != nil {
				log.Errorf("updater: auto-apply failed: %v", err)
			}
		}
	}
}

// NormalizePolicy returns a supported update policy, defaulting to notify.
func NormalizePolicy(policy Policy) Policy {
	switch policy {
	case PolicyDisabled, PolicyNotify, PolicyAuto:
		return policy
	default:
		return PolicyNotify
	}
}

// IsValidPolicy reports whether the given update policy is supported.
func IsValidPolicy(policy Policy) bool {
	return policy == NormalizePolicy(policy)
}

// check performs a single update check and updates the status.
func (self *Updater) check(ctx context.Context) {
	release, err := CheckLatestRelease(ctx)
	now := time.Now()

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.status.LastChecked = &now

	if err != nil {
		self.status.Error = err.Error()
		log.Warningf("updater: check failed: %v", err)
		return
	}

	self.status.Error = ""
	self.status.LatestVersion = release.Version()

	newer, err := IsNewer(release.Version(), version.Version())
	if err != nil {
		self.status.Error = fmt.Sprintf("version comparison failed: %v", err)
		return
	}

	self.status.UpdateAvailable = newer
	if newer {
		self.status.Available = release
		log.Infof("updater: update available: %s → %s", version.Version(), release.Version())
	} else {
		self.status.Available = nil
	}
}
