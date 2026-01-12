package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fine-report-printer/internal/monitor"
	"fine-report-printer/internal/printer"
	"fine-report-printer/internal/proxy"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultPrinterName   = "A5"
	finePrintProcessName = "FinePrint.exe"
	logDirName           = "logs"
	logFileName          = "autoprint.log"

	// FinePrint 监控开关配置
	// true: 启用 FinePrint.exe 进程监控（默认）
	// false: 禁用 FinePrint.exe 进程监控
	finePrintMonitorEnabled = false
)

// App struct
type App struct {
	ctx                context.Context
	printer            *printer.Service
	proxy              *proxy.Server
	proxyBase          string
	remoteBase         string
	isWindowVisible    bool
	finePrintCancel    context.CancelFunc
	finePrintActive    bool
	cleanupCompleted   bool
	allowExit          bool
	autoPrintTriggered bool
	monitor            *monitor.Scheduler
	monitorConfig      *monitor.Config
}

// PrintJob captures a subset of properties returned by Get-PrintJob.
type PrintJob struct {
	ID            int    `json:"id"`
	ComputerName  string `json:"computerName"`
	PrinterName   string `json:"printerName"`
	DocumentName  string `json:"documentName"`
	SubmittedTime string `json:"submittedTime"`
	JobStatus     string `json:"jobStatus"`
}

// NewApp creates a new App application struct
func NewApp() *App {
	defaults := printer.DefaultParams()
	return &App{
		printer:    printer.NewService(printer.Config{}),
		remoteBase: extractBase(defaults.EntryURL),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.initLogger()
	a.printer.SetContext(ctx)
	a.startProxy(ctx)

	if finePrintMonitorEnabled {
		a.startFinePrintMonitor()
		a.logInfo("FinePrint 监控已启用")
	} else {
		a.logInfo("FinePrint 监控已禁用")
	}

	// Initialize API monitor
	a.startAPIMonitor()

	// Window is already hidden via StartHidden option
	a.isWindowVisible = false
}

// OnBeforeClose is called when the window is about to close
// Return true to prevent the window from closing
func (a *App) OnBeforeClose(ctx context.Context) bool {
	if a.allowExit {
		return false
	}
	// Hide window instead of closing
	a.HideWindow()
	// Update tray menu items
	updateTrayMenuOnHide()
	// Return true to prevent default close behavior
	return true
}

func (a *App) shutdown(ctx context.Context) {
	if a.finePrintCancel != nil {
		a.finePrintCancel()
	}
	if a.proxy != nil {
		if err := a.proxy.Stop(ctx); err != nil {
			runtime.LogError(ctx, "stop proxy: "+err.Error())
		}
	}
	if a.monitor != nil {
		a.monitor.Stop()
	}
}

// ShowWindow shows the main window
func (a *App) ShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
		runtime.WindowUnminimise(a.ctx)
		a.isWindowVisible = true
	}
}

// HideWindow hides the main window
func (a *App) HideWindow() {
	if a.ctx != nil {
		runtime.WindowHide(a.ctx)
		a.isWindowVisible = false
	}
}

// QuitApp quits the application
func (a *App) QuitApp() {
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

// DefaultPrintParams exposes the suggested base payload to the UI.
func (a *App) DefaultPrintParams() printer.PrintParams {
	params := printer.DefaultParams()
	if entry := a.printer.EntryURL(); entry != "" {
		params.EntryURL = entry
	}
	if printURL := a.printer.PrintURL(); printURL != "" {
		params.PrintURL = printURL
	}
	return params
}

// StartPrint orchestrates the FineReport printing workflow.
func (a *App) StartPrint(params printer.PrintParams) (*printer.PrintResult, error) {
	return a.printer.Print(params)
}

// NotifyPrintResult is triggered from the frontend once the JS automation resolves.
func (a *App) NotifyPrintResult(result printer.PrintResult) {
	a.printer.NotifyResult(result)
}

// PausePrinter uses Set-Printer to effectively disable the queue by limiting the print window.
func (a *App) PausePrinter(name string) error {
	target := strings.TrimSpace(name)
	if target == "" {
		return fmt.Errorf("printer name is required")
	}

	_, offset := time.Now().Zone()
	offsetMinutes := offset / 60
	start := (1440 - offsetMinutes) % 1440
	if start < 0 {
		start += 1440
	}
	until := (start + 2) % 1440

	cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf("Set-Printer -Name %q -StartTime %d -UntilTime %d", target, start, until))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pause printer %s failed: %w: %s", target, err, strings.TrimSpace(string(output)))
	}

	return nil
}

// ResumePrinter restores the printer by removing the time restriction.
func (a *App) ResumePrinter(name string) error {
	target := strings.TrimSpace(name)
	if target == "" {
		return fmt.Errorf("printer name is required")
	}

	// Remove time restrictions by setting StartTime and UntilTime to null (0 means 24/7 available)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf("Set-Printer -Name %q -StartTime 0 -UntilTime 0", target))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("resume printer %s failed: %w: %s", target, err, strings.TrimSpace(string(output)))
	}

	return nil
}

// PrinterStatus represents the status information of a printer.
type PrinterStatus struct {
	Name          string `json:"name"`
	PrinterStatus int    `json:"printerStatus"`
	StartTime     int    `json:"startTime"`
	UntilTime     int    `json:"untilTime"`
	IsPaused      bool   `json:"isPaused"`
}

// GetPrinterStatus returns the status information of the specified printer.
func (a *App) GetPrinterStatus(name string) (*PrinterStatus, error) {
	target := strings.TrimSpace(name)
	if target == "" {
		target = defaultPrinterName
	}

	script := fmt.Sprintf(`$ErrorActionPreference='Stop';
$OutputEncoding=[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new();
$printer = Get-Printer -Name %q;
$isPaused = (($printer.StartTime -eq 0) -and ($printer.UntilTime -eq 2));
$status = @{
    name = $printer.Name;
    printerStatus = $printer.PrinterStatus;
    startTime = $printer.StartTime;
    untilTime = $printer.UntilTime;
    isPaused = $isPaused;
};
$status | ConvertTo-Json -Depth 3`, target)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("get printer status for %s failed: %w: %s", target, err, strings.TrimSpace(string(output)))
	}

	raw := strings.TrimSpace(string(output))
	var status PrinterStatus
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return nil, fmt.Errorf("decode printer status for %s: %w", target, err)
	}

	return &status, nil
}

// RemovePrintJob deletes a print job from the specified printer.
func (a *App) RemovePrintJob(printerName string, jobID int) error {
	target := strings.TrimSpace(printerName)
	if target == "" {
		target = defaultPrinterName
	}

	cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf("Remove-PrintJob -PrinterName %q -ID %d", target, jobID))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove print job %d from printer %s failed: %w: %s", jobID, target, err, strings.TrimSpace(string(output)))
	}

	return nil
}

// GetPrinterJobs returns the current print queue items for the requested printer (default: HP LaserJet Pro P1100 plus series).
func (a *App) GetPrinterJobs(name string) ([]PrintJob, error) {
	target := strings.TrimSpace(name)
	if target == "" {
		target = defaultPrinterName
	}

	script := fmt.Sprintf(`$ErrorActionPreference='Stop';
$OutputEncoding=[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new();
$jobs = Get-PrintJob -PrinterName %q | Select-Object @{Name='id';Expression={$_.Id}}, @{Name='computerName';Expression={$_.ComputerName}}, @{Name='printerName';Expression={$_.PrinterName}}, @{Name='documentName';Expression={$_.DocumentName}}, @{Name='submittedTime';Expression={ if ($_.SubmittedTime) { $_.SubmittedTime.ToString('yyyy-MM-dd HH:mm:ss') } else { '' } }}, @{Name='jobStatus';Expression={ if ($_.JobStatus) { $_.JobStatus.ToString() } else { '' } }};
$jobs = @($jobs);
$jobs | ConvertTo-Json -Depth 3`, target)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("get jobs for printer %s failed: %w: %s", target, err, strings.TrimSpace(string(output)))
	}

	raw := strings.TrimSpace(string(output))
	if raw == "" || raw == "[]" || raw == "null" {
		return []PrintJob{}, nil
	}

	var jobs []PrintJob
	if strings.HasPrefix(raw, "{") {
		var job PrintJob
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			return nil, fmt.Errorf("decode printer job for %s: %w", target, err)
		}
		return []PrintJob{job}, nil
	}
	if err := json.Unmarshal([]byte(raw), &jobs); err != nil {
		return nil, fmt.Errorf("decode printer jobs for %s: %w", target, err)
	}

	return jobs, nil
}

func (a *App) startProxy(ctx context.Context) {
	if a.remoteBase == "" {
		return
	}

	server, err := proxy.New(a.remoteBase)
	if err != nil {
		runtime.LogError(ctx, "init proxy: "+err.Error())
		return
	}
	baseURL, err := server.Start()
	if err != nil {
		runtime.LogError(ctx, "start proxy: "+err.Error())
		return
	}
	a.proxy = server
	a.proxyBase = baseURL

	defaults := printer.DefaultParams()
	entry := swapBase(defaults.EntryURL, a.remoteBase, baseURL)
	printURL := swapBase(defaults.PrintURL, a.remoteBase, baseURL)
	a.printer.SetEndpoints(entry, printURL)

	log.Printf("FineReport proxy ready: %s -> %s", a.remoteBase, baseURL)
}

func extractBase(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	base := parsed.Scheme + "://" + parsed.Host
	if parsed.Port() == "" && strings.Contains(parsed.Host, ":") {
		base = parsed.Scheme + "://" + parsed.Host
	}
	return base
}

func swapBase(raw, from, to string) string {
	if raw == "" || from == "" || to == "" {
		return raw
	}
	if strings.HasPrefix(raw, from) {
		return to + strings.TrimPrefix(raw, from)
	}
	return raw
}

func (a *App) startFinePrintMonitor() {
	if a.finePrintCancel != nil {
		a.finePrintCancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.finePrintCancel = cancel

	go a.watchFinePrintProcess(ctx)
}

func (a *App) watchFinePrintProcess(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.evaluateFinePrintState()
		}
	}
}

func (a *App) evaluateFinePrintState() {
	if a.cleanupCompleted {
		return
	}

	running, err := isProcessRunning(finePrintProcessName)
	if err != nil {
		a.logError("检测 FinePrint.exe 进程失败: %v", err)
		return
	}

	if !running && !a.finePrintActive {
		return
	}

	if err := a.ensurePrinterPaused(); err != nil {
		a.logError("自动暂停打印机失败: %v", err)
		return
	}

	jobs, err := a.GetPrinterJobs(defaultPrinterName)
	if err != nil {
		a.logError("获取打印队列失败: %v", err)
		return
	}

	if len(jobs) == 0 {
		if running && !a.finePrintActive {
			a.logInfo("检测到 FinePrint.exe，已暂停打印机，等待队列出现任务")
			a.finePrintActive = true
			a.triggerAutoPrint()
		} else if !running && a.finePrintActive {
			a.logInfo("已发送自动打印命令，正在等待任务进入队列")
		}
		return
	}

	a.logInfo("检测到 %d 个打印任务，准备删除", len(jobs))

	removed, err := a.removeAllPrinterJobs()
	if err != nil {
		a.logError("自动删除打印任务失败: %v", err)
		return
	}
	if removed == 0 {
		a.logInfo("队列中的任务已被其他程序清理，无需额外操作，继续恢复打印机")
	}

	if err := a.ResumePrinter(defaultPrinterName); err != nil {
		a.logError("自动恢复打印机失败: %v", err)
		return
	}

	a.finePrintActive = false
	a.cleanupCompleted = true
	a.allowExit = true

	a.logInfo("已删除 %d 个任务，打印机已恢复，准备退出", removed)
	if a.finePrintCancel != nil {
		a.finePrintCancel()
	}

	if a.ctx != nil {
		a.logInfo("即将关闭应用")
		runtime.Quit(a.ctx)
	}
}

func (a *App) ensurePrinterPaused() error {
	status, err := a.GetPrinterStatus(defaultPrinterName)
	if err != nil {
		return err
	}
	if status != nil && status.IsPaused {
		return nil
	}
	return a.PausePrinter(defaultPrinterName)
}

func isProcessRunning(imageName string) (bool, error) {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", imageName))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("tasklist: %w", err)
	}

	lowered := strings.ToLower(string(output))
	return strings.Contains(lowered, strings.ToLower(imageName)), nil
}

func (a *App) removeAllPrinterJobs() (int, error) {
	jobs, err := a.GetPrinterJobs(defaultPrinterName)
	if err != nil {
		a.logError("获取打印队列失败: %v", err)
		return 0, err
	}

	removed := 0
	for _, job := range jobs {
		if err := a.RemovePrintJob(defaultPrinterName, job.ID); err != nil {
			a.logError("自动删除任务 %d 失败: %v", job.ID, err)
			continue
		}
		a.logInfo("已删除任务 %d（%s）", job.ID, job.DocumentName)
		removed++
	}
	return removed, nil
}

func (a *App) triggerAutoPrint() {
	if a.autoPrintTriggered {
		return
	}

	wd, err := os.Getwd()
	if err != nil {
		a.logError("获取工作目录失败: %v", err)
		return
	}

	exePath := filepath.Join(wd, "fix-printer.exe")
	if _, err := os.Stat(exePath); err != nil {
		a.logError("未找到 fix-printer.exe: %v", err)
		return
	}

	cmd := exec.Command(exePath)
	a.logInfo("准备调用 fix-printer.exe，继续监测打印队列")

	if err := cmd.Start(); err != nil {
		a.logError("启动 fix-printer.exe 失败: %v", err)
		return
	}

	if err := cmd.Process.Release(); err != nil {
		a.logError("释放 fix-printer.exe 进程句柄失败: %v", err)
	}

	a.autoPrintTriggered = true
	a.logInfo("fix-printer.exe 已后台启动 (PID %d)", cmd.Process.Pid)
}

func (a *App) initLogger() {
	wd, err := os.Getwd()
	if err != nil {
		log.Printf("[ERROR] 获取工作目录失败: %v", err)
		return
	}
	dir := filepath.Join(wd, logDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.logError("创建日志目录失败: %v", err)
		return
	}
	logPath := filepath.Join(dir, logFileName)
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		a.logError("打开日志文件失败: %v", err)
		return
	}
	log.SetOutput(file)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	a.logInfo("日志输出已写入 %s", logPath)
}

func (a *App) logInfo(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	log.Printf("[INFO] %s", message)
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, message)
	}
}

func (a *App) logError(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	log.Printf("[ERROR] %s", message)
	if a.ctx != nil {
		runtime.LogError(a.ctx, message)
	}
}

// IsFinePrintMonitorEnabled returns whether FinePrint monitoring is enabled via config
func (a *App) IsFinePrintMonitorEnabled() bool {
	return finePrintMonitorEnabled
}

// IsFinePrintMonitorRunning returns whether FinePrint monitor is currently running
func (a *App) IsFinePrintMonitorRunning() bool {
	return a.finePrintCancel != nil
}

// StartFinePrintMonitor manually starts the FinePrint process monitor
func (a *App) StartFinePrintMonitor() error {
	if a.finePrintCancel != nil {
		return fmt.Errorf("FinePrint 监控已在运行中")
	}
	a.startFinePrintMonitor()
	a.logInfo("手动启动 FinePrint 监控")
	return nil
}

// StopFinePrintMonitor manually stops the FinePrint process monitor
func (a *App) StopFinePrintMonitor() error {
	if a.finePrintCancel == nil {
		return fmt.Errorf("FinePrint 监控未运行")
	}
	a.finePrintCancel()
	a.finePrintCancel = nil
	a.finePrintActive = false
	a.logInfo("手动停止 FinePrint 监控")
	return nil
}

// API Monitor Task Management

// startAPIMonitor initializes the API monitor
func (a *App) startAPIMonitor() {
	config, err := monitor.LoadConfig("")
	if err != nil {
		a.logError("加载监控配置失败: %v", err)
		return
	}

	a.monitorConfig = config
	a.monitor = monitor.NewScheduler(config, "monitor.json")

	if err := a.monitor.Start(); err != nil {
		a.logError("启动监控调度器失败: %v", err)
		return
	}

	a.logInfo("API 监控已启动")
}

// GetMonitorConfig returns the current monitor configuration
func (a *App) GetMonitorConfig() *monitor.Config {
	if a.monitorConfig == nil {
		cfg, _ := monitor.LoadConfig("")
		a.monitorConfig = cfg
	}
	return a.monitorConfig
}

// SaveMonitorConfig saves the monitor configuration
func (a *App) SaveMonitorConfig(cfgJSON string) error {
	var cfg monitor.Config
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return fmt.Errorf("解析配置失败: %w", err)
	}

	if err := cfg.SaveConfig("monitor.json"); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	a.monitorConfig = &cfg
	return nil
}

// ReloadMonitor reloads and restarts the monitor
func (a *App) ReloadMonitor() error {
	if a.monitor == nil {
		return fmt.Errorf("监控未初始化")
	}

	if err := a.monitor.Reload(); err != nil {
		return err
	}

	a.logInfo("监控配置已重新加载")
	return nil
}

// GetMonitorStatus returns the status of all monitoring tasks
func (a *App) GetMonitorStatus() map[string]monitor.TaskStatus {
	if a.monitor == nil {
		return make(map[string]monitor.TaskStatus)
	}
	return a.monitor.GetStatus()
}

// AddMonitorTask adds a new monitoring task
func (a *App) AddMonitorTask(taskJSON string) error {
	var task monitor.TaskConfig
	if err := json.Unmarshal([]byte(taskJSON), &task); err != nil {
		return fmt.Errorf("解析任务失败: %w", err)
	}

	if a.monitor == nil {
		return fmt.Errorf("监控未初始化")
	}

	return a.monitor.AddTask(task)
}

// RemoveMonitorTask removes a monitoring task
func (a *App) RemoveMonitorTask(taskName string) error {
	if a.monitor == nil {
		return fmt.Errorf("监控未初始化")
	}

	return a.monitor.RemoveTask(taskName)
}

// UpdateMonitorTask updates an existing monitoring task
func (a *App) UpdateMonitorTask(taskJSON string) error {
	var task monitor.TaskConfig
	if err := json.Unmarshal([]byte(taskJSON), &task); err != nil {
		return fmt.Errorf("解析任务失败: %w", err)
	}

	if a.monitor == nil {
		return fmt.Errorf("监控未初始化")
	}

	return a.monitor.UpdateTask(task)
}

// TestPushPlus tests the pushplus notification
func (a *App) TestPushPlus(token, title, content string) error {
	exec := monitor.NewExecutor()
	return exec.TestPushPlus(token, title, content)
}

// ParseCURL parses a curl command and returns the parsed result
func (a *App) ParseCURL(curlCmd string) (*monitor.ParsedRequest, error) {
	return monitor.ParseCURLCommand(curlCmd)
}
