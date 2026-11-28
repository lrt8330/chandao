package ui

import (
	"context"
	"fmt"
	"time"

	"chandao/zentaotray/internal/infrastructure/notifyhistory"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

const notificationHistoryLimit = 100

// notificationHistoryDialog 通知历史对话框：列出最近已通知的变化，支持
// 一键全部标为已读。数据来自本地 SQLite 通知历史。
func notificationHistoryDialog(cx *core.Scope, store *notifyhistory.Store, open *core.Signal[bool]) core.Node {
	entries := core.NewSignalFunc(cx, []notifyhistory.Entry(nil), func(a, b []notifyhistory.Entry) bool {
		return len(a) == len(b)
	})
	unread := core.NewSignal(cx, 0)
	load := func() {
		if store == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if list, err := store.Recent(ctx, notificationHistoryLimit); err == nil {
			entries.Set(list)
		}
		if count, err := store.UnreadCount(ctx); err == nil {
			unread.Set(count)
		}
	}
	loadOnOpen := func() {
		if open.Get() {
			load()
		}
	}
	markRead := func() {
		if store == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.MarkAllRead(ctx); err == nil {
			unread.Set(0)
			load()
		}
	}
	close := func() { open.Set(false) }

	// 组件随 Workbench 每次渲染重建：无条件加载一次，保证打开对话框时
	// 记录与未读计数是最新的。
	load()
	description := "最近已通知的变化记录（本地保留，最多 100 条）。"
	return ui.Dialog{
		TestID: "notify-history", Title: "通知历史",
		DescriptionFunc: func() string {
			if count := unread.Get(); count > 0 {
				return fmt.Sprintf("未读 %d 条。%s", count, description)
			}
			return description
		},
		Open: open.Get,
		OnOpenChange: func(shown bool, _ base.Reason) {
			if shown {
				loadOnOpen()
				return
			}
			close()
		},
		Style: dialogChrome(cx),
		Children: []core.Node{
			ui.ScrollView{
				Style:        ui.FillRest,
				ContentStyle: core.Style{Gap: 8, Padding: core.Edges{Right: 4}},
				Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
					return historyList(cx, entries.Get)
				})},
			},
		},
		Footer: []core.Node{
			ui.Button{
				Text: "全部标为已读", TestID: "mark-notifications-read", Variant: ui.ButtonOutline,
				Disabled: func() bool { return unread.Get() == 0 },
				OnPress:  markRead,
			},
			ui.Button{Text: "关闭", TestID: "close-notify-history", Variant: ui.ButtonSecondary, OnPress: close},
		},
	}
}

func historyList(cx *core.Scope, entries func() []notifyhistory.Entry) core.Node {
	colors := ui.ColorsOf(cx)
	list := entries()
	if len(list) == 0 {
		return core.View{TestID: "notify-history-empty", Style: core.Style{Padding: core.All(24), Gap: 10, AlignItems: core.AlignCenter}, Children: []core.Node{
			core.Icon{SVG: lucide.Bell(), Label: "", Style: core.Style{FontSize: 24}, StyleFunc: ui.Colored(colors.Faint)},
			core.Text{Content: "还没有通知记录", Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
		}}
	}
	rows := make([]core.Node, 0, len(list))
	for _, entry := range list {
		entry := entry
		rows = append(rows, historyRow(cx, entry))
	}
	return core.View{Style: core.Style{Gap: 8}, Children: rows}
}

func historyRow(cx *core.Scope, entry notifyhistory.Entry) core.Node {
	colors := ui.ColorsOf(cx)
	ink := ui.Colored(colors.Text)
	if entry.Read {
		ink = ui.Colored(colors.Muted)
	}
	transition := ""
	if entry.Kind == "status" && entry.OldStatus != "" {
		transition = fmt.Sprintf("%s → %s", entry.OldStatus, entry.NewStatus)
	}
	return core.View{
		TestID: "notify-entry-" + fmt.Sprint(entry.ID),
		Style:  core.Style{Gap: 4, Padding: core.Symmetric(8, 10), CornerRadius: ui.RadiusControl, MinWidth: core.Pt(0)},
		StyleFunc: func() core.Style {
			return core.Style{Background: colors.Inset(), BorderWidth: 1, BorderColor: colors.Border()}
		},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, MinWidth: core.Pt(0)}, Children: []core.Node{
				core.View{Style: core.Style{Padding: core.Symmetric(2, 8), CornerRadius: 6, BorderWidth: 1, FlexShrink: 0}, StyleFunc: func() core.Style {
					return core.Style{Background: colors.Inset(), BorderColor: colors.FieldBorder()}
				}, Children: []core.Node{core.Text{
					Content: notifyhistory.KindLabel(entry.Kind), Style: ui.TypeCaption,
					StyleFunc: func() core.Style { return core.Style{Color: colors.Accent()} },
				}}},
				core.Text{ContentFunc: func() string { return entry.Title }, MaxLines: 1, Style: ui.TypeLabel.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}), StyleFunc: ink},
				core.Text{ContentFunc: func() string { return entry.Time.Local().Format("01-02 15:04") }, Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono}), StyleFunc: ui.Colored(colors.Muted)},
			}},
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, Wrap: core.Wrap, MinWidth: core.Pt(0)}, Children: []core.Node{
				core.Text{ContentFunc: func() string {
					if transition != "" {
						return "状态 " + transition
					}
					return entry.Project
				}, MaxLines: 1, Style: ui.TypeCaption.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}), StyleFunc: ui.Colored(colors.Muted)},
				unreadDot(cx, entry.Read),
			}},
		},
	}
}

// unreadDot 未读行显示一个小钴蓝点，已读行留白保持对齐。
func unreadDot(cx *core.Scope, read bool) core.Node {
	if read {
		return core.View{Style: core.Style{Width: core.Pt(8), FlexShrink: 0}}
	}
	return core.View{Style: core.Style{Width: core.Pt(8), Height: core.Pt(8), CornerRadius: ui.RadiusFull, FlexShrink: 0}, StyleFunc: func() core.Style {
		return core.Style{Background: ui.ColorsOf(cx).Accent()}
	}}
}
