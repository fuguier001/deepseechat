# 开机自启 / 崩溃自愈（macOS · Linux · Windows）

把 `dsc` 跑成常驻后台服务：开机自动拉起、崩溃自动重启。按系统对号入座：

- [macOS — launchd](#macos-launchd)
- [Linux — systemd（用户服务）](#linux-systemd)
- [Windows — 任务计划 或 NSSM](#windows)

> **密钥纪律**：真实 key 只放环境变量或 `config.toml` 的 `${VAR}` 占位符，**绝不硬编码进要提交的文件**。[`examples/autostart/`](./examples/autostart/) 里的示例全部是占位符。

---

## macOS（launchd）

以下为实测可用的完整配置。

**1. 安装 plist** 到 `~/Library/LaunchAgents/com.fuguier001.dsc.plist`（可直接改 [`examples/autostart/com.fuguier001.dsc.plist`](./examples/autostart/com.fuguier001.dsc.plist)）：

关键点：
- `ProgramArguments` 指向 dsc 绝对路径 + `start`
- `EnvironmentVariables` 里**必须显式给 `PATH` 和 `HOME`**，外加服务商三件套（或完全依赖 config.toml 的 `[[providers]]`）
- `RunAtLoad`（开机拉起）+ `KeepAlive`（崩溃重启）
- 日志走 `StandardOutPath` / `StandardErrorPath` → `~/.dsc/daemon.log`

**2. 加载**

```bash
chmod 600 ~/Library/LaunchAgents/com.fuguier001.dsc.plist   # 里面有密钥
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.fuguier001.dsc.plist
```

改完重载：

```bash
launchctl bootout gui/$(id -u)/com.fuguier001.dsc
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.fuguier001.dsc.plist
```

**实打实踩过的坑**

- **launchd 不继承 shell 的 PATH**：不显式设置 `PATH`，守护进程能起，但 agent 报 `agent CLI not found in PATH` 直接躺平。`PATH` 里必须包含 `claude`/`codex` 所在目录。
- 手动起的 `dsc` 没杀干净时，launchd 副本会死在实例锁上（`another dsc instance is already running`）。杀掉手动那个，`KeepAlive` 会自动把服务拉回来。
- plist 环境变量和 `config.toml` 的 provider 配置是两套来源，可以并存，但要保持一致——只给 `ANTHROPIC_BASE_URL` 不给 token，拉起的 CLI 就会报 `Not logged in`。

---

## Linux（systemd）

**1. 创建用户单元** `~/.config/systemd/user/dsc.service`（模板见 [`examples/autostart/dsc.service`](./examples/autostart/dsc.service)）：

- `Environment=PATH=...` 同样必须显式（systemd 用户服务也没有你的 shell PATH）
- 密钥可用 `EnvironmentFile=%h/.config/dsc/env`（chmod 600）与单元文件解耦
- `Restart=always` + `WantedBy=default.target`

**2. 启用**

```bash
systemctl --user daemon-reload
systemctl --user enable --now dsc
journalctl --user -u dsc -f          # 看日志
```

**3. 注销不掉线 / 无头开机自启**（通常需要）：

```bash
loginctl enable-linger $USER
```

不开启 linger，用户服务会随登出一起停。

---

## Windows

`dsc.exe start` 是控制台程序，用任务计划（登录自启）或 NSSM（真服务）包一层。

### 方案 A — 任务计划（最简单）

```powershell
# 环境变量持久化（一次性），重启后生效
setx ANTHROPIC_BASE_URL "https://open.bigmodel.cn/api/anthropic"
setx ANTHROPIC_AUTH_TOKEN "YOUR_TOKEN"
setx ANTHROPIC_MODEL "glm-4.5-air"

schtasks /Create /TN "DSC" /SC ONLOGON /RL HIGHEST `
  /TR "%LOCALAPPDATA%\Programs\dsc\dsc.exe start"
```

注意：任务计划用的是**系统环境**而非终端环境，`claude.cmd` 等 CLI 必须在系统 PATH 里。

### 方案 B — NSSM（推荐 7×24）

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

NSSM 崩溃自动重启 + 开机即启，无需登录。

---

## 通用验收（三平台一致）

1. 重启机器
2. `dsc status` 或看 `~/.dsc/daemon.log` 出现 `dsc is running`
3. 微信给机器人发条消息——有回复 = 整条链路（launchd/systemd/NSSM → dsc → agent CLI → 服务商）都活着
