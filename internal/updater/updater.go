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
type Updater struct {
	policy        Policy
	checkInterval time.Duration

	mutex       sync.RWMutex
	status      Status
	stopChannel chan struct{}
	isContainer bool
}

// New creates a new Updater with the given policy and check interval.
// If checkInterval is zero, DefaultCheckInterval is used.
func New(policy Policy, checkInterval time.Duration) *Updater {
	if checkInterval <= 0 {
		checkInterval = DefaultCheckInterval
	}
	policy = NormalizePolicy(policy)

	isContainer := IsContainerEnvironment()
	return &Updater{
		policy:        policy,
		checkInterval: checkInterval,
		status: Status{
			CurrentVersion: version.Version(),
			IsContainer:    isContainer,
			Policy:         policy,
		},
		stopChannel: make(chan struct{}),
		isContainer: isContainer,
	}
}

// Start begins periodic update checking in the background.
// Does nothing if the policy is disabled or running in a container.
func (self *Updater) Start(ctx context.Context) {
	if self.policy == PolicyDisabled {
		log.Info("updater: disabled by policy")
		return
	}
	if runtime.GOOS == "windows" && self.policy == PolicyAuto {
		log.Warning("updater: auto-apply is not supported on Windows; falling back to notify-only checks")
		self.policy = PolicyNotify
		self.mutex.Lock()
		self.status.Policy = self.policy
		self.mutex.Unlock()
	}
	if self.isContainer {
		log.Info("updater: container environment detected, disabling auto-update")
		return
	}

	go self.run(ctx)
	log.Infof("updater: started (policy=%s, interval=%s)", self.policy, self.checkInterval)
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

// run is the periodic check loop.
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

	self.check(ctx)

	// If auto-apply and update available, apply it.
	if self.policy == PolicyAuto {
		self.mutex.RLock()
		available := self.status.Available
		self.mutex.RUnlock()
		if available != nil {
			if err := self.Apply(ctx); err != nil {
				log.Errorf("updater: auto-apply failed: %v", err)
			}
		}
	}

	ticker := time.NewTicker(self.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			self.check(ctx)
			if self.policy == PolicyAuto {
				self.mutex.RLock()
				available := self.status.Available
				self.mutex.RUnlock()
				if available != nil {
					if err := self.Apply(ctx); err != nil {
						log.Errorf("updater: auto-apply failed: %v", err)
					}
				}
			}
		case <-self.stopChannel:
			return
		case <-ctx.Done():
			return
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
