package main

import (
	"context"
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
	ctx        context.Context
	printer    *printer.Service
	proxy      *proxy.Server
	proxyBase  string
	remoteBase string
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
}

func (a *App) shutdown(ctx context.Context) {
	if a.proxy != nil {
		if err := a.proxy.Stop(ctx); err != nil {
			runtime.LogError(ctx, "stop proxy: "+err.Error())
		}
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
