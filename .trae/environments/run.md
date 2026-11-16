# 项目启动说明

## 通用前提

环境与账号配置参见 [README](../../README.md)。需要 Node.js 20.19+、Go 1.22+。

## Action: web

使用 [run.json](run.json) 中的启动配置，在项目根目录构建前端并启动 Go 服务。

本机缺少 Go 时已通过 Homebrew 安装 Go 1.27.4。前端依赖已安装。

全栈打包技能已安装到 `~/.trae-cn/skills/miaoda-app-assemble/`。启动配置设置 `MIAODA_ASSEMBLE_SCRIPT` 指向其中的 `scripts/export-miaoda-app.sh`，后端无需依赖默认技能目录扫描。

技能包内 11 个文件已完成 SHA256 一致性核对，Shell 与 JavaScript 语法检查通过，所需本地命令已齐全。服务已重启，健康检查通过，并确认后端进程继承了打包脚本环境变量。实际全栈打包任务尚未验证。

该配置使用 README 的 npm 启动入口。仓库 `scripts/start.sh` 会终止占用端口的服务，启动前应确认端口所属项目。

已验证首页标题为“妙搭部署平台”，`/api/health` 返回 `{"status":"ok"}`，`/api/cli` 能检测飞书 CLI 与租户配置。浏览器能显示应用部署页面，点击导航后能切换到应用打包页面。实际打包和部署任务未执行。
