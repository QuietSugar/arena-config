#!/bin/sh
# arena installer —— 从 GitHub Release 下载安装 arena 二进制（唯一冷启动入口）。
#
#   curl -fsSL https://raw.githubusercontent.com/QuietSugar/arena-config/main/scripts/install-arena.sh | sh
#
# 环境变量覆盖：
#   ARENA_INSTALL_DIR   安装目录（默认 ~/bin —— 跨沙箱快照保留；勿用 /usr/local/bin）
#   ARENA_VERSION       指定 tag（默认 latest，例如 v0.1.0）
#   GITHUB_TOKEN        私有仓库必填：有 repo 读权限的 token（或 GH_TOKEN）
#                       公开仓库可省略
set -eu

REPO="QuietSugar/arena-config"
INSTALL_DIR="${ARENA_INSTALL_DIR:-$HOME/bin}"
TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"

# 认证下载：私有仓库的 Release 资产匿名 404
auth_curl() {
  if [ -n "$TOKEN" ]; then
    curl -fsSL -H "Authorization: Bearer $TOKEN" "$@"
  else
    curl -fsSL "$@"
  fi
}

os="$(uname -s)"
case "$os" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) echo "error: 不支持的系统：$os（仅 darwin/linux）" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="amd64" ;;
  *) echo "error: 不支持的架构：$arch" >&2; exit 1 ;;
esac

tag="${ARENA_VERSION:-}"
if [ -z "$tag" ]; then
  # 用 releases/latest 的重定向解析最新 tag（不走 API，不占配额）；私有仓库带 token
  tag="$(auth_curl -sSI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')"
fi
if [ -z "$tag" ] || [ "$tag" = "latest" ]; then
  echo "error: 解析不到最新 release tag" >&2
  exit 1
fi
version="${tag#v}"

archive="arena_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "下载 arena $tag（$os/$arch）..."
auth_curl -o "$tmp/$archive" "$base/$archive"
auth_curl -o "$tmp/checksums.txt" "$base/checksums.txt"

cd "$tmp"
# 先取出期望行再校验：管道里 grep 落空会把空 stdin 喂给校验器，sh 没有 pipefail 可能空过
expected="$(grep " $archive\$" checksums.txt || true)"
if [ -z "$expected" ]; then
  echo "error: checksums.txt 里没有 $archive —— 中止" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  printf '%s\n' "$expected" | sha256sum -c - >/dev/null
else
  printf '%s\n' "$expected" | shasum -a 256 -c - >/dev/null
fi
echo "校验和 OK。"

tar -xzf "$archive"
mkdir -p "$INSTALL_DIR"
install -m 0755 arena "$INSTALL_DIR/arena"

echo "已安装：$INSTALL_DIR/arena"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo ""
    echo "提示：$INSTALL_DIR 不在 PATH。写进 shell rc："
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac

"$INSTALL_DIR/arena" --version
