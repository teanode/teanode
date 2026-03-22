# Changelog

All notable changes to TeaNode will be documented in this file.

The format is based loosely on Keep a Changelog, and versions are recorded using repository tags.

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
