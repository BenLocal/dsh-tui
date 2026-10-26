# 开发环境 / 推送 / 发布

> 开发环境依赖、凭据存放约定、Git 推送与本地安装。

## 开发环境
- Go 1.27.1，PATH 上有 `dsh`（`npm i -g @deepseek-ai/dsh`）。单元测试不需要 dsh。
- 手工试跑会读 `$DSH_HOME`（默认 `~/.dsh`）下的 profile 和会话日志，并真实调用模型。试跑时用临时目录作 `-cwd`，优先 `-mode read-only`。

> ⚠️ API key 等凭据**不入库**：用 dsh 网页「模型」页的凭据存储，或放环境变量。需要记录的本地信息放未入库文件（如 `dev-env.local.md`，加入 `.gitignore`）。

## Git 推送 / 拉取
仓库还没有配置远端。

## 发布
没有发布流程：`make install` 装到 `~/go/bin/dsh-tui`。
