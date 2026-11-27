// Command chandao is the NSGo desktop app for Zentao assigned work.
package main

import (
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"chandao/zentaotray/internal/ui"
)

const (
	mainWindowKey = "workbench"
	trayTooltip   = "禅道助手"
)

func main() { nsgo.Run(App) }

// App mounts the workbench in the main desktop window. Runtime owns the
// session and application lifecycle; this scope owns the window and UI signals.
func App(cx *core.Scope) { mountWorkbench(cx) }

func mountWorkbench(cx *core.Scope) *workbenchRuntime {
	desktop.SetShutdownMode(cx, desktop.ShutdownOnExplicit)
	data := core.NewSignalFunc(cx, ui.WorkbenchData{
		NeedsLogin:           true,
		PollSeconds:          60,
		NotificationsEnabled: true,
	}, nil)
	selectedKey := core.NewSignal(cx, "")

	var window *desktop.Window
	showWindow := func() {
		if window != nil {
			window.Show()
			window.Focus()
		}
	}
	runtime := newWorkbenchRuntime(cx, data, selectedKey, showWindow)
	workbench := func(windowCx *core.Scope) core.Node {
		return ui.Workbench(windowCx, ui.WorkbenchProps{
			DataFunc:    data.Get,
			SelectedKey: selectedKey,
			OnLogin:     runtime.login,
			OnRefresh:   runtime.refresh,
			OnSettings:  runtime.updateSettings,
			OnOpenURL:   runtime.openURL,
		})
	}

	// WindowClosing has no Forced flag in NSGo v0.3.4. QuitRequest does, so
	// remember a system shutdown before any later close event reaches the window.
	allowWindowClose := false
	desktop.OnQuitRequest(cx, func(request *desktop.QuitRequest) {
		if request.Forced() {
			allowWindowClose = true
		}
	})

	window = desktop.OpenWindow(cx, desktop.WindowOptions{
		Key: mainWindowKey, Title: trayTooltip, Width: 1200, Height: 800,
	}, workbench)
	if window == nil {
		// Platforms without independent windows use their one main surface.
		cx.SetMainView(workbench)
	} else {
		desktop.SetMainWindow(cx, window)
		window.OnClosing(cx, func(closing *desktop.WindowClosing) {
			if allowWindowClose {
				return
			}
			closing.Cancel()
			window.Hide()
		})
		window.Show()
	}

	desktop.NewTray(cx, workbenchTrayOptions(showWindow, func() []desktop.MenuItem {
		return buildTrayMenu(data.Get(), showWindow, runtime.refresh, runtime.updateSettings, func() {
			desktop.RequestQuit(cx)
		})
	}))
	runtime.start()
	return runtime
}

func workbenchTrayOptions(showWindow func(), menu func() []desktop.MenuItem) desktop.TrayOptions {
	return desktop.TrayOptions{
		Icon:    trayIcon,
		Title:   trayTooltip,
		Tooltip: trayTooltip,
		OnClick: showWindow,
		Menu:    menu,
	}
}

func buildTrayMenu(data ui.WorkbenchData, showWindow, refresh func(), updateSettings func(int, bool), quit func()) []desktop.MenuItem {
	return []desktop.MenuItem{
		{Label: "打开工作台", OnSelect: showWindow},
		{Label: "立即刷新", Disabled: data.Loading || data.NeedsLogin, OnSelect: refresh},
		{Separator: true},
		{
			Label:     "桌面通知",
			Checkable: true,
			Checked:   data.NotificationsEnabled,
			OnSelect: func() {
				updateSettings(data.PollSeconds, !data.NotificationsEnabled)
			},
		},
		{Label: "退出", OnSelect: quit},
	}
}
