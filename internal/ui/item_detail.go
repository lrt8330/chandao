package ui

import (
	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
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
			return ui.FinOf(cx).Card().Over(core.Style{
				Background: colors.Surface(), Color: colors.Text(), BorderWidth: 1,
				BorderColor: colors.Border(), CornerRadius: ui.RadiusCard,
			})
		},
		Children: []core.Node{
			core.View{Style: core.Style{Gap: 8}, Children: []core.Node{
				ui.Badge{TextFunc: func() string { return itemTypeLabel(item().Type) }},
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
		Children:  []core.Node{itemDetailBody(cx, item)},
		Footer: []core.Node{
			ui.Button{Text: "关闭", TestID: "close-item-detail", Variant: ui.ButtonSecondary, OnPress: close},
			openZentaoButton(item, onOpenURL),
		},
	}
}

func itemDetailBody(cx *core.Scope, item func() domain.Item) core.Node {
	return core.View{Style: core.Style{Gap: 12}, Children: []core.Node{
		detailField(cx, "类型", func() string { return itemTypeLabel(item().Type) }, "detail-type"),
		detailField(cx, "编号", func() string { return item().ID }, "detail-id"),
		detailField(cx, "标题", func() string { return item().Title }, "detail-title"),
		detailField(cx, "项目", func() string { return item().Project }, "detail-project"),
		detailField(cx, "状态", func() string { return item().Status }, "detail-status"),
		detailField(cx, "更新时间", func() string { return formatUpdatedAt(item().UpdatedAt) }, "detail-updated"),
		detailField(cx, "链接", func() string { return item().URL }, "detail-url"),
		detailField(cx, "标识", func() string { return item().Key() }, "detail-key"),
	}}
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
		Disabled: func() bool { return item().URL == "" || onOpenURL == nil },
		OnPress: func() {
			if onOpenURL != nil && item().URL != "" {
				onOpenURL(item().URL)
			}
		},
	}
}
