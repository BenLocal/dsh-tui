# dsh-tui

DeepSeek Harness 的终端客户端。它在后台启动 `dsh --profile sdk`，通过 stdio 上的 JSON-RPC 驱动 agent，在终端里显示对话、思考过程和工具调用。

## 安装

需要 Go 1.27+ 和 PATH 上的 `dsh`（`npm i -g @deepseek-ai/dsh`）。

```bash
make install      # 装到 ~/go/bin/dsh-tui
make test         # vet + 带 race 检测的单元测试，不调用模型
```

## 使用

```bash
dsh-tui                         # 在当前目录开一个新会话
dsh-tui -cwd ~/proj -mode read-only
dsh-tui -cwd ~/proj -resume tui-20261026-113752-51feeccc
```

| 参数 | 说明 |
|---|---|
| `-cwd` | agent 的工作目录，也是沙箱的工作区根目录，默认当前目录 |
| `-mode` | `read-only`、`workspace-write`（默认）、`danger-full-access`，对应 dsh 的 `DSH_PERMISSION_MODE` |
| `-model` | `<provider>/<model>`。不传时沿用 web profile 的模型，没有 web profile 时用 `deepseek-official/deepseek-flash` |
| `-model-from` | 沿用指定 profile 的模型和 provider 定义 |
| `-reasoning` | 推理强度，如 `high` |
| `-provider-base-url`、`-provider-key-env` | 定义 `-model` 里的 provider（OpenAI 兼容接口） |
| `-resume` | 续跑一个已持久化的会话，`-cwd` 要和当初一致 |
| `-patch` | 额外的 dsh 配置覆盖层，可重复 |

按键：回车发送，`ctrl+j` 或 `alt+enter` 换行，`ctrl+t` 展开或收起思考过程，`pgup`/`pgdown` 翻页，`end` 回到底部，`ctrl+c` 退出。退出时会打印续跑命令。

## 实现说明

- **不改 dsh 配置**：sdk profile 只认内置 provider。web profile 里自定义的 provider（比如 opencode-go）是从它的 `cordis.patch.yml` 读出 `llm-pi-ai` 条目，写进临时 `--patch` 文件传给 dsh，退出时删除。`~/.dsh` 下的文件只读不写。
- **凭据**：sdk profile 默认启用 dsh 的凭据存储，dsh 网页「模型」页里保存的 key 直接可用。
- **续跑的历史**：`session/resume` 不会把旧事件重新推送出来，历史是从 `~/.dsh/sessions/*/<会话 ID>/session.v4.jsonl.zstd` 读出来再画的。

代码结构：

| 包 | 职责 |
|---|---|
| `internal/sdk` | 启动 dsh 子进程、JSON-RPC 收发、协议类型 |
| `internal/profile` | 读 profile 的模型配置，生成临时 patch |
| `internal/history` | 读持久化的会话日志 |
| `internal/ui` | Bubble Tea 界面：事件折叠成对话记录并渲染 |

## 已知限制

这些来自 dsh sdk 协议本身（dsh 0.2.1-rc.2）：

- **没有逐字流式输出**：模型每一步的回复整段到达，等待期间状态栏显示「运行中」。
- **不能中途取消**：协议没有取消方法，`ctrl+c` 会退出整个客户端，当前这一轮会被中断。会话已持久化，可以 `-resume` 接着聊。
- **没有审批交互**：服务端还不会向客户端发审批请求，权限只能在启动时用 `-mode` 选定。
- **发消息时正在运行**：消息会排队，当前这一轮结束后处理。
