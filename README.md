# Codex Manager

# WARNING COMPLETELY LLM/VIBE CODED I HAVE NEVER LOOKED AT THE CODE

Browse and render Codex session `.jsonl` files with a web UI, and optionally share rendered HTML via a dedicated share server (with optional Tailscale integration).

## Features
- Indexes `~/.codex/sessions/{year}/{month}/{day}` and lists sessions by date.
- Adds `/active` for day-by-day active threads across all directories, with manual end/reopen controls.
- Accepts Codex CLI notification webhooks on `/hook` and lets you inspect them on `/notifications`.
- Renders conversations as HTML with markdown support and dark theme.
- Shows user/agent messages, non-empty reasoning summaries, and response items such as tool calls, tool outputs, web searches, custom tool activity, and ghost snapshots.
- Consecutive messages and reasoning items are merged; visible tool/system items stay separate. For user groups, only the last message is kept.
- User messages can be trimmed to content after `## My request for Codex:` (default on).
- Share button saves a hard‑to‑guess HTML file to `~/.codex/shares` and copies its URL.
- Separate share server serves only exact filenames (no directory listing).
- Native htmlbucket sharing support.
- htmlbucket is auto-enabled when `~/.hb/auth.json` exists and is valid.
- `-hb` bootstraps htmlbucket auth by prompting for an API key if auth is missing.
- Optional Tailscale `serve`/`funnel` integration for public share URLs.

## Install Go
Go must be installed and available on your PATH (so `go` works in a terminal).

Examples:
```bash
# macOS (Homebrew)
brew install go

# Ubuntu/Debian
sudo apt-get update && sudo apt-get install -y golang-go

# Windows (winget)
winget install --id GoLang.Go
```

## Usage
```bash
# Run the main UI (default: :8080) and share server (:8081)
go run ./cmd/codex-manager

# Run and open the UI in your browser
go run ./cmd/codex-manager --open-browser

# Show help
 go run ./cmd/codex-manager -h

# Disable trimming to the request marker
 go run ./cmd/codex-manager -full

# Enable Tailscale serve/funnel for share URLs
 go run ./cmd/codex-manager -ts

# Force htmlbucket setup on startup (prompts for API key if needed)
 go run ./cmd/codex-manager -hb
```

Visit:
- UI: http://localhost:8080/
- Share server: http://localhost:8081/

Key pages:
- `/` date/directory browser
- `/active` active thread list (default: today in your browser time zone)
- `/notifications` received webhook notifications

## Autostart (systemd --user)
If your environment supports user services (WSL with systemd, Linux desktops), you can keep it running.

1) Build the binary:
```bash
make build
```

2) Create `~/.config/systemd/user/codex-manager.service`:
```ini
[Unit]
Description=Codex Manager
After=network.target

[Service]
Type=simple
WorkingDirectory=%h/codex-manager
ExecStart=%h/codex-manager/bin/codex-manager
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
```
`%h` expands to your home directory in systemd user services.

3) Enable and start:
```bash
systemctl --user daemon-reload
systemctl --user enable --now codex-manager
```

Rebuild + restart after code changes:
```bash
make build
systemctl --user restart codex-manager
```

## Flags
- `--sessions-dir` (default `~/.codex/sessions`)
- `--addr` (default `:8080`)
- `--share-addr` (default `:8081`)
- `--share-dir` (default `~/.codex/shares`)
- `--rescan-interval` (default `2m`)
- `--open-browser` open the UI in your browser on startup
- `-hb` enable htmlbucket sharing (and prompt/write `~/.hb/auth.json` if missing)
- `-ts` enable Tailscale serve/funnel
- `-full` disable trimming to `## My request for Codex:`
- `-h` / `--help`

## HTMLBucket notes
- Auth file path: `~/.hb/auth.json`
- Auth file format:
  ```json
  {
    "api_key": "THE_API_KEY_HERE"
  }
  ```
- If `~/.hb/auth.json` exists and is valid, htmlbucket is used for Share by default.
- If `-hb` is used and auth file is missing, Codex Manager prompts once for an API key and writes:
  - `~/.hb` with mode `0700`
  - `~/.hb/auth.json` with mode `0600`
- If auth file exists but is invalid/unreadable, startup fails with a configuration error.
- htmlbucket share uploads call `POST https://api.htmlbucket.com/v1/upload` and return the upstream URL.
- If htmlbucket is active and upload fails, Share returns an error (no silent fallback to local file shares).

## Tailscale notes
When `-ts` is enabled, Codex Manager:
- Runs `tailscale serve --bg --yes --http <share-port>`
- Runs `tailscale funnel --bg --yes <share-port>`
- Uses `tailscale status --json` to discover your Tailscale DNS name and builds share URLs like `https://<tailscale-host>/<uuid>.html`.

The binary is auto-detected at:
- `/Applications/Tailscale.app/Contents/MacOS/Tailscale` (macOS)
- `tailscale` on PATH

## Share behavior
Clicking “Share”:
- If htmlbucket is active: uploads rendered HTML and copies the returned `https://<id>.htmlbucket.com` URL.
- Otherwise: renders to a UUID-like filename at `~/.codex/shares/<uuid>.html`.
- Copies the share URL to your clipboard
- Displays a banner showing the copied URL

## Active thread state
- New top-level threads are treated as active by default.
- `/active` uses your browser time zone (stored in a cookie) to decide which day a thread belongs to.
- A thread is shown as:
  - `Waiting for user`
  - `Waiting for agent`
  - `Ended`
- `Ended` is a manual flag stored in `~/.codex/session_state.json`.
- If a thread receives new JSONL activity after being marked ended, it automatically returns to active.
- `/active` refreshes while the page is open without changing the global `--rescan-interval`.

## Codex CLI notifications
You can point Codex CLI `notify` to the local server:

```toml
notify = [
  "curl",
  "-sS",
  "-X", "POST",
  "http://localhost:8080/hook",
  "-H", "Content-Type: application/json",
  "--data-binary"
]
```

- Incoming requests are accepted on `/hook`.
- Received notifications are appended to `~/.codex/notifications.jsonl`.
- `/notifications` shows the newest notifications first, with headers and body.
- The page auto-refreshes while open so you can inspect payloads as they arrive.

## Development
```bash
go test ./...
```

## Directory layout
- `cmd/codex-manager/main.go` – server startup
- `internal/config` – CLI flags + config
- `internal/sessions` – scanning + parsing
- `internal/web` – HTTP handlers + share logic
- `internal/render` – templates + CSS
