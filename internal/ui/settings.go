package ui

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	minPollSeconds = 30
	maxPollSeconds = 300
	pollErrorText  = "轮询间隔必须是 30 到 300 之间的整数"
)

func settingsButton(open *core.Signal[bool]) core.Node {
	return ui.Button{
		Text: "设置", TestID: "open-settings", Variant: ui.ButtonOutline,
		OnPress: func() { open.Set(true) },
	}
}

func settingsDialog(cx *core.Scope, props WorkbenchProps, open *core.Signal[bool]) core.Node {
	colors := ui.ColorsOf(cx)
	pollText := core.NewSignal(cx, strconv.Itoa(props.Data.PollSeconds))
	notifications := core.NewSignal(cx, props.Data.NotificationsEnabled)
	validationError := core.NewSignal(cx, "")
	close := func() { open.Set(false) }
	save := func() {
		seconds, err := strconv.Atoi(strings.TrimSpace(pollText.Get()))
		if err != nil || seconds < minPollSeconds || seconds > maxPollSeconds {
			validationError.Set(pollErrorText)
			return
		}
		if props.OnSettings != nil {
			props.OnSettings(seconds, notifications.Get())
		}
		close()
	}

	cx.Effect(func() {
		if open.Get() {
			pollText.Set(strconv.Itoa(props.Data.PollSeconds))
			notifications.Set(props.Data.NotificationsEnabled)
			validationError.Set("")
		}
	})

	return ui.Dialog{
		TestID: "settings-dialog", Title: "设置", Description: "调整自动刷新和桌面通知。",
		Open: open.Get,
		OnOpenChange: func(shown bool, _ base.Reason) {
			if !shown {
				close()
			}
		},
		Style: core.Style{Background: colors.Surface(), BorderColor: colors.Border()},
		Children: []core.Node{
			core.View{Style: core.Style{Gap: 8}, Children: []core.Node{
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
						return core.View{
							TestID: "settings-error", Role: core.RoleAlert,
							Style: core.Style{Padding: core.Symmetric(8, 10), Gap: 8, BorderWidth: 1, CornerRadius: ui.RadiusControl},
							StyleFunc: func() core.Style {
								return core.Style{Background: ui.Alpha(colors.Danger(), 0.08), BorderColor: ui.Alpha(colors.Danger(), 0.25)}
							},
							Children: []core.Node{core.Text{ContentFunc: validationError.Get, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Danger)}},
						}
					},
				},
			}},
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Padding: core.Symmetric(4, 0)},
				Children: []core.Node{
					ui.Switch{
						TestID: "notifications-enabled", Label: "桌面通知", Checked: notifications.Get, OnChange: notifications.Set,
					},
					core.View{Style: core.Style{Gap: 4, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
						core.Text{Content: "桌面通知", Style: ui.TypeLabel, StyleFunc: ui.Colored(colors.Text)},
						core.Text{Content: "有新增或状态变化时提醒我", Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
					}},
				},
			},
		},
		Footer: []core.Node{
			ui.Button{Text: "取消", TestID: "cancel-settings", Variant: ui.ButtonSecondary, OnPress: close},
			ui.Button{Text: "保存", TestID: "save-settings", OnPress: save},
		},
	}
}
