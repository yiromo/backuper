# backuper - AI Context

## Project Overview

**backuper** is a terminal-based (TUI) PostgreSQL backup manager designed for Kubernetes and local Postgres instances. It provides a k9s-style interactive interface for managing backups, with support for multiple destination types, cron-based scheduling with automatic directory organization, and age-encrypted secrets storage.

### Key Features
- **Interactive TUI** built with `bubbletea` (Charm library)
- **Targets**: Kubernetes pod exec (`pg_dumpall`) or local `pg_dump`/`pg_dumpall`
- **Destinations**: Local directory, SCP, or rsync over SSH
- **Scheduling**: Cron expressions with configurable retention (`keep_last`) and automatic schedule-based directory organization
- **Secrets**: Age-encrypted store; values never displayed in the UI
- **History**: SQLite log of every backup run with size, duration, and output
- **Daemon**: Headless mode with encrypted passphrase for unattended operation

### Architecture

```
cmd/backuper/main.go          - CLI entry point (Cobra commands + TUI launcher)
internal/
  config/config.go            - YAML config loading, validation, saving
                              - ScheduleType: automatic derivation from cron expression
  target/                     - Backup source abstraction
    target.go                 - Interface definition
    kubernetes.go             - K8s pod exec backup (client-go)
    local.go                  - Local pg_dump/pg_dumpall
  destination/                - Backup destination abstraction
    destination.go            - Interface definition
    local.go                  - Local filesystem copy
    scp.go                    - SCP transfer
    rsync.go                  - Rsync over SSH
  backup/
    runner.go                 - Orchestrates dump → compress → transfer → retention
    history.go                - SQLite history tracking (modernc/sqlite)
  scheduler/                  - Cron-based scheduling (robfig/cron)
  secrets/
    store.go                  - Age-encrypted secrets (filippo.io/age)
  tui/                        - Bubbletea TUI pages
    app.go, dashboard.go, targets.go, schedules.go, history.go, secrets.go, run.go, styles.go
```

## Building and Running

### Prerequisites
- Go 1.22+
- [age](https://github.com/FiloSottile/age) for secrets encryption

### Build
```bash
go build -o backuper ./cmd/backuper
```

### Install
```bash
go install backuper@latest
```

### Run TUI
```bash
backuper
```

### Run headless daemon (scheduler only)
```bash
# First-time setup (encrypts passphrase for unattended starts)
backuper daemon --save-passphrase

# Subsequent starts (no prompt)
backuper daemon
```

### One-shot backup
```bash
backuper run <target> [-d dest]
```

### CLI Commands
| Command | Description |
|---|---|
| `backuper` | Open interactive TUI |
| `backuper daemon [--save-passphrase]` | Headless scheduler |
| `backuper run <target>` | One-shot backup |
| `backuper list targets` | List configured targets |
| `backuper list schedules` | List schedules |
| `backuper list history [-t target] [-l limit]` | List backup history |
| `backuper secrets set <ref>` | Set/update a secret |
| `backuper secrets delete <ref>` | Delete a secret |
| `backuper secrets list` | List secret references |
| `backuper config validate` | Validate config file |

### Configuration
Default config path: `~/.config/backuper/config.yaml`

See `configs/backuper.yaml.example` for a full example.

## Technology Stack

| Category | Technology |
|---|---|
| Language | Go 1.22 |
| CLI Framework | [spf13/cobra](https://github.com/spf13/cobra) |
| TUI Framework | [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) |
| TUI Components | [bubbles](https://github.com/charmbracelet/bubbles), [lipgloss](https://github.com/charmbracelet/lipgloss) |
| Kubernetes | [client-go](https://github.com/kubernetes/client-go) v0.30 |
| Secrets Encryption | [age](https://github.com/FiloSottile/age) |
| Scheduling | [robfig/cron](https://github.com/robfig/cron) v3 |
| Database | [modernc/sqlite](https://gitlab.com/cznic/sqlite) (pure-Go) |
| Config | [gopkg.in/yaml.v3](https://gopkg.in/yaml.v3) |

## Data Files

| File | Purpose |
|---|---|
| `~/.config/backuper/config.yaml` | Configuration |
| `~/.config/backuper/secrets.age` | Encrypted secrets (database credentials, SSH passwords) |
| `~/.config/backuper/.backuper_passphrase` | Encrypted secrets store passphrase (for daemon) |
| `~/.config/backuper/.backuper_key` | Age private key for decrypting daemon passphrase |
| `~/.config/backuper/history.db` | Backup run history (SQLite) |
| `~/.config/backuper/backuper.log` | Structured JSON log |

## Schedule-Based Directory Organization

Backup files are automatically organized into subdirectories based on the cron expression. The schedule type is derived from the cron pattern:

| Cron pattern | Schedule type | Directory structure |
|---|---|---|
| `0 3 * * *` | daily | `{base}/daily/` |
| `0 2 * * 1` | weekly | `{base}/weekly/2026-W15/` |
| `0 2 1 * *` | monthly | `{base}/monthly/2026-04/` |
| `0 2 1 1 *` | yearly | `{base}/yearly/2026/` |
| anything else | custom | `{base}/` (root) |

### Schedule Type Derivation Logic
1. **Yearly**: `dom=1`, `month!=*`, `dow=*`
2. **Monthly**: `dom=1`, `month=*`, `dow=*`
3. **Weekly**: `dow!=*`, `dom=*`
4. **Daily**: `dom=*`, `month=*`, `dow=*`, minute/hour not `*/N`
5. **Custom**: anything else (complex patterns, multiple times per day)

## Development Conventions

- **No external binaries**: Kubernetes backup uses client-go exec directly (no `kubectl` binary required)
- **Secrets never displayed**: The TUI and CLI never echo secret values
- **Progress logging**: Dump progress is streamed via `progressWriter` with periodic MB markers
- **Temp file cleanup**: Backup dumps to temp file first; cleaned up on failure or after successful transfer
- **Retention**: Sorts files by name (date-embedded) and deletes oldest beyond `keep_last`
- **Error handling**: Failures are recorded in history with error messages
- **Daemon passphrase**: Automatically encrypted with age X25519 when `--save-passphrase` is used

## CI/CD

The `.github/workflows/ci.yml` workflow:
- **On PR/push to main**: Runs tests (`-v -race -cover`) and builds
- **On push to main only**: Cross-compiles release binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 with stripped symbols (`-ldflags="-s -w"`)
