package ui

import (
	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

func itemDetailPane(cx *core.Scope, item func() domain.Item, onOpenURL func(string), actions *itemActionController, close func()) core.Node {
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
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween}, Children: []core.Node{
				detailIdentity(cx, item), ui.Button{Text: "收起详情", TestID: "collapse-detail", Variant: ui.ButtonGhost, OnPress: close},
			}},
			ui.ScrollView{
				Style:        ui.FillRest,
				ContentStyle: core.Style{Gap: 14, Padding: core.Edges{Right: 4}},
				Children:     []core.Node{detailTitle(item), itemDetailBody(cx, item)},
			},
			core.Dynamic(func(cx *core.Scope) core.Node { return actions.bar(cx, item) }),
			detailLinks(cx, item, onOpenURL),
		},
	}
}

func itemDetailDialog(cx *core.Scope, item func() domain.Item, onOpenURL func(string), close func(), actions *itemActionController) core.Node {
	return ui.Dialog{
		TestID: "item-detail",
		Open:   func() bool { return item().ID != "" },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				close()
			}
		},
		Label:    "事项详情",
		Style:    dialogChrome(cx),
		Children: []core.Node{detailIdentity(cx, item), detailTitle(item), itemDetailBody(cx, item)},
		Footer: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node { return actions.bar(cx, item) }),
			ui.Button{Text: "关闭", TestID: "close-item-detail", Variant: ui.ButtonOutline, OnPress: close},
			detailLinks(cx, item, onOpenURL),
		},
	}
}

func detailTitle(item func() domain.Item) core.Node {
	return core.Text{ContentFunc: func() string { return item().Title }, TestID: "detail-title", Role: core.RoleHeader, Style: ui.TypeHeading, Selectable: core.SelectableOn}
}

func detailIdentity(cx *core.Scope, item func() domain.Item) core.Node {
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node { return typePill(cx, item().Type) }),
		core.Text{TestID: "detail-id", ContentFunc: func() string { return "#" + item().ID }, Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono}), StyleFunc: ui.Colored(ui.ColorsOf(cx).Muted)},
	}}
}

// itemDetailBody 按列表组排布字段，字段之间用 hairline 分隔。
func itemDetailBody(cx *core.Scope, item func() domain.Item) core.Node {
	colors := ui.ColorsOf(cx)
	fields := []core.Node{
		detailField(cx, "项目", func() string { return item().Project }, "detail-project"),
		detailField(cx, "状态", func() string { return item().Status }, "detail-status"),
		detailField(cx, "更新时间", func() string { return formatUpdatedAt(item().UpdatedAt) }, "detail-updated"),
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

func detailLinks(cx *core.Scope, item func() domain.Item, onOpenURL func(string)) core.Node {
	return core.View{Style: core.Style{Direction: core.Row, Gap: 6, Wrap: core.Wrap, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
		ui.Button{Text: "复制链接", TestID: "copy-item-link", Variant: ui.ButtonGhost, Icon: lucide.Copy,
			Disabled: func() bool { return item().URL == "" }, OnPress: func() {
				if link := item().URL; link != "" {
					cx.SetClipboard(link)
				}
			},
		},
		openZentaoButton(item, onOpenURL),
	}}
}

func openZentaoButton(item func() domain.Item, onOpenURL func(string)) core.Node {
	return ui.Button{
		Text: "在禅道打开", TestID: "open-zentao", Variant: ui.ButtonOutline,
		Icon:     lucide.ExternalLink,
		Disabled: func() bool { return item().URL == "" || onOpenURL == nil },
		OnPress: func() {
			if onOpenURL != nil && item().URL != "" {
				onOpenURL(item().URL)
			}
		},
	}
}
