#!/usr/bin/env bash
# vault.sh — 加密保管私密配置
#
#   仓库里只提交密文 vault.enc，明文解密到 private/（已被 .gitignore 排除，永不提交）。
#   算法：OpenSSL AES-256-CBC，密钥由密码经 PBKDF2-SHA256（600000 轮）加随机 salt 派生。
#
# 用法：
#   ./vault.sh unlock        解密 vault.enc -> private/
#   ./vault.sh setup         unlock（如果还没解密）+ 运行 private/scripts/setup-ssh.sh
#   ./vault.sh lock          把 private/ 加密写回 vault.enc（内容没变就跳过）
#   ./vault.sh rekey         更换密码（用旧密码解密，再用新密码加密）
#   ./vault.sh status        查看状态
#   ./vault.sh clean         删除本地明文 private/
#
# 密码来源（按优先级）：
#   1. 环境变量 VAULT_PASSWORD
#   2. 文件 ~/.arena-vault-pass（可选；chmod 600。存了就等于把密码留在这台机器上）
#   3. 交互输入（需要终端）
set -euo pipefail

cd "$(dirname "$0")"
ENC=vault.enc
SUM=vault.sha256          # 明文 tar 的 sha256，用来判断内容是否变化，避免无意义的重新加密
DIR=private
ITER=600000
CIPHER=(-aes-256-cbc -pbkdf2 -iter "$ITER" -md sha256 -salt)

die() { echo "✗ $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null || die "缺少命令：$1"; }
need openssl; need tar; need sha256sum

get_pass() {   # $1 = 提示语；结果放进 VAULT_PASSWORD
  if [ -n "${VAULT_PASSWORD:-}" ]; then return; fi
  if [ -f "$HOME/.arena-vault-pass" ]; then
    VAULT_PASSWORD=$(head -n1 "$HOME/.arena-vault-pass"); export VAULT_PASSWORD; return
  fi
  [ -r /dev/tty ] || die "没有可用终端：请设置 VAULT_PASSWORD 环境变量"
  read -r -s -p "$1: " VAULT_PASSWORD </dev/tty; echo >&2
  [ -n "$VAULT_PASSWORD" ] || die "密码为空"
  export VAULT_PASSWORD
}

make_tar() {   # 确定性打包：同样的内容产生同样的字节，便于比对是否有变化
  tar --sort=name --mtime='2000-01-01 00:00Z' --owner=0 --group=0 --numeric-owner --mode=go-rwx \
      --exclude="$DIR/bin" -cf - "$DIR"
}

do_unlock() {
  [ -f "$ENC" ] || die "找不到 $ENC"
  get_pass "Vault 密码"
  local tmp; tmp=$(mktemp)
  if ! openssl enc -d "${CIPHER[@]}" -pass env:VAULT_PASSWORD -in "$ENC" -out "$tmp" 2>/dev/null \
     || ! tar -tf "$tmp" >/dev/null 2>&1; then
    rm -f "$tmp"; die "解密失败：密码错误，或 vault.enc 已损坏"
  fi
  if [ -f "$SUM" ] && [ "$(sha256sum < "$tmp" | cut -d' ' -f1)" != "$(cat "$SUM")" ]; then
    rm -f "$tmp"; die "校验失败：解密结果与 $SUM 不一致"
  fi
  rm -rf "$DIR"; tar -xf "$tmp"; rm -f "$tmp"
  chmod -R go-rwx "$DIR"
  echo "✓ 已解密到 $DIR/（不会被提交）。先阅读 $DIR/AGENTS.md"
}

do_lock() {
  [ -d "$DIR" ] || die "没有 $DIR/，无需加密"
  local tmp new; tmp=$(mktemp)
  make_tar > "$tmp"
  new=$(sha256sum < "$tmp" | cut -d' ' -f1)
  if [ "${1:-}" != "--force" ] && [ -f "$ENC" ] && [ -f "$SUM" ] && [ "$new" = "$(cat "$SUM")" ]; then
    rm -f "$tmp"; echo "✓ 内容没有变化，vault.enc 保持不变"; return
  fi
  get_pass "Vault 密码（加密用）"
  # 已有 vault 时先用这个密码试解一次，防止输错密码导致新旧密码不一致
  if [ "${1:-}" != "--force" ] && [ -f "$ENC" ] && \
     ! openssl enc -d "${CIPHER[@]}" -pass env:VAULT_PASSWORD -in "$ENC" 2>/dev/null | tar -t >/dev/null 2>&1; then
    rm -f "$tmp"; die "密码与现有 vault.enc 不一致（要换密码请用 rekey）"
  fi
  openssl enc -e "${CIPHER[@]}" -pass env:VAULT_PASSWORD -in "$tmp" -out "$ENC.tmp"
  mv "$ENC.tmp" "$ENC"; echo "$new" > "$SUM"; rm -f "$tmp"
  echo "✓ 已加密写入 $ENC。记得 git add $ENC $SUM && git commit && git push"
}

do_rekey() {
  do_unlock
  unset VAULT_PASSWORD
  local p1 p2
  [ -r /dev/tty ] || [ -n "${NEW_VAULT_PASSWORD:-}" ] || die "请设置 NEW_VAULT_PASSWORD"
  if [ -n "${NEW_VAULT_PASSWORD:-}" ]; then p1=$NEW_VAULT_PASSWORD; else
    read -r -s -p "新密码: " p1 </dev/tty; echo >&2
    read -r -s -p "再输一次: " p2 </dev/tty; echo >&2
    [ "$p1" = "$p2" ] || die "两次输入不一致"
  fi
  [ ${#p1} -ge 12 ] || die "密码至少 12 位"
  VAULT_PASSWORD=$p1 do_lock --force
}

case "${1:-}" in
  unlock) do_unlock ;;
  setup)  [ -d "$DIR" ] || do_unlock; bash "$DIR/scripts/setup-ssh.sh" "${@:2}" ;;
  lock)   do_lock "${2:-}" ;;
  rekey)  do_rekey ;;
  clean)  rm -rf "$DIR"; echo "✓ 已删除明文 $DIR/" ;;
  status)
    echo "vault.enc : $( [ -f "$ENC" ] && echo "存在，$(stat -c %s "$ENC") 字节" || echo 无)"
    echo "private/  : $( [ -d "$DIR" ] && echo 已解密 || echo 未解密)"
    if [ -d "$DIR" ] && [ -f "$SUM" ]; then
      [ "$(make_tar | sha256sum | cut -d' ' -f1)" = "$(cat "$SUM")" ] && echo "变更      : 无" || echo "变更      : 有未加密的修改 → ./vault.sh lock"
    fi ;;
  *) sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; exit 1 ;;
esac
