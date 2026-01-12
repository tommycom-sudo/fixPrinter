# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

FineReport 自动打印工具是一个轻量级的 Wails（Go + WebView2）桌面应用程序，主要用于：

1. **处方单自动打印**：在医院环境中自动化处方单打印，加载 FineReport 页面，等待 `FR` 对象就绪，然后执行 `FR.doURLPrint()` 来打印处方文档
2. **API 监控与报警**：解析 curl 命令，按 Cron 表达式定时执行 HTTP 请求，监控响应时间，超时或失败时通过 PushPlus 发送告警通知

**目标环境**：仅限 Windows（使用 PowerShell 进行打印机控制，WebView2 用于 UI）

## 构建命令

```powershell
# 开发模式（HMR 热更新，默认端口：http://localhost:34115）
wails dev

# 生产构建（输出到 build/bin/xautoprint.exe）
wails build

# 自定义输出文件名构建
wails build -o fixprint请用管理员身份运行本程序.exe
```

**重要提示**：应用程序需要管理员权限才能通过 PowerShell 管理打印队列。

## 架构设计

### 后端 (Go)

**入口文件**：`main.go`
- 通过 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` 禁用 WebView2 安全策略（iframe 访问所需）
- 应用程序启动时隐藏（最小化到系统托盘）
- 通过 `setupTray()` 初始化系统托盘

**核心应用** (`app.go`):
- **App 结构体**：中央协调器，包含打印服务、反向代理和 FinePrint 进程监控
- **打印机控制**：使用 PowerShell（`Set-Printer`、`Get-Printer`、`Get-PrintJob`、`Remove-PrintJob`）管理 A5 打印机
- **打印工作流**：`PausePrinter`（暂停）-> `StartPrint`（打印）-> `RemovePrintJob`（清除任务）-> `ResumePrinter`（恢复）-> `QuitApp`（退出）
- **FinePrint 监控**：后台 goroutine 每 5 秒监视 `FinePrint.exe` 进程，检测到时自动触发打印
- **运行时 API**：`IsFinePrintMonitorEnabled()`、`IsFinePrintMonitorRunning()`、`StartFinePrintMonitor()`、`StopFinePrintMonitor()`

**打印服务** (`internal/printer/printer.go`):
- 领域模型：`PrintParams`、`Reportlet`、`PrintResult`
- 服务管理异步 JS 执行，使用请求/响应通道模式（通过 `waiters` map 和 channel 实现请求-响应同步）
- 默认目标：`hihis.smukqyy.cn` 上的 `hi/his/bil/test_printer.cpt`
- **超时配置**：`ReadyTimeout`（45 秒）、`ReadyInterval`（400ms）、`FrameLoadTimeout`（25 秒）、`ResultTimeout`（60 秒）

**反向代理** (`internal/proxy/server.go`):
- 轻量级 HTTP 代理：`127.0.0.1:<随机端口>` -> `https://hihis.smukqyy.cn:443`
- 移除 `X-Frame-Options` 和 `Content-Security-Policy` 头以允许 iframe 嵌入
- URL 通过 `swapBase()` 函数动态重写

**系统托盘** (`tray.go`):
- 使用 `github.com/getlantern/systray`
- 菜单：显示/隐藏窗口、退出
- 图标嵌入自 `build/windows/icon.ico`

**API 监控服务** (`internal/monitor/`):
- **配置管理** (`config.go`)：从 `monitor.json` 加载/保存监控任务配置，支持 PushPlus token 配置
- **CURL 解析器** (`curl_parser.go`)：解析 curl 命令字符串，提取 URL、headers、body 等信息
- **执行器** (`executor.go`)：执行 HTTP 请求，测量响应时间，超时或失败时触发 PushPlus 告警
- **调度器** (`scheduler.go`)：基于 Cron 表达式调度任务执行，管理任务生命周期

### 前端

**技术栈**：原生 JS + Vite，无框架

**主界面** (`frontend/src/main.js`):
- JSON 编辑器用于打印参数
- 嵌入式 iframe 用于 FineReport 会话
- 打印任务监控表格（每 5 秒自动刷新）
- 自动删除功能：打印机暂停时自动删除新出现的打印任务

**核心函数**:
- `handlePrint()`: 在 iframe 中加载 FineReport 页面
- `handlePausePrinter()`: 暂停打印机并清空队列
- `handleResumePrinter()`: 恢复打印机
- `refreshJobs()`: 通过 Go 绑定监控打印队列
- `loadReportFrame()`: 加载 iframe 并等待页面加载完成

### 打印数据流

1. 用户在 UI 中编辑 JSON 参数
2. `StartPrint()` 生成唯一的 `requestId`，通过 `runtime.WindowExecJS()` 调用 `window.__xAutoPrint.start()`
3. 前端在 iframe 中加载 FineReport 入口 URL
4. 等待 iframe 加载完成（`FrameLoadTimeout` 超时）
5. 等待 `window.FR` 对象可用（`ReadyTimeout` 超时，每 `ReadyInterval` 检查一次）
6. 使用提供的参数调用 `FR.doURLPrint()`
7. 前端调用 `NotifyPrintResult()` 通知 Go 后端
8. Go 后端解除阻塞并返回结果给调用者

**注意**：当前前端实现简化了打印流程，仅加载 FineReport 页面到 iframe，未自动执行 `FR.doURLPrint()`。实际打印由用户在 iframe 中手动完成或通过 FinePrint 进程监控自动触发。

### FinePrint 进程监控

应用程序监控 `FinePrint.exe` 以自动化处方打印工作流（`watchFinePrintProcess`）：

1. 检测到 `FinePrint.exe` 时，通过 `Set-Printer -StartTime/-UntilTime` 自动暂停打印机
2. 监控打印队列，自动删除新任务
3. 触发 `fix-printer.exe` 继续打印
4. 所有任务清除后，恢复打印机并退出应用程序

### API 监控与报警

应用程序提供 API 监控功能，用于定时检查 HTTP 接口响应状态：

1. **配置文件** (`monitor.json`)：存储 PushPlus token 和监控任务列表
2. **CURL 解析**：自动解析 curl 命令，提取 URL、headers、cookies、body 等信息
3. **定时执行**：基于 Cron 表达式（如 `*/1 * * * *` 表示每分钟执行）调度任务
4. **超时检测**：配置响应时间阈值（默认 1000ms），超时即触发告警
5. **PushPlus 告警**：请求失败、超时或返回非 2xx 状态码时，通过 PushPlus 发送通知

**运行时 API**：`GetMonitorConfig()`、`SaveMonitorConfig()`、`GetMonitorStatus()`、`AddMonitorTask()`、`RemoveMonitorTask()`、`UpdateMonitorTask()`、`TestPushPlus()`、`ParseCURL()`

## 配置

**默认打印机**：`A5`（硬编码在 `app.go` 的 `defaultPrinterName` 常量中）

**API 监控配置** (`monitor.json`)：
```json
{
  "pushPlusToken": "your_pushplus_token",
  "tasks": [
    {
      "name": "订单计算接口",
      "cron": "*/1 * * * *",
      "curl": "curl 'https://example.com/api/...' -H 'Content-Type: application/json' ...",
      "timeoutMs": 1000,
      "enabled": true
    }
  ]
}
```
- `pushPlusToken`：PushPlus 通知 token（从 pushplus.plus 获取）
- `tasks`：监控任务列表，每个任务包含名称、Cron 表达式、curl 命令、超时阈值、启用状态
- Cron 表达式格式：`秒 分 时 日 月`，如 `*/1 * * * *` = 每分钟，`0 */5 * * *` = 每 5 分钟

**FinePrint 监控开关**：`finePrintMonitorEnabled`（`app.go` 常量）
- `true`：启用 FinePrint.exe 进程监控（检测到进程时自动暂停打印机并清理队列）
- `false`：禁用 FinePrint.exe 进程监控（默认值）
- 可通过运行时 API `StartFinePrintMonitor()` / `StopFinePrintMonitor()` 动态控制

**FineReport URLs**（`internal/printer/printer.go` 中的默认值）:
- 入口: `https://hihis.smukqyy.cn/webroot/decision/view/report?viewlet=hi%252Fhis%252Fbil%252Ftest_printer.cpt...`
- 打印: `https://hihis.smukqyy.cn/webroot/decision/view/report`

**日志**：日志写入 `logs/autoprint.log`，带时间戳和微秒精度

## 依赖

主要 Go 包（Go 1.23+）：
- `github.com/wailsapp/wails/v2` v2.11.0 - 桌面应用框架
- `github.com/getlantern/systray` v1.2.2 - 系统托盘集成
- `github.com/google/uuid` v1.6.0 - 请求 ID 生成
- `github.com/robfig/cron/v3` v3.0.1 - Cron 表达式调度

前端：
- `vite` ^3.0.7 - 构建工具

## 文件结构说明

- `wails.json` 包含应用程序元数据和构建配置
- `monitor.json` API 监控配置文件（程序运行时自动创建）
- `frontend/src/` 中的前端源代码通过 `//go:embed all:frontend/dist` 打包到 Go 二进制文件中
- Windows 图标位于 `build/windows/icon.ico`（通过 `//go:embed` 嵌入到 `tray.go`）
- `fix-printer.exe`（外部二进制文件）用于自动打印触发，需与主程序同目录

## 开发说明

1. **WebView 安全**：应用在 `configureWebViewSecurity()` 中明确禁用 Web 安全（`--disable-web-security`）以允许跨域 iframe 访问。这仅在可信的内网环境中可接受。

2. **PowerShell 命令**：所有打印机操作使用隐藏的 PowerShell 窗口（`syscall.SysProcAttr{HideWindow: true}`）。

3. **暂停机制**：打印机暂停使用时间窗口操作（`StartTime`/`UntilTime`）而非 `Suspend-PrintQueue`。暂停状态检测条件：`StartTime == 0` 且 `UntilTime == 2`。`ensurePrinterPaused()` 函数用于确保打印机处于暂停状态。

4. **代理 URL 重写**：代理启动时，`PrintParams` 中的 `entryUrl` 和 `printUrl` 会自动重写为指向 `127.0.0.1:<proxy_port>` 而非远程服务器。

5. **窗口状态管理**：窗口默认隐藏（`StartHidden: true`），关闭时隐藏到系统托盘而非真正退出，除非 `allowExit` 标志为 `true`（在 FinePrint 清理完成后设置）。

6. **FinePrint 监控配置**：通过修改 `app.go` 中的 `finePrintMonitorEnabled` 常量来控制是否在启动时自动开启 FinePrint 进程监控。默认为 `false`（禁用）。运行时可通过 `StartFinePrintMonitor()` 和 `StopFinePrintMonitor()` API 动态控制。

7. **API 监控配置**：编辑 `monitor.json` 文件配置监控任务和 PushPlus token。程序启动时自动加载配置并启动调度器。修改配置后可通过 `ReloadMonitor()` API 重新加载。Cron 表达式格式为 6 段式（秒 分 时 日 月 周），支持标准 Cron 语法。
