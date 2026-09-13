# Downloader - 多线程下载工具

基于 Go + Wails v2 + React/TypeScript 的 GUI 多线程下载工具（对标 IDM 的 HTTP 下载场景）。

## 功能

- **多连接分段下载**：单文件按 Range 切片并发下载（默认 8 连接，1–32 可配），全局任务并发排队（默认 3）
- **断点续传**：数据写 `xxx.part`，分段完成位图持久化在应用数据目录；暂停/失败/退出后可恢复，全部完成才改名落盘
- **任务持久化**：任务列表存 `%APPDATA%\downloader\`，重启自动恢复现场
- **限速**：全局下载限速（字节/秒，0 不限），设置即时生效
- **自定义请求头 / User-Agent**：每行一条 `Key: Value`
- **代理**：不使用 / 系统代理 / 自定义（http、https、socks5）
- **GitHub 镜像加速**：命中 github.com 系域名时按模板自动改写（默认 `https://gh-proxy.com/{url}`，模板可配）
- **剪贴板监听**：复制下载链接自动弹出预填的新建下载（可关闭）
- **系统通知**：下载完成 / 失败弹 Windows 通知
- **系统托盘**：点关闭最小化到托盘，托盘菜单可唤出 / 退出
- **界面**：亮色 / 暗色 / 跟随系统三态主题 + 8 种强调色；任务筛选（状态分类）与搜索；中文界面

## REST API

应用启动后默认在 `127.0.0.1:8199` 提供本地 REST API（仅本机可访问，端口可改，改后重启生效）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/ping` | 健康检查 |
| GET | `/api/v1/tasks` | 任务列表（含实时进度） |
| POST | `/api/v1/tasks` | 新建任务，body：`{"url": "...", "saveDir": "...", "connections": 8}` |
| POST | `/api/v1/tasks/{id}/pause` | 暂停 |
| POST | `/api/v1/tasks/{id}/continue` | 恢复 |
| DELETE | `/api/v1/tasks/{id}` | 删除（已完成任务的文件保留） |
| GET | `/api/v1/settings` | 读取设置 |
| PUT | `/api/v1/settings` | 保存设置（代理/限速等即时生效） |

示例：

```sh
curl -X POST http://127.0.0.1:8199/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/bigfile.zip"}'
```

## 开发

```sh
go test ./engine/ -race   # 引擎单元测试 + 集成测试
wails dev                 # 开发模式（前端热更新）
wails build               # 构建单文件 exe 到 build/bin/
```

## 架构

```
main.go / app.go        Wails 应用：窗口、托盘、通知、剪贴板、绑定
engine/                 下载引擎（与 UI 解耦，可独立复用）
  manager.go            任务队列、并发调度、事件推送、设置
  download.go           分段下载、重试、续传、落盘
  api.go                本地 REST API
  ratelimit.go          全局限速器（令牌匀速累积）
  mirror.go             GitHub 镜像 URL 改写
  store.go              设置与任务持久化（原子写 JSON）
frontend/src            React + TypeScript 界面
```
