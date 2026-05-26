# Changelog

All notable changes to TeaNode will be documented in this file.

The format is based loosely on Keep a Changelog, and versions are recorded using repository tags.

## [0.2.0] - 2026-05-26

### Changed

- Release automation now sources release notes from each merged PR's `## Changelog` block (template-scaffolded) instead of `## [Unreleased]` in `CHANGELOG.md`. The `Changelog Guard` workflow rejects PRs whose description block is still the placeholder (override with `skip-changelog`). Each release bullet is annotated `(#NN)` linking to its source PR. (#49)

## [0.1.16] - 2026-05-16

### Fixed

- Stabilize mobile and desktop scroll by falling back to a simple (non-virtualized) list when the item count is below 200, eliminating visible jumps caused by react-virtuoso's height-estimation cycle during iOS momentum scrolling and mixed-height items on desktop.
- Isolate streaming re-renders to the active message bubble via `StreamTextStore`, preventing full-list re-measurement on every token.
- Add `minHeight` and `display:block` on lazy-loaded images in `ToolResult` and `MessageBubble` to reduce layout shift.

## [0.1.15] - 2026-05-16

### Fixed

- Strip hidden `<!--suggestions:[...]-->` markers from messages sent to Discord and Telegram channels.

## [0.1.14] - 2026-05-08

### Fixed

- Smoke-test entry to verify the auto-release pipeline picks up patch bumps from `### Fixed`.

## [0.1.13] - 2026-05-07

### Fixed

- Fixed scheduled jobs firing at the wrong timezone after the host timezone changes by detecting OS timezone updates without requiring a process restart.
- Added a TTL cache to `LocalLocation` and corrected `TZ=""` semantics so empty-string timezone is handled correctly.

## [0.1.12] - 2026-05-07

### Added

- Suggestion chips: assistant responses can include clickable suggestion chips in the chat UI, giving users quick follow-up actions without typing.
- Native chart rendering: LLM agents can emit chart data using backtick-fenced chart blocks that render as interactive charts inline in messages, with fullscreen view support.

### Fixed

- Fixed chart label overlap and made fullscreen view use the full viewport.

## [0.1.11] - 2026-04-07

### Added

- Artifact panel: LLM agents can wrap long-form content (plans, documents, code) in `:::artifact{title="..."}` fences. Artifacts render as compact inline chips in message bubbles and open in a dedicated side panel on desktop (responsive 440–680px) or full-screen overlay on mobile. Content streams live into the panel as the LLM generates it, with auto-open on streaming and a copy button in the title bar.
- System prompt now includes host environment details (hostname, username, home directory, platform, architecture) so agents are aware of the machine they run on.

### Fixed

- Fixed oversized horizontal scrollbar in code blocks by applying thin scrollbar styling to both axes.
- Fixed all golangci-lint errcheck violations in the updater package and removed unused `getPageMetadata` function from browser snapshot.

## [0.1.10] - 2026-04-02

### Added

- Detect stale frontend builds on reconnect: the web UI now shows a notification with a reload button when a newer build is available after the backend restarts.
- Added missing i18n strings (`connectionLost`, `connectionRestored`, `staleBuild`, `reload`) for zh and ja locales.

## [0.1.9] - 2026-03-30

### Added

- `node.update` tool responses now include release notes when available.

### Changed

- `node.update` now schedules restart through the normal node lifecycle flow so the LLM can finish its turn before TeaNode restarts.
- Settings → Updates now renders release notes as markdown instead of plain text.

### Fixed

- Fixed a web UI reconnect race where Approve actions could fail silently after tab switching and leave the approval controls stuck in a disabled state.
- Fixed the same reconnect race for `ask_user_question` submissions so answer buttons recover correctly after RPC failures.
- Disabled approval/question action buttons while the backend websocket is disconnected to avoid no-op clicks during reconnect windows.

## [0.1.8] - 2026-03-30

### Added

- Added `teanode version` to print the current TeaNode version, commit, and platform.

### Fixed

- Fixed self-update restart on Linux by resolving and capturing the executable path at process startup instead of recomputing it from `/proc/self/exe` after the running binary has been replaced.

## [0.1.7] - 2026-03-30

### Fixed

- Fixed Linux self-update apply across different filesystems by falling back from `rename(2)` to copy-into-target-directory plus atomic rename when the staged binary is on a different mount (for example `/tmp` on tmpfs).
- Added Unix updater tests covering rename/copy fallback, backup restore, stale backup cleanup, directory writability, and permission preservation.

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
