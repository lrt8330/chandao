package ui

import (
	"fmt"
	"strings"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// itemActionDef 是一条禅道生命周期操作：id 对应禅道的操作方法名。
type itemActionDef struct {
	id, label string
}

// itemActionsFor 返回事项在当前状态下可用的操作，与禅道页面上的操作
// 按钮一一对应。
func itemActionsFor(item domain.Item) []itemActionDef {
	status := strings.ToLower(strings.TrimSpace(item.Status))
	switch item.Type {
	case domain.ItemTask:
		switch status {
		case "wait":
			return []itemActionDef{{"start", "开始"}, {"cancel", "取消"}}
		case "doing":
			return []itemActionDef{{"finish", "完成"}, {"pause", "暂停"}, {"cancel", "取消"}}
		case "pause":
			return []itemActionDef{{"start", "继续"}, {"cancel", "取消"}}
		case "done", "cancel", "canceled", "cancelled":
			return []itemActionDef{{"close", "关闭"}}
		}
	case domain.ItemBug:
		switch status {
		case "active":
			return []itemActionDef{{"resolve", "解决"}, {"close", "关闭"}}
		case "resolved":
			return []itemActionDef{{"activate", "激活"}, {"close", "关闭"}}
		case "closed":
			return []itemActionDef{{"activate", "激活"}}
		}
	case domain.ItemStory:
		// 需求的操作（变更/关闭）暂不开放，保持只读。
	}
	return nil
}

// itemActionRequest 是待确认的一次操作。
type itemActionRequest struct {
	item   domain.Item
	action itemActionDef
}

// itemActionController 承载操作按钮与确认弹窗的共享状态：按钮把请求
// 放进 pending，弹窗确认后经 OnRunItemAction 执行，结果落在 feedback。
type itemActionController struct {
	pending  *core.Signal[itemActionRequest]
	feedback *core.Signal[string]
}

func newItemActionController(cx *core.Scope) *itemActionController {
	return &itemActionController{
		pending:  core.NewSignal(cx, itemActionRequest{}),
		feedback: core.NewSignal(cx, ""),
	}
}

// bar 是详情里的操作按钮行；没有可用操作（如需求）时为空视图。
func (c *itemActionController) bar(cx *core.Scope, item func() domain.Item) core.Node {
	defs := itemActionsFor(item())
	if len(defs) == 0 {
		return core.View{}
	}
	colors := ui.ColorsOf(cx)
	children := make([]core.Node, 0, len(defs)+1)
	for _, def := range defs {
		def := def
		children = append(children, ui.Button{
			Text: def.label, TestID: "item-action-" + def.id, Variant: ui.ButtonOutline,
			OnPress: func() {
				c.feedback.Set("")
				c.pending.Set(itemActionRequest{item: item(), action: def})
			},
		})
	}
	children = append(children, core.Dynamic(func(cx *core.Scope) core.Node {
		if message := c.feedback.Get(); message != "" {
			return core.Text{Content: message, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Danger)}
		}
		return core.View{}
	}))
	return core.View{
		TestID: "item-actions",
		Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Wrap: core.Wrap},
		Children: append([]core.Node{
			core.Text{Content: "禅道操作", Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
		}, children...),
	}
}

// confirmDialog 是操作确认弹窗：写禅道的动作都先经过这里。
func (c *itemActionController) confirmDialog(cx *core.Scope, props WorkbenchProps) core.Node {
	request := func() itemActionRequest { return c.pending.Get() }
	confirm := func() {
		current := request()
		c.pending.Set(itemActionRequest{})
		if current.action.id == "" || props.OnRunItemAction == nil {
			return
		}
		c.feedback.Set("正在执行…")
		props.OnRunItemAction(current.item, current.action.id, func(errorMessage string) {
			c.feedback.Set(errorMessage)
		})
	}
	return ui.AlertDialog{
		TestID: "item-action-confirm",
		Open:   func() bool { return request().action.id != "" },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				c.pending.Set(itemActionRequest{})
			}
		},
		TitleFunc: func() string {
			if current := request(); current.action.id != "" {
				return fmt.Sprintf("确认%s？", current.action.label)
			}
			return ""
		},
		DescriptionFunc: func() string {
			if current := request(); current.action.id != "" {
				return fmt.Sprintf("将直接在禅道执行「%s %s：%s」，页面预填的工时与解决方案等保持默认。", itemTypeLabel(current.item.Type), current.item.ID, current.item.Title)
			}
			return ""
		},
		Actions: []ui.AlertAction{
			{Text: "取消", Role: core.AlertCancel, TestID: "cancel-item-action"},
			{Text: "确认", Variant: ui.ButtonPrimary, Default: true, TestID: "confirm-item-action", OnPress: confirm},
		},
		Style: dialogChrome(cx),
	}
}
