# Changelog

All notable changes to TeaNode will be documented in this file.

The format is based loosely on Keep a Changelog, and versions are recorded using repository tags.

## [Unreleased]

## [0.1.6] - 2026-03-30

### Fixed

- Fixed Linux self-update apply preflight checks to validate directory writability instead of opening the running executable for writing, avoiding `text file busy` failures during rename-based updates.

## [0.1.5] - 2026-03-30

### Added

- Self-update system: check, download, verify (SHA256), and apply updates from GitHub Releases.
- CLI `teanode update` command with `check` and `apply` subcommands.
- WebSocket RPC endpoints: `update.status`, `update.check`, `update.apply` (admin-only).
- Periodic background update checking with configurable interval (default: 30 minutes).
- Config section `update` (renamed from `autoUpdate`) with `policy` (disabled/notify/auto, default: notify) and `checkIntervalHours`.
- Updater reads config from the store on each cycle, so runtime policy changes take effect without a restart.
- Update availability surfaced in `connect` handshake response for admin users.
- Container environment detection to disable self-update in Docker/Kubernetes/LXC.
- Platform-specific binary replacement: atomic rename on Unix, rename+copy on Windows.
- Backup of current binary before apply with automatic restore on failure.
- Settings > Updates admin page for checking status and applying updates from the web UI.
- `node` tool update controls for checking cached/fresh update status and applying updates when available.

### Changed

- Unified the `node` tool update flow under `action: "update"` with `forceCheck` and `applyIfAvailable` arguments.
- Update settings page now shows when the local build is ahead of the latest tagged release.

### Fixed

- Fixed the Settings update page showing "The updater is not enabled on this instance." after a browser refresh before backend connection state was ready.

## [0.1.4] - 2026-03-30

### Added

- AI-friendly interactive browser snapshots with stable `[ref=N]` markers for the headless `browser` tool.
- Ref-based browser actions: `click_ref`, `type_ref`, `hover_ref`, and `select_option`.
- Browser wait modes for `selector`, `navigation`, `network_idle`, and `timeout`.
- Multi-step browser execution via `browser.execute_script`.
- Named browser tab/instance support in `browser_tabs` with `name` and `resolve`.
- Lightweight browser-side network capture via `intercept_start`, `get_intercepted`, and `intercept_stop`.
- AI-friendly interactive snapshot/ref support for the attached-tab `tab` tool, including `clickRef`, `typeRef`, `hoverRef`, `selectOption`, `wait`, and `executeSteps`.
- Embedded browser page scripts loaded from standalone JS files via Go `embed` for snapshot, wait, idle-tracking, and interception helpers.

### Changed

- Replaced the browser snapshot pipeline's unreliable accessibility-tree dependency with a DOM-driven interactive snapshot/ref model.
- Hardened browser ref lifecycle cleanup, tab naming state, navigation wait semantics, and network-idle tracking behavior.
- Made `get_intercepted` non-destructive while keeping `intercept_stop` as the destructive drain-and-stop path.
- Improved Chrome extension request correlation so overlapping requests are tracked safely.
- Cleaned up browser tooling internals by extracting larger page-side JavaScript snippets out of Go source into embedded JS assets.

### Fixed

- Fixed headless browser interactive snapshots returning only `RootWebArea` with no actionable refs in some environments.
- Fixed extension/frontend build and format issues around the page bridge XHR/network-idle tracking changes.

## [0.1.3] - 2026-03-22

### Added

- GitHub release publishing workflow for tagged builds.
- Release checksum publishing via `SHA256SUMS` manifest.
- MIT license.
- Windows platform support for OS-specific process, signal, pidfile, log rotation, and command execution behavior.
- Manual release workflow dispatch support.

### Changed

- Release workflow now accepts both `v*` and bare semantic version tags.
- Release archives now package standard project metadata files alongside the binary.

## [0.1.2] - 2026-03-21

### Added

- Cloud proxy support with yamux multiplexing.
- Flattened API package structure for cloud support.

## [0.1.1] - 2026-03-10

### Added

- Confluence tool integration.
- Expanded Mattermost tool coverage for threads, teams, channels, and posts.

### Changed

- Improved project documentation and contribution guidance.
- Fixed flaky concurrent access test behavior.

## Unreleased

### Planned

- Continue summarizing notable user-facing and operator-facing changes for each tagged release.
