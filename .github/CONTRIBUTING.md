# Contributing to backuper

Thank you for considering a contribution! Here's everything you need to get started.

## Getting started

1. **Fork** the repository and clone your fork
2. **Create a branch** from `main`:
   ```bash
   git checkout -b feat/your-feature
   ```
3. Make your changes, then run tests:
   ```bash
   go test ./...
   go vet ./...
   ```
4. **Commit** using the [conventional commits](#commit-style) format
5. Open a **Pull Request** against `main`

## Development setup

**Requirements**: Go 1.22+

```bash
git clone https://github.com/YOUR_USERNAME/backuper.git
cd backuper
go build ./...
go test ./...
```

No external services required for unit tests. Integration tests (Kubernetes, S3) are skipped unless the necessary environment is available.

## What to work on

- Check [Issues](https://github.com/YOUR_USERNAME/backuper/issues) for open tasks
- Features tagged `good first issue` are a great starting point
- Bug reports and documentation improvements are always welcome

## Commit style

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add Windows binary to release matrix
fix: correct retention sort order for weekly backups
docs: update README install instructions
chore: bump minio-go to v7.0.90
refactor: extract schedule type derivation to helper
test: add config validation edge cases
```

**Types**: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `ci`

Breaking changes: append `!` after the type, e.g. `feat!: rename secret_ref to credentials_ref`.

## Pull request guidelines

- Keep PRs focused — one feature or fix per PR
- Include tests for new behavior where practical
- Update `README.md` or `configs/backuper.yaml.example` if your change affects configuration or CLI flags
- Ensure `go vet ./...` and `go test ./...` pass before submitting

## Code style

- Standard Go formatting (`gofmt`); no custom linter config required
- Follow existing patterns for adding destinations, targets, or notifiers (see `internal/destination/local.go`, `internal/target/local.go`, `internal/notify/telegram.go` as reference)
- Secrets must never be logged or displayed in the TUI

## Reporting issues

Please include:
- Go version (`go version`)
- OS and architecture
- Relevant config snippet (sanitize secrets and IPs)
- Steps to reproduce and actual vs expected behavior

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](../LICENSE).
