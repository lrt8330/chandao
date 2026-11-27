package ui

import (
	"fmt"
	"time"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	messagePopupWidth  = 380
	messagePopupHeight = 104
	messagePopupGap    = 8
	messagePopupMargin = 16
	maxMessagePopups   = 4
)

type messagePopup struct {
	id        string
	window    *desktop.Window
	timer     *time.Timer
	remaining time.Duration
	started   time.Time
	hovered   bool
	closing   bool
}

type MessagePopupManager struct {
	cx      *core.Scope
	onPress func(string)
	active  []*messagePopup
}

func NewMessagePopupManager(cx *core.Scope, onPress func(string)) *MessagePopupManager {
	return &MessagePopupManager{cx: cx, onPress: onPress}
}

func (m *MessagePopupManager) Show(changes []domain.Change, seconds int) {
	if m == nil || m.cx == nil || len(changes) == 0 {
		return
	}
	m.cx.Post(func() {
		duration := time.Duration(seconds) * time.Second
		for _, change := range changes {
			m.show(change, duration)
		}
	})
}

func (m *MessagePopupManager) show(change domain.Change, duration time.Duration) {
	for _, active := range m.active {
		if active.id == change.Item.Key() {
			m.close(active)
			break
		}
	}
	for len(m.active) >= maxMessagePopups {
		m.close(m.active[0])
	}

	popup := &messagePopup{id: change.Item.Key(), remaining: duration}
	popup.window = desktop.OpenPanel(m.cx, desktop.PanelOptions{
		Width: core.Points(messagePopupWidth), Height: core.Points(messagePopupHeight),
		Transparent: true, NoActivate: true,
	}, func(panelCx *core.Scope) core.Node {
		if duration > 0 {
			popup.started = time.Now()
			popup.timer = time.AfterFunc(duration, func() {
				m.cx.Post(func() { m.close(popup) })
			})
			panelCx.Cleanup(func() { popup.stopTimer() })
		}
		return messagePopupView(panelCx, m, popup, change)
	})
	if popup.window == nil {
		return
	}
	m.active = append(m.active, popup)
	m.layout()
}

func (m *MessagePopupManager) close(popup *messagePopup) {
	if popup == nil || popup.closing {
		return
	}
	popup.closing = true
	popup.stopTimer()
	for index, active := range m.active {
		if active == popup {
			m.active = append(m.active[:index], m.active[index+1:]...)
			break
		}
	}
	popup.window.Close()
	m.layout()
}

func (m *MessagePopupManager) press(popup *messagePopup, key string) {
	m.close(popup)
	if m.onPress != nil {
		m.onPress(key)
	}
}

func (m *MessagePopupManager) hover(popup *messagePopup, entered bool) {
	if popup == nil || popup.closing || popup.timer == nil || popup.hovered == entered {
		return
	}
	popup.hovered = entered
	if entered {
		if popup.timer.Stop() {
			popup.remaining -= time.Since(popup.started)
			if popup.remaining < 0 {
				popup.remaining = 0
			}
		}
		return
	}
	if popup.remaining <= 0 {
		m.close(popup)
		return
	}
	popup.started = time.Now()
	popup.timer = time.AfterFunc(popup.remaining, func() {
		m.cx.Post(func() { m.close(popup) })
	})
}

func (p *messagePopup) stopTimer() {
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
}

func (m *MessagePopupManager) layout() {
	display := primaryDisplay(m.cx.Env().Displays())
	area := display.VisibleFrame
	if area.Size.Width == 0 || area.Size.Height == 0 {
		area = core.Bounds{Size: core.Size{Width: 1200, Height: 800}}
	}
	right := area.Origin.X + area.Size.Width
	bottom := area.Origin.Y + area.Size.Height
	for index, popup := range m.active {
		anchor := core.Bounds{Origin: core.Point{
			X: right - core.Points(messagePopupMargin) - core.Points(messagePopupWidth)/2,
			Y: bottom - core.Points(messagePopupMargin) + core.Points(6) - core.Points(index*(messagePopupHeight+messagePopupGap)),
		}}
		_ = popup.window.ShowAt(anchor, core.PlaceTop)
	}
}

func primaryDisplay(displays []core.DisplayInfo) core.DisplayInfo {
	for _, display := range displays {
		if display.Main {
			return display
		}
	}
	if len(displays) > 0 {
		return displays[0]
	}
	return core.DisplayInfo{}
}

func messagePopupView(cx *core.Scope, manager *MessagePopupManager, popup *messagePopup, change domain.Change) core.Node {
	cx.SetTheme(workbenchTheme())
	colors := ui.ColorsOf(cx)
	title := fmt.Sprintf("新增%s · %s", itemTypeLabel(change.Item.Type), change.Item.ID)
	if change.Kind == domain.ChangeStatus {
		title = fmt.Sprintf("%s状态变化 · %s", itemTypeLabel(change.Item.Type), change.Item.ID)
	}
	description := fmt.Sprintf("%s\n%s · %s", change.Item.Title, change.Item.Project, change.Item.Status)
	if change.Kind == domain.ChangeStatus {
		description += fmt.Sprintf("\n状态变化：%s → %s", change.Old, change.New)
	}
	return core.View{
		TestID: "message-popup-" + change.Item.Key(), Role: core.RoleButton,
		Label: title + ". " + description,
		Style: core.Style{
			FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0),
			Direction: core.Row, AlignItems: core.AlignStart, Gap: 12, Padding: core.All(14),
			Background: colors.Surface(), BorderWidth: 1, BorderColor: colors.Border(),
			CornerRadius: 8, BorderCurve: core.BorderCurveContinuous,
			Shadows: []core.BoxShadow{ui.Shadow(0, 8, 24, 0, core.RGBA(cloudInk, 0.16))},
		},
		OnPress:      func(core.PressEvent) { manager.press(popup, change.Item.Key()) },
		OnMouseEnter: func(core.MouseEvent) { manager.hover(popup, true) },
		OnMouseLeave: func(core.MouseEvent) { manager.hover(popup, false) },
		Children: []core.Node{
			popupChip(cx, change.Item.Type),
			core.View{Style: core.Style{Gap: 4, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
				core.Text{Content: title, Style: ui.TypeLabel, StyleFunc: ui.Colored(colors.Text)},
				core.Text{Content: description, MaxLines: 3, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
			}},
			ui.IconButton{Icon: lucide.X, Label: "关闭消息", Size: 24, TestID: "close-" + change.Item.Key(), OnPress: func() { manager.close(popup) }},
		},
	}
}

// popupChip 用语义图标代替文字徽章：任务走琥珀座，Bug 维持红色语义，
// 夜色下换浅琥珀墨色保持对比。
func popupChip(cx *core.Scope, itemType domain.ItemType) core.Node {
	accent, line, ink, svg := core.RGB(cobaltSoft), core.RGB(cobaltChipLine), core.RGB(cobaltDark), lucide.ListTodo()
	if itemType == domain.ItemBug {
		accent, line, ink, svg = core.RGB(bugSoftFill), core.RGB(bugSoftLine), core.RGB(cloudDanger), lucide.Bug()
	}
	return core.View{
		Style:     core.Style{Width: core.Pt(28), Height: core.Pt(28), CornerRadius: 7, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, FlexShrink: 0, BorderWidth: 1},
		StyleFunc: func() core.Style { return core.Style{Background: accent, BorderColor: line} },
		Children: []core.Node{core.Icon{
			SVG: svg, Label: "",
			Style: core.Style{FontSize: 16}, StyleFunc: func() core.Style { return core.Style{Color: ink} },
		}},
	}
}
