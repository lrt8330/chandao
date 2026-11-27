package ui

import (
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

func loginPanel(cx *core.Scope, props WorkbenchProps) core.Node {
	colors := ui.ColorsOf(cx)
	account := core.NewSignal(cx, "")
	password := core.NewSignal(cx, "")
	canSubmit := func() bool {
		return strings.TrimSpace(account.Get()) != "" && password.Get() != "" && !props.Data.Loading
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

	return core.View{
		TestID: "login-panel",
		Style: core.Style{
			FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Padding: core.All(24),
			AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width: core.Pt(420), MaxWidth: core.Pct(100), Padding: core.All(24), Gap: 16,
				},
				StyleFunc: func() core.Style {
					return ui.FinOf(cx).Card().Over(core.Style{
						Background: colors.Surface(), BorderWidth: 1, BorderColor: colors.Border(),
						CornerRadius: ui.RadiusCard, BorderCurve: core.BorderCurveContinuous,
					})
				},
				Children: []core.Node{
					core.View{Style: core.Style{Gap: 6}, Children: []core.Node{
						core.Text{Content: "请先登录禅道账号", Role: core.RoleHeader, Style: ui.TypeHeading},
						core.Text{Content: "登录后即可查看分配给你的任务和 Bug", Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
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
						Disabled: func() bool { return !canSubmit() }, OnPress: submit,
						Style: core.Style{Width: core.Pct(100)},
					},
				},
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
