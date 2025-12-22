package main

import (
	"embed"
	"io"
	"os"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var iconData embed.FS

var (
	appInstance    *App
	showMenuItem   *systray.MenuItem
	hideMenuItem   *systray.MenuItem
	toggleMenuItem *systray.MenuItem
	quitMenuItem   *systray.MenuItem
)

// setupTray initializes the system tray
func setupTray(app *App) {
	appInstance = app

	// Run systray (icon will be set in onReady)
	systray.Run(onReady, onExit)
}

// onReady is called when systray is ready
func onReady() {
	// Set icon and tooltip
	iconData := getIconData()
	if iconData != nil {
		systray.SetIcon(iconData)
	}
	systray.SetTooltip("FineReport 慢修复")

	// Create menu items
	showMenuItem = systray.AddMenuItem("显示窗口", "显示主窗口")
	hideMenuItem = systray.AddMenuItem("隐藏窗口", "隐藏到系统托盘")
	toggleMenuItem = systray.AddMenuItem("显示/隐藏", "切换窗口显示状态")

	systray.AddSeparator()

	quitMenuItem = systray.AddMenuItem("退出", "退出应用程序")

	// Initially window is hidden, so show "显示窗口"
	hideMenuItem.Hide()
	toggleMenuItem.Hide()

	// Handle menu clicks
	go func() {
		for {
			select {
			case <-showMenuItem.ClickedCh:
				showWindow()
			case <-hideMenuItem.ClickedCh:
				hideWindow()
			case <-toggleMenuItem.ClickedCh:
				toggleWindow()
			case <-quitMenuItem.ClickedCh:
				systray.Quit()
				if appInstance != nil && appInstance.ctx != nil {
					runtime.Quit(appInstance.ctx)
				}
				os.Exit(0)
			}
		}
	}()
}

// onExit is called when systray exits
func onExit() {
	// Cleanup if needed
}

// updateTrayMenuOnHide updates tray menu items when window is hidden
func updateTrayMenuOnHide() {
	if showMenuItem != nil {
		showMenuItem.Show()
	}
	if hideMenuItem != nil {
		hideMenuItem.Hide()
	}
	if toggleMenuItem != nil {
		toggleMenuItem.Hide()
	}
}

// updateTrayMenuOnShow updates tray menu items when window is shown
func updateTrayMenuOnShow() {
	if showMenuItem != nil {
		showMenuItem.Hide()
	}
	if hideMenuItem != nil {
		hideMenuItem.Show()
	}
	if toggleMenuItem != nil {
		toggleMenuItem.Show()
	}
}

// getIconData returns the icon data
func getIconData() []byte {
	iconFile, err := iconData.Open("build/windows/icon.ico")
	if err != nil {
		return nil
	}
	defer iconFile.Close()

	iconBytes, err := io.ReadAll(iconFile)
	if err != nil {
		return nil
	}

	return iconBytes
}

// showWindow shows the main window
func showWindow() {
	if appInstance != nil && appInstance.ctx != nil {
		runtime.WindowShow(appInstance.ctx)
		runtime.WindowUnminimise(appInstance.ctx)
		appInstance.isWindowVisible = true
		// Update menu
		updateTrayMenuOnShow()
	}
}

// hideWindow hides the main window
func hideWindow() {
	if appInstance != nil && appInstance.ctx != nil {
		runtime.WindowHide(appInstance.ctx)
		appInstance.isWindowVisible = false
		// Update menu
		updateTrayMenuOnHide()
	}
}

// toggleWindow toggles window visibility
func toggleWindow() {
	if appInstance != nil && appInstance.ctx != nil {
		// Try to show first, if already visible it will stay visible
		// Then check if we should hide by trying to minimize
		// Simple approach: always toggle between show and hide
		// We'll track state manually
		if appInstance.isWindowVisible {
			hideWindow()
		} else {
			showWindow()
		}
	}
}
