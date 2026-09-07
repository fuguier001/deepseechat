# Autostart / Boot Persistence (macOS · Linux · Windows)

Run `dsc` as a resilient background service that survives reboots and crashes. Pick your OS:

- [macOS — launchd](#macos-launchd)
- [Linux — systemd (user service)](#linux-systemd)
- [Windows — Task Scheduler or NSSM](#windows)

> **Secrets rule**: keys live in environment variables or `${VAR}` placeholders in `config.toml` — never hardcode real keys into files you commit. Example files in [`examples/autostart/`](./examples/autostart/) all use placeholders.

---

## macOS (launchd)

Tested configuration (this is exactly what a working deployment looks like).

**1. Install the plist** at `~/Library/LaunchAgents/com.fuguier001.dsc.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>com.fuguier001.dsc</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/YOU/.npm-global/bin/dsc</string>
        <string>start</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/Users/YOU/.npm-global/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
        <key>HOME</key><string>/Users/YOU</string>
        <!-- provider credentials (or rely on config.toml provider settings) -->
        <key>ANTHROPIC_BASE_URL</key><string>https://open.bigmodel.cn/api/anthropic</string>
        <key>ANTHROPIC_AUTH_TOKEN</key><string>YOUR_TOKEN</string>
        <key>ANTHROPIC_MODEL</key><string>glm-4.5-air</string>
    </dict>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPath</key><string>/Users/YOU/.dsc/daemon.log</string>
    <key>StandardErrorPath</key><string>/Users/YOU/.dsc/daemon.log</string>
</dict>
</plist>
```

A ready-to-edit copy lives at [`examples/autostart/com.fuguier001.dsc.plist`](./examples/autostart/com.fuguier001.dsc.plist).

**2. Load it**

```bash
chmod 600 ~/Library/LaunchAgents/com.fuguier001.dsc.plist   # it holds secrets
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.fuguier001.dsc.plist
```

Reload after edits:

```bash
launchctl bootout gui/$(id -u)/com.fuguier001.dsc
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.fuguier001.dsc.plist
```

**Pitfalls (all hit in real deployments)**

- **launchd does not inherit your shell PATH.** Without an explicit `PATH`, the daemon starts but the agent fails with `agent CLI not found in PATH`. Always set `PATH` including the directory that holds `claude`/`codex`.
- If a `dsc` you started manually is still running, the launchd copy dies on the instance lock (`another dsc instance is already running`). Kill the manual one; `KeepAlive` restarts the service copy.
- Two sources of truth for provider env (plist vs `config.toml` `[[providers]]`) is fine, but keep them consistent — a stray `ANTHROPIC_BASE_URL` without a token produces `Not logged in` from the spawned CLI.

---

## Linux (systemd)

**1. Create the user unit** at `~/.config/systemd/user/dsc.service`:

```ini
[Unit]
Description=DeepSeeChat (DSC) daemon
After=network-online.target
Wants=network-online.target

[Service]
Environment=PATH=%h/.local/bin:%h/.npm-global/bin:/usr/local/bin:/usr/bin:/bin
Environment=ANTHROPIC_BASE_URL=https://open.bigmodel.cn/api/anthropic
Environment=ANTHROPIC_AUTH_TOKEN=YOUR_TOKEN
Environment=ANTHROPIC_MODEL=glm-4.5-air
ExecStart=%h/.local/bin/dsc start
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
```

Ready-to-edit copy: [`examples/autostart/dsc.service`](./examples/autostart/dsc.service).

Prefer keeping secrets out of the unit? Put them in `~/.config/dsc/env` (chmod 600) and use `EnvironmentFile=%h/.config/dsc/env`.

**2. Enable & start**

```bash
systemctl --user daemon-reload
systemctl --user enable --now dsc
journalctl --user -u dsc -f          # logs
```

**3. Survive logout / headless boot** (optional but usually wanted):

```bash
loginctl enable-linger $USER
```

Without linger the user service stops when you log out.

---

## Windows

`dsc.exe start` is a console program; wrap it with Task Scheduler (logon startup) or NSSM (true service).

### Option A — Task Scheduler (simplest)

```powershell
# env vars persist for your user (one-time)
setx ANTHROPIC_BASE_URL "https://open.bigmodel.cn/api/anthropic"
setx ANTHROPIC_AUTH_TOKEN "YOUR_TOKEN"
setx ANTHROPIC_MODEL "glm-4.5-air"

schtasks /Create /TN "DSC" /SC ONLOGON /RL HIGHEST `
  /TR "%LOCALAPPDATA%\Programs\dsc\dsc.exe start"
```

Note: `setx` env is read at process start — reboot (or log off/on) once after setting. CLI tools (`claude.cmd` etc.) must be on the system PATH, since Task Scheduler uses the system environment, not your terminal's.

### Option B — NSSM (recommended for 24/7)

```powershell
nssm install DSC "C:\Users\YOU\AppData\Local\Programs\dsc\dsc.exe" "start"
nssm set DSC AppDirectory "C:\Users\YOU\.dsc"
nssm set DSC AppEnvironmentExtra `
  "ANTHROPIC_BASE_URL=https://open.bigmodel.cn/api/anthropic" `
  "ANTHROPIC_AUTH_TOKEN=YOUR_TOKEN" `
  "ANTHROPIC_MODEL=glm-4.5-air" `
  "PATH=C:\Users\YOU\AppData\Roaming\npm;C:\Windows\System32;C:\Windows"
nssm set DSC AppStdout "C:\Users\YOU\.dsc\daemon.log"
nssm set DSC AppStderr "C:\Users\YOU\.dsc\daemon.log"
nssm start DSC
```

NSSM restarts the process on crash and starts it at boot, no login required.

---

## Verify it worked (all platforms)

1. Reboot the machine.
2. Check the daemon: `dsc status` (management API) or tail `~/.dsc/daemon.log` for `dsc is running`.
3. Send a WeChat message to the bot — a reply means the whole chain (launchd/systemd/NSSM → dsc → agent CLI → provider) survived the reboot.
