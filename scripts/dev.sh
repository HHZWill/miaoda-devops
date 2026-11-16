#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT_PATH="$ROOT_DIR/scripts/dev.sh"
BACKEND_DIR="$ROOT_DIR/be"
FRONTEND_DIR="$ROOT_DIR/fe"
API_PORT="${PORT:-18080}"
FRONTEND_PORT="${FRONTEND_PORT:-5173}"
RUNTIME_DIR="${TMPDIR:-/tmp}/miaoda-deployment-platform-dev-${UID}"
WATCHER_PID_FILE="$RUNTIME_DIR/be-watcher.pid"
BACKEND_PID_FILE="$RUNTIME_DIR/be-server.pid"
FRONTEND_PID_FILE="$RUNTIME_DIR/fe.pid"
BACKEND_LOG="$RUNTIME_DIR/be.log"
FRONTEND_LOG="$RUNTIME_DIR/fe.log"
SERVER_BIN="$RUNTIME_DIR/server"

find_go() {
  local go_bin
  go_bin="$(command -v go || true)"
  # PATH 中无 Go 时从本地 SDK 目录查找。
  if [[ -z "$go_bin" && -d "$HOME/sdk" ]]; then
    go_bin="$(find "$HOME/sdk" -maxdepth 3 -type f -path '*/bin/go' -perm -111 2>/dev/null | sort | tail -n 1)"
  fi
  printf '%s' "$go_bin"
}

read_pid() {
  local file="$1"
  # PID 文件不存在时返回空值。
  if [[ ! -f "$file" ]]; then
    return
  fi
  tr -cd '0-9' < "$file"
}

wait_for_exit() {
  local pid="$1"
  local attempt
  # 最多等待两秒让进程完成清理。
  for attempt in {1..20}; do
    if ! kill -0 "$pid" 2>/dev/null; then
      return
    fi
    sleep 0.1
  done
}

stop_pid_file() {
  local file="$1"
  local label="$2"
  local pid
  pid="$(read_pid "$file")"
  # 仅停止仍存活的已记录进程。
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    echo "停止${label}：$pid"
    kill "$pid" 2>/dev/null || true
    wait_for_exit "$pid"
  fi
  rm -f "$file"
}

stop_port() {
  local port="$1"
  local pids
  pids="$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
  # 清理占用开发端口的旧进程。
  if [[ -n "$pids" ]]; then
    echo "停止占用端口 $port 的进程：$pids"
    kill $pids 2>/dev/null || true
  fi
}

stop_dev() {
  mkdir -p "$RUNTIME_DIR"
  stop_pid_file "$FRONTEND_PID_FILE" "前端开发服务"
  stop_pid_file "$WATCHER_PID_FILE" "后端监听服务"
  stop_pid_file "$BACKEND_PID_FILE" "后端开发服务"
}

start_detached() {
  local log_file="$1"
  shift
  # Node 独立会话让服务脱离当前终端并保留目录权限。
  node - "$log_file" "$@" <<'NODE'
const { spawn } = require('node:child_process')
const fs = require('node:fs')

const [logFile, command, ...args] = process.argv.slice(2)
const output = fs.openSync(logFile, 'a')
const child = spawn(command, args, {
  detached: true,
  env: process.env,
  stdio: ['ignore', output, output],
})
child.unref()
console.log(child.pid)
NODE
}

backend_signature() {
  find "$BACKEND_DIR" -type f \
    \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) \
    -exec stat -f '%N:%m:%z' {} + 2>/dev/null |
    LC_ALL=C sort |
    shasum |
    awk '{print $1}'
}

watch_backend() {
  local go_bin="$1"
  local backend_pid=""
  local previous_signature
  local current_signature

  cleanup_backend() {
    # 退出监听器时同步停止后端服务。
    if [[ -n "$backend_pid" ]] && kill -0 "$backend_pid" 2>/dev/null; then
      kill "$backend_pid" 2>/dev/null || true
      wait "$backend_pid" 2>/dev/null || true
    fi
    rm -f "$BACKEND_PID_FILE"
  }

  build_and_restart() {
    local next_bin="$RUNTIME_DIR/server.next"
    echo "[$(date '+%F %T')] 检测到后端变更，开始编译。"
    # 编译成功后再替换运行中的服务。
    if ! "$go_bin" -C "$BACKEND_DIR" build -o "$next_bin" ./cmd/server; then
      echo "[$(date '+%F %T')] 后端编译失败，保留当前服务。"
      rm -f "$next_bin"
      return
    fi
    # 新版本就绪后停止旧进程。
    if [[ -n "$backend_pid" ]] && kill -0 "$backend_pid" 2>/dev/null; then
      kill "$backend_pid" 2>/dev/null || true
      wait "$backend_pid" 2>/dev/null || true
    fi
    mv "$next_bin" "$SERVER_BIN"
    (
      cd "$BACKEND_DIR"
      PORT="$API_PORT" exec "$SERVER_BIN"
    ) &
    backend_pid=$!
    printf '%s\n' "$backend_pid" > "$BACKEND_PID_FILE"
    echo "[$(date '+%F %T')] 后端已启动：http://127.0.0.1:$API_PORT/"
  }

  trap cleanup_backend EXIT INT TERM
  previous_signature="$(backend_signature)"
  build_and_restart
  # 轮询源码指纹，实现无额外依赖的自动重启。
  while true; do
    sleep 0.8
    current_signature="$(backend_signature)"
    # 源码未变化时继续等待。
    if [[ "$current_signature" == "$previous_signature" ]]; then
      continue
    fi
    previous_signature="$current_signature"
    build_and_restart
  done
}

run_frontend() {
  local vite_bin="$1"
  cd "$FRONTEND_DIR"
  exec "$vite_bin" --host 127.0.0.1 --port "$FRONTEND_PORT"
}

start_dev() {
  local go_bin
  local vite_bin="$FRONTEND_DIR/node_modules/.bin/vite"
  local watcher_pid
  local frontend_pid
  local attempt
  local api_ready=false
  local frontend_ready=false

  go_bin="$(find_go)"
  # 校验 Node.js 和 npm。
  if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
    echo "错误：未找到 Node.js 或 npm。" >&2
    exit 1
  fi
  # 校验 Go 环境。
  if [[ -z "$go_bin" ]]; then
    echo "错误：未找到 Go，请安装 Go 1.22 或更高版本。" >&2
    exit 1
  fi
  # 首次启动时安装前端依赖。
  if [[ ! -x "$vite_bin" ]]; then
    npm --prefix "$FRONTEND_DIR" ci
  fi

  mkdir -p "$RUNTIME_DIR"
  stop_dev
  stop_port "$API_PORT"
  stop_port "$FRONTEND_PORT"
  : > "$BACKEND_LOG"
  : > "$FRONTEND_LOG"

  watcher_pid="$(start_detached "$BACKEND_LOG" /usr/bin/env \
    PORT="$API_PORT" FRONTEND_PORT="$FRONTEND_PORT" \
    "$SCRIPT_PATH" __watch_backend "$go_bin")"
  printf '%s\n' "$watcher_pid" > "$WATCHER_PID_FILE"

  frontend_pid="$(start_detached "$FRONTEND_LOG" /usr/bin/env \
    PORT="$API_PORT" FRONTEND_PORT="$FRONTEND_PORT" \
    "$SCRIPT_PATH" __run_frontend "$vite_bin")"
  printf '%s\n' "$frontend_pid" > "$FRONTEND_PID_FILE"

  # 等待前后端完成首次启动。
  for attempt in {1..50}; do
    if curl -fsS "http://127.0.0.1:$API_PORT/api/health" >/dev/null 2>&1; then
      api_ready=true
    fi
    if curl -fsS "http://127.0.0.1:$FRONTEND_PORT/" >/dev/null 2>&1; then
      frontend_ready=true
    fi
    if [[ "$api_ready" == true && "$frontend_ready" == true ]]; then
      break
    fi
    sleep 0.2
  done

  # 任一服务未就绪时输出日志并退出。
  if [[ "$api_ready" != true || "$frontend_ready" != true ]]; then
    echo "错误：热启动失败。" >&2
    tail -n 20 "$BACKEND_LOG" "$FRONTEND_LOG" >&2 || true
    stop_dev
    exit 1
  fi

  echo "热启动完成：http://127.0.0.1:$FRONTEND_PORT/"
  echo "前端修改自动刷新，后端 Go 文件修改自动编译重启。"
  echo "日志目录：$RUNTIME_DIR"
  echo "停止服务：./scripts/dev.sh stop"
}

show_status() {
  local watcher_pid
  local frontend_pid
  watcher_pid="$(read_pid "$WATCHER_PID_FILE")"
  frontend_pid="$(read_pid "$FRONTEND_PID_FILE")"
  # 两个守护进程都存活才视为运行中。
  if [[ -n "$watcher_pid" && -n "$frontend_pid" ]] &&
    kill -0 "$watcher_pid" 2>/dev/null &&
    kill -0 "$frontend_pid" 2>/dev/null; then
    echo "热启动运行中：http://127.0.0.1:$FRONTEND_PORT/"
    echo "后端监听 PID：${watcher_pid}，前端 PID：${frontend_pid}"
    return
  fi
  echo "热启动未运行。"
  exit 1
}

ACTION="${1:-start}"
# 内部模式仅负责监听后端文件。
if [[ "$ACTION" == "__watch_backend" ]]; then
  watch_backend "$2"
  exit
fi
# 内部模式仅负责运行 Vite 开发服务。
if [[ "$ACTION" == "__run_frontend" ]]; then
  run_frontend "$2"
  exit
fi

case "$ACTION" in
  start)
    start_dev
    ;;
  stop)
    stop_dev
    echo "热启动已停止。"
    ;;
  restart)
    stop_dev
    start_dev
    ;;
  status)
    show_status
    ;;
  *)
    echo "用法：./scripts/dev.sh [start|stop|restart|status]" >&2
    exit 2
    ;;
esac
