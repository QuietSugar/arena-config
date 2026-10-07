# 机密处理：公开仓 + 运行时注入

> 一句话：**公开仓里没有任何机密；机密全部在一份运行时交给你的 `secrets.json` 里。**

## 1. 模型

```
┌─ 公开仓（本仓库）─────────────────────────┐
│  代码、模板、文档、schema                  │
│  占位符 {{TRANSIT_HOST}} / {{WS_PREFIX}}   │
└───────────────────────────────────────────┘
                 ▲ 渲染（arena import）
┌─ secrets.json（用户粘贴，唯一私密载体）───┐
│  密钥(base64)、主机、端口、前缀、模式串    │
└───────────────────────────────────────────┘
```

机密清单（这些东西**只能**出现在 `secrets.json` 和本机磁盘上）：

- SSH 私钥（两把：一把给目标机+中转机跳转，一把给代码托管）
- 主机地址、端口、内网 IP、域名
- 目标机上的工作路径

> ⚠️ **base64 不是加密。** 它只保证字节精确（粘贴不会改动尾随换行、注释字段），
> 不提供任何保密性。整份 JSON 是明文，**泄漏即全部泄漏**。

## 2. 三道防线

| 防线 | 机制 | 拦得住什么 |
|---|---|---|
| 1. `.gitignore` | 忽略 `secrets.json`、`AGENTS.local.md`、`secret-patterns`、`*.local.md`、密钥文件 | 日常 `git add .` 不会误加 |
| 2. `hooks/pre-commit` | 敏感文件名 + 通用机密特征 + **本环境专属模式** | 绕过了 gitignore 的 `git add -f`、文档里手写进去的地址 |
| 3. `arena scan` | 工作区 + **全历史对象** + 不可达对象 | 历史残留、别人推上来的东西 |

模式串（真实 IP、域名、前缀）不写在公开的代码里，而是来自
`~/.arena-secrets/secret-patterns` —— 由 `arena import` 从 JSON 生成。
所以公开仓的检查逻辑是通用的，**项目专属的部分留在了本机**。

## 3. 已知的坑（都踩过）

### 坑 1：`git add` 会立刻把 blob 写进本地对象库，钩子拦不住

钩子拦的是**提交**，但 `git add -f <文件>` 在钩子运行**之前**就已经把内容写进 `.git/objects` 了。
提交被拦下后，这些内容变成**不可达对象**，静静留在本地磁盘上。

- **好消息**：`git push` 只发送可达对象，这些残留**不会推到远端**（实测过）
- **坏消息**：它们还在本地。`git fsck` 能看到，明文就在里面
- **处理**：`git reflog expire --expire=now --all && git gc --prune=now`

`arena scan` 的第 4 项会报出「有 N 个不可达对象，其中 M 个含机密特征」并给出清理命令。

### 坑 2：沙箱快照会丢弃 `.git/config` → 钩子静默失效

平台把 `.git/config` 当敏感路径排除，所以沙箱恢复后：

- 没有远端、没有上游分支
- **`core.hooksPath` 一起消失 → pre-commit 钩子形同未装**

第 3 条是**安全隐患**，不是不便：钩子没了，含机密的文件能直接提交成功（实测过）。
`arena bootstrap` 的第 0 步专门修这个。**每轮开工先跑它。**

自查一行：`git config core.hooksPath` 应输出 `hooks`，空输出就是没在跑。

### 坑 3：快照还会剥离可执行位

`~/bin/arena`、`hooks/pre-commit`、`scripts/*.sh` 的文件都在，但权限变成 `0644`。
**别误判成「缺失」去重装**：如果 arena 自身不可执行，bootstrap 还没启动就会失败，
无法靠它修复自己。先由系统 shell 恢复二进制执行位，再交给 bootstrap 修复其余环境：

```sh
chmod 755 "$HOME/bin/arena" && "$HOME/bin/arena" bootstrap
```

自定义安装目录时替换两处路径并保留双引号；不要对密钥或整个目录批量 `chmod`。
`chmod` 本身不需要网络，后续 bootstrap 的自更新与连通检查仍可能联网。
文件确实不存在则应重新安装。完整区分与排障见
[使用手册：快照恢复急救](../USAGE.md#快照恢复急救)。

### 坑 4：OpenSSH 不认 `$HOME` 环境变量

测试隔离环境时容易上当：`HOME=/tmp/x ssh ...` **不会**让 ssh 去读 `/tmp/x/.ssh/config`，
它用 passwd 里的家目录。要隔离必须显式指定：

```bash
ssh -F /tmp/x/.ssh/config -o UserKnownHostsFile=/tmp/x/.ssh/known_hosts <别名>
```

（这也意味着：别指望用 `HOME=` 前缀来「换一套配置」，要换就换整个用户。）

### 坑 5：不要把私密数据目录叫 `.arena`

平台的快照排除名单里包含 `.arena`、`.cache`、`.local` 等目录名。
放进去的文件**不会**跨会话保留，且是静默丢失。
所以本方案用 `~/.arena-secrets/`。

## 4. 用户怎么把 JSON 交给我

三种方式（脚本都支持）：

```bash
# 直接贴进来（推荐一次粘贴完成）
arena import --from-stdin <<'JSON'
{ "schema": "arena-secrets/1", ... }
JSON

# 或先落盘
install -m 600 /dev/stdin ~/.arena-secrets/secrets.json
arena import

# 或走参数
arena import --from-json /path/to/secrets.json
```

导入脚本会：写入密钥（权限 600）→ 校验私钥↔公钥配对 → 渲染 ssh config →
用**主机公钥**生成 `known_hosts`（主机名哈希）→ 写派生配置与模式串 → 渲染 `AGENTS.local.md`。

**粘贴即视为进入对话记录。** 需要更严格的话，让用户把 JSON 放到磁盘上、只告诉你路径。

## 5. 泄漏了怎么办

按顺序，**先止血再收拾**：

1. **吊销凭据**（根本解决，假设已泄露）
   - 目标机：换掉 `authorized_keys` 里的那把公钥
   - 代码托管：删除并重建 Deploy Key
   - 中转机：换路径前缀（注意三处：中转机两个配置 + `secrets.json`）
   - 重新生成 `secrets.json` 并**重新导入每个在用环境**
2. **判断范围**
   - 只是本地不可达对象 → `git gc --prune=now` 即可
   - 进了提交且**推过** → 撤不回来，走第 3 步
3. **更彻底**：本仓库很小，重建成本极低。删库重建比清洗历史可靠得多。
   若必须清洗，GitHub Support 可协助清除服务端的不可达对象（服务端 GC 不保证及时）。
4. **假设攻击者手里有旧值**：轮换要彻底，别只换一半。

> 记住：**删分支 ≠ 删数据**。托管平台按 SHA 提供对象，旧提交对象只要还在服务端，
> 知道 SHA 且有读权限的人就能 `git fetch <远端> <SHA>` 取回来。

## 6. 定期自查

```bash
arena scan            # 工作区 + 全历史
arena scan --remote   # 顺便看远端分支
```

推送前跑一次；改动敏感内容后跑一次。
