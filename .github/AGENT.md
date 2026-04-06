# backuper - QWEN Context

## Project Overview

**backuper** is a terminal-based (TUI) PostgreSQL backup manager designed for Kubernetes and local Postgres instances. It provides a k9s-style interactive interface for managing backups, with support for multiple destination types, cron-based scheduling, and age-encrypted secrets storage.

### Key Features
- **Interactive TUI** built with `bubbletea` (Charm library)
- **Targets**: Kubernetes pod exec (`pg_dumpall`) or local `pg_dump`/`pg_dumpall`
- **Destinations**: Local directory, SCP, or rsync over SSH
- **Scheduling**: Cron expressions with configurable retention (`keep_last`)
- **Secrets**: Age-encrypted store; values never displayed in the UI
- **History**: SQLite log of every backup run with size, duration, and output

### Architecture

```
cmd/backuper/main.go          - CLI entry point (Cobra commands + TUI launcher)
internal/
  config/config.go            - YAML config loading, validation, saving
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
| `backuper daemon` | Headless scheduler |
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
| `~/.config/backuper/secrets.age` | Encrypted secrets |
| `~/.config/backuper/history.db` | Backup run history (SQLite) |
| `~/.config/backuper/backuper.log` | Structured JSON log |

## Development Conventions

- **No external binaries**: Kubernetes backup uses client-go exec directly (no `kubectl` binary required)
- **Secrets never displayed**: The TUI and CLI never echo secret values
- **Progress logging**: Dump progress is streamed via `progressWriter` with periodic MB markers
- **Temp file cleanup**: Backup dumps to temp file first; cleaned up on failure or after successful transfer
- **Retention**: Sorts files by name (date-embedded) and deletes oldest beyond `keep_last`
- **Error handling**: Failures are recorded in history with error messages
