package ui

import (
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// loginPanel 按登录原型排布：宽屏是「说明区 + 进入卡片」双栏，窄屏只剩
// 居中卡片。登录是这一屏的唯一主操作。
func loginPanel(cx *core.Scope, props WorkbenchProps) core.Node {
	account := core.NewSignal(cx, "")
	password := core.NewSignal(cx, "")
	canSubmit := func() bool {
		return strings.TrimSpace(account.Get()) != "" && password.Get() != "" && !props.data().Loading
	}
	submit := func() {
		if !canSubmit() {
			return
		}
		if props.OnLogin != nil {
			props.OnLogin(account.Get(), password.Get())
		}
		password.Set("")
	}

	card := loginCard(cx, account, password, props, submit, canSubmit)
	return core.View{
		TestID: "login-panel",
		Style: core.Style{
			FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Padding: core.All(24),
			AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 32,
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				if cx.Env().Window().Width < 860 {
					return core.Group{card}
				}
				return core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 48, Wrap: core.Wrap},
					Children: []core.Node{
						loginIntro(cx),
						card,
					},
				}
			}),
		},
	}
}

func loginIntro(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "login-intro",
		Style:  core.Style{Width: core.Pt(300), Gap: 12},
		Children: []core.Node{
			core.Show{When: func() bool { return cx.Env().Window().Height >= 700 }, Then: func(*core.Scope) core.Node { return illustration("login-art", assignmentDesk) }},
			monoCap(cx, "禅道助手", colors.Accent),
			core.Text{
				Content: "登录禅道，只看指派给你的事", Role: core.RoleHeader,
				Style: ui.TypeHeading.Merge(core.Style{FontSize: 26, LineHeight: 32, LetterSpacingEm: -0.02}),
			},
			core.Text{
				Content: "在一个工作台查看指派给你的任务、需求和 Bug。",
				Style:   ui.TypeBody, StyleFunc: ui.Colored(colors.Muted),
			},
			// 参考页登录说明区的要点列表：图标 + 一句话能力说明。
			core.View{Style: core.Style{Gap: 10, Padding: core.Edges{Top: 4}}, Children: []core.Node{
				loginPoint(cx, lucide.RefreshCw, "每 30–300 秒自动轮询，数据保持新鲜"),
				loginPoint(cx, lucide.BellRing, "新增或状态变化时，桌面右下角弹窗提醒"),
				loginPoint(cx, lucide.MousePointerClick, "点击通知直达该事项的详情"),
			}},
		},
	}
}

func loginPoint(cx *core.Scope, svg func() *core.SVG, text string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10}, Children: []core.Node{
		core.Icon{SVG: svg(), Label: "", Style: core.Style{FontSize: 15}, StyleFunc: ui.Colored(colors.Accent)},
		core.Text{Content: text, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
	}}
}

func loginCard(cx *core.Scope, account, password *core.Signal[string], props WorkbenchProps, submit func(), canSubmit func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "login-card",
		Style: core.Style{
			Width: core.Pt(400), MaxWidth: core.Pct(100), Padding: core.All(24), Gap: 16,
		},
		StyleFunc: func() core.Style {
			return paperSurface(cx)
		},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
				accentChip(cx, lucide.LogIn),
				core.View{Style: core.Style{Gap: 2}, Children: []core.Node{
					core.Text{Content: "请先登录禅道账号", Role: core.RoleHeader, Style: ui.TypeTitle},
					core.Text{Content: "使用禅道的账号和密码", Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
				}},
			}},
			loginField(cx, "账号", ui.Input{
				TestID: "login-account", Label: "禅道账号", Placeholder: "请输入禅道账号",
				Value: account.Get, OnChange: account.Set, AutoComplete: core.AutoCompleteUsername,
				AutoCapitalize: core.AutoCapitalizeNone, NoAutoCorrect: true,
				ReturnKeyType: core.ReturnKeyNext, SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { submit() },
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
			}),
			loginField(cx, "密码", ui.Input{
				TestID: "login-password", Label: "禅道密码", Placeholder: "请输入密码", Masked: true,
				Value: password.Get, OnChange: password.Set, AutoComplete: core.AutoCompletePassword,
				AutoCapitalize: core.AutoCapitalizeNone, NoAutoCorrect: true,
				ReturnKeyType: core.ReturnKeyGo, SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { submit() },
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
			}),
			ui.Button{
				Text: "登录", TestID: "login-submit", Size: ui.SizeLarge,
				Icon:      lucide.LogIn,
				Color:     ui.Fixed(core.RGB(cobalt)),
				TextStyle: core.Style{Color: core.RGB(0xFFFFFF)},
				Tint:      ui.Fixed(core.RGB(0xFFFFFF)),
				Disabled:  func() bool { return !canSubmit() }, OnPress: submit,
				Style: core.Style{Width: core.Pct(100)},
			},
		},
	}
}

func loginField(cx *core.Scope, label string, input ui.Input) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Gap: 6}, Children: []core.Node{
		core.Text{Content: label, Style: ui.TypeLabel, StyleFunc: ui.Colored(colors.Text)},
		input,
	}}
}
