package ui

import (
	"fmt"
	"sync/atomic"
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
	// summaryPopupThreshold 单批变化超过该数量时只弹一个摘要卡，
	// 避免大批次连续开合上千个面板。
	summaryPopupThreshold = 20
	summaryPopupHeight    = 128
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
	cx              *core.Scope
	onPress         func(string)
	onOpenWorkbench func()
	active          []*messagePopup
	disposed        atomic.Bool
}

func NewMessagePopupManager(cx *core.Scope, onPress func(string), onOpenWorkbench func()) *MessagePopupManager {
	return &MessagePopupManager{cx: cx, onPress: onPress, onOpenWorkbench: onOpenWorkbench}
}

func (m *MessagePopupManager) Show(changes []domain.Change, seconds int) {
	if m == nil || m.cx == nil || len(changes) == 0 || m.disposed.Load() {
		return
	}
	if len(changes) > summaryPopupThreshold {
		m.cx.Post(func() {
			if m.disposed.Load() {
				return
			}
			m.showSummary(changes, seconds)
		})
		return
	}
	m.cx.Post(func() {
		if m.disposed.Load() {
			return
		}
		duration := time.Duration(seconds) * time.Second
		for _, change := range changes {
			m.show(change, duration)
		}
	})
}

// Dispose 丢弃已排队但未展示的通知并关闭现有弹窗（切换账号时调用）。
func (m *MessagePopupManager) Dispose() {
	if m == nil {
		return
	}
	m.disposed.Store(true)
	active := m.active
	m.active = nil
	for _, popup := range active {
		popup.stopTimer()
		if popup.window != nil {
			popup.window.Close()
		}
	}
}

// summarizeCounts 统计一批变化的新增与状态变化数量。
func summarizeCounts(changes []domain.Change) (newCount, statusCount int) {
	for _, change := range changes {
		switch change.Kind {
		case domain.ChangeNew:
			newCount++
		case domain.ChangeStatus:
			statusCount++
		}
	}
	return newCount, statusCount
}

// showSummary 为大批次弹一张汇总卡：计数 + 打开工作台的入口，
// 点击卡片即打开工作台（工作台的「仅看本轮变化」可查看全部明细）。
func (m *MessagePopupManager) showSummary(changes []domain.Change, seconds int) {
	if m.disposed.Load() {
		return
	}
	if len(m.active) >= maxMessagePopups {
		m.close(m.active[0])
	}
	newCount, statusCount := summarizeCounts(changes)
	title := fmt.Sprintf("新增 %d · 状态变化 %d", newCount, statusCount)
	description := fmt.Sprintf("共 %d 条变化，点击打开工作台查看全部明细。", len(changes))

	duration := time.Duration(seconds) * time.Second
	panel := &messagePopup{id: "summary", remaining: duration}
	panel.window = desktop.OpenPanel(m.cx, desktop.PanelOptions{
		Width: core.Points(messagePopupWidth), Height: core.Points(summaryPopupHeight),
		Transparent: true, NoActivate: true,
	}, func(panelCx *core.Scope) core.Node {
		if duration > 0 {
			panel.started = time.Now()
			panel.timer = time.AfterFunc(duration, func() {
				m.cx.Post(func() { m.close(panel) })
			})
			panelCx.Cleanup(func() { panel.stopTimer() })
		}
		colors := ui.ColorsOf(panelCx)
		glass := ui.FinOf(panelCx).Glass()
		return core.View{
			TestID: "popup-summary", Role: core.RoleButton,
			Label: title + ". " + description,
			Style: core.Style{
				FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0),
				Direction: core.Column, AlignItems: core.AlignStart, Gap: 8, Padding: core.All(14),
				Background: colors.Surface(), BorderWidth: 1, BorderColor: colors.Border(),
				CornerRadius: ui.RadiusCard, BorderCurve: core.BorderCurveContinuous,
				Shadows: glass.Shadows,
			},
			OnPress: func(core.PressEvent) {
				m.close(panel)
				if m.onOpenWorkbench != nil {
					m.onOpenWorkbench()
				}
			},
			OnMouseEnter: func(core.MouseEvent) { m.hover(panel, true) },
			OnMouseLeave: func(core.MouseEvent) { m.hover(panel, false) },
			Children: []core.Node{
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
					popupChip(panelCx, domain.ItemTask),
					core.Text{Content: title, Style: ui.TypeTitle, StyleFunc: ui.Colored(colors.Text)},
				}},
				core.Text{Content: description, MaxLines: 2, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
			},
		}
	})
	if panel.window == nil {
		return
	}
	m.active = append(m.active, panel)
	m.layout()
}

func (m *MessagePopupManager) show(change domain.Change, duration time.Duration) {
	if m.disposed.Load() {
		return
	}
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
	if m.disposed.Load() {
		popup.closing = true
		popup.stopTimer()
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

func popupChip(cx *core.Scope, itemType domain.ItemType) core.Node {
	tone := itemTone(itemType)
	icon := lucide.ListTodo
	switch itemType {
	case domain.ItemStory:
		icon = lucide.Bookmark
	case domain.ItemBug:
		icon = lucide.Bug
	}
	return core.View{
		Style: core.Style{Width: core.Pt(28), Height: core.Pt(28), CornerRadius: 7, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, FlexShrink: 0, BorderWidth: 1},
		StyleFunc: func() core.Style {
			return core.Style{Background: core.RGB(tone.fill), BorderColor: core.RGB(tone.line)}
		},
		Children: []core.Node{core.Icon{
			SVG: icon(), Label: "",
			Style: core.Style{FontSize: 16}, StyleFunc: func() core.Style { return core.Style{Color: core.RGB(tone.ink)} },
		}},
	}
}
