# 架构：怎么从沙箱到达 NAT 后面的机器

> 本文不含任何真实地址。实际值在 `AGENTS.local.md`（由 `arena import` 渲染）与 `secrets.json` 里。

## 1. 约束与设计

| 约束 | 后果 |
|---|---|
| 目标机在 NAT 后面 | 公网无法直连，必须由目标机**主动**建立隧道 |
| 沙箱需要出网 22 | 唯一通道经中转机 SSH 跳板，不能出 22 的环境不可用 |
| 中转机只有转发权 | 它上面不能装我们的工具、不能有我们的业务逻辑 |

两段式解决：**目标机主动连出去 → 沙箱经中转机跳进去**。

```
┌──────────┐   SSH/22   ┌──────────┐   127.0.0.1:2222   ┌──────────┐
│  沙箱     │ ─────────> │  中转机   │ ─────────────────> │ 目标机    │
│  ssh     │  ProxyJump │ sshd     │  （反向隧道入口）     │ sshd     │
└──────────┘            └──────────┘                    └──────────┘
                              ▲                              │
                              └──── ssh -R 2222:... ─────────┘
                                    目标机主动建立的反向隧道
```

> 历史：本项目曾有一条 wss/443 主通道（wstunnel + nginx），用于"只能出网 443"
> 的沙箱。在确认使用环境无 443 限制后已移除（git 历史可考古）。

## 2. 部件

### 2.1 目标机侧：反向隧道

目标机执行一次（长期挂着）：

```
ssh -R 2222:localhost:22 root@<中转机>
```

含义：**目标机的 22 端口**被映射到**中转机的 127.0.0.1:2222**。
注意方向 —— 是目标机连出去，所以不需要中转机主动访问目标机，NAT 不是问题。

断线后需要重建。仓库自带守护脚本（目标机上运行，幂等、可 status/stop、
PID 复用免疫、同机多隧道按端口区分）：

```bash
bash scripts/tunnel-reverse.sh start
```

没有克隆仓库时，手动命令见 `AGENTS.local.md` 排障表。

### 2.2 中转机侧

只需标准的 **sshd 监听 22 端口**，外加上面的反向隧道入口。**没有任何属于我们的组件**，
部署层面无事可做 —— 这也是为什么中转机能保持"别人只提供转发"的边界。

### 2.3 沙箱侧

ssh config 里一个 `ProxyJump` 就完事：

```
Host <目标别名>
    HostName 127.0.0.1
    Port 2222
    ProxyJump <中转机别名>
    ...
```

ssh 先连中转机，**在中转机上**再连 `127.0.0.1:2222`（反向隧道入口），
最终落到目标机 sshd。沙箱侧零额外安装。

## 3. 通道与指纹

只有一条通道（ProxyJump/22）。它落到的是反向隧道尽头的 **sshd**，
主机指纹由 `secrets.json` 里的主机公钥在导入时写入 `known_hosts`
（主机名哈希，`StrictHostKeyChecking yes` 常开）。

## 4. 排障

**只根据 ssh 自己的报错判断**，不要试别的端口/路径。报错 → 含义：

| 报错 | 含义 | 处置 |
|---|---|---|
| `Connection timed out during banner exchange` | 反向隧道那头没响应 | 让用户重建反向隧道 |
| `kex_exchange_identification: Connection closed` | 同上 | 同上 |
| `channel 0: open failed: connect failed: Connection refused` | 跳过去了，但 2222 没人听 | 同上 |
| `Permission denied (publickey)` | 隧道通了，是认证问题 | 检查沙箱私钥；检查目标机 `authorized_keys` |
| `REMOTE HOST IDENTIFICATION HAS CHANGED` | 指纹变了 | **停下问用户**，不要 `ssh-keygen -R` |

**失败时不要做的事**：改 ssh 选项、换端口、试别的路径、`StrictHostKeyChecking=no`、
删 `known_hosts`。这些都是把「可诊断的故障」变成「不可诊断的故障」。

## 5. 安全性质

| 性质 | 由什么保证 |
|---|---|
| 无法被中间人冒充 | `known_hosts` 由 JSON 里的主机公钥生成，`StrictHostKeyChecking yes` |
| 目标机不需要暴露任何公网端口 | 反向隧道是目标机主动发起 |
| 中转机被拿下也只是一个 SSH 跳板 | 其上无我们的组件与数据；入口靠它自己的 sshd 认证 |
| 沙箱只持有一把私钥 | 同一密钥用于目标机与中转跳转 |

## 6. 从零重建这条链路

1. **中转机**：只需能 SSH（22）登录 + 运行上面的反向隧道命令；无部署
2. **目标机**：`ssh-keygen` 生成密钥（或沿用），把公钥加入 `authorized_keys`；
   起反向隧道 `ssh -R 2222:localhost:22 root@<中转机>`
3. **收集主机公钥**：从每台机器的 `/etc/ssh/ssh_host_ed25519_key.pub` 拿到公钥本身
   （不要用指纹 —— 指纹无法反推公钥）
4. **写 `secrets.json`**：密钥、主机、主机公钥、模式串
5. **导入**：`arena import --from-json secrets.json`
6. **验证**：`arena check` 与 `arena sync ssh hostname`

### 反向隧道长期化

`ssh -R` 在会话断开后就没了。要长期稳定，有两个方向：

- 在**目标机**上用 systemd 拉起这条命令并 `Restart=always`（推荐，改的是自己的机器）
- 或让用户手动维持 —— 断了就按排障表里的命令重建

（在**中转机**上做持久化属于改别人的机器，不在本仓库的范围内。）
