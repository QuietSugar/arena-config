# USAGE —— arena 使用手册

> 给人也给 agent：只看这篇就能把环境跑起来、把日常工作做完。
> 每条命令都遵循同一契约：**退出码 0 = 成功 / 1 = 业务失败 / 2 = 用法错误**，
> 全部支持 `--json` 结构化输出（供程序消费）。想改代码请看 [AGENTS.md](AGENTS.md)。

## 1. 安装 arena

空沙箱唯一要做的预置（需要能访问 GitHub）：

```bash
curl -fsSL https://raw.githubusercontent.com/QuietSugar/arena-config/main/scripts/install-arena.sh | sh
```

- 装到 `~/bin/arena`（跨沙箱快照保留），自动完成 sha256 校验
- 本仓库为**公开仓**，匿名下载即可；若你 fork 成了私有部署，给脚本一个有读权限的
  token（`export GITHUB_TOKEN=<有读权限的 token>`）即可
- 指定版本：`ARENA_VERSION=v0.1.0`；指定目录：`ARENA_INSTALL_DIR=...`
- 无 Release 时（开发期）：克隆仓库后 `go build -o bin/arena ./cmd/arena`

## 2. 第一次使用

向目标机管理员索取一份 `secrets.json`（明文 JSON，含密钥与主机信息，**别到处粘贴**），然后：

```bash
arena import --from-json /path/to/secrets.json   # 或：cat secrets.json | arena import --from-stdin
```

导入会做：写密钥（含配对校验）→ 渲染 ssh 配置 → 生成哈希 known_hosts →
写派生配置 → 启用 pre-commit 钩子 → 自安装到 `~/bin`。
在仓库目录里跑还会渲染出 `AGENTS.local.md`（环境专属规则）。

```bash
arena check     # 期望：[OK] 可达：<主机名> <用户>
```

## 3. 日常速查

| 要做什么 | 命令 |
|---|---|
| 目标机通不通 | `arena check [--json]` |
| 沙箱被回收后恢复 | `arena bootstrap`（修 git 配置/权限/工具，并自更新到最新版） |
| 把远端代码拉下来 | `arena sync pull [远端路径]`（省略路径用导入时配置的默认根） |
| 看 push 会改什么 | `arena sync diff [远端路径]`（**push 前必看**） |
| 推回去 | `arena sync push [远端路径]` |
| 在远端跑本地脚本 | `arena sync run ./script.sh [参数...]`（免引号地狱，推荐） |
| 远端执行一条命令 | `arena sync ssh '<命令>'` |
| 两端文件对比 | `arena sync status [远端路径]` |
| 提交/推送前审计 | `arena scan [--remote]`（工作区+全历史+悬空对象查机密） |
| 看网络通不通外网 | `arena probe [--json]` |
| 查看任何命令用法 | `arena <命令> --help` |

改代码的固定循环（顺序不能乱）：

```bash
arena sync pull <路径>     # 1. 先拉
#   2. 本地改
arena sync diff <路径>     # 3. 干跑确认
arena sync push <路径>     # 4. 推回
arena sync ssh 'cd <路径> && <验证命令>'   # 5. 远端验证
```

注意：`push` 默认不删远端文件；镜像式同步要显式 `push --delete`，且务必先 `diff`。
远端跑过构建之后（会自动重写 option/session/build 等文件），**必须先 pull 再改**。

## 4. 连不上时怎么办

`arena check` 只如实转述 ssh 的报错，并对照下表给出处置——**不要自己改 ssh
选项、换端口、找别的路径**，修好后再重试：

| ssh 报错 | 含义 | 给谁修 |
|---|---|---|
| `banner exchange` 超时 / `kex_exchange_identification` / `Connection closed` | 反向隧道断了 | 内网机器执行 `bash scripts/tunnel-reverse.sh start`（仓库内，含 status/stop/自动保活；无仓库时用原始命令 `ssh -R 2222:localhost:22 root@<中转机>`，确切值见 `AGENTS.local.md`） |
| `channel 0: open failed: connect failed: Connection refused` | 同上，反向隧道没在监听 | 同上：`bash scripts/tunnel-reverse.sh start` |
| `Permission denied (publickey)` | 通道通，认证失败 | 查私钥是否完整 / 服务端 authorized_keys |
| `REMOTE HOST IDENTIFICATION HAS CHANGED` | 主机指纹变了 | **停下来问目标机管理员**（可能重装，也可能被中间人），绝不 `ssh-keygen -R` |
| `Could not resolve hostname` | 环境没导入 | 先 `arena import` |

使用纪律（任何时候都成立）：

- 中转机是 TRANSIT ONLY：只让流量经 `ssh <目标别名>` 穿过，不登录、不探测、不在上面排查。
- `arena sync` 写远端走 rsync/脚本上传，**不要**用 ssh 重定向写文件（嵌套引号必翻车）。
- 机密只走 `secrets.json` → `arena import` 一条路；怀疑泄漏立刻 `arena scan` 并见
  [docs/secrets-handling.md](docs/secrets-handling.md)。

## 5. 其他场景

- **换 secrets.json（轮换密钥/换环境）**：直接重新 `arena import`，幂等覆盖派生文件。
- **想知道 JSON 各字段含义**：`arena import --print-schema`。
- **离线/内网环境**：本工具假定 GitHub 可达（下载二进制与 Release 更新）；不具备时
  改为在联网机器构建好二进制后拷贝进来。
- **开发/贡献**：见 [AGENTS.md](AGENTS.md)；产品能力介绍见 [README.md](README.md)。
