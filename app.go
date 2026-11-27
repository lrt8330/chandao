// Command chandao is the NSGo desktop app for Zentao assigned work.
package main

import (
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"chandao/zentaotray/internal/ui"
)

const mainWindowKey = "workbench"

func main() { nsgo.Run(App) }

// App mounts the workbench in the main desktop window. Runtime owns the
// session and application lifecycle; this scope owns the window and UI signals.
func App(cx *core.Scope) {
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

	window = desktop.OpenWindow(cx, desktop.WindowOptions{
		Key: mainWindowKey, Title: "禅道助手", Width: 1200, Height: 800,
	}, workbench)
	desktop.SetMainWindow(cx, window)
	if window == nil {
		// Platforms without independent windows use their one main surface.
		cx.SetMainView(workbench)
	} else {
		window.Show()
	}
	runtime.start()
}
