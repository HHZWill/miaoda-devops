#!/bin/bash

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
go_bin="$(command -v go || true)"
# 构建时从本机 SDK 查找 Go，成品启动不依赖 Go。
if [[ -z "$go_bin" && -d "$HOME/sdk" ]]; then
  go_bin="$(find "$HOME/sdk" -maxdepth 3 -type f -path '*/bin/go' -perm -111 2>/dev/null | sort | tail -n 1)"
fi
if [[ -z "$go_bin" ]] || ! command -v node >/dev/null || ! command -v npm >/dev/null; then
  printf '%s\n' "构建需要 Node.js、npm 和 Go。" >&2
  exit 1
fi
# 前端仅在构建启动包时编译。
if [[ ! -d "$root/fe/node_modules" ]]; then
  npm --prefix "$root/fe" ci
fi
npm --prefix "$root/fe" run build
mkdir -p "$root/dist"
stage="$(mktemp -d "$root/dist/.windows-app.XXXXXXXX")"
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/web"
cp -R "$root/fe/dist/." "$stage/web/"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 "$go_bin" -C "$root/be" \
  build -trimpath -o "$stage/server.exe" ./cmd/server
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 "$go_bin" -C "$root/be" \
  build -trimpath -ldflags '-H=windowsgui' -o "$stage/妙搭部署平台.exe" ./cmd/desktop
node --input-type=module - "$root" "$stage" <<'NODE'
import fs from 'node:fs'
import path from 'node:path'

const [root, stage] = process.argv.slice(2)
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'packages/manifest.json'), 'utf8'))
const packages = manifest.packages.filter((item) => !item.optional && ['windows/x64', 'shared'].includes(item.platform))
// 启动包仅附带当前平台的运行依赖。
for (const item of packages) {
  const target = path.join(stage, 'packages', item.path)
  fs.mkdirSync(path.dirname(target), { recursive: true })
  fs.copyFileSync(path.join(root, 'packages', item.path), target)
}
fs.writeFileSync(path.join(stage, 'packages/manifest.json'), JSON.stringify({ ...manifest, packages, total_size_bytes: packages.reduce((sum, item) => sum + item.size_bytes, 0) }, null, 2))
fs.writeFileSync(path.join(stage, '使用说明.txt'), '完整解压后双击“妙搭部署平台.exe”，浏览器将打开本地工作台。\r\n缺少依赖时进入初始环境，一键使用附带安装包完成安装。\r\n运行无需预装 Node.js、npm 或 Go。飞书授权和妙搭操作仍需访问相应服务。\r\n')
NODE
# 原生 ZIP 打包保留中文名称并避免重复压缩离线安装包。
"$go_bin" -C "$root/be" run ./cmd/bundle -source "$stage" -output "$root/dist/妙搭部署平台-windows-x64.zip"
