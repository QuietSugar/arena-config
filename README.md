# arena-config

沙箱 Agent 的个人配置仓库。**所有私密内容都加密存放在 `vault.enc` 中**，clone 不需要密码，使用前需要密码解密。

| 文件 | 说明 |
|---|---|
| `vault.enc` | 加密的配置包（AES-256-CBC，密钥由密码经 PBKDF2-SHA256 60 万轮派生） |
| `vault.sha256` | 明文包的校验值，用来确认解密结果完整、判断内容是否有变化 |
| `vault.sh` | 解密、加密、换密码的工具，只依赖 `openssl`、`tar`、`sha256sum` |
| `private/` | 解密后的明文目录，**已被 `.gitignore` 排除，永远不会提交** |

## 使用

```bash
git clone https://github.com/QuietSugar/arena-config.git ~/arena-config
cd ~/arena-config

# 解密并安装（密码由仓库所有者提供）
VAULT_PASSWORD='<密码>' ./vault.sh setup

# 然后阅读 private/AGENTS.md 和 private/README.md
```

有终端时可以省略 `VAULT_PASSWORD`，脚本会提示输入密码。

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
