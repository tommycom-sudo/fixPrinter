# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

FineReport 自动打印工具是一个轻量级的 Wails（Go + WebView2）桌面应用程序，用于在医院环境中自动化处方单打印。它加载 FineReport 页面，等待 `FR` 对象就绪，然后执行 `FR.doURLPrint()` 来打印处方文档。

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
- **打印机控制**：使用 PowerShell（`Set-Printer`、`Get-Printer`、`Get-PrintJob`、`Remove-PrintJob`）管理 HP LaserJet Pro P1100 plus series
- **打印工作流**：`PausePrinter`（暂停）-> `StartPrint`（打印）-> `RemovePrintJob`（清除任务）-> `ResumePrinter`（恢复）-> `QuitApp`（退出）
- **FinePrint 监控**：后台 goroutine 监视 `FinePrint.exe` 进程，检测到时自动触发打印

**打印服务** (`internal/printer/printer.go`):
- 领域模型：`PrintParams`、`Reportlet`、`PrintResult`
- 服务管理异步 JS 执行，使用请求/响应通道模式
- 默认目标：`hihis.smukqyy.cn` 上的 `hi/his/bil/test_printer.cpt`

**反向代理** (`internal/proxy/server.go`):
- 轻量级 HTTP 代理：`127.0.0.1:<随机端口>` -> `https://hihis.smukqyy.cn:443`
- 移除 `X-Frame-Options` 和 `Content-Security-Policy` 头以允许 iframe 嵌入
- URL 通过 `swapBase()` 函数动态重写

**系统托盘** (`tray.go`):
- 使用 `github.com/getlantern/systray`
- 菜单：显示/隐藏窗口、退出
- 图标嵌入自 `build/windows/icon.ico`

### 前端

**技术栈**：原生 JS + Vite，无框架

**主界面** (`frontend/src/main.js`):
- JSON 编辑器用于打印参数
- 嵌入式 iframe 用于 FineReport 会话
- 打印任务监控表格（每 5 秒自动刷新）
- 自动删除功能：打印机暂停时删除打印任务

**核心函数**:
- `handlePrint()`: 在 iframe 中加载 FineReport 页面
- `handlePausePrinter()`: 暂停打印机并清空队列
- `handleResumePrinter()`: 恢复打印机
- `refreshJobs()`: 通过 Go 绑定监控打印队列

### 打印数据流

1. 用户在 UI 中编辑 JSON 参数
2. `StartPrint()` 生成唯一的 `requestId`，通过 `runtime.WindowExecJS()` 调用 `window.__xAutoPrint.start()`
3. 前端在 iframe 中加载 FineReport 入口 URL
4. 等待 `window.FR` 对象可用（45 秒超时）
5. 使用提供的参数调用 `FR.doURLPrint()`
6. 前端调用 `NotifyPrintResult()` 通知 Go 后端
7. Go 后端解除阻塞并返回结果给调用者

### FinePrint 进程监控

应用程序监控 `FinePrint.exe` 以自动化处方打印工作流：

1. 检测到 `FinePrint.exe` 时，通过 `Set-Printer -StartTime/-UntilTime` 自动暂停打印机
2. 监控打印队列，自动删除新任务
3. 触发 `fix-printer.exe` 继续打印
4. 所有任务清除后，恢复打印机并退出应用程序

## 配置

**默认打印机**：`HP LaserJet Pro P1100 plus series`（硬编码在 `app.go` 中）

**FineReport URLs**（`internal/printer/printer.go` 中的默认值）:
- 入口: `https://hihis.smukqyy.cn/webroot/decision/view/report?viewlet=hi%2Fhis%2Fbil%2Ftest_printer.cpt...`
- 打印: `https://hihis.smukqyy.cn/webroot/decision/view/report`

**日志**：日志写入 `logs/autoprint.log`，带时间戳

## 依赖

主要 Go 包：
- `github.com/wailsapp/wails/v2` - 桌面应用框架
- `github.com/getlantern/systray` - 系统托盘集成
- `github.com/google/uuid` - 请求 ID 生成

前端：
- `vite` - 构建工具

## 文件结构说明

- `wails.json` 包含应用程序元数据和构建配置
- `frontend/src/` 中的前端源代码通过 `//go:embed` 打包到 Go 二进制文件中
- Windows 图标位于 `build/windows/icon.ico`
- `fix-printer.exe`（外部二进制文件）用于自动打印触发

## 开发说明

1. **WebView 安全**：应用明确禁用 Web 安全以允许跨域 iframe 访问。这仅在可信的内网环境中可接受。

2. **PowerShell 命令**：所有打印机操作使用隐藏的 PowerShell 窗口（`SysProcAttr.HideWindow = true`）。

3. **暂停机制**：打印机暂停使用时间窗口操作（`StartTime`/`UntilTime`）而非 `Suspend-PrintQueue`。暂停状态检测条件：`StartTime == 0` 且 `UntilTime == 2`。

4. **代理 URL 重写**：代理启动时，`PrintParams` 中的 `entryUrl` 和 `printUrl` 会自动重写为指向 `127.0.0.1:<proxy_port>` 而非远程服务器。
