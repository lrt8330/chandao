// Command chandao is the NSGo desktop app for Zentao assigned work.
package main

import (
	"context"
	"path/filepath"
	"time"

	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"chandao/zentaotray/internal/infrastructure/notifyhistory"
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
		PopupSeconds:         8,
	}, nil)
	selectedKey := core.NewSignal(cx, "")

	var window *desktop.Window
	showWindow := func() {
		if window != nil {
			window.Show()
			window.Focus()
		}
	}
	// 通知历史（SQLite）：打开失败仅降级为无历史，弹窗通知不受影响。
	history, historyErr := notifyhistory.Open(filepath.Join(cx.DataDir(), "notify.db"))
	if historyErr != nil {
		history = nil
	} else {
		cx.Cleanup(func() { history.Close() })
		// 启动时把历史裁剪到最近 1 万条：通知历史同时是报表数据源，
		// 裁剪防止长期积累无限增长，同时保留足够的回看窗口。
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = history.Trim(ctx, 10000)
		}()
	}
	runtime := newWorkbenchRuntime(cx, data, selectedKey, showWindow, history)
	workbench := func(windowCx *core.Scope) core.Node {
		return ui.Workbench(windowCx, ui.WorkbenchProps{
			DataFunc:        data.Get,
			SelectedKey:     selectedKey,
			OnLogin:         runtime.login,
			OnRefresh:       runtime.refresh,
			OnSettings:      runtime.updateSettings,
			OnOpenURL:       runtime.openURL,
			OnOpenReport:    runtime.openReport,
			OnRunItemAction: runtime.runItemAction,
			NotifyHistory:   history,
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

func buildTrayMenu(data ui.WorkbenchData, showWindow, refresh func(), updateSettings func(int, bool, int), quit func()) []desktop.MenuItem {
	return []desktop.MenuItem{
		{Label: "打开工作台", OnSelect: showWindow},
		{Label: "立即刷新", Disabled: data.Loading || data.NeedsLogin, OnSelect: refresh},
		{Separator: true},
		{
			Label:     "消息弹窗",
			Checkable: true,
			Checked:   data.NotificationsEnabled,
			OnSelect: func() {
				updateSettings(data.PollSeconds, !data.NotificationsEnabled, data.PopupSeconds)
			},
		},
		{Label: "退出", OnSelect: quit},
	}
}
