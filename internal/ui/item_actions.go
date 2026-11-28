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

// itemActionFeedback 把提示绑定到具体事项，避免一个请求的结果出现在另一
// 条事项的操作行上。
type itemActionFeedback struct {
	key     string
	message string
}

// itemActionController 承载操作按钮与确认弹窗的共享状态：按钮把请求
// 放进 pending，弹窗确认后经 OnRunItemAction 执行，结果落在 feedback。
type itemActionController struct {
	props    WorkbenchProps
	selected func() domain.Item
	pending  *core.Signal[itemActionRequest]
	feedback *core.Signal[itemActionFeedback]
	// running 记录正在执行的事项；同一事项在完成前不接受重复提交。
	running *core.Signal[string]
}

func newItemActionController(cx *core.Scope, props WorkbenchProps, selected func() domain.Item) *itemActionController {
	c := &itemActionController{
		props:    props,
		selected: selected,
		pending:  core.NewSignal(cx, itemActionRequest{}),
		feedback: core.NewSignal(cx, itemActionFeedback{}),
		running:  core.NewSignal(cx, ""),
	}
	// 待确认的事项被刷新移除、状态变化或用户改选其他事项时，关闭确认框，
	// 避免对着过期内容点确认。
	cx.Effect(func() {
		current := c.pending.Get()
		if current.action.id == "" {
			return
		}
		if c.staleMessage(current) != "" || c.selected().Key() != current.item.Key() {
			c.pending.Set(itemActionRequest{})
		}
	})
	return c
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
			Disabled: func() bool { return c.running.Get() == item().Key() },
			OnPress: func() {
				if c.running.Get() != "" {
					return
				}
				c.feedback.Set(itemActionFeedback{})
				c.pending.Set(itemActionRequest{item: item(), action: def})
			},
		})
	}
	children = append(children, core.Dynamic(func(cx *core.Scope) core.Node {
		feedback := c.feedback.Get()
		if feedback.key != "" && feedback.key == item().Key() && feedback.message != "" {
			return core.Text{Content: feedback.message, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Danger)}
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

// confirmDialog 是操作确认弹窗：写禅道的动作都先经过这里。确认时用最新
// 数据重新核对事项状态；状态已变化则不发请求，避免提交过期的写操作。
func (c *itemActionController) confirmDialog(cx *core.Scope) core.Node {
	request := func() itemActionRequest { return c.pending.Get() }
	confirm := func() {
		current := request()
		c.pending.Set(itemActionRequest{})
		if current.action.id == "" || c.props.OnRunItemAction == nil {
			return
		}
		key := current.item.Key()
		if c.running.Get() != "" {
			c.feedback.Set(itemActionFeedback{key: key, message: "上一个操作仍在执行，请稍候。"})
			return
		}
		if message := c.staleMessage(current); message != "" {
			c.feedback.Set(itemActionFeedback{key: key, message: message})
			return
		}
		c.running.Set(key)
		c.feedback.Set(itemActionFeedback{key: key, message: "正在执行…"})
		c.props.OnRunItemAction(current.item, current.action.id, func(errorMessage string) {
			c.running.Set("")
			if errorMessage == "" {
				c.feedback.Set(itemActionFeedback{key: key, message: "已提交，正在刷新…"})
				return
			}
			c.feedback.Set(itemActionFeedback{key: key, message: errorMessage})
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

// staleMessage 返回阻止提交的原因；空串表示当前状态仍匹配。
func (c *itemActionController) staleMessage(request itemActionRequest) string {
	for _, item := range c.props.data().Items {
		if item.Key() != request.item.Key() {
			continue
		}
		if item.Status != request.item.Status {
			return "事项状态已变化，请刷新后重新选择操作。"
		}
		return ""
	}
	return "事项已不在当前列表中，可能已完成或被移除，请刷新后重试。"
}
