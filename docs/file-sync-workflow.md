# 代码同步工作流（沙箱 ↔ 目标机）

> 🚫 规则见 [AGENTS.md](../AGENTS.md)：中转机只转发，不探测、不修改。本文所有命令都走 `ssh target`。

**什么时候需要本文**：要改目标机上的代码/配置时。核心原则一句话 ——
**别用 ssh 直接往远端写文件，改成「拉到本地改、再推回去」**。原因见 [第 3 节](#3-为什么不要用-ssh-直接写文件)。

---

## 1. 工具：`arena sync`

arena 二进制自带，走 `ssh target`（ProxyJump 经中转机 22，需要出网 22）。

> `arena import` 会把 arena 自安装到 **`~/bin/arena`**（`~/bin` 已写进 `.bashrc` 的 PATH）。
> ⚠️ 不要用 `/usr/local/bin` —— 它不在快照范围内，重启即丢。

```bash
arena bootstrap   # 先确保环境就绪
SYNC="arena sync"

# 典型循环
$SYNC pull   <远端路径>      # 1. 拉到 ~/sync/<目录名>/
#    2. 用本地编辑器改
$SYNC diff   <远端路径>      # 3. 干跑，看清会改什么
$SYNC push   <远端路径>      # 4. 推回
$SYNC ssh    'cd <远端路径> && <验证命令>'   # 5. 远端验证
```

| 子命令 | 作用 |
|---|---|
| `pull <远端路径> [本地目录]` | 远端 → 本地（**改文件前先拉**） |
| `push <远端路径> [本地目录]` | 本地 → 远端 |
| `diff <远端路径> [本地目录]` | 干跑，只看会改什么（**推送前必看**） |
| `status <远端路径> [本地目录]` | 两边文件数/大小对比 |
| `run <本地脚本> [参数...]` | 把本地脚本推到远端执行（**免引号地狱，推荐**） |
| `ssh <命令...>` | 在远端执行一条命令 |

- 本地目录省略时默认 `~/sync/<远端目录名>`
- `push` 默认**不删**远端多余文件；确需镜像要显式 `push --delete <远端路径>`

---

## 2. rsync 的四个参数为什么必须这样设

```bash
OPTS=(-a --no-owner --no-group --no-perms --checksum --human-readable --itemize-changes)
```

| 参数 | 不加会怎样 |
|---|---|
| `--no-perms` | 沙箱快照把本地权限压成 0644，而远端 `scripts/*.sh` 是 **0775**。不加会把远端的**执行位抹掉**，之后远端直接跑脚本报 `Permission denied` |
| `--no-owner --no-group` | 沙箱的 `user:user` 会覆盖远端的 `dev:dev` |
| `--checksum` | 沙箱恢复后所有文件 mtime 都变了，不加会**无脑重传全部文件**（还会把远端 mtime 刷成新时间） |
| `-e ssh` | 走 `ssh/config` 里的 `target` 别名，自动套用 ProxyJump 配置，无需手写跳转 |

### ⚠️ `-e` 后面**不要带主机名**（含别名）

rsync 的 `-e` 只接受「ssh + 选项」，**主机名由远端规格提供**，rsync 会自己追加。带了就重复：

```bash
# ✗ 错法一：-e 里塞了完整 ssh 命令（含主机名）
SSH_CMD="ssh -i ~/.ssh/id_ed25519 -J root@... -p 2222 <目标机用户>@<目标机>"
rsync -e "$SSH_CMD" ./local/ "<目标机用户>@<目标机>:$R/"
#   → rsync 执行：ssh ... <目标机用户>@<目标机> <目标机用户>@<目标机>:...  → zsh: command not found: 127.0.0.1

# ✗ 错法二：-e 里带了 ssh 别名（同样会被当主机名再追加一次）
rsync -e "ssh target" ./local/ "target:$R/"
#   → rsync 执行：ssh target target:...  → zsh:1: command not found: target

# ✓ 对：-e 只给 ssh（主机名走 ~/.ssh/config 的别名解析）
rsync -e ssh ./local/ "target:$R/"
```

**记法**：`-e` 管「怎么连」，远端规格管「连哪里」。两者不能都写主机名。

> `arena sync` 内部把这两者分开：`RSYNC_RSH`（只给 `-e`，默认 `ssh`）与 `SSH_HOST`（远端规格，默认 `target`）。

### 排除项

```bash
--exclude=logs/             # 远端构建日志，同步过来只污染
--exclude=config/session/   # loom 会话缓存，两端各自演化，双向同步只会互相覆盖
```

`config/session/` 值得特别说明：它是 loom 的交互会话缓存（gitignore），**不要在两端之间同步**。
它曾在沙箱侧存有几十个文件、而远端只剩 1 个（远端被别人更新过）——此时若 `push --delete`，
会把远端的会话记录删掉。排除它之后 rsync 也会保护它不被 `--delete` 波及。

---

## 3. 为什么不要用 ssh 直接写文件

```bash
# ✗ 反面教材：嵌套引号被本地和远端两层 shell 各解析一次
ssh target "cat > /path/file.yml <<'EOF'
command: bash -c 'DAYS=\${DAYS:-20} ...'
EOF"
```

症状是 `syntax error near unexpected token`、内容被截断、或变量被提前展开。
**正确做法**：本地写好脚本 → 传上去执行：

```bash
$SYNC run ./scripts/apply-change.sh          # 脚本内容不经过任何一层 shell 解析
```

`run` 内部用 stdin 把脚本送到远端 `/tmp` 再 `bash` 执行，结束后自动清理。

---

## 4. 顺序陷阱：远端跑过构建之后，必须先拉再推

目标机上跑构建类工作流（如 `loom -f build`）会**重写**这些文件：

```
config/option/select-since-date.json     # 自动生成的候选快照
config/option/select-since-commit.json
build/modules.txt                        # 本次要构建的模块列表
config/session/*.yml                     # 交互会话
```

此时**远端比本地新**。如果直接 `push`，会把这些新文件覆盖回旧版 —— 数据被静默回退，而且不容易发现。

**固定顺序**：

```bash
# 远端跑完构建
$SYNC pull   <远端路径>     # ← 先拉，把远端的新文件同步下来
$SYNC diff   <远端路径>     # ← 确认 diff 为空，再动手改
```

同理，`push` 前一定先 `diff`。若 `diff --dry-run` 里出现反方向的变化（远端有本地没有的东西），
说明本地落后了，先 pull。

---

## 5. 验证闭环

推送后**必须在远端验证**，不能只看本地：

```bash
# 语法/结构自检（YAML/JSON/shell 都能就地校验）
$SYNC run ./scripts/verify.sh

# 或者直接跑真实流程
$SYNC ssh 'cd <远端路径> && <构建或测试命令>'
```

经验：**内容改动容易过，环境相关的坑通常在真跑时才暴露**。例如改工作流 YAML 时，
YAML 解析能过不代表跑得通（枚举值、变量展开、执行顺序都可能出问题）。

---

## 6. 加速：连接复用

每次建连都要做完整的握手（含经中转机跳转的两跳），实测**单次 2.8~3.5 秒**。
启用连接复用后，后续连接降到 **约 0.53 秒（提升 5 倍）**。

**已默认开启**（写在仓库的 `ssh/config` 模板里，`arena import` 会建好 `~/.ssh/cm`）：

```
ControlMaster auto
ControlPath ~/.ssh/cm/%n.sock
ControlPersist 10m
```

> ⚠️ **socket 路径必须用 `%n`（别名）而不是 `%r@%h:%p`。**
> 目标机的 `%h:%p` 是 `127.0.0.1:2222`，如果以后加了别名不同的新通道，
> 用 `%h:%p` 会让不同通道共用 socket，互相复用到失效连接，现象极难排查。
> `%n` 展开为命令行里写的别名，天然隔离。

**失效 socket 的表现（已实测）**：主连接进程被强杀、socket 文件残留时，`ControlMaster auto`
会自动回退到新建连接（3.5s 成功），**不会**抛出难读的 socket 错误 —— 因此不影响
「只凭 ssh 报错判断隧道状态」的纪律。

---

## 7. 远端的 git 操作

目标机上的代码库集中放在一个目录下（多个托管平台并存，含内网 Git 服务）。
具体路径由 `secrets.json` 的 `default_remote_root` 给出。

```bash
# 提交前建议加两道自检：只暂存目标子树 + 扫描暂存区有无凭据
$SYNC run ./scripts/commit.sh
```

- 目标机的 git 配了 HTTP 代理（`http.proxy` / `https.proxy`），**HTTPS 方式依赖该代理可用；SSH 出网不走代理**
- 远端登录 shell 是 **zsh**，`~/dir/*/*/x` 这类 glob 匹配不到会直接报 `no matches found` 并中断。
  用 `bash -lc '...'` 包裹，或写完整路径
- 推送前确认分支与远端的关系：`git status -sb`、`git rev-list --count @{u}..HEAD`

---

## 8. 常见报错对照

| 报错 | 原因 | 处理 |
|---|---|---|
| `zsh:1: no matches found: ...` | 远端是 zsh，glob 未命中 | 用 `bash -lc '...'` 包裹或写完整路径 |
| `Permission denied` 跑远端 `*.sh` | 执行位被同步抹掉了 | `--no-perms`；已损坏则 `$SYNC ssh 'chmod +x <路径>'` |
| `zsh:1: command not found: 127.0.0.1` | rsync `-e` 里带了主机名 | 改成 `-e ssh` |
| `Permission denied (publickey)` | 本地私钥权限被压成 0644 | `arena bootstrap` |
| `command not found: rsync` | 沙箱重启丢了软件 | 同上（会自动装） |
| `UNPROTECTED PRIVATE KEY FILE` | 私钥权限 0644 | 同上（会自动修） |
| 全量重传、很慢 | 少了 `--checksum` | 加上 |
| 远端文件被回退到旧版 | 没先 pull 就 push | 见第 4 节 |
