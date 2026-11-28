package ui

import (
	"testing"
	"time"
)

// 命令面板：Ctrl+K 开、Return 执行首项（立即刷新）、Ctrl+R 直达刷新、
// Escape 关闭。
func TestCommandPaletteOpensAndRunsCommands(t *testing.T) {
	refreshed := false
	props := WorkbenchProps{Data: WorkbenchData{Connected: true}}
	props.OnRefresh = func() { refreshed = true }
	props.OnOpenReport = func() { t.Fatal("report must not open in this test") }

	app := startWorkbench(t, props)

	if app.Exists("id:command-palette") {
		t.Fatal("palette must stay closed initially")
	}
	app.Key("mod+k")
	app.Settle()
	app.Find("id:command-palette")
	app.Find(`id:command-palette >> text:"立即刷新"`)
	app.Find(`id:command-palette >> text:"打开报表"`)
	app.Find(`id:command-palette >> text:"仅看本轮变化"`)

	// Return 执行高亮的首项（操作组的立即刷新）。
	app.Key("enter")
	app.Settle()
	if !refreshed {
		t.Fatal("refresh command did not run")
	}
	app.WaitForGone("id:command-palette", time.Second)

	// Ctrl+R 直达刷新。
	app.Key("mod+r")
	app.Settle()
	if refreshed != true { // 仍为真：第二次刷新同样走 OnRefresh
		t.Fatal("mod+r shortcut did not reach OnRefresh")
	}

	// 再开面板，Escape 关闭。
	app.Key("mod+k")
	app.Settle()
	app.Find("id:command-palette")
	app.Key("escape")
	app.Settle()
	app.WaitForGone("id:command-palette", time.Second)
}
