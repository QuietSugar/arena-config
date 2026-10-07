# AGENTS.md —— 在这个仓库里开发前先读（开发文档）

> **本文件面向开发者（大概率是 AI agent）**：仓库规则、架构、代码组织、构建验证。
> 使用手册见 [USAGE.md](USAGE.md)（安装/日常操作/排障），产品介绍见 [README.md](README.md)。
>
> **本仓库是公开仓，里面没有任何机密**（真实主机、密钥、路径前缀都不在），
> 它们只在一份运行时才交给你的 `secrets.json` 里。
> 本文件是通用规则与项目说明；导入机密后还会渲染出 `AGENTS.local.md`
> （环境专属规则，含真实主机信息，已被 gitignore）。**两者冲突时以 `AGENTS.local.md` 为准。**

---

## 1. 项目概述

**arena-config** 是一个 Go 单二进制工具（`arena`）+ 两个零依赖 shell 底线文件：
在沙箱里安全地操作一台 NAT 后面的目标机 —— 建立连接、同步代码、跑远端脚本，
同时保证**机密永不进入这个仓库**。公开仓只含源码、模板（占位符）和文档；
所有私密值由用户在开工时粘贴一份 `secrets.json`，由 `arena import` 渲染成可用环境。

模型一句话：**目标机主动建反向隧道连到中转机，中转机只做转发，沙箱经 SSH 跳板（22）连进去。**

```
你的沙箱 ──SSH/22（ProxyJump）──> 中转机(sshd) ──> 目标机 sshd
                                             （127.0.0.1:2222 反向隧道，
                                               由目标机主动建立）
```

- **目标机**在 NAT 后面，公网无法直连；它的 `ssh -R 2222:localhost:22 root@<中转机>`
  把本机 22 端口映射为中转机上的 `127.0.0.1:2222`。
- **中转机**只做转发。我们在它上面没有任何操作权限，也不需要有 —— 其上无任何我们的组件。
- **沙箱 → 中转机需要出网 22**（ProxyJump）。不能出 22 的环境不可用本工具。

## 2. 技术栈与依赖

- **主体是 bash 脚本仓库**（救场类工具：导入、自检、同步、钩子），无 Makefile 类构建系统；
  核心操作全部在 **Go 单二进制 `arena`**（cmd/arena，子命令式），单测齐全；shell 只剩两个零依赖底线文件（pre-commit 钩子、install-arena.sh）。
- 语言：bash + **Go**（单二进制 `arena`，`cmd/arena/`）；secrets.json 的解析/校验/渲染在
  `internal/secrets`，模板内嵌二进制（go:embed），空沙箱无需克隆仓库即可导入。
  Go 部分仅依赖 golang.org/x/crypto（构建期拉取，发布物为静态二进制），`go build -o bin/arena ./cmd/arena` 即可构建。
- 依赖（沙箱侧）：`bash`（pre-commit 钩子与 install-arena.sh）、`rsync`、`git`、`curl`、OpenSSH 客户端；
  arena 本体为静态 Go 二进制，无运行时依赖；
  开发/构建 Go 工具需 Go 1.23+（沙箱里通常预装，缺失时按 README「环境假设」从官方下载）。
  目标机侧无需安装任何东西；中转机侧只需标准 sshd，无任何我们的组件。
- 许可：MIT（见 `LICENSE`）。

## 3. 代码组织

| 路径 | 职责 |
|---|---|
| `scripts/arena-import.sh` | **主入口**：`secrets.json` → 密钥、ssh config、known_hosts、派生配置、`AGENTS.local.md`。幂等，`--print-schema` 打印 JSON 结构说明 |
| `cmd/arena/` | **Go 单二进制**（构建产物 `bin/arena`，走 GitHub Release 分发）：`import` 建环境 / `bootstrap` 自检修复 / `check` 连通判定 / `scan` 泄露审计 / `probe` 出网探测 / `sync` 代码同步 |
| `internal/secrets/` | secrets.json 的解析校验、密钥配对、known_hosts 哈希（HMAC-SHA1，与 ssh-keygen -H 逐字节一致） |
| `arena scan` | **泄露审计**：工作区 + git 全历史 + 不可达对象 + 敏感文件名 + 提交元数据（`--remote` 顺带查远端） |
| `hooks/pre-commit` | 提交前拦截机密（敏感文件名 + 通用特征 + 本环境专属模式） |
| `cmd/arena/templates/` | 带 `{{占位符}}` 的 ssh config 与 AGENTS.local 模板（go:embed 内嵌进二进制），由 arena import 用 secrets.json 渲染 |
| `docs/` | `architecture.md`（链路怎么搭、怎么排障）、`file-sync-workflow.md`（rsync 工作流与踩坑）、`secrets-handling.md`（机密存取与泄漏处置） |

运行时派生文件（全部**不进仓库**，由 `.gitignore` 排除）：`AGENTS.local.md`、`secrets.json`、
`~/.arena-secrets/`（secrets.json、env.sh、secret-patterns）、`~/.ssh/config`、`~/.ssh/known_hosts`。

## 4. 开工三步

开发与使用的共同前置（完整用法与排障见 [USAGE.md](USAGE.md)）。
维护者每次开工先扫一眼 [Issues](https://github.com/QuietSugar/arena-config/issues)
——使用者的改进诉求都记在那里（每条只写大概要求）：

```bash
arena import --from-json /path/to/secrets.json   # 1. 导入机密（或 --from-stdin / 默认路径）
arena bootstrap                                  # 2. 沙箱被回收过就自检修复
arena check                                      # 3. 确认连通
```

导入后 `ssh <目标机别名>`、`arena sync` 即可用，`AGENTS.local.md` 会被渲染出来。

## 5. 硬规则

### ① 中转机是 TRANSIT ONLY

命中任意一条即为违规：直接 `ssh` 到中转机执行命令（它的别名只允许被 `ProxyJump` 使用）、
读取或修改它的任何文件、对它做端口扫描 / 指纹探测 / `ssh-keyscan`、在它上面「排查问题」。
允许的只有一条路径：流量经 `ssh <目标机别名>` 穿过。

### ② 目标机连不上时，只报告，不绕路

只看 ssh 自己的报错，把原文交给用户，并给出对应处置（具体命令见 `AGENTS.local.md`）：

| ssh 报错 | 含义 |
|---|---|
| `banner exchange` 超时 / `kex_exchange_identification` | HTTPS 反向隧道断了，需要用户在内网机器重建（`ssh -R 2222:localhost:22 root@<中转机>`） |
| `channel 0: open failed: connect failed: Connection refused` | 同上，反向隧道没在监听 |
| `Permission denied (publickey)` | 隧道通了，是认证问题（查私钥 / authorized_keys） |
| `REMOTE HOST IDENTIFICATION HAS CHANGED` | 主机指纹变了 → **停下问用户**，绝不 `ssh-keygen -R` |

**不要**为了让命令跑通去改 ssh 选项、换端口、找别的路径。等用户修好，然后重试。

### ③ 主机指纹

`known_hosts` 由导入脚本从 secrets.json 里的主机**公钥**生成（主机名哈希），
`StrictHostKeyChecking yes` 一直开着。指纹变了 = 要么对方重装，要么有人在中间。
**停下，报告，等用户决定。** 不要 `-o StrictHostKeyChecking=no`，不要删 known_hosts。

### ④ 代码同步用 arena sync，不要用 ssh 写文件

```bash
arena sync pull  <远端路径>    # 改文件前先拉
# 本地编辑
arena sync diff  <远端路径>    # 推送前必看
arena sync push  <远端路径>
```

直接用 ssh 往远端写文件时，嵌套引号会被本地和远端两层 shell 各解析一次，很容易出错。
详见 `docs/file-sync-workflow.md`。

### ⑤ 永远不要提交私密信息

这是公开仓。真实 IP、域名、内网段、路径前缀、任何密钥都不得进入提交。
三道防线：`.gitignore`（本环境专属文件）→ `hooks/pre-commit`（文件名 + 通用特征 +
本环境专属模式，模式串来自 `~/.arena-secrets/secret-patterns`）→ `arena scan`（含全历史）。
万一误提交：**视为已泄露**，先轮换密钥与路径前缀，再处理仓库（删分支 ≠ 删数据，
详见 `docs/secrets-handling.md`）。

## 6. 构建与验证

没有构建步骤。改动脚本后的验证方式（按顺序）：

```bash
# 1. 语法检查（bash 部分）
for f in scripts/*.sh arena sync hooks/pre-commit; do bash -n "$f"; done

# 1b. Go 部分：格式 / 静态检查 / 单测 / 构建
gofmt -l ./cmd                       # 无输出 = 格式 OK
go vet ./... && go test ./...
go build -o bin/arena ./cmd/arena    # bin/ 已被 gitignore

# 2. 不带机密的功能自测
arena import --print-schema             # 应打印 schema 说明
arena probe                             # 不需要任何私钥，可直接跑

# 3. 提交前 / 推送前必跑（全历史泄露审计，退出码 = 问题数）
arena scan
arena scan --remote            # 顺带看远端分支

# 4. 钩子生效性自查（沙箱快照会静默丢掉 .git/config → 钩子失效）
git config core.hooksPath                       # 应输出 hooks；空输出 = 没在跑，先跑 arena bootstrap
```

环境级验证需要 secrets.json 已导入：`arena bootstrap`（自检修复）、
`arena check`（连通性）、`arena sync status`（同步状态）。

## 7. 代码风格与约定

- 所有脚本是 bash，头部注释块写清**用途、用法、退出码语义**；`usage()` 从头注释提取（`sed -n ... "$0"`）。
- 严格模式：主流程脚本 `set -euo pipefail`，容错型自检脚本 `set -u`（如 hooks/pre-commit）。
- 输出统一用彩色助手函数：`ok`（绿）/ `warn`（黄）/ `bad|die`（红），消息用中文。
- 仓库内**绝不出现真实主机信息**：模板和脚本一律用 `{{占位符}}` 或从 `~/.arena-secrets/env.sh` 运行时派生
  （如 arena sync 的 `SSH_HOST` 取 `DCUPSYNC_HOST > ARENA_TARGET_ALIAS > target`）。
- `arena sync` 里的 rsync 参数（`--no-perms --no-owner --no-group --checksum`）和排除项是踩坑后的固化结论，
  改动前先读 `docs/file-sync-workflow.md` 第 2 节；`rsync -e` 里**绝不能带主机名**。
- 沙箱是会话制的，写代码时记住：只有 `$HOME`（含 `~/bin`、`~/.ssh`、`~/.arena-secrets`）跨会话保留；
  快照会丢 `.git/config`、剥离可执行位（只 `chmod +x` 即可，别误判成缺失去重装）、丢已装软件；
  不要把持久化文件放 `/usr/local/bin` 或叫 `.arena` 的目录（在平台排除名单里）。
- 提交信息用中文（参考 git log 里的既有风格）。

## 8. 文档索引

- [USAGE.md](USAGE.md) — **使用手册**（安装/导入/日常/排障；给人也给 agent）
- [README.md](README.md) — 产品介绍（作用与能力，不含开发内容）
- `docs/architecture.md` — 单通道链路的完整设计、安全性质、从零重建步骤
- `docs/file-sync-workflow.md` — arena sync 用法、rsync 参数依据、顺序陷阱、连接复用、远端 git 注意事项
- `docs/secrets-handling.md` — secrets.json 模型、三道防线、已知的坑、泄漏处置流程
- `docs/release.md` — **版本发布流程**（推 tag 自动发版；发版时必改的三处默认版本号示例）
