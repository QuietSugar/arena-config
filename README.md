# arena-config

沙箱 Agent 的个人配置仓库。**所有私密内容都加密存放在 `vault.enc` 中**，clone 不需要密码，使用前需要密码解密。

| 文件 | 说明 |
|---|---|
| `vault.enc` | 加密的配置包（AES-256-CBC，密钥由密码经 PBKDF2-SHA256 60 万轮派生） |
| `vault.sha256` | 明文包的校验值，用来确认解密结果完整、判断内容是否有变化 |
| `vault.sh` | 解密、加密、换密码的工具，只依赖 `openssl`、`tar`、`sha256sum` |
| `private/` | 解密后的明文目录，**已被 `.gitignore` 排除，永远不会提交** |

## 使用

> ⚠️ **仓库是私有的**：clone 需要凭据（Deploy Key 私钥或访问令牌），
> 由仓库所有者提供给新沙箱。拿到之后按下面步骤操作。

```bash
# 1. 写入凭据（内容由仓库所有者提供，路径以 ssh/ 里的说明为准）
#    chmod 600 <凭据文件>

# 2. 克隆（走 SSH over 443）
git clone ssh://git@ssh.github.com:443/QuietSugar/arena-config.git ~/arena-config
cd ~/arena-config

# 3. 解密并安装（密码由仓库所有者提供）
VAULT_PASSWORD='<密码>' ./vault.sh setup

# 4. 然后阅读 private/AGENTS.md 和 private/README.md
```

有终端时可以省略 `VAULT_PASSWORD`，脚本会提示输入密码。

> 如果哪天把本仓库改成公开，clone 就能免凭据（直接 `git clone https://…`）。
> 但请注意：改成公开后，`vault.enc` 及其全部历史对所有人可见 —— 内容是密文，
> 但密码一旦泄露就等于全公开。

## 修改配置

```bash
# 编辑 private/ 下的文件，然后：
VAULT_PASSWORD='<密码>' ./vault.sh lock
git add vault.enc vault.sha256 && git commit -m "update vault" && git push
```

- 换密码：`./vault.sh rekey`
- 查看状态：`./vault.sh status`
- 删除本地明文：`./vault.sh clean`

> ⚠️ 不要把 `private/` 的任何内容、密码或解密结果提交到仓库。
