# Downloader v3 — Wails v3（桌面 + Android）

本目录是 Downloader 的 **Wails v3 版本**，独立 Go module（`module downloader`），与仓库根目录的 Wails v2 版本并存、互不影响。**v2 版本的说明见[根目录 README](../README.md)**，本文档只描述 v3。

v3 的目标是在完整继承 v2 下载能力的前提下，通过 Wails v3 的移动端支持落地 **Android 应用**。Wails v3.0.0（2026-10-26 发布）桌面端为稳定版，Android/iOS 为**实验性**支持。

## 快速开始

环境要求：Go 1.27+（开发机为 1.27.1）、Node 18+、Wails CLI v3（`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`）。

```sh
cd v3
wails3 task build        # 桌面产物：bin/downloader.exe（生产模式，含嵌入资源）
wails3 task dev          # 开发模式（前端热更新，Vite 端口 9245）
```

直接运行 exe 即可，免安装、无外部运行时依赖。数据目录与 v2 相同（`%APPDATA%\downloader\`），v2 的任务列表与设置在 v3 下**直接可用**；命令行可带下载链接直接建任务：

```sh
downloader.exe https://example.com/file.zip
```

## 功能

**下载引擎与队列能力与 v2 完全一致**（engine 包原样迁移）：多连接分段下载、字节级断点续传、全局/每任务限速、指数退避重试、空闲超时看门狗、每任务认证头、代理、GitHub 镜像加速、校验和、磁盘空间预检查、带宽时间窗、队列总闸与批量操作、下载统计、压缩包自动解压、JSON 导入导出、本地 REST API（`127.0.0.1:8199`，安全模型不变）。完整功能说明见 v2 README 的[功能](../README.md#功能)章节，此处不再重复。

**界面与集成能力按平台拆分**：

| 能力 | Windows 桌面 | Android |
|---|---|---|
| 任务列表 / 筛选 / 搜索 / 详情 / 统计页 | ✓ | ✓（WebView 渲染，自适应布局） |
| 新建下载（多行批量、序号展开、目录分类） | ✓ | ✓ |
| 系统通知（按事件开关） | ✓ Wails 通知服务 | ✓ 原生 Notify 桥接 |
| 系统托盘 | ✓ `app.SystemTray` | —（无托盘概念） |
| 关闭行为（询问/退出/最小化） | ✓ | —（无窗口关闭按钮） |
| 剪贴板监听自动建任务 | ✓ | —（系统限制后台读剪贴板，粘贴手动触发） |
| 单实例 + 命令行拉起 | ✓ | 应用本身 `singleTask` 单实例 |
| REST API | ✓ | ✓ |

## Android 支持（实验性）

### 构建前置条件

- **JDK 17+**（gradle 需要；`JAVA_HOME` 指向 JDK 或 `java` 在 PATH）
- **Android SDK**（`ANDROID_HOME` 指向 SDK 根目录）
- **Android NDK**（`sdkmanager "ndk;28.2.13676358"` 或更高；`ANDROID_NDK_HOME` 或放在 `$ANDROID_HOME/ndk/` 下自动探测）
- 首次可运行 `wails3 task android:install:deps` 自检依赖

### 构建命令

```sh
wails3 task android:build            # 只编译 Go → libwails.so（arm64 默认）
wails3 task android:assemble:apk     # gradle 出调试 APK → bin/downloader.apk
wails3 task android:package          # 生产模式整链路：前端 + Go + 签名 APK（arm64）
wails3 task android:package:fat      # arm64 + x86_64 通用包
ARCH=amd64 wails3 task android:build # 模拟器架构
```

构建链路：前端 vite 构建 → `wails3 android overlay:gen` 生成 Android 入口注册 → NDK 交叉编译为 `libwails.so`（c-shared，含 JNI 桥）→ gradle 打包 APK。

**已内置的 Windows / 国内网络适配**（无需额外配置）：

- Windows 宿主：`uname` 探测 MINGW/MSYS 自动改用 NDK 的 `windows-x86_64` 工具链；gradle 经 `cmd /c gradlew.bat` 启动（Windows 无法直接执行 sh 脚本）
- gradle 发行版走腾讯镜像、maven 依赖走阿里云镜像（`services.gradle.org` / `dl.google.com` 直连超时时仍可构建）；`buildToolsVersion` 固定 35.0.0（与 compileSdk 35 配套，本机 SDK 无 34.x）

### Android 平台行为差异

- **数据目录**：应用私有目录 `activity.getFilesDir()` 下的 `downloader/`（任务列表、设置、续传状态、下载暂存都在这里），卸载即清除。
- **默认保存目录**：`<私有目录>/downloads`。下载到公共目录（如 `/sdcard/Download`）需要在系统设置里给应用授予**「所有文件访问」权限**（Manifest 已声明 `MANAGE_EXTERNAL_STORAGE` + `requestLegacyExternalStorage`）；这是范围存储（Scoped Storage）下的现实约束。
- **通知**：经 `application.Android.Notify()` 原生桥接发送；Android 13+ 首次通知由系统引导授权。
- **不支持**：系统托盘、窗口关闭行为、剪贴板监听（详见上表）。

### 包名与 JNI 约束

Java `namespace` 必须保持 `com.wails.app`——Wails v3 的 JNI 导出函数名（`Java_com_wails_app_WailsBridge_*`）硬编码了这个包名；设备上的应用包名通过 gradle 的 `applicationId`（当前 `com.windchen.downloader`）独立修改，二者互不影响。

## 架构

```
v3/
  go.mod                  独立 module downloader（Go 1.27.1，Wails v3.0.0）
  main.go                 应用装配：Options/Services/主窗口/单实例；runApp 桌面由 main() 调用
  app.go                  AppService：全部前端绑定方法（名称与 v2 一致）+ ServiceStartup/Shutdown 生命周期
  notify.go               通知分流（按事件开关、语言缓存），平台后端见下
  platform_desktop.go     桌面平台实现：数据目录、托盘、剪贴板监听、Wails 通知服务
  platform_android.go     Android 平台实现：StoragePath 数据目录、原生通知、托盘/剪贴板空实现
  engine/                 下载引擎（自 v2 原样复制，纯标准库，无任何改动）
  sysaction/              系统动作（自 v2 原样复制）
  Taskfile.yml            wails3 task 构建入口（build/dev/按平台分发）
  build/
    config.yml            应用身份与 dev 模式配置
    windows/  darwin/  linux/   桌面平台资源（图标/manifest/打包脚本）
    android/              gradle 工程（MainActivity/WailsBridge/前台服务）+ 编译任务
  frontend/
    src/                  React + TypeScript 界面（自 v2 迁移，见下）
    bindings/             wails3 generate bindings 生成（JSDoc 类型，已提交）
```

### v3 相对 v2 的技术变化

| v2 | v3 |
|---|---|
| `wails.Run(&options.App{Bind: ...})` | `application.New(Options{Services: ...})`，服务生命周期走 `ServiceStartup/ServiceShutdown` |
| `wailsjs/go/main/App` 生成绑定 | `wails3 generate bindings` → `frontend/bindings/`（JSDoc 类型） |
| `runtime.EventsEmit/EventsOn(name, (data) => ...)` | `app.Event.Emit(name, data)` / `Events.On(name, (ev) => ev.data)` |
| 托盘用 `fyne.io/systray`（自管消息泵 + LockOSThread） | 内置 `app.SystemTray` |
| `runtime.SendNotification` | `pkg/services/notifications` 服务（桌面）/ `application.Android.Notify`（移动） |
| `runtime.ClipboardGetText` | `app.Clipboard.Text()`；前端复制改用 `navigator.clipboard` |
| `OnBeforeClose` 拦截关闭 | `win.OnWindowEvent(events.Common.WindowClosing)` + `ev.Cancel()` |
| `SingleInstanceLock` | `Options.SingleInstance`（同形态） |

前端迁移只有三处实质改动：`api.ts` 换绑定导入、`App.tsx` 事件回调改为 `ev.data` 载荷、任务详情复制改 `navigator.clipboard`；组件、样式、i18n、统计逻辑零改动。

### engine 同步策略

`v3/engine` 是 v2 引擎的**副本**（v2 与 v3 是两个独立 module，无法互相引用）。在 v3 分支期间引擎改动应落在 v2 并同步复制到 v3；两份代码当前完全一致（`diff -r engine v3/engine` 可校验）。

## 开发

```sh
cd v3
go test ./engine/ -race        # 引擎单元测试 + 集成测试（含竞态检测）
go vet ./...                   # 静态检查
cd frontend && npm test        # 前端单元测试（vitest，35 用例）
wails3 task dev                # 开发模式（前端热更新）
wails3 task build              # 桌面生产构建

# Go 代码改动绑定面（AppService 方法签名/新增方法）后重新生成绑定：
wails3 generate bindings       # 产物 frontend/bindings/，需提交
```

构建注意：若上级目录存在 `go.work`，设 `GOWORK=off` 再执行 go/wails3 命令，避免 workspace 干扰。

`settings.json` / `tasks.json` 格式与 v2 相同；手动编辑请保存为**无 BOM 的 UTF-8**。

## 数据位置

- **Windows 桌面**（与 v2 完全一致）：任务列表与设置 `%APPDATA%\downloader\`，下载统计 `stats.json` 同目录，续传状态 `%APPDATA%\downloader\state\`，下载暂存 `%LOCALAPPDATA%\downloader\staging\`
- **Android**：统一在应用私有目录 `filesDir/downloader/`（`tasks.json`、`settings.json`、`stats.json`、`state/`、`staging/`、默认保存目录 `downloads/`）

## License

[MIT](../LICENSE) © 2026 WindChen
