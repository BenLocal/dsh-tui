# 系统架构

> 包边界、关键链路、数据流与设计决策；动结构、加包、改与 dsh 的交互方式前先看本文。

本文回答「这个程序由哪些部分组成、消息怎么流过它们、为什么这么切」。**技术选型和验证命令在 `tech-stack.md`**，两边别重复。

## 一、整体结构

```
终端 ⇄ ui (Bubble Tea) ──Prompt──> sdk.Client ──stdin JSON-RPC──> dsh --profile sdk 子进程
             ^                          │  <──stdout 响应/通知────┘
             └──── Notifications() ─────┘
main ──> profile（读 $DSH_HOME/profiles/*，写临时 --patch）
main ──> history（续跑时读 $DSH_HOME/sessions/*/<id>/session.v4.jsonl.zstd）
```

- 部署形态：单个本地二进制，每次运行启动一个 dsh 子进程，退出时关闭。
- 进程边界：dsh-tui 与 dsh 是两个进程，只经子进程的 stdio 通信；stderr 留最近一段尾巴用于报错。

## 二、包与依赖方向

| 包 | 职责 | 允许依赖 | 禁止依赖 |
|---|---|---|---|
| `main` | 解析参数、选模型、组装 dsh 参数、串起其余包 | 全部 `internal/*` | — |
| `internal/sdk` | 启动 dsh 子进程、JSON-RPC 收发、协议类型与通知解码 | 标准库 | 其他 `internal/*` |
| `internal/profile` | 读 profile 的模型配置与 provider 条目，生成临时 patch | 标准库 | 其他 `internal/*` |
| `internal/history` | 只读解码持久化的会话日志 | `internal/sdk`（复用 `Event`） | `ui`、`profile` |
| `internal/ui` | 事件折叠成对话记录（`Transcript`）并渲染，处理按键 | `internal/sdk` | `profile`、`history` |

- 依赖方向硬规则：`internal/sdk` 是底层，不依赖其他内部包；`ui` 不直接读磁盘上的 dsh 文件，续跑历史由 `main` 读好后经 `ui.Session.PriorLogs` 传入。
- 新增包：放在 `internal/` 下，由 `main` 组装。

## 三、关键链路

**启动一个会话**
1. `main.run`：校验 `-mode` / `-cwd`；`chooseModel` 决定 provider/model，必要时从 web profile 取 provider 条目。
2. 有 provider 条目时 `profile.WritePatch` 写临时文件，作为 `--patch` 传给 dsh，`defer os.Remove` 清理。
3. `sdk.Start` 启动 `dsh --profile sdk`，`Initialize` 握手（2 分钟超时）；续跑时再调 `session/resume`。
4. `tea.NewProgram(ui.New(...))` 进入界面；退出后打印续跑命令。

**发一条消息**
1. 回车 → `ui.Model.send` 调 `Client.Prompt`（`session/prompt`，30 秒超时）。它只是入队，马上返回。
2. 回合运行期间 dsh 推送 `session.event` / `session.status` / `subagent.*` 通知；`waitNote` 每次取一条，`sdk.Decode` 解码后交给 `Transcript.Apply` 或更新运行状态。
3. 通知 channel 关闭表示 dsh 退出：界面转为断开状态，显示退出原因和 stderr 尾部。

## 四、数据与存储

- 会话的权威记录是 dsh 自己写的会话日志，本程序只读。
- 本程序唯一写入的文件是临时 patch，退出即删。
- 续跑：`session/resume` 不会重放旧事件，所以历史从日志文件解码，`Transcript` 用同一套 `Apply` 渲染。

## 五、外部依赖

| 依赖 | 用途 | 协议 | 超时 | 挂了怎么办 |
|---|---|---|---|---|
| dsh（sdk profile） | agent 运行时 | 子进程 stdio 上按行分隔的 JSON-RPC 2.0 | initialize 2 分钟、prompt 30 秒、关闭 10 秒 | 进行中的调用返回 `ErrClosed`，界面显示断开原因；会话已持久化，可 `-resume` |

## 六、关键设计决策与取舍

- **不改用户的 dsh 配置**
  - 背景：sdk profile 只认内置 provider，web profile 里的自定义 provider 它看不到。
  - 取舍：从 web profile 的 `cordis.patch.yml` 摘出 `llm-pi-ai` 条目写临时 patch，而不是修改 sdk profile。代价是要跟随 patch 格式变化。
  - 重新评估的信号：dsh 的 sdk profile 能直接引用其他 profile 的 provider。
- **历史从磁盘日志读，不靠协议重放**
  - 背景：dsh 0.2.1-rc.2 的 `session/resume` 不推送旧事件。
  - 取舍：依赖 dsh 的日志路径和 `session.v4` 格式。
  - 重新评估的信号：协议支持重放，或日志格式版本变化。
- **不做流式输出、取消、审批交互**：协议本身不支持（见 `README.md`《已知限制》），不要在客户端模拟。

## 七、扩展点

- 新的通知类型：在 `sdk.Decode` 加解码，在 `ui` 里处理，参照 `session.status`。
- 新的 RPC 方法：在 `internal/sdk/protocol.go` 加一个 `Client` 方法包装 `Call`，参照 `Prompt` / `Resume`。
- 明确禁止的改法：写 `$DSH_HOME` 下的任何文件；在 `ui` 里直接起子进程或读 dsh 文件；绕过 `sdk.Client` 自己读写 dsh 的 stdio。
