# Downloader - 多线程下载工具

基于 Go + Wails v2 + React/TypeScript 的 GUI 多线程下载工具（对标 IDM 的 HTTP 下载场景）。Windows 优先，单文件 exe，无外部运行时依赖。

## 功能

### 下载引擎

- **多连接分段下载**：单文件按 Range 切片并发下载（默认 8 连接，1–32 可配），每个分段独占一条 TCP 连接（禁用 HTTP/2 多路复用，确保多连接真正聚合带宽）；全局任务并发排队（默认 3，1–10 可配）
- **字节级断点续传**：数据写 `xxx.part`，续传状态（每分段已收字节数）持久化在应用数据目录；暂停、单段重试、恢复下载、进程崩溃重启都从断点字节继续，不浪费已下载数据；全部完成后才改名落盘
- **单连接回退**：服务器不支持 Range 时自动退回单连接流式下载（同样支持流式续传）
- **全局限速**：字节/秒，0 不限，设置保存即时生效
- **自定义请求头 / User-Agent**：每行一条 `Key: Value`
- **代理**：不使用 / 系统代理 / 自定义（http、https、socks5），保存即时生效
- **GitHub 镜像加速**：命中 github.com 系域名时按模板自动改写（默认 `https://gh-proxy.com/{url}`，模板可配）；续传校验仍用原始 URL，切换镜像不丢进度
- **失败重试**：分段级指数退避重试（最多 5 次），重试同样从段内断点继续

### 界面与体验

- 任务列表：进度条、百分比、已下载/总大小、实时速度、剩余时间、连接数、状态标签
- 任务筛选（全部/下载中/排队中/已暂停/已完成/失败，带计数）与搜索（文件名/链接）
- 新建下载对话框：粘贴链接、选保存目录、设连接数
- **打开目录按钮**：已完成的任务在资源管理器中打开目录并选中文件，其余打开所在目录
- 删除任务时可选「仅删除记录」或「连文件一起删除」
- 亮色 / 暗色 / 跟随系统三态主题 + 8 种强调色
- **系统托盘**：点关闭最小化到托盘，托盘菜单唤出主窗口 / 退出
- **系统通知**：下载完成 / 失败弹 Windows 通知
- **剪贴板监听**：复制下载链接自动弹出预填的新建下载（可关闭）

### 集成

- **单实例**：重复启动 exe 不会开新进程，自动唤起已运行实例的主窗口
- **命令行拉起下载**：`downloader.exe https://example.com/file.zip` 直接创建任务（不合法链接则只唤起窗口）
- **本地 REST API**：默认 `127.0.0.1:8199`，供脚本 / 自动化调用（见下文）

## REST API

仅绑定本机回环地址；端口可在设置中修改（重启应用生效）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/ping` | 健康检查 |
| GET | `/api/v1/tasks` | 任务列表（含实时进度） |
| POST | `/api/v1/tasks` | 新建任务，body：`{"url": "...", "saveDir": "...", "connections": 8}`（后两项可省略） |
| POST | `/api/v1/tasks/{id}/pause` | 暂停 |
| POST | `/api/v1/tasks/{id}/continue` | 恢复 |
| DELETE | `/api/v1/tasks/{id}` | 删除记录；加 `?files=1` 连同已下载文件一起删除 |
| GET | `/api/v1/settings` | 读取设置 |
| PUT | `/api/v1/settings` | 保存设置（代理/限速等即时生效，API 端口除外） |

示例：

```sh
curl -X POST http://127.0.0.1:8199/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/bigfile.zip"}'

curl -X DELETE "http://127.0.0.1:8199/api/v1/tasks/<id>?files=1"
```

## 开发

环境要求：Go 1.25+、Node 18+、Wails CLI v2（`go install github.com/wailsapp/wails/v2/cmd/wails@latest`）。

```sh
go test ./engine/ -race    # 引擎单元测试 + 集成测试（含竞态检测）
wails dev                  # 开发模式（前端热更新）
wails build                # 构建单文件 exe 到 build/bin/
```

## 架构

```
main.go                  Wails 应用入口：窗口、单实例锁
app.go                   前端绑定（任务/设置/打开目录）
app_tray.go              系统托盘（fyne.io/systray）
app_notify.go            系统通知（完成/失败）
app_clipboard.go         剪贴板监听
engine/                  下载引擎（与 UI 解耦，可独立复用，仅标准库依赖）
  manager.go             任务队列、并发调度、事件推送、设置应用
  download.go            分段下载、字节级续传、重试、落盘改名
  api.go                 本地 REST API
  ratelimit.go           全局限速器（令牌匀速累积，暂停感知）
  mirror.go              GitHub 镜像 URL 改写
  probe.go               URL 探测（大小/Range 支持/文件名推断）
  client.go              HTTP 客户端构建（代理/UA/超时策略）
  chunk.go               Range 分片计算
  sidecar.go             续传状态模型（分段位图 + 段内已收字节）
  store.go               设置与任务持久化（原子写 JSON，Windows 重试）
frontend/src             React + TypeScript 界面
  components/            任务列表（筛选/搜索）、新建对话框、设置页、删除对话框、图标
```

## 数据位置

- 任务列表与设置：`%APPDATA%\downloader\`（`tasks.json`、`settings.json`）
- 续传状态：`%APPDATA%\downloader\state\<任务ID>.json`
- 下载数据：`<保存目录>\<文件名>.part`，完成后原地改名
