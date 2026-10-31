# Downloader v3 · Android 真机测试计划

> 对应版本：v3（Wails v3.0.0，commit `935203e` 之后的同步适配构建）
> 执行与结果回填：见同目录 [android-test-report.md](android-test-report.md)

## 1. 测试环境

| 项 | 值 |
|---|---|
| 设备 | Redmi K20 Pro（raphael），Android 17（API 37），1080×2340 |
| APK | `v3/bin/downloader.apk`，applicationId `com.windchen.downloader`，arm64 生产构建（debug 签名） |
| 连接 | USB（adb），`adb reverse tcp:18080 tcp:18080` 把设备 `127.0.0.1:18080` 转发到 PC 测试服务器 |
| 测试服务器 | PC 上 `127.0.0.1:18080`（见下），设备经 adb reverse 访问同名地址 |
| 预授权 | `appops set … MANAGE_EXTERNAL_STORAGE allow`（公共目录写入）、`pm grant … POST_NOTIFICATIONS` |

## 2. 测试素材

PC 临时目录生成确定性伪随机文件（字节级确定性，SHA256 可复算）：

| 文件 | 大小 | 用途 | SHA256 |
|---|---|---|---|
| `range-1mb.bin` | 1 MB | 多连接分段下载（标准 Range） | `d2cc9a66604dca5cfde3b86acc5e9859dbbc409675faeb0ec55326ebc8575f1b` |
| `range-10mb.bin` | 10 MB | 多连接 + 完成校验 | `f5ea0e7afd77d76880e6b712c03180c5a926d9587b27f15dcabb9e006f98d428` |
| `norange-2mb.bin` | 2 MB | 不支持 Range → 单连接回退 | `76821c422c927329742792d155a9fe89b3067f6398a21ff3b38f95d0ed804f52` |
| `slow-20mb.bin` | 20 MB | 限速端点（~210KB/s/连接）→ 暂停/续传窗口 | `d0cfbf5f2c6dd5581e545a42134aa89ffc7b39a87e7596e3656d6b39437c1aae` |

服务器端点（仅本机绑定，经 adb reverse 供设备访问）：

- `/range/<file>`：标准 `http.FileServer`，支持 Range（多连接分段）
- `/norange/<file>`：忽略 Range、无 Content-Length（触发单连接流式回退）
- `/slow/<file>`：支持 Range 但每 64KB 睡 300ms（限速，留出暂停操作窗口）

## 3. 测试项

| # | 测试项 | 步骤 | 预期 |
|---|---|---|---|
| T1 | 安装与启动 | `adb install -r` → `am start` | 安装成功，应用启动无崩溃，任务列表渲染 |
| T2 | 界面渲染 | 截图任务列表 | 中文界面完整：顶栏/筛选/空态或列表 |
| T3 | 默认保存目录改为公共目录 | 设置 → 下载 → 默认保存目录 = `/sdcard/Download/downloader-test` → 保存 | 保存成功，无报错 |
| T4 | 多连接下载（1MB） | 新建 `http://127.0.0.1:18080/range/range-1mb.bin` | 任务完成；设备上文件 SHA256 与素材一致 |
| T5 | 多连接下载（10MB，8 连接） | 新建同上 `range-10mb.bin` | 任务完成；SHA256 一致 |
| T6 | 单连接回退 | 新建 `/norange/norange-2mb.bin` | 任务完成（回退路径）；SHA256 一致 |
| T7 | 暂停 / 恢复（字节级续传） | 新建 `/slow/slow-20mb.bin`，下载中点暂停 → 截图 → 恢复 | 暂停即时生效、状态正确；恢复后完成；SHA256 一致（续传不丢进度） |
| T8 | 校验和验证 | 新建 `range-1mb.bin` 副本任务，填入 SHA256 期望值 | 完成且校验标记为 ok；再次以错误期望值建任务 → 校验失败标记 |
| T9 | 任务持久化 | `am force-stop` → 重新启动 | 任务列表与状态完整保留 |
| T10 | 完成通知 | 通知权限已授予，完成 1 个任务 | 系统通知出现「下载完成」 |
| T11 | 设置的平台适配 | 打开设置页 | 移动端不显示「窗口」区（关闭行为/最小化到托盘）与「剪贴板监听」；其余设置正常 |
| T12 | 语言切换 | 设置 → English → 返回 | 界面即时切换英文；再次切回中文正常 |
| T13 | 稳定性 | 全程 `adb logcat` | 无 FATAL/AndroidRuntime 崩溃 |

## 4. 判定标准

- T4–T8 以设备端 `sha256sum` 输出与素材哈希**逐字节一致**为准
- 界面类以截图为准（存于 `docs/assets/`，在报告中引用）
- 任一测试项失败 → 记录现象、logcat 片段，修复后回归
