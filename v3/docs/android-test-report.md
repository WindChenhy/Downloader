# Downloader v3 · Android 真机测试报告

> 执行日期：2026-10-31 · 测试计划见 [android-test-plan.md](android-test-plan.md)
> 结论：**13/13 项通过**；测试过程发现并修复 1 个真机专属 bug（状态栏遮挡头部控件），另记录 3 项非阻塞改进项。

## 1. 测试环境（实际）

| 项 | 值 |
|---|---|
| 设备 | Redmi K20 Pro（raphael），**Android 17（API 37）**，1080×2340，MIUI/HyperOS |
| APK | `v3/bin/downloader.apk`（arm64，最终为含 insets 修复的生产构建；中途用 debug 包做过 verbose 诊断） |
| 通道 | USB adb：`adb reverse tcp:18080`（设备→PC 测试服务器）、`adb forward tcp:8199`（PC→设备 REST API） |
| REST | 引擎默认开启 API（`127.0.0.1:8199`，token 在设置页），全部测试经 REST 驱动 + 设备端文件哈希断言 |
| 授权 | `MANAGE_EXTERNAL_STORAGE`（appops）、`POST_NOTIFICATIONS`（运行时弹窗授予） |

**观测通道说明**：本机 MIUI 对 `adb screencap` 返回全黑（对照实验：桌面截图同样全黑，系统级限制，与 app 无关）。改用 **`screenrecord` 录屏 + ffmpeg 提帧** 作为视觉证据通道，配合 REST JSON、`run-as`（debug 包期间）与 logcat。截图证据存于 [assets/](assets/)。

## 2. 测试结果总览

| # | 测试项 | 结果 | 关键证据 |
|---|---|---|---|
| T1 | 安装与启动 | ✅ 通过 | `adb install Success`；`WailsBridge: Wails bridge initialized`；libwails.so 加载 ok |
| T2 | 界面渲染 | ✅ 通过 | [T2-tasklist.png](assets/T2-tasklist.png)：顶栏/筛选/批量操作/任务卡完整 |
| T3 | 保存目录（公共存储） | ✅ 通过 | 引擎默认目录 `/sdcard/Downloads` 可写；下载文件全部落盘可读 |
| T4 | 多连接下载 1MB | ✅ 通过 | 8 连接完成；SHA256 = `d2cc9a66…f1b` 与素材一致 |
| T5 | 多连接下载 10MB | ✅ 通过 | 8 连接完成；SHA256 = `f5ea0e7a…428` 一致 |
| T6 | 单连接回退（无 Range） | ✅ 通过 | 探测 `totalSize=0`（无 Content-Length）→ 回退流式下载；SHA256 一致 |
| T7 | 暂停 / 恢复（字节级续传） | ✅ 通过 | 5,767,168 字节处暂停 → 静置 4s 进度冻结 → 恢复**从断点继续**（未从 0 开始）→ SHA256 一致 |
| T8 | 校验和验证 | ✅ 通过 | 正确值 → `completed + checksum=ok`；错误值 → `failed + checksum=mismatch`（错误信息含期望/实际值） |
| T9 | 任务持久化 | ✅ 通过 | force-stop 后重启：8 个任务含状态完整保留 |
| T10 | 完成通知 | ✅ 通过 | `dumpsys notification`：活动 NotificationRecord，channel=`wails_default`，**title=下载完成** |
| T11 | 设置页平台适配 | ✅ 通过 | [T11-settings.png](assets/T11-settings.png)/[T11-integration.png](assets/T11-integration.png)：移动端无「窗口」区（关闭行为/最小化到托盘）、无「剪贴板监听」；REST API/Token/白名单正常 |
| T12 | 语言切换 | ✅ 通过 | [T12-english.png](assets/T12-english.png)：English 即时生效（无重启）；切回中文正常 |
| T13 | 稳定性 | ✅ 通过 | 全程 logcat 无 FATAL / AndroidRuntime / SIGSEGV |

## 3. 测试过程发现的问题

### 3.1 已修复：头部控件被系统状态栏遮挡（真机专属 bug）🔥

**现象**：设置齿轮/加号等头部按钮在可见位置点不到（任务行点击正常）。截图放大校准确认点击坐标无误（[gear-zoom.png](assets/gear-zoom.png)，红框即点击点、正中齿轮）。

**根因**：Android 15+ 强制 edge-to-edge，WebView 铺满整窗（0→2340），应用头部渲染在**系统状态栏窗口底下**——可见但状态栏拦截了该区域的所有触摸。

**修复**：`MainActivity.onCreate` 给 `main_container` 挂 `OnApplyWindowInsetsListener`，按 systemBars insets 给容器加内边距，WebView 内容从状态栏下方开始（状态栏底色与应用背景一致，视觉无缝）。

**复测**：[T13-safearea-fixed.png](assets/T13-safearea-fixed.png) 头部与状态栏完全分离；齿轮一次点击即打开设置页。

### 3.2 改进项（非阻塞，待后续处理）

| # | 现象 | 建议 |
|---|---|---|
| I-1 | 任务详情的「下载路径」显示为 Windows 反斜杠风格（`\sdcard\Downloads\…`）——`TaskDetailDialog` 硬编码了 `replace(/\//g,'\\')` | 按平台显示原生分隔符 |
| I-2 | 桌面启动器图标为 Wails 模板默认「W」，未用应用品牌图标 | 用 `build/appicon.png` 生成各密度 mipmap 替换 |
| I-3 | 模板构建日志有 `gcc_android.c: dlfcn.h` 报错（工具链探测噪音，不影响产物；本次 libwails.so 体积/哈希变化已确认新代码入包） | 上游关注；可忽略 |

### 3.3 测试方法备注

- `screencap` 全黑为 MIUI 系统限制（与应用无关，桌面截图同样全黑）；`screenrecord + ffmpeg` 不受限，作为视觉证据通道。
- `x86_64` 的 libwails.so 未随本轮重编（`android:package` 默认仅 arm64）；模拟器包请用 `ARCH=amd64` 或 `package:fat`。
- REST 驱动 + 设备端 `sha256sum` 断言的组合全程可靠；限速测试端点（/slow，210KB/s/连接）为自建服务器行为，用于留出暂停窗口。

## 4. 设备端遗留物（可随时清理）

- 任务记录 11 条（含测试任务）与下载文件：`/sdcard/Downloads/{range-1mb,range-10mb,norange-2mb,slow-20mb,checksum-ok,checksum-bad,notify-*}.bin`
- 应用数据：`filesDir/downloader/`（settings/tasks/stats/state/staging）
- 清理命令：`adb shell pm clear com.windchen.downloader` + `adb shell rm -f /sdcard/Downloads/*.bin`

## 5. 最终交付物

- `v3/bin/downloader.apk` — arm64 生产构建（含 insets 修复 + 平台适配设置页 + Platform 绑定）
- 代码改动：`v3/app.go`（Platform 绑定）、`v3/frontend/src`（api.ts/SettingsPage 平台适配 + i18n）、`v3/build/android/.../MainActivity.java`（insets 修复）、`v3/frontend/bindings`（重新生成）
- 测试文档：本报告 + [android-test-plan.md](android-test-plan.md) + [assets/](assets/) 证据截图
