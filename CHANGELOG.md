# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com),
and this project adheres to [Semantic Versioning](https://semver.org).

## [v0.3.0] - 2026-02-22

### Added

- Interactive TUI browser with bubbletea
- Changelog view with release notes
- `--export` and `--export-json` flags
- Health indicators, outdated checks, and metadata display
- Browse and install extensions from within TUI
- Manage extensions: update, remove, and prune
- Direct argument support for quick lookup
- Updating indicator while extensions are being upgraded
- Local indicator for locally-developed extensions
- Makefile, .gitignore, and re-recorded demo

### Changed

- Restyle TUI for gh-native look and feel
- Replace bubbles/list with minimal custom picker
- Preserve health data across refresh, fix commit-hash versions
- Update tagline to "in depth"
- Prepare for open source

### Fixed

- GoReleaser asset naming for `gh extension install`
- GoReleaser config to produce raw binaries

### Removed

- Stale indicator from list view

[v0.3.0]: https://github.com/maxbeizer/gh-exts/releases/tag/v0.3.0
