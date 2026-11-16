# 妙搭部署平台

在本机打包妙搭应用，并用安装包在所选飞书租户创建和发布新应用。界面使用 React + Vite，服务使用 Go 标准库。

## 启动

需要 Node.js 20.19+ 和 Go 1.22+。在项目根目录运行：

```sh
npm --prefix frontend ci
npm --prefix frontend start
```

macOS / Linux 可以使用 `./scripts/start.sh`；Windows 可以双击 `scripts\start.bat`，或在项目根目录运行：

```bat
scripts\start.bat
```

启动脚本会首次安装前端依赖、清理端口上的旧服务、构建前端并启动后端。默认地址为 [http://127.0.0.1:18080/](http://127.0.0.1:18080/)，可通过 `PORT` 修改。Windows CMD 使用 `set PORT=18081`，PowerShell 使用 `$env:PORT = '18081'`。

平台启动后会检测 lark-cli。若未安装，在终端运行 `npm install -g @larksuite/cli`，然后在页面重新检测。平台读取 `lark-cli profile list` 中的租户配置，用所选配置登录和执行任务。添加租户配置时，在终端运行 `lark-cli config init --name 租户别名`。首次登录可在页面完成飞书授权。凭据由 lark-cli 管理。

## 打包与部署

1. 在**应用打包**页输入应用名称，搜索当前租户可见的全栈应用。通过结果行的「打包」开始，查看各步骤并在完成后下载 ZIP。
2. 在**应用部署**页填写应用名称，选择或拖入 ZIP，再点击「确认部署」。平台优先读取安装包元数据；无清单时会根据工程目录识别应用类型。
3. 在进度面板查看成功、失败、跳过或需检查的步骤。部署完成后，点击面板底部的「打开生产环境」进入已发布的系统，也可以在面板顶部复制访问地址。部署失败且目标应用已创建时，从该任务重试会复用已记录的目标应用 ID。

覆盖已有应用时，填写妙搭应用地址并导入全栈安装包，点击「检查目标应用」。核对应用 ID、当前账号和影响清单，再输入目标应用 ID 并确认。初始化会删除清单中的开发库业务表和枚举、清空生产库业务表数据、删除自定义角色和环境变量，并停用旧自动化。生产表结构随新版本发布更新。清理失败时任务停在「初始化目标应用」。

前端和创意应用使用 lark-cli 源码导出、Git 和妙搭发布命令。全栈应用打包需要本机安装 `miaoda-app-assemble` 技能及其脚本依赖，部署由服务端 Go 代码直接完成。平台从 `~/.agents/skills` 或 `~/.codex/skills` 查找打包脚本，也可通过 `MIAODA_ASSEMBLE_SCRIPT` 指定入口路径。全栈包包含源码、配置和数据库结构，源应用业务表数据由妙搭侧单独迁移。

全栈打包脚本需要 `bash`、`node`、`git`、`zip`、`unzip`、`rsync` 和 `python3`。用自定义路径安装 lark-cli 时可设置 `MIAODA_LARK_CLI` 指向可执行文件。

## 文件与开发

- `data/jobs/` 保存步骤状态、生成的 ZIP 与部署续跑记录。
- `data/imports/` 保存上传的 ZIP 与识别结果。
- `logs/server.log` 保存服务、请求及任务执行日志。服务启动时自动创建文件，重启后继续追加，终端也会显示日志。
- 安装包可能包含妙搭环境变量值。请仅通过受控渠道分享，并妥善保管项目内的 `data/`。
- `frontend/src/` 存放界面，`backend/` 存放 API 和任务流程。

开发模式下在两个终端分别运行 `go -C backend run ./cmd/server` 和 `npm --prefix frontend run dev`，访问 [http://127.0.0.1:5173/](http://127.0.0.1:5173/)。自定义端口时，两端使用相同的 `PORT`。
