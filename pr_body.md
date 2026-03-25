This PR introduces a unified Provider-based architecture to replace the previous siloed implementation for Claude and Codex.

### Key Changes
- **Unified Domain Model**: Introduced `internal/chat` with shared types (`Session`, `Message`, `Event`, `Attachment`).
- **Provider Interface**: All chat agents now implement a common `Provider` interface, supporting messaging, command execution, and attachments.
- **Generic API**: HTTP endpoints are now dynamic: `/api/chat/{provider}/sessions`.
- **Dynamic Frontend**: The React sidebar and hooks now dynamically discover and render all registered providers.
- **Improved Dev Experience**: Separated dashboard assets into `assets_dev.go` and `assets_release.go` (using build tags), allowing the backend to build without a pre-built frontend.

### Fixes
- Fixed CI build failures related to missing attachments in the Provider interface.
- Fixed `gofmt` issues.
- Updated all integration tests to match the new API structure.

This refactoring prepares the codebase for easy integration of **Gemini**, **OpenCode**, and other models.