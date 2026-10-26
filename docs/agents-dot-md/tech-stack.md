# 技术栈与实现规范

> 技术选型、依赖与配置约定、构建与验证命令；写代码前先看本文，按既有方式扩展。

本文回答「这个项目用了什么、新代码该照着什么写、改完怎么验证」。**系统结构（包边界、关键链路、设计决策）在 `architecture.md`**，两边别重复。

## 一、项目概览

- 定位：DeepSeek Harness（dsh）的终端客户端。后台启动 `dsh --profile sdk`，经 stdio 上的 JSON-RPC 驱动 agent，在终端显示对话、思考过程和工具调用。
- 仓库形态：单个 Go module（`module dsh-tui`），单一二进制，入口 `main.go`，业务代码在 `internal/` 下。
- 主要语言与版本：Go 1.27.1（`go.mod`）。
- 运行依赖：PATH 上的 `dsh`（`npm i -g @deepseek-ai/dsh`），开发时对照的版本是 0.2.1-rc.2。
- 结构总览：见 `architecture.md`

## 二、核心技术栈

| 类别 | 选型 | 版本 | 备注 |
|---|---|---|---|
| 语言 / 运行时 | Go | 1.27.1 | |
| TUI 框架 | `charmbracelet/bubbletea` | v1.3.10 | Elm 式 Model / Update / View |
| TUI 组件 | `charmbracelet/bubbles` | v1.0.0 | textarea、viewport、spinner |
| 样式 | `charmbracelet/lipgloss` | v1.1.0 | |
| 压缩 | `klauspost/compress`（zstd） | v1.20.3 | 只用于读 dsh 的会话日志 |
| JSON-RPC | 标准库 `encoding/json` 手写 | — | 不引入 JSON-RPC 库 |
| 构建工具 | `go` + `Makefile` | — | |

> 只列**实际在用**的；同类能力已有选型时不再引入第二套（见 `code-checklist.md`）。

## 三、配置

- 本程序自身没有配置文件，全部行为由命令行参数决定（参数表见 `README.md`）。
- dsh 的配置在 `$DSH_HOME`（默认 `~/.dsh`，见 `profile.Home()`）：**只读不写**。需要覆盖时生成临时 `--patch` 文件传给 dsh，退出时删除。
- 权限模式通过环境变量 `DSH_PERMISSION_MODE` 传给 dsh 子进程。
- API key：不经本程序传递；用 dsh 凭据存储，或 `-provider-key-env` 指定的环境变量名（只传变量名，不传值）。

## 四、构建与验证命令

改完代码至少跑一遍对应命令再交付。测试全部离线，不调用模型（`internal/sdk` 用 helper 进程假扮 dsh）。

```bash
# 编译到 bin/dsh-tui（bin/ 已在 .gitignore）
make build

# vet + 带 race 检测的全量单元测试
make test

# 单包测试（改动集中在某包时用这个，快）
go test -race ./internal/ui/

# 格式化
gofmt -l -w .

# 安装到 $GOBIN（~/go/bin）
make install
```

真机验证需要 PATH 上有 dsh 并能连上模型：`go run . -cwd <临时目录> -mode read-only`。

## 五、发布方式

本地工具，没有部署流程：`make install` 装到 `~/go/bin/dsh-tui`。

## 六、其他约定

- 用户可见文案（报错提示、状态栏、通知）用简体中文；代码注释和标识符用英文。
- 错误统一在 `main` 里打印成 `dsh-tui: <err>` 并以非零码退出：参数错误返回 2，运行错误返回 1。
- dsh 返回的错误附上子进程 stderr 尾部（`withStderr`），便于定位。

## 七、快速参考：支持一种新的 dsh 通知

以「显示一种新的会话事件类型」为例：

1. `internal/sdk/protocol.go` — 在 `Decode` / `EventData` 里加解码。
2. `internal/ui/transcript.go` — 在 `Transcript.Apply` 里把事件折叠成 entry，必要时在 `Render` 里加渲染。
3. `internal/ui/transcript_test.go` — 用 `ev(...)` 构造事件，断言渲染结果。
4. 验证：`make test`。

> 挂载点与禁止的改法见 `architecture.md` 的《扩展点》。
