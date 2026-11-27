package ui

import (
	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

func itemDetailPane(cx *core.Scope, item func() domain.Item, onOpenURL func(string)) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "item-detail",
		Style: core.Style{
			Width: core.Pt(380), FlexShrink: 0, MinHeight: core.Pt(0),
			Padding: core.All(20), Gap: 16,
		},
		StyleFunc: func() core.Style {
			style := paperSurface(cx)
			style.Color = colors.Text()
			return style
		},
		Children: []core.Node{
			core.View{Style: core.Style{Gap: 8}, Children: []core.Node{
				typePill(cx, item().Type),
				core.Text{ContentFunc: func() string { return itemDetailTitle(item()) }, Role: core.RoleHeader, Style: ui.TypeHeading},
				core.Text{ContentFunc: func() string { return item().Key() }, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
			}},
			ui.ScrollView{
				Style:        ui.FillRest,
				ContentStyle: core.Style{Gap: 14, Padding: core.Edges{Right: 4}},
				Children:     []core.Node{itemDetailBody(cx, item)},
			},
			openZentaoButton(item, onOpenURL),
		},
	}
}

func itemDetailDialog(cx *core.Scope, item func() domain.Item, onOpenURL func(string), selection *ui.Selection, selectedKey *core.Signal[string]) core.Node {
	close := func() {
		selection.Clear()
		selectedKey.Set("")
	}
	return ui.Dialog{
		TestID: "item-detail",
		Open:   func() bool { return selectedKey.Get() != "" },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				close()
			}
		},
		TitleFunc: func() string { return itemDetailTitle(item()) },
		Style:     dialogChrome(cx),
		Children:  []core.Node{itemDetailBody(cx, item)},
		Footer: []core.Node{
			ui.Button{Text: "关闭", TestID: "close-item-detail", Variant: ui.ButtonOutline, OnPress: close},
			openZentaoButton(item, onOpenURL),
		},
	}
}

// itemDetailBody 按列表组排布字段，字段之间用 hairline 分隔。
func itemDetailBody(cx *core.Scope, item func() domain.Item) core.Node {
	colors := ui.ColorsOf(cx)
	fields := []core.Node{
		detailField(cx, "类型", func() string { return itemTypeLabel(item().Type) }, "detail-type"),
		detailField(cx, "编号", func() string { return item().ID }, "detail-id"),
		detailField(cx, "标题", func() string { return item().Title }, "detail-title"),
		detailField(cx, "项目", func() string { return item().Project }, "detail-project"),
		detailField(cx, "状态", func() string { return item().Status }, "detail-status"),
		detailField(cx, "更新时间", func() string { return formatUpdatedAt(item().UpdatedAt) }, "detail-updated"),
		detailField(cx, "链接", func() string { return item().URL }, "detail-url"),
		detailField(cx, "标识", func() string { return item().Key() }, "detail-key"),
	}
	children := make([]core.Node, 0, len(fields)*2-1)
	for index, field := range fields {
		if index > 0 {
			children = append(children, core.View{Style: core.Style{Height: core.Pt(1)}, StyleFunc: func() core.Style {
				return core.Style{Background: colors.Border()}
			}})
		}
		children = append(children, field)
	}
	return core.View{Style: core.Style{Gap: 10}, Children: children}
}

func detailField(cx *core.Scope, label string, value func() string, testID string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignStart, Gap: 12}, Children: []core.Node{
		core.Text{Content: label, Style: ui.TypeCaption.Merge(core.Style{Width: core.Pt(56), FlexShrink: 0}), StyleFunc: ui.Colored(colors.Muted)},
		core.Text{
			ContentFunc: value, TestID: testID, Selectable: core.SelectableOn,
			Style:     ui.TypeBody.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}),
			StyleFunc: ui.Colored(colors.Text),
		},
	}}
}

func openZentaoButton(item func() domain.Item, onOpenURL func(string)) core.Node {
	return ui.Button{
		Text: "在禅道打开", TestID: "open-zentao", Size: ui.SizeLarge,
		Icon:      lucide.ExternalLink,
		Color:     ui.Fixed(core.RGB(cobalt)),
		TextStyle: core.Style{Color: core.RGB(0xFFFFFF)},
		Tint:      ui.Fixed(core.RGB(0xFFFFFF)),
		Disabled:  func() bool { return item().URL == "" || onOpenURL == nil },
		OnPress: func() {
			if onOpenURL != nil && item().URL != "" {
				onOpenURL(item().URL)
			}
		},
	}
}
