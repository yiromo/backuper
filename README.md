# backuper

A keyboard-driven TUI for managing database backups — supports PostgreSQL, ClickHouse, and Redis via Kubernetes pods or local/remote execution, with SCP/rsync/S3/local destinations, cron scheduling, and an age-encrypted secrets store.

## Features

- **Interactive TUI** — keyboard-driven interface (bubbletea)
- **Engines** — PostgreSQL (`pg_dump`/`pg_dumpall`), ClickHouse (`clickhouse-client`), or Redis (`redis-cli --rdb`)
- **Runtimes** — local, remote (connect to external host), or Kubernetes pod exec
- **Destinations** — local directory, SCP, rsync over SSH, or S3-compatible storage (AWS S3, Minio, etc.)
- **Scheduling** — cron expressions with configurable retention (`keep_last`) and schedule-based directory organization
- **Notifications** — Telegram or SMTP email alerts on backup success/failure (per-notification toggle)
- **Secrets** — age-encrypted store; values never displayed in the UI
- **History** — SQLite log of every backup run with size, duration, and output

## Install

### Download binary

Pre-built binaries are available on the [Releases](https://github.com/yiromo/backuper/releases/latest) page:

| Platform | Binary |
|---|---|
| Linux x86-64 | [backuper-linux-amd64](https://github.com/yiromo/backuper/releases/latest/download/backuper-linux-amd64) |
| Linux ARM64 | [backuper-linux-arm64](https://github.com/yiromo/backuper/releases/latest/download/backuper-linux-arm64) |
| macOS x86-64 | [backuper-darwin-amd64](https://github.com/yiromo/backuper/releases/latest/download/backuper-darwin-amd64) |
| macOS ARM64 (Apple Silicon) | [backuper-darwin-arm64](https://github.com/yiromo/backuper/releases/latest/download/backuper-darwin-arm64) |

```bash
# Linux x86-64 example
curl -L https://github.com/yiromo/backuper/releases/latest/download/backuper-linux-amd64 -o backuper
chmod +x backuper
sudo mv backuper /usr/local/bin/
```

### Build from source

```bash
git clone https://github.com/yiromo/backuper.git
cd backuper
go build -o backuper ./cmd/backuper
```

## Quick start

```bash
# Copy and edit the example config
cp configs/backuper.yaml.example ~/.config/backuper/config.yaml

# Store a secret (e.g. Postgres password)
backuper secrets set mydb-password

# Launch the TUI
backuper
```

## Configuration

Default path: `~/.config/backuper/config.yaml`

```yaml
targets:
  - name: myapp-k8s
    engine: postgres
    runtime: kubernetes
    namespace: production
    pod_selector: "myapp-postgres-.*"
    db_user: postgres
    k8s_secret:
      name: postgres-secret
      key: password

  - name: local-pg
    engine: postgres
    runtime: local
    db_user: postgres
    db_name: mydb          # omit for pg_dumpall
    secret_ref: local-pg-pass

  - name: ch-trends
    engine: clickhouse
    runtime: remote
    host: "203.0.113.10"
    db_user: trends_admin
    db_name: trends
    secret_ref: ch-password

  - name: cache-redis
    engine: redis
    runtime: local
    host: "localhost"        # default: localhost
    port: "6379"             # default: 6379
    secret_ref: redis-password

destinations:
  - name: nas
    type: rsync
    host: 192.168.1.10
    user: backup
    remote_path: /backups/postgres
    auth: key
    ssh_key_path: ~/.ssh/id_rsa

  - name: local-archive
    type: local
    path: ~/backups/postgres

  - name: aws-backup
    type: s3
    bucket: my-backup-bucket
    region: us-east-1
    remote_path: postgres-backups
    use_ssl: true
    access_key_ref: aws_access_key
    secret_key_ref: aws_secret_key

schedules:
  - target: myapp-k8s
    destination: nas
    cron: "0 3 * * *"     # daily at 03:00
    compress: gzip
    tmp_dir: /tmp
    retention:
      keep_last: 7

notifications:
  - name: telegram-alerts
    type: telegram
    bot_token_ref: tg_bot_token   # secret ref
    chat_id: "-100123456789"      # group/chat ID
    # thread_id: 42               # optional: forum topic
    on_success: true
    on_failure: true
```

### Target configuration

Targets use `engine` (what database) and `runtime` (how to connect):

**Engines**: `postgres`, `clickhouse`, `redis`
**Runtimes**: `local`, `remote`, `kubernetes`, `docker`

| Field | `engine` | `runtime` | Description |
|---|---|---|---|
| `db_user` | postgres, clickhouse | all | Required |
| `db_name` | postgres | all | Optional; empty = `pg_dumpall` |
| `db_name` | clickhouse | all | Required |
| `secret_ref` | all | local/remote/docker | Required |
| `k8s_secret` | all | kubernetes | Optional; falls back to `secret_ref` |
| `host` | clickhouse, redis | local/remote | Required for clickhouse; default `localhost` for redis |
| `port` | clickhouse, redis | local/remote | Optional (default `6379` for redis) |
| `namespace` | all | kubernetes | Required |
| `pod_selector` | all | kubernetes | Required (regex) |
| `container_name` | all | docker | Required (Docker container name or ID) |

**ClickHouse backup format**: Schema via `SHOW CREATE TABLE` + data via `SELECT * FORMAT Native` per table, combined into a `.tar.gz` archive. Restore: extract tar, run `schema.sql`, then `INSERT INTO table FORMAT Native` per table.

**Redis backup format**: RDB dump via `redis-cli --rdb`, producing a `.rdb` file. Restore: stop Redis, replace `dump.rdb` in the data directory, start Redis.

### Destination types

| Field | `local` | `scp` | `rsync` | `s3` |
|---|---|---|---|---|
| `path` | required | — | — | — |
| `host` | — | required | required | — |
| `user` | — | required | required | — |
| `remote_path` | — | required | required | optional (prefix) |
| `auth` | — | `key`/`password` | `key`/`password` | — |
| `bucket` | — | — | — | required |
| `endpoint` | — | — | — | optional (Minio/custom) |
| `region` | — | — | — | optional |
| `access_key_ref` | — | — | — | required |
| `secret_key_ref` | — | — | — | required |
| `session_token_ref` | — | — | — | optional (temp creds) |
| `use_ssl` | — | — | — | optional (default: `false`) |

### Schedule-based directory organization

Backup files are automatically organized into subdirectories based on the cron expression. The schedule type is derived from the cron pattern:

| Cron pattern | Schedule type | Directory structure |
|---|---|---|
| `0 3 * * *` | daily | `{base}/daily/` |
| `0 2 * * 1` | weekly | `{base}/weekly/2026-W15/` |
| `0 2 1 * *` | monthly | `{base}/monthly/2026-04/` |
| `0 2 1 1 *` | yearly | `{base}/yearly/2026/` |
| anything else | custom | `{base}/` (root) |

Example with a monthly and weekly schedule for the same target:

```yaml
schedules:
  - target: myapp-k8s
    destination: local-archive
    cron: "0 2 1 * *"     # monthly → ~/backups/postgres/monthly/2026-04/
    compress: gzip
    retention:
      keep_last: 12

  - target: myapp-k8s
    destination: local-archive
    cron: "0 2 * * 1"     # weekly  → ~/backups/postgres/weekly/2026-W15/
    compress: gzip
    retention:
      keep_last: 4
```

Each schedule runs independently, so you can have different retention policies per schedule type (e.g. keep 4 weekly backups and 12 monthly backups separately).

### Notifications

Notifications are sent after each backup run. Supports **Telegram** and **SMTP email**.

#### Telegram

```yaml
notifications:
  - name: telegram-alerts
    type: telegram
    bot_token_ref: tg_bot_token   # stored in secrets: backuper secrets set tg_bot_token
    chat_id: "-100123456789"      # Telegram group/chat ID
    thread_id: 42                 # optional: forum topic / subgroup thread ID
    on_success: true              # send on successful backups
    on_failure: true              # send on failed backups
```

| Field | Required | Description |
|---|---|---|
| `bot_token_ref` | yes | Secret reference for the Telegram bot token |
| `chat_id` | yes | Telegram group or chat ID (string, can be negative) |
| `thread_id` | no | Forum topic / subgroup thread ID |
| `on_success` | yes | Send notification on successful backup |
| `on_failure` | yes | Send notification on failed backup |

#### SMTP email

```yaml
notifications:
  - name: email-alerts
    type: smtp
    smtp_host: smtp.example.com
    smtp_port: 587               # default 587, use 465 for implicit TLS
    from: backuper@example.com
    to:
      - admin@example.com
      - ops@example.com
    username: backuper@example.com # optional, defaults to from
    password_ref: smtp_password    # stored in secrets: backuper secrets set smtp_password
    use_tls: true                  # STARTTLS (default true for port 587)
    insecure_tls: false            # skip TLS cert verification (for self-signed)
    on_success: true
    on_failure: true
```

| Field | Required | Description |
|---|---|---|
| `smtp_host` | yes | SMTP server hostname |
| `smtp_port` | no | SMTP port (default 587) |
| `from` | yes | Sender email address |
| `to` | yes | List of recipient email addresses |
| `username` | no | SMTP auth username (defaults to `from`) |
| `password_ref` | yes | Secret reference for SMTP password |
| `use_tls` | no | Use STARTTLS (default true for port 587) |
| `insecure_tls` | no | Skip TLS certificate verification |
| `on_success` | yes | Send notification on successful backup |
| `on_failure` | yes | Send notification on failed backup |

Setup:
```bash
# Store credentials in the encrypted secrets store
backuper secrets set tg_bot_token
backuper secrets set smtp_password
```

Notification failures are logged but never block or fail the backup.

## Running the daemon

To keep backups running persistently, use the `daemon` command.

### First-time setup (one-time)

Run with `--save-passphrase` — it prompts for your secrets passphrase once, encrypts it with age (X25519), and saves it for unattended starts:

```bash
./backuper daemon --save-passphrase
# Enter secrets passphrase: <type it once>
# Passphrase saved encrypted to: ~/.config/backuper/.backuper_passphrase
# Private key saved to: ~/.config/backuper/.backuper_key
```

### Start the daemon (no prompt needed)

```bash
./backuper daemon
```

The daemon auto-detects the saved encrypted passphrase and decrypts it on startup.

### Systemd service (recommended for production)

Create `/etc/systemd/system/backuper.service`:

```ini
[Unit]
Description=Backuper - Database Backup Daemon
After=network-online.target

[Service]
Type=simple
ExecStart=/root/backuper/backuper daemon
Environment=HOME=/root
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Then enable and start:

```bash
systemctl daemon-reload
systemctl enable --now backuper
systemctl status backuper
```

Logs go to `~/.config/backuper/backuper.log` (structured JSON).

### Alternative: environment variable or file

If you prefer not to use the encrypted passphrase file:

```bash
# Environment variable
BACKUPER_PASSPHRASE="your-passphrase" ./backuper daemon

# Plaintext file (chmod 600)
./backuper --passphrase-file ~/.backuper_passphrase daemon
```

### HTTP API

Enable the API by adding to your config:

```yaml
api:
  enabled: true
  listen_addr: "0.0.0.0:8080"
```

Then start the daemon — the API server starts automatically:

| Method | Path | Description |
|---|---|---|
| GET | `/healthz` | Deep health check (scheduler + DB) |
| GET | `/livez` | Liveness probe |
| GET | `/api/targets` | List configured targets |
| GET | `/api/schedules` | List schedules with next-run times |
| GET | `/api/history?target=&limit=` | Query run history |
| GET | `/api/runs/{id}/log` | Get log for a run |
| GET | `/api/runs/{id}/log/stream` | SSE stream for active run logs |
| POST | `/api/run` | Trigger backup `{"target","destination"}` |
| POST | `/api/stop` | Cancel running backup `{"run_id"}` |

All responses use `{"ok": bool, "data": ..., "error": ...}`. Log streaming uses Server-Sent Events (SSE).

Example:

```bash
# Check health
curl http://localhost:8080/healthz

# Trigger a backup
curl -X POST http://localhost:8080/api/run \
  -H "Content-Type: application/json" \
  -d '{"target":"local-pg","destination":"local-archive"}'

# Stream logs for a running backup
curl http://localhost:8080/api/runs/{run_id}/log/stream
```

## CLI commands

```
backuper                          # open TUI
backuper daemon                   # headless scheduler
backuper run <target> [-d dest]   # one-shot backup
backuper list targets
backuper list schedules
backuper list history [-t target] [-l limit]
backuper secrets set <ref>
backuper secrets delete <ref>
backuper secrets list
backuper config validate
```

## TUI keyboard shortcuts

| Key | Action |
|---|---|
| `d` | Dashboard |
| `t` | Targets |
| `s` | Schedules |
| `h` | History |
| `r` | Run backup |
| `S` | Secrets |
| `?` | Help |
| `q` / `ctrl+c` | Quit |
| `↑/↓` or `j/k` | Navigate |
| `enter` | Select / confirm |
| `a` | Add |
| `e` | Edit |
| `D` | Delete |
| `f` | Filter (history) |
| `esc` | Cancel / back |

## Secrets store

Secrets are stored at `~/.config/backuper/secrets.age`, encrypted with [age](https://github.com/FiloSottile/age) scrypt (passphrase-based). The passphrase is prompted on startup and kept in memory — it is never written to disk.

**Passphrase requirements** (on store creation):
- Minimum 12 characters
- At least one uppercase letter, one lowercase letter, one digit, and one symbol
- Confirmation prompt (must match)

**Behavior by mode**:
- `backuper` (TUI) — always prompts for passphrase interactively
- `backuper daemon` — uses the saved encrypted passphrase (no prompt)
- `backuper run`, `secrets`, `list` — prompts interactively

## Data files

| File | Purpose |
|---|---|
| `~/.config/backuper/config.yaml` | Configuration |
| `~/.config/backuper/secrets.age` | Encrypted secrets |
| `~/.config/backuper/.backuper_passphrase` | Encrypted passphrase for daemon (age X25519) |
| `~/.config/backuper/.backuper_key` | Age private key for daemon passphrase decryption |
| `~/.config/backuper/history.db` | Backup run history (SQLite) |
| `~/.config/backuper/backuper.log` | Structured JSON log |

## Dependencies

- [bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [lipgloss](https://github.com/charmbracelet/lipgloss) + [bubbles](https://github.com/charmbracelet/bubbles) — styling and components
- [cobra](https://github.com/spf13/cobra) — CLI
- [client-go](https://github.com/kubernetes/client-go) — Kubernetes exec (no `kubectl` binary required)
- [age](https://github.com/FiloSottile/age) — secrets encryption
- [robfig/cron](https://github.com/robfig/cron) — scheduler
- [modernc/sqlite](https://gitlab.com/cznic/sqlite) — pure-Go SQLite driver
- [minio-go](https://github.com/minio/minio-go) — S3-compatible storage client (AWS S3, Minio, etc.)