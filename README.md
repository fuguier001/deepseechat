# DeepSeeChat（DSC）

**在微信里跟你的 AI agent 对话。** DSC 是一个轻量守护进程，把即时通讯平台（ilink 微信个人号，以及 Telegram / QQ / 飞书 / 钉钉 / Slack / Discord …）桥接到本地编码 agent（Agent CLI、Codex、Gemini CLI、OpenCode …），模型服务商可插拔。


## 快速开始（微信 + DeepSeek Harness）

机器人大脑是真正的 **DeepSeek Harness（DSH）** 运行时——和你交互式会话用同一套 profiles、技能和模型路由。无需配任何 API key。

```bash
# 1) 编译安装 DSC
make build && cp dsc ~/.local/bin/          # 或：go build -o dsc ./cmd/dsc

# 2) 安装 DSH 启动器（稳定全局命令）
npm install -g @deepseek-ai/dsh
dsh --version                                # 能打印版本号即成功

# 3) 绑定微信：终端出二维码，手机微信扫码
dsc weixin setup --project my-project

# 4) 启动
dsc start
```

最小可用 `~/.dsc/config.toml`：

```toml
language = "zh"

[[projects]]
name = "my-project"
admin_from = "你的微信用户ID"          # 发 /whoami 获取

[projects.agent]
type = "dsh"                          # 原生 DeepSeek Harness 适配器

[projects.agent.options]
work_dir = "/path/to/your/project"
# cli_path = "/absolute/dsh"          # 可选；默认自动发现：
                                        # PATH → npm 全局 → npx 缓存

[[projects.platforms]]
type = "weixin"

[projects.platforms.options]
allow_from = "你的微信用户ID"          # 锁白名单；"*" 不安全
```

重启不失联：[开机自启指南](./docs/autostart.zh-CN.md)（macOS launchd / Linux systemd / Windows）。

## 本地 NSFW 聊天大脑（Apple Silicon，可选）

把微信大脑从 DSH 换成**本地去审查（abliterated）MLX 模型**——零 API 费、断网可聊、且完全不往 DSH 会话库里写东西。

```bash
pip install -U mlx-lm huggingface_hub
export HF_ENDPOINT=https://hf-mirror.com            # HuggingFace 国内镜像

# 去审查版 Qwen3.5-9B，MLX 4-bit（约 4.8GB）；更轻替代：mlx-community/Josiefied-Qwen3-4B-abliterated-v1-4bit
huggingface-cli download huihui-ai/Huihui-Qwen3.5-9B-abliterated-mlx-4bit \
  --local-dir ~/models/huihui-qwen35-9b-abl

# transformers 4.x 兼容：仓库自带的是 v5 才有的 tokenizer 类
sed -i '' 's/"tokenizer_class": "TokenizersBackend"/"tokenizer_class": "PreTrainedTokenizerFast"/' \
  ~/models/huihui-qwen35-9b-abl/tokenizer_config.json
```

**镜像对单连接限速时**（实测 0.17MB/s、预估 10 小时），并行分段下载约提速 8 倍：

```bash
URL="https://hf-mirror.com/huihui-ai/Huihui-Qwen3.5-9B-abliterated-mlx-4bit/resolve/main/model.safetensors"
SZ=$(curl -sIL "$URL" | grep -i '^content-length' | tail -1 | tr -dc '0-9')
CHUNK=$((SZ/8+1))
for i in $(seq 0 7); do
  S=$((i*CHUNK)); E=$(((i+1)*CHUNK-1)); [ $E -ge $SZ ] && E=$((SZ-1))
  curl -sL --retry 5 -r $S-$E -o part_$i "$URL" &
done; wait
cat part_0 part_1 part_2 part_3 part_4 part_5 part_6 part_7 > model.safetensors   # 校验大小 == $SZ
```

`-L` 必须加——镜像会 302 跳转 CDN，不加的话每段都只会下回 1KB 的跳转页。

**起服务 + 桥接**（模板见 [`scripts/`](./scripts)，含 launchd plist）：

```bash
python3 -m mlx_lm.server --model ~/models/huihui-qwen35-9b-abl --port 8080
cp scripts/dsc-mlx-bridge.sh ~/.local/bin/dsc-mlx && chmod +x ~/.local/bin/dsc-mlx
```

```toml
[projects.agent.options]
cli_path = "/Users/you/.local/bin/dsc-mlx"   # dsh 适配器调用方式：dsc-mlx --profile headless "<提示词>"
```

桥接脚本替你处理好的坑：

- 新版 `mlx_lm.server` 会**按请求体 `model` 字段现查现载**——必须传本地绝对路径，传别的会被当成 HF 仓库名去联网查找。
- 这是思考型模型：不传 `"chat_template_kwargs": {"enable_thinking": false}` 的话回复全憋在 `reasoning` 里，`content` 返回为空。
- **安全设计（v3）**：桥只提供纯 Python 实现的只读工具（列目录/读文件/算大小），**不提供任何 shell 执行**。教训来自实测：给去审查模型开放 shell + 黑名单拦截时，模型会主动换写法绕过黑名单执行破坏性命令。

## 踩坑实录（血泪经验）

- `provider_refs` 属于 `[projects.agent]` 层；**激活用**的 `provider` 键在 `[projects.agent.options]` 里。放反了不报错，只会静默 `Not logged in · Please run /login`。
- 守护进程继承 shell 环境——只 export 了 `ANTHROPIC_BASE_URL` 而没有 token 时，拉起的 CLI 会拿错误端点做认证。要么三件套全 export（`BASE_URL` + `AUTH_TOKEN` + `MODEL`），要么全靠 provider 配置。
- 微信 ilink 二维码约 2 分钟过期，`weixin setup` 自动刷新 3 次。手机拿到手、扫一扫就绪后再生成。
- 首次必须由白名单内的微信账号**先发一条消息**完成 `context_token` 握手，之后 `/new` 等指令才会应答。

## 微信遥控电脑

| 命令 | 作用 |
|---|---|
| `/shell <命令>` 或 `! <命令>` | 执行任意 shell（仅 admin） |
| `/show <路径>` | 读文件 |
| `/dir <路径>` | 切换工作目录 |
| `/mode auto\|yolo\|plan` | agent 权限模式 |
| `/whoami` `/status` | 身份与健康状态 |

特权命令由 `admin_from` 把关；`mode = "auto"` 下 agent 的高危操作会以审批提示形式弹回聊天窗口。

## 开机自启（重启不失联）

把 `dsc` 跑成常驻服务：[macOS（launchd）](./docs/autostart.zh-CN.md#macos-launchd)、[Linux（systemd）](./docs/autostart.zh-CN.md#linux-systemd)、[Windows（任务计划 / NSSM）](./docs/autostart.zh-CN.md#windows)，可直接改的示例配置在 [`docs/examples/autostart/`](./docs/examples/autostart/)。

## 文档

各平台接入（weixin / wecom / telegram …）、桥接协议、管理 API 见 [`docs/`](./docs)。

## 许可
