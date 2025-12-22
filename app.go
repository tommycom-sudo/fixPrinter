package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"strings"
	"syscall"

	"fine-report-printer/internal/printer"
	"fine-report-printer/internal/proxy"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx             context.Context
	printer         *printer.Service
	proxy           *proxy.Server
	proxyBase       string
	remoteBase      string
	isWindowVisible bool
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
	a.printer.SetContext(ctx)
	a.startProxy(ctx)

	// Window is already hidden via StartHidden option
	a.isWindowVisible = false
}

// OnBeforeClose is called when the window is about to close
// Return true to prevent the window from closing
func (a *App) OnBeforeClose(ctx context.Context) bool {
	// Hide window instead of closing
	a.HideWindow()
	// Update tray menu items
	updateTrayMenuOnHide()
	// Return true to prevent default close behavior
	return true
}

func (a *App) shutdown(ctx context.Context) {
	if a.proxy != nil {
		if err := a.proxy.Stop(ctx); err != nil {
			runtime.LogError(ctx, "stop proxy: "+err.Error())
		}
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

	cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf("Set-Printer -Name %q -StartTime 0 -UntilTime 2", target))
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
		target = "MS"
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
		target = "MS"
	}

	cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf("Remove-PrintJob -PrinterName %q -ID %d", target, jobID))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove print job %d from printer %s failed: %w: %s", jobID, target, err, strings.TrimSpace(string(output)))
	}

	return nil
}

// GetPrinterJobs returns the current print queue items for the requested printer (default: MS).
func (a *App) GetPrinterJobs(name string) ([]PrintJob, error) {
	target := strings.TrimSpace(name)
	if target == "" {
		target = "MS"
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
