# Downloader - 多线程下载工具

基于 Go + Wails v2 + React/TypeScript 的 GUI 多线程下载工具（对标 IDM 的 HTTP 下载场景）。Windows 优先，单文件 exe，无外部运行时依赖。

应用图标与主界面品牌标识一致（蓝色渐变圆角方块 + 白色下载箭头），资源嵌入 exe 与系统托盘；ICO 小尺寸使用 DIB、大尺寸使用 PNG，兼容 Windows 资源管理器显示。

## 功能

### 下载引擎

- **多连接分段下载**：单文件按 Range 切片并发下载（默认 8 连接，1–128 可配），每个分段独占一条 TCP 连接（禁用 HTTP/2 多路复用，确保多连接真正聚合带宽）；全局任务并发排队（默认 3，1–10 可配）
- **字节级断点续传**：数据写 `xxx.part`，续传状态（每分段已收字节数）持久化在应用数据目录；暂停、单段重试、恢复下载、进程崩溃重启都从断点字节继续，不浪费已下载数据；全部完成后才改名落盘
- **单连接回退**：服务器不支持 Range 时自动退回单连接流式下载（同样支持流式续传）
- **全局限速**：字节/秒，0 不限，设置保存即时生效
- **自定义请求头 / User-Agent**：每行一条 `Key: Value`
- **代理**：不使用 / 系统代理 / 自定义（http、https、socks5），保存即时生效
- **GitHub 镜像加速**：命中 github.com 系域名时按模板自动改写（默认 `https://gh-proxy.com/{url}`，模板可配）；续传校验仍用原始 URL，切换镜像不丢进度
- **失败重试**：分段级指数退避重试（最多 5 次），重试同样从段内断点继续
- **平均速度**：按已下载字节 / 活跃下载时长计算，完成后冻结展示
- **可选校验和**：新建时可填 MD5/SHA1/SHA256 期望值；完成后自动计算并比对，不匹配则任务标记失败
- **每任务限速**：可为单个任务设置限速（0 跟随全局）；与全局限速叠加生效
- **任务优先级**：高优先出队；列表可上移/下移调整同级顺序
- **定时下载**：可为任务指定开始时间，到点后自动进入下载
- **每任务认证头**：单个任务可填 Cookie / Referer，覆盖全局自定义请求头中的同名项（用于需登录的直链与防盗链站点）；跨子域重定向时逐跳重新施加——标准库会在重定向中剥离 Cookie，不补回来就会 403
- **磁盘空间预检查**：任务启动前确认目标盘写得下；装不下时任务停在队列里并说明「剩余多少、还差多少」，腾出空间后自动继续（可选额外保留余量）
- **带宽时间窗**：只在指定时段内启动新任务（如 22:00–06:00），窗外自动闭闸、到点自动放行；正在下载的任务不会被中断
- **下载统计**：今日 / 近 7 天 / 累计下载量、速度曲线（全局与单任务）、每日柱状、峰值与均速；字节量按相邻快照的差值精确累加（不是速度积分），按天聚合与最近 15 个任务的曲线落盘，跨重启保留

### 界面与体验

- 任务列表：进度条、百分比、已下载/总大小、实时速度、剩余时间、连接数、状态标签
- 任务筛选（全部/下载中/排队中/已暂停/已完成/失败，带计数）与搜索（文件名/链接）
- **队列批量操作**（列表工具栏）：暂停全部 / 恢复全部 / 只重试失败任务 / 清理已完成记录（仅移除记录，磁盘文件保留，需二次确认）
- **队列总闸**：「暂停队列」后不再启动任何任务（正在下载的不受影响，新任务停在排队中），开闸时按优先级批量放行；状态写入设置，跨重启保持
- **队列停摆提示**：工具栏下方横幅区分「用户闭闸 / 带宽时间窗外（含下次放行时刻）/ N 个任务因磁盘空间不足在等」，任务行内还会列出剩余与需求量
- **统计页**（顶栏图表图标）：汇总卡片 + 速度曲线（手写 SVG，无图表依赖）+ 近 14 天柱状 + 单任务曲线与汇总
- 新建下载对话框：粘贴链接（**支持多行批量创建**）、自定义文件名（留空自动从链接/响应头获取）、选保存目录、设连接数（1–128）、优先级、每任务限速、定时开始、可选校验算法与期望值、可选认证头（Cookie / Referer，折叠区，批量时对每条链接生效）
- **序列号批量**：链接中可写 `{1..10}` 或 `{1..100:5}`，自动展开为多条下载
- **下载链接输入框**：最小高度与普通输入框一致，禁止拖拽调高，按内容自动增高（过长时内部滚动）
- **任务详情**：点击任务行查看文件名、链接、下载路径（均可一键复制）、大小、状态、连接数、优先级、限速、Cookie（掩码显示，复制取原文）/Referer、定时、下载耗时、总耗时、平均速度、校验和、创建/结束时间
- **耗时统计**：
  - **下载耗时**（`activeMs`）：仅累计真正处于下载状态的时间，暂停/排队不计
  - **总耗时**：从创建到结束的墙钟时间（含暂停）；完成/失败后冻结，不再随当前时间增长
- **打开目录按钮**：已完成的任务在资源管理器中打开目录并选中文件，其余打开所在目录
- 删除任务时可选「仅删除记录」或「连文件一起删除」
- **导入 / 导出任务列表**：设置页可将任务导出为 JSON 备份，或从 JSON 导入（已完成任务跳过，暂停/失败/排队会重建）；导出默认**剥离**每任务的 Cookie / Referer（这两项常含会话凭据），需要自行迁移任务包时在设置页勾选带出
- 亮色 / 暗色 / 跟随系统三态主题 + 8 种强调色；顶栏与设置页主题控件统一使用线性 SVG 图标
- **多语言界面**：简体中文 / English / 跟随系统（按浏览器语言），切换即时生效；解析后的语言同步进引擎设置，系统通知跟随切换（托盘菜单在下次启动时应用）
- **系统托盘**：托盘菜单可唤出主窗口 / 退出；退出时会正确移除托盘图标并结束进程（无残留幽灵进程）
- **关闭行为可配置**（设置 → 窗口）：
  - **每次询问**（默认）：点关闭弹出确认框——「最小化到托盘」/「直接退出」/「取消」；主操作为「直接退出」，不会默认最小化；可勾选「记住我的选择，不再询问」
  - **直接退出**：点关闭即退出程序
  - **最小化到系统托盘**：点关闭隐藏窗口，任务继续在后台下载
- **全部完成动作**（设置 → 全部完成动作）：所有任务结束后可选择不执行 / 打开下载目录 / 关机（60 秒）/ 睡眠 / 退出本程序；仍有排队中、下载中或已暂停的任务时不会触发
- **系统通知**（设置 → 通知，可按事件开关）：
  - **下载完成 / 失败**：默认开启
  - **创建任务 / 暂停任务**：默认关闭（避免批量添加或频繁暂停时打扰）
- **剪贴板监听**：复制下载链接自动弹出预填的新建下载（可关闭）

### 集成

- **单实例**：重复启动 exe 不会开新进程，自动唤起已运行实例的主窗口
- **命令行拉起下载**：`downloader.exe https://example.com/file.zip` 直接创建任务（不合法链接则只唤起窗口）
- **本地 REST API**：默认 `127.0.0.1:8199`，供脚本 / 自动化调用（见下文）

## REST API

仅绑定本机回环地址；端口与开关在设置中修改，保存后立即启停生效（无需重启应用）。

所有请求须通过本机边界校验（`ping` 除外须携带令牌）：

- **令牌**：携带 `X-API-Token: <token>` 或 `Authorization: Bearer <token>`；令牌在开启 API 时自动生成，可在设置页查看/复制，保存设置时留空表示保留现有令牌
- **Host 校验**：Host 头必须是 `127.0.0.1` / `localhost` / `[::1]`，防 DNS rebinding
- **Origin 校验**：携带 Origin 头的跨站请求一律拒绝，防浏览器 CSRF
- **Content-Type**：带请求体的 POST/PUT 必须为 `application/json`
- **保存目录白名单**：API 新建任务与「连文件删除」只允许默认下载目录及设置中配置的白名单目录；JSON 导入时白名单外的目录会收敛为默认下载目录

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/ping` | 健康检查（免令牌） |
| GET | `/api/v1/tasks` | 任务列表（含实时进度） |
| POST | `/api/v1/tasks` | 新建任务，body：`{"url": "...", "saveDir": "...", "connections": 8, "fileName": "自定义文件名", "checksumAlgo": "sha256", "checksumExpected": "...", "priority": 1, "startAt": "RFC3339", "speedLimit": 0, "cookie": "sid=...", "referer": "https://site/page"}`（除 url 外均可省略；`fileName` 与 `customName` 同义，同时提供时 `fileName` 优先） |
| POST | `/api/v1/tasks/{id}/pause` | 暂停 |
| POST | `/api/v1/tasks/{id}/continue` | 恢复 |
| DELETE | `/api/v1/tasks/{id}` | 删除记录；加 `?files=1` 连同已下载文件一起删除（目录须在白名单内） |
| POST | `/api/v1/queue/pause-all` | 暂停全部（排队中的立即暂停，运行中的取消本轮）；返回 `{"count": n}` |
| POST | `/api/v1/queue/resume-all` | 恢复全部已暂停与失败的任务 |
| POST | `/api/v1/queue/retry-failed` | 只重试失败任务 |
| POST | `/api/v1/queue/clear-completed` | 清理已完成记录（保留磁盘文件） |
| PUT | `/api/v1/queue/suspended` | 开关队列总闸，body：`{"suspended": true}`；闭闸后不再启动任何任务 |
| GET | `/api/v1/queue/status` | 队列放行状态：用户闸门、时间窗（含下次放行时刻）、因空间不足停在队列的任务数 |
| PUT | `/api/v1/queue/window` | 带宽时间窗，body：`{"enabled": true, "start": "22:00", "end": "06:00"}`（跨零点合法；时间非法返回 400） |
| GET | `/api/v1/stats` | 下载统计：今日/7 天/累计、速度与峰值、按天聚合、全局曲线与任务汇总（汇总不含单任务曲线，避免每次轮询全量序列化） |
| GET | `/api/v1/stats/tasks/{id}` | 单任务速度曲线（含最近采样点）；该任务暂无统计记录时返回 404 |
| GET | `/api/v1/settings` | 读取设置 |
| PUT | `/api/v1/settings` | 保存设置（代理/限速/API 启停等即时生效） |

示例（`<token>` 见设置页）：

```sh
curl -H "X-API-Token: <token>" http://127.0.0.1:8199/api/v1/tasks

curl -X POST http://127.0.0.1:8199/api/v1/tasks \
  -H "X-API-Token: <token>" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/bigfile.zip"}'

curl -X DELETE "http://127.0.0.1:8199/api/v1/tasks/<id>?files=1" \
  -H "X-API-Token: <token>"

# 闭闸（不再启动任何任务）与批量暂停
curl -X PUT http://127.0.0.1:8199/api/v1/queue/suspended \
  -H "X-API-Token: <token>" \
  -H "Content-Type: application/json" \
  -d '{"suspended": true}'

curl -X POST http://127.0.0.1:8199/api/v1/queue/pause-all \
  -H "X-API-Token: <token>"

# 只在夜里跑（跨零点窗口），并查看队列为什么不动
curl -X PUT http://127.0.0.1:8199/api/v1/queue/window \
  -H "X-API-Token: <token>" \
  -H "Content-Type: application/json" \
  -d '{"enabled":true,"start":"22:00","end":"06:00"}'

curl http://127.0.0.1:8199/api/v1/queue/status -H "X-API-Token: <token>"
curl http://127.0.0.1:8199/api/v1/stats -H "X-API-Token: <token>"
curl http://127.0.0.1:8199/api/v1/stats/tasks/<id> -H "X-API-Token: <token>"
```

### 设置生效时机

| 设置项 | 生效时机 |
|---|---|
| 代理 / User-Agent / 自定义请求头 / 镜像模板 / 全局限速 | 保存后即时生效（对新建任务；运行中的任务沿用其开始时的快照，避免同一文件的不同分段走不同出口） |
| API 开关 / 端口 | 保存后立即启停，无需重启 |
| 连接数 / 每任务限速 / 优先级 / 定时 | 对新建或下一轮运行的任务生效 |
| 队列总闸 | 切换即时生效：闭闸只阻止后续启动，开闸立即按优先级放行已排队任务；状态持久化，重启后保持 |
| 带宽时间窗 | 保存后下一个进度周期（≈500ms）生效：进窗自动放行排队任务，出窗不中断正在下载的任务；配置持久化 |
| 磁盘空间预检查 | 对下一轮启动生效；被拦住的任务每 2 秒复检一次（剩余空间有缓存），不再依赖手动重试 |
| 每任务 Cookie / Referer | 对该任务的下一轮运行生效（本轮沿用开始时的请求头快照） |

注意 `PUT /api/v1/settings` 是**整体替换**：请求体里没写的字段按零值写入，因此一份不带 `queueSuspended` 的旧 JSON 模板存回去就等于开闸。只改闸门请用 `PUT /api/v1/queue/suspended`，只改时间窗同理用 `PUT /api/v1/queue/window`。

## 开发

环境要求：Go 1.25+、Node 18+、Wails CLI v2（`go install github.com/wailsapp/wails/v2/cmd/wails@latest`）。

```sh
go test ./engine/ -race    # 引擎单元测试 + 集成测试（含竞态检测）
go vet ./...               # 静态检查（CI 同款）
cd frontend && npm test    # 前端单元测试（vitest）
wails dev                  # 开发模式（前端热更新）
wails build                # 构建单文件 exe 到 build/bin/
```

CI（`.github/workflows/ci.yml`）：后端 gofmt/vet/`go test -race`，前端 vitest 与 tsc 构建，并在 Windows 上完成 `go build` 验证嵌入产物完整性。

Windows 构建注意：若上级目录存在引用缺失模块的 `go.work`，可设 `GOWORK=off` 再执行 `wails build`，避免 workspace 干扰。

应用图标源文件：
- `build/appicon.png` — 1024×1024 品牌图
- `build/windows/icon.ico` — 多尺寸 ICO（exe 资源与托盘共用；16/24/32/48 为 DIB，64/128/256 为 PNG）

## 架构

```
main.go                  Wails 应用入口：窗口、单实例锁、OnShutdown 强制收尾
app.go                   前端绑定（任务/设置/打开目录/关闭行为/导入导出）与 REST API 装配
app_tray.go              系统托盘（fyne.io/systray）；退出时 systray.Quit + 进程收尾
app_notify.go            系统通知（按设置：创建/暂停/完成/失败；原子缓存开关）
app_clipboard.go         剪贴板监听
sysaction/               全部完成后的系统动作（开目录/关机/睡眠/退出信号）；动作取值的唯一定义处
engine/                  下载引擎（与 UI 解耦，可独立复用，仅标准库依赖）
  events.go              类型化事件契约（EventName 常量 + Event 载荷）
  errors.go              哨兵错误（ErrTaskNotFound / ErrInvalidState / ErrNoSpace）
  manager.go             任务队列、优先级调度、定时唤醒、事件推送、设置应用
  queueops.go            队列总闸与批量操作（暂停全部/恢复全部/只重试失败/清理已完成）：一次加锁、一次落盘、一次推送
  window.go              带宽时间窗：HH:MM 判定（含跨零点）、下一次放行时刻、队列停摆状态
  disk.go                磁盘空间预检查：落盘需求量、按卷缓存剩余空间、拦下原因文案
  disk_windows.go        GetDiskFreeSpaceExW（取调用方可写的字节数，仅标准库 syscall）
  disk_unix.go           statfs（取普通用户可用块，供非 Windows 开发与 CI）
  stats.go               下载统计与速度曲线：字节差值聚合、窗口平均速率采样、节流落盘与保留上限
  download.go            分段下载、字节级续传、重试、每任务认证头与跨子域重定向保留、落盘改名
  importexport.go        任务列表 JSON 导出/导入（导出默认剥离 Cookie/Referer）
  checksum.go            MD5/SHA1/SHA256 摘要计算与校验
  ratelimit.go           全局限速 + 每任务限速（令牌匀速累积）
  mirror.go              GitHub 镜像 URL 改写
  probe.go               URL 探测（大小/Range 支持/文件名推断）
  client.go              HTTP 客户端构建（代理/UA/超时策略）
  chunk.go               Range 分片计算
  sidecar.go             续传状态模型（分段位图 + 段内已收字节）
  store.go               设置与任务持久化（原子写 JSON，Windows 重试）
  api/                   本地 REST API（独立子包，反向依赖 engine 类型）
    server.go            服务生命周期：Sync 按设置启停/换端口
    handler.go           路由与鉴权守卫（回环 Host/Origin/令牌/Content-Type）
frontend/src             React + TypeScript 界面
  components/            任务列表、新建对话框、设置页、删除/关闭确认对话框、SVG 图标
  lib/format.ts          字节/速度/耗时格式化、URL 解析与序列号展开
```

设置中的 `closeAction` 取值：`ask` | `exit` | `minimize`（默认 `ask`）。主动退出前会置 `quitting` 标志，避免 `OnBeforeClose` 在「每次询问」下二次拦截导致进程残留。

## 数据位置

- 任务列表与设置：`%APPDATA%\downloader\`（`tasks.json`、`settings.json`、`stats.json` 下载统计：按天聚合 + 最近一段曲线 + 最近 15 个任务的曲线，体积有上限）
- 续传状态：`%APPDATA%\downloader\state\<任务ID>.json`
- 下载中的暂存数据：`%LOCALAPPDATA%\downloader\staging\<任务ID>.part`——下载过程不再散落 `.part` 文件在下载目录里；完成后同盘原地改名到保存目录、跨盘则一次性复制，暂存目录自动清空移除；启动时自动清理不属于任何任务的孤儿暂存文件（旧版本直接放保存目录的 `<文件名>.part` 与 `<保存目录>\.downloader\` 也会在启动时自动迁移）

## License

[MIT](LICENSE) © 2026 WindChen

