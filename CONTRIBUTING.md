# Contributing

## Development Workflow

1. Branch from `main`
2. Keep commits in Conventional Commit format
3. Run `make fmt`, `make test`, and `make vet` before opening a PR
4. Add or update tests for behavior changes

## Local Commands

```bash
make build
make test
make vet
make fmt
```

## Pull Requests

- Describe the behavior change, not only the code change
- Call out config or operational impact explicitly
- Keep documentation in sync with runtime behavior

## Reporting Bugs

Open a GitHub issue with:

- Your OS and architecture
- Go version if building from source
- Claude Code CLI version
- The relevant RCOD command or Telegram action
- Sanitized logs or reproduction steps
