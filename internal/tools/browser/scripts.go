package browser

import "embed"

//go:embed scripts/*
var scriptFiles embed.FS

// mustReadScript reads a script from the embedded scripts directory.
// It panics if the file cannot be read, catching missing files at init time.
func mustReadScript(name string) string {
	data, err := scriptFiles.ReadFile("scripts/" + name)
	if err != nil {
		panic("browser: embedded script not found: " + name + ": " + err.Error())
	}
	return string(data)
}

// Page-injected scripts loaded from scripts/*.js at init time.
var (
	domSnapshotScript          = mustReadScript("dom_snapshot.js")
	navigationWaitStateScript  = mustReadScript("navigation_wait_state.js")
	networkIdleTrackerScript   = mustReadScript("network_idle_tracker.js")
	interceptStartScriptFormat = mustReadScript("intercept_start.js")
	interceptStopScript        = mustReadScript("intercept_stop.js")
)
