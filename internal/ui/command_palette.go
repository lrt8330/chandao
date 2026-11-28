package ui

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// commandDialog 是 Ctrl+K 命令面板：把工作台的全部动作收进一个可搜索
// 的快速入口。条目分组展示，选中即执行并关闭。
func commandDialog(cx *core.Scope, props WorkbenchProps, settingsOpen, notifyHistoryOpen, changesOnly, open *core.Signal[bool]) core.Node {
	items := func() []base.CommandItem {
		refresh := base.CommandItem{
			ID: "refresh", Label: "立即刷新", Group: "操作",
			Description: "重新拉取禅道指派数据", Shortcut: "mod+r",
			Icon: lucide.RefreshCw(),
		}
		if props.data().Loading || props.data().NeedsLogin {
			refresh.Disabled = true
		}
		list := []base.CommandItem{
			refresh,
			{ID: "report", Label: "打开报表", Group: "导航",
				Description: "趋势与事件明细（独立窗体）", Icon: lucide.ChartLine()},
			{ID: "history", Label: "通知历史", Group: "导航",
				Description: "最近已通知的变化记录", Icon: lucide.Bell()},
			{ID: "settings", Label: "设置", Group: "导航",
				Description: "轮询与通知偏好", Icon: lucide.Settings()},
			{ID: "changes-only", Label: "仅看本轮变化", Group: "筛选",
				Description: "列表只显示本轮检出的变化", Icon: lucide.Sparkles(),
				Checked: changesOnly.Get()},
		}
		if props.NotifyHistory == nil {
			list = append(list[:2], list[3:]...) // 无历史库时隐藏通知历史
		}
		return list
	}
	onSelect := func(id string) {
		open.Set(false)
		switch id {
		case "refresh":
			if props.OnRefresh != nil {
				props.OnRefresh()
			}
		case "report":
			if props.OnOpenReport != nil {
				props.OnOpenReport()
			}
		case "history":
			notifyHistoryOpen.Set(true)
		case "settings":
			settingsOpen.Set(true)
		case "changes-only":
			changesOnly.Set(!changesOnly.Get())
		}
	}
	return ui.CommandDialog{
		TestID: "command-palette",
		Open:   open.Get,
		OnOpenChange: func(shown bool, _ base.Reason) {
			open.Set(shown)
		},
		Command: ui.Command{
			Items:       items,
			OnSelect:    onSelect,
			Placeholder: "搜索命令…",
			Empty:       "没有匹配的命令",
			Label:       "命令面板",
			Style:       dialogChrome(cx),
		},
	}
}
