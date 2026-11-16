# 项目启动说明

## 通用前提

环境与账号配置参见 [README](../../README.md)。需要 Node.js 20.19+、Go 1.22+。

## Action: web

使用 [run.json](run.json) 中的启动配置，在项目根目录构建前端并启动 Go 服务。

前端位于 `fe/`，后端位于 `be/`。启动入口先构建前端，再运行 Go 服务。

全栈打包技能已安装到 `~/.trae-cn/skills/miaoda-app-assemble/`。启动配置设置 `MIAODA_ASSEMBLE_SCRIPT` 指向其中的 `scripts/export-miaoda-app.sh`，后端无需依赖默认技能目录扫描。

该配置使用 README 的 npm 启动入口。启动前确认端口 `18080` 未被其他应用占用。

健康检查：`/api/health` 应返回 `{"status":"ok"}`；`/api/cli` 用于检测飞书 CLI 与租户配置。
