# 版本发布流程

推 tag 即发布：`release.yml`（GitHub Actions + GoReleaser）自动构建四平台二进制、
算出 `checksums.txt`、创建 Release。**无需本地构建或手工上传资产。**

## 发版清单

1. **确认 master 就绪**：CI 是绿的，代码是自洽的。
2. **改默认版本号（最容易漏的一步）**：仓库里有三处写给"自动解析失败的人"看的
   硬编码示例，发版时必须改成新 tag：
   - `scripts/install-arena.sh` 头部注释的 `ARENA_VERSION` 示例（约第 8 行）
   - `scripts/install-arena.sh` 解析失败报错里的提示（`ARENA_VERSION=vX.Y.Z ...`）
   - `USAGE.md` 安装一节里的 `ARENA_VERSION=vX.Y.Z` 示例
3. 提交并推送 master。
4. 打 tag 并推送（tag 带 `v` 前缀）：

   ```bash
   git tag v0.1.2
   git push origin v0.1.2
   ```

5. 等 Actions 跑完后验证：
   - Release 页面资产齐全：`arena_0.1.2_{linux,darwin}_{amd64,arm64}.tar.gz` + `checksums.txt`
   - 匿名走一遍真实安装路径（装到临时目录，别覆盖 `~/bin/arena`）：

     ```bash
     curl -fsSL https://raw.githubusercontent.com/QuietSugar/arena-config/master/scripts/install-arena.sh \
       | ARENA_INSTALL_DIR="$(mktemp -d)" sh
     ```

   - `arena --version` 应输出新版本号。

## 版本号从哪来

- 二进制里的版本**不用手改**：`main.go` 的 `var version = "dev"` 由 GoReleaser
  构建时注入（`.goreleaser.yaml` 的 `-X main.version={{ .Version }}`），
  `.Version` 取自 tag（去掉 `v`），所以 `v0.1.2` 的 `--version` 输出是 `arena 0.1.2`。
- 需要手改的永远只有上一步列出的**文档/脚本示例**，它们存在的意义是：
  自动解析 latest 失败的网络里，用户照抄示例也能装上当前版本。版本不一致 = 装到旧版。

## 规则

- 只从 master 打 tag；tag 只前进不复用（已推送的 tag 要改内容，只能升版本号）。
- 发版顺序不能乱：**先改示例并推送，再打 tag**——否则 Release 里的报错提示还在指旧版。
