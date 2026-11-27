package ui

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	minPollSeconds  = 30
	maxPollSeconds  = 300
	maxPopupSeconds = 60
	pollErrorText   = "轮询间隔必须是 30 到 300 之间的整数"
	popupErrorText  = "消息弹窗保留时间必须是 0 到 60 之间的整数"
)

func settingsDialog(cx *core.Scope, props WorkbenchProps, open *core.Signal[bool]) core.Node {
	colors := ui.ColorsOf(cx)
	pollText := core.NewSignal(cx, strconv.Itoa(props.data().PollSeconds))
	notifications := core.NewSignal(cx, props.data().NotificationsEnabled)
	popupText := core.NewSignal(cx, strconv.Itoa(props.data().PopupSeconds))
	validationError := core.NewSignal(cx, "")
	popupError := core.NewSignal(cx, "")
	close := func() { open.Set(false) }
	save := func() {
		seconds, err := strconv.Atoi(strings.TrimSpace(pollText.Get()))
		if err != nil || seconds < minPollSeconds || seconds > maxPollSeconds {
			validationError.Set(pollErrorText)
			return
		}
		popupSeconds, err := strconv.Atoi(strings.TrimSpace(popupText.Get()))
		if err != nil || popupSeconds < 0 || popupSeconds > maxPopupSeconds {
			popupError.Set(popupErrorText)
			return
		}
		if props.OnSettings != nil {
			props.OnSettings(seconds, notifications.Get(), popupSeconds)
		}
		close()
	}

	wasOpen := false
	cx.Effect(func() {
		shown := open.Get()
		if !shown {
			wasOpen = false
			return
		}
		if wasOpen {
			return
		}
		wasOpen = true
		data := props.data()
		pollText.Set(strconv.Itoa(data.PollSeconds))
		notifications.Set(data.NotificationsEnabled)
		popupText.Set(strconv.Itoa(data.PopupSeconds))
		validationError.Set("")
		popupError.Set("")
	})

	return ui.Dialog{
		TestID: "settings-dialog", Title: "设置", Description: "调整自动刷新、消息弹窗和桌面通知。",
		Open: open.Get,
		OnOpenChange: func(shown bool, _ base.Reason) {
			if !shown {
				close()
			}
		},
		Style: dialogChrome(cx),
		Children: []core.Node{
			settingsGroup(cx, []core.Node{
				core.Text{Content: "轮询间隔（秒）", Style: ui.TypeLabel, StyleFunc: ui.Colored(colors.Text)},
				ui.Input{
					TestID: "poll-seconds", Label: "轮询间隔（秒）", Placeholder: "30–300",
					Value: pollText.Get,
					OnChange: func(text string) {
						pollText.Set(text)
						if validationError.Get() != "" {
							validationError.Set("")
						}
					},
					Invalid: func() bool { return validationError.Get() != "" },
					Style:   core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				},
				core.Text{Content: "每 30–300 秒自动刷新一次", Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
				core.Show{
					When: func() bool { return validationError.Get() != "" },
					Then: func(cx *core.Scope) core.Node {
						return settingsError(cx, validationError.Get, "settings-error")
					},
				},
			}),
			settingsGroup(cx, []core.Node{
				core.Text{Content: "消息弹窗保留时间（秒）", Style: ui.TypeLabel, StyleFunc: ui.Colored(colors.Text)},
				ui.Input{
					TestID: "popup-seconds", Label: "消息弹窗保留时间（秒）", Placeholder: "0–60",
					Value: popupText.Get,
					OnChange: func(text string) {
						popupText.Set(text)
						if popupError.Get() != "" {
							popupError.Set("")
						}
					},
					Invalid: func() bool { return popupError.Get() != "" },
					Style:   core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				},
				core.Text{Content: "0 表示一直保留；鼠标悬停可暂停自动消失", Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
				core.Show{
					When: func() bool { return popupError.Get() != "" },
					Then: func(cx *core.Scope) core.Node {
						return settingsError(cx, popupError.Get, "popup-settings-error")
					},
				},
			}),
			settingsGroup(cx, []core.Node{
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
					Children: []core.Node{
						ui.Switch{
							TestID: "notifications-enabled", Label: "消息弹窗", Checked: notifications.Get, OnChange: notifications.Set,
						},
						core.View{Style: core.Style{Gap: 4, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
							core.Text{Content: "消息弹窗", Style: ui.TypeLabel, StyleFunc: ui.Colored(colors.Text)},
							core.Text{Content: "有新增或状态变化时在屏幕右下角提醒我", Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
						}},
					},
				},
			}),
		},
		Footer: []core.Node{
			ui.Button{Text: "取消", TestID: "cancel-settings", Variant: ui.ButtonOutline, OnPress: close},
			ui.Button{
				Text: "保存", TestID: "save-settings",
				Color:     ui.Fixed(core.RGB(cobalt)),
				TextStyle: core.Style{Color: core.RGB(0xFFFFFF)},
				OnPress:   save,
			},
		},
	}
}

// settingsGroup 是对话框里的浅底列表组：字段、辅文和错误按 iOS 设置的
// 分组排布，组间用间距分隔，不再给每组画卡片边框。
func settingsGroup(cx *core.Scope, children []core.Node) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		Style: core.Style{Gap: 8, Padding: core.All(12)},
		StyleFunc: func() core.Style {
			return core.Style{Background: colors.Inset(), CornerRadius: 12, BorderCurve: core.BorderCurveContinuous}
		},
		Children: children,
	}
}

func settingsError(cx *core.Scope, message func() string, testID string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: testID, Role: core.RoleAlert,
		Style: core.Style{Padding: core.Symmetric(8, 10), Gap: 8, CornerRadius: ui.RadiusControl},
		StyleFunc: func() core.Style {
			return core.Style{Background: ui.Alpha(colors.Danger(), 0.08), BorderColor: ui.Alpha(colors.Danger(), 0.25)}
		},
		Children: []core.Node{core.Text{ContentFunc: message, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Danger)}},
	}
}
