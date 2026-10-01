# TODO —— 当期不做、后续可能做

> 每个 Phase 开工前先扫一眼这里。做完一项勾一项。

- [ ] `arena import --merge`：把合并式安装（`Include` 拼回、冲突密钥 `.arena` 改名）做成一等能力，替代现在手工挪移-拼回流程
- [ ] `arena sync` 原生 SFTP 实现：去掉 rsync 运行时依赖（rsync 参数语义见 docs/file-sync-workflow.md 第 2 节，移植时逐条对齐）
- [ ] `arena scan` 机密熵值/相似度检测：补纯模式串匹配抓不住的漏
- [ ] `arena scan` 元数据检查对 rebase 过敏：任何 rebase/amend 过的历史都会报"时间不一致"，考虑降噪（如仅当差异异常大时升级）
- [ ] 主通道（wss/443）若复活，分层诊断代码从 git 历史捞回（最后存在于 `cmd/target-check` @ 7e49aef 的父提交）
- [ ] `arena update` 独立子命令（当前自我更新内嵌在 `arena bootstrap` 里）
- [ ] Windows 构建：GoReleaser 加 `GOOS=windows`，install 脚本补 PowerShell 版
- [ ] secrets.json 多 profile 支持（如 prod/staging 多环境一份 JSON 切换）
- [ ] pre-commit 薄封装化：`arena scan --staged`（等 arena 二进制在各沙箱普及后再替换 bash 版钩子）
- [ ] CI 增加端到端冒烟： Release 出来后自动跑 `install-arena.sh` + `arena probe --json` 验证分发链路
