#!/usr/bin/env bash
# tunnel-reverse.sh —— 目标机（C 端）反向隧道守护脚本
#
# 把本机 22 端口推到中转机（B）的 127.0.0.1:<REMOTE_PORT>：
#   C(本机) sshd:22  ←──  B 127.0.0.1:<REMOTE_PORT>  ←──  沙箱经 ProxyJump 进来
#
# 用法（在目标机上）：
#   bash tunnel-reverse.sh start     # 起隧道（幂等；已运行则直接成功退出）
#   bash tunnel-reverse.sh stop      # 停隧道（只杀验证过的自家进程，PID 复用不误杀）
#   bash tunnel-reverse.sh restart
#   bash tunnel-reverse.sh status    # running / stopped（校验进程身份，不只看 PID 文件）
#
# 一台机器要开多条隧道（如另一台机器占了 2222）：用环境变量覆盖即可，互不干扰：
#   REMOTE_PORT=2223 bash tunnel-reverse.sh start
#
# 配置项均可环境变量覆盖：
#   B_USER / B_HOST      中转机登录（默认 root@<占位，请按实际改或传环境变量>）
#   REMOTE_PORT          中转机环回端口（默认 2222；同机多隧道必须错开）
#   LOCAL_TARGET         本机被转发目标（默认 localhost:22）
#   PIDFILE / LOGFILE    状态与日志路径（默认按 REMOTE_PORT 区分）
#
# 退出码：0 = 成功（含幂等命中）；1 = 失败（起不来/状态异常）；2 = 用法错误。
set -u

B_USER="${B_USER:-root}"
B_HOST="${B_HOST:-<中转机>}"
REMOTE_PORT="${REMOTE_PORT:-2222}"
LOCAL_TARGET="${LOCAL_TARGET:-localhost:22}"
PIDFILE="${PIDFILE:-/tmp/tunnel-${REMOTE_PORT}.pid}"
LOGFILE="${LOGFILE:-/tmp/tunnel-${REMOTE_PORT}.log}"

# 判定"这个 PID 确实是我们这条 ssh -R 隧道"：看 /proc/<pid>/cmdline，
# 防止 PID 复用后把无辜进程当成隧道（status 误判 / stop 误杀）。
is_our_tunnel() {
  local pid="$1"
  [ -n "$pid" ] && [ -d "/proc/$pid" ] || return 1
  tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -q "ssh .*-R 127\.0\.0\.1:${REMOTE_PORT}:${LOCAL_TARGET}"
}

current_pid() {
  [ -f "$PIDFILE" ] || return 1
  local pid
  pid="$(cat "$PIDFILE" 2>/dev/null)"
  is_our_tunnel "$pid" && { echo "$pid"; return 0; }
  rm -f "$PIDFILE"   # 残留 PID 文件，清理掉
  return 1
}

do_start() {
  if pid=$(current_pid); then
    echo "already running (pid $pid, port $REMOTE_PORT)"
    return 0
  fi
  # 不用 ssh -f + pgrep 抓 PID（会抓错/抓多）；nohup 后台后 $! 就是真 PID
  nohup ssh -N \
    -o ServerAliveInterval=30 \
    -o ServerAliveCountMax=3 \
    -o ExitOnForwardFailure=yes \
    -o BatchMode=yes \
    -R "127.0.0.1:${REMOTE_PORT}:${LOCAL_TARGET}" \
    "${B_USER}@${B_HOST}" >>"$LOGFILE" 2>&1 &
  echo $! > "$PIDFILE"
  sleep 1   # 给 ExitOnForwardFailure 一个失败即退的窗口（如端口被占）
  if pid=$(current_pid); then
    echo "tunnel started (pid $pid, 127.0.0.1:${REMOTE_PORT} -> ${LOCAL_TARGET} via ${B_USER}@${B_HOST})"
    echo "log: $LOGFILE"
    return 0
  fi
  rm -f "$PIDFILE"
  echo "failed to start; last log lines:" >&2
  tail -5 "$LOGFILE" >&2 2>/dev/null || true
  echo "（常见原因：REMOTE_PORT 在中转机上被占 / 到中转机的 SSH 认证失败）" >&2
  return 1
}

do_stop() {
  if pid=$(current_pid); then
    kill "$pid" 2>/dev/null
    for _ in 1 2 3 4 5; do is_our_tunnel "$pid" || break; sleep 1; done
    is_our_tunnel "$pid" && kill -9 "$pid" 2>/dev/null
    echo "tunnel stopped (was pid $pid, port $REMOTE_PORT)"
  else
    echo "not running (port $REMOTE_PORT)"
  fi
  rm -f "$PIDFILE"
  return 0
}

do_status() {
  if pid=$(current_pid); then
    echo "running (pid $pid, 127.0.0.1:${REMOTE_PORT} -> ${LOCAL_TARGET})"
    return 0
  fi
  echo "stopped (port $REMOTE_PORT)"
  return 1
}

case "${1:-}" in
  start)            do_start ;;
  stop)             do_stop ;;
  restart)          do_stop; do_start ;;
  status)           do_status ;;
  -h|--help|help)
    sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'
    ;;
  *)
    echo "usage: $0 {start|stop|restart|status}" >&2
    exit 2
    ;;
esac
