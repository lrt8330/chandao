package ui

import (
	"strconv"
	"testing"
	"time"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestWorkbenchShowsLoginPanel(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{NeedsLogin: true, Items: testItems()}})

	app.Find("text:未登录")
	app.Find("id:login-panel")
	app.Find("text:请先登录禅道账号")
	app.Find("id:login-account")
	password := app.Find("id:login-password")
	if !password.Input.Masked {
		t.Fatal("login password is not masked")
	}
	if !app.Find("id:login-submit").Props.Disabled {
		t.Fatal("login submit is enabled with empty credentials")
	}
	if app.Exists("id:item-list") || app.Exists("id:filter-all") {
		t.Fatal("workbench controls are visible before login")
	}
	if !app.Find("id:refresh").Props.Disabled {
		t.Fatal("refresh is enabled before login")
	}
}

func TestWorkbenchLoginSubmitsAndClearsPassword(t *testing.T) {
	const (
		account  = "rt.liang"
		password = "test-only-secret"
	)
	var gotAccount, gotPassword string
	var calls int
	app := startWorkbench(t, WorkbenchProps{
		Data: WorkbenchData{NeedsLogin: true},
		OnLogin: func(account, password string) {
			calls++
			gotAccount, gotPassword = account, password
		},
	})

	app.Type("id:login-account", account)
	app.Type("id:login-password", password)
	if app.Find("id:login-submit").Props.Disabled {
		t.Fatal("login submit is disabled with complete credentials")
	}
	assertPasswordNotRendered(t, app.Find("id:workbench"), password)
	app.Press("id:login-submit")
	if calls != 1 || gotAccount != account || gotPassword != password {
		t.Fatal("login callback did not receive the entered credentials")
	}
	if got := app.Find("id:login-password").Text; got != "" {
		t.Fatal("password was not cleared immediately after submission")
	}
	if got := app.Find("id:login-account").Text; got != account {
		t.Fatalf("account after submission = %q, want %q", got, account)
	}
	if !app.Find("id:login-submit").Props.Disabled {
		t.Fatal("login submit is enabled after the password was cleared")
	}
}

func TestWorkbenchLoginClearsPasswordWithoutCallback(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{NeedsLogin: true}})
	app.Type("id:login-account", "account")
	app.Type("id:login-password", "test-only-secret")
	app.Press("id:login-submit")
	if got := app.Find("id:login-password").Text; got != "" {
		t.Fatal("password was not cleared when OnLogin is nil")
	}
}

func TestWorkbenchLoginSubmitDisabledUntilComplete(t *testing.T) {
	for _, tc := range []struct {
		name, account, password string
		loading                 bool
	}{
		{name: "empty"},
		{name: "account only", account: "account"},
		{name: "password only", password: "test-only-secret"},
		{name: "blank account", account: "   ", password: "test-only-secret"},
		{name: "loading", account: "account", password: "test-only-secret", loading: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			app := startWorkbench(t, WorkbenchProps{
				Data: WorkbenchData{NeedsLogin: true, Loading: tc.loading},
				OnLogin: func(string, string) {
					calls++
				},
			})
			if tc.account != "" {
				app.Type("id:login-account", tc.account)
			}
			if tc.password != "" {
				app.Type("id:login-password", tc.password)
			}
			if !app.Find("id:login-submit").Props.Disabled {
				t.Fatal("login submit is enabled while submission is unavailable")
			}
			app.Submit("id:login-password")
			if calls != 0 {
				t.Fatal("unavailable login submission called OnLogin")
			}
		})
	}
}

func TestWorkbenchLoginShowsErrorAndFitsNarrowScreen(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{
		NeedsLogin: true, ErrorMessage: "账号或密码不正确，请重试",
	}}, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{
		OS: core.OSWindows, Idiom: core.IdiomPhone, Locale: "zh-CN",
	}))

	banner := app.Find("id:error-banner")
	if banner.Props.Role != core.RoleAlert {
		t.Fatalf("login error role = %v, want alert", banner.Props.Role)
	}
	app.Find("text:账号或密码不正确，请重试")
	panel := app.Find("id:login-panel")
	if panel.Frame.Size.Width > 390 {
		t.Fatalf("login panel width = %v, exceeds the window", panel.Frame.Size.Width)
	}
	for _, id := range []string{"login-account", "login-password", "login-submit"} {
		field := app.Find("id:" + id)
		bounds := field.Bounds()
		if bounds.Origin.X+core.Points(field.Frame.Size.Width) > 390 {
			t.Errorf("%s extends beyond the narrow window", id)
		}
	}
}

func TestWorkbenchShowsLoadingAndErrorStates(t *testing.T) {
	t.Run("loading", func(t *testing.T) {
		app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Connected: true, Loading: true, Items: testItems()}})
		app.Find("id:loading-state")
		app.Find("text:正在加载禅道数据…")
		refresh := app.Find("id:refresh")
		if !refresh.Props.Disabled || !refresh.Props.Busy {
			t.Fatalf("refresh disabled = %v, busy = %v; want both true", refresh.Props.Disabled, refresh.Props.Busy)
		}
		app.Find("label:修复登录页")
	})

	t.Run("error", func(t *testing.T) {
		app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{
			Connected: true, ErrorMessage: "网络不可用，请稍后重试", Items: testItems(),
		}})
		banner := app.Find("id:error-banner")
		if banner.Props.Role != core.RoleAlert {
			t.Fatalf("error banner role = %v, want alert", banner.Props.Role)
		}
		app.Find("text:网络不可用，请稍后重试")
		app.Find("label:修复登录页")
	})
}

func TestWorkbenchShowsEmptyStates(t *testing.T) {
	t.Run("no assigned items", func(t *testing.T) {
		app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Connected: true}})
		app.Find("id:empty-state")
		app.Find("text:暂无待办任务和 Bug")
	})

	t.Run("no filter matches", func(t *testing.T) {
		app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Connected: true, Items: testItems()[:1]}})
		app.Press("id:filter-bug")
		app.Find("id:empty-state")
		app.Find("text:没有符合当前筛选条件的记录")
	})
}

func TestWorkbenchUsesDialogOnNarrowScreens(t *testing.T) {
	items := testItems()
	var opened string
	app := startWorkbench(t, WorkbenchProps{
		Data:      WorkbenchData{Connected: true, Items: items},
		OnOpenURL: func(url string) { opened = url },
	}, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{
		OS: core.OSWindows, Idiom: core.IdiomPhone, Locale: "zh-CN",
	}))

	app.Press("label:修复登录页")
	detail := app.Find("id:item-detail")
	if detail.Props.Role != core.RoleDialog {
		t.Fatalf("narrow detail role = %v, want dialog", detail.Props.Role)
	}
	if detail.Frame.Size.Width > 390 {
		t.Fatalf("narrow dialog width = %v, exceeds the window", detail.Frame.Size.Width)
	}
	app.Find("id:detail-title")
	app.Press("id:open-zentao")
	if opened != items[0].URL {
		t.Fatalf("opened URL = %q, want %q", opened, items[0].URL)
	}
	app.Press("id:close-item-detail")
	app.WaitForGone("id:item-detail", time.Second)
}

func TestWorkbenchSelectsRowAndOpensZentao(t *testing.T) {
	items := testItems()
	var opened string
	app := startWorkbench(t, WorkbenchProps{
		Data:      WorkbenchData{Connected: true, Items: items},
		OnOpenURL: func(url string) { opened = url },
	})

	if app.Exists("id:item-detail") {
		t.Fatal("detail pane is visible before a row is selected")
	}
	app.Press("label:修复登录页")
	detail := app.Find("id:item-detail")
	if got := detail.Frame.Size.Width; got != 380 {
		t.Fatalf("detail width = %v, want 380", got)
	}
	for id, want := range map[string]string{
		"detail-type":    "任务",
		"detail-id":      "1",
		"detail-title":   "修复登录页",
		"detail-project": "Web 平台",
		"detail-status":  "doing",
		"detail-updated": "2026-11-27 10:00",
		"detail-url":     items[0].URL,
		"detail-key":     items[0].Key(),
	} {
		if got := app.Find("id:" + id).Text; got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}

	app.Press("id:open-zentao")
	if opened != items[0].URL {
		t.Fatalf("opened URL = %q, want %q", opened, items[0].URL)
	}

	app.Press("label:登录后白屏")
	if got := app.Find("id:detail-title").Text; got != items[1].Title {
		t.Fatalf("detail title after selecting another row = %q, want %q", got, items[1].Title)
	}
}

func TestWorkbenchFiltersChangesOnly(t *testing.T) {
	items := testItems()
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{
		Connected: true,
		Items:     items,
		Changes: []domain.Change{
			{Kind: domain.ChangeNew, Item: items[0]},
			{Kind: domain.ChangeStatus, Item: items[1], Old: "new", New: "active"},
		},
	}})

	app.Find("id:changes-only")
	app.Press("id:changes-only")
	app.Find("label:修复登录页")
	app.Find("label:登录后白屏")
	if app.Exists("label:补充接口文档") {
		t.Fatal("unchanged row remains after enabling changes-only")
	}
	if !app.Find("id:changes-only").Props.Checked {
		t.Fatal("changes-only switch is not checked")
	}

	app.Press("id:filter-task")
	app.Find("label:修复登录页")
	if app.Exists("label:登录后白屏") {
		t.Fatal("type and changes-only filters were not combined")
	}

	app.Press("id:changes-only")
	app.Find("label:补充接口文档")
}

func TestWorkbenchFiltersByType(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Connected: true, Items: testItems()}})

	app.Find("id:filter-all")
	app.Find("id:filter-task")
	app.Find("id:filter-bug")
	app.Press("id:filter-task")
	app.Find("label:修复登录页")
	app.Find("label:补充接口文档")
	if app.Exists("label:登录后白屏") {
		t.Fatal("Bug row remains after selecting the task tab")
	}

	app.Press("id:filter-bug")
	app.Find("label:登录后白屏")
	if app.Exists("label:修复登录页") || app.Exists("label:补充接口文档") {
		t.Fatal("task rows remain after selecting the Bug tab")
	}

	app.Press("id:filter-all")
	app.Find("label:修复登录页")
	app.Find("label:登录后白屏")
	app.Find("label:补充接口文档")
}

func TestWorkbenchDataFuncUpdatesWithoutResettingFilters(t *testing.T) {
	items := testItems()
	initial := WorkbenchData{
		Account:     "initial.account",
		Connected:   true,
		LastSuccess: time.Date(2026, 11, 27, 18, 30, 0, 0, time.Local),
		Items:       items,
	}
	var data *core.Signal[WorkbenchData]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		data = core.NewSignalFunc(cx, initial, nil)
		return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)}, Children: []core.Node{
			Workbench(cx, WorkbenchProps{
				Data:     WorkbenchData{Account: "stale.account"},
				DataFunc: data.Get,
			}),
		}}
	}, workbenchTestOptions()...)

	app.Find("text:账号：initial.account")
	app.Find("label:修复登录页")
	app.Press("id:filter-bug")
	app.Find("label:登录后白屏")

	refreshed := WorkbenchData{
		Account:      "refreshed.account",
		Connected:    false,
		Loading:      true,
		LastSuccess:  time.Date(2026, 11, 27, 19, 30, 0, 0, time.Local),
		ErrorMessage: "刷新完成，请检查连接",
		Items: []domain.Item{
			{ID: "4", Type: domain.ItemTask, Title: "刷新后的任务", Status: "doing"},
			{ID: "5", Type: domain.ItemBug, Title: "刷新后的 Bug", Status: "active"},
			{ID: "6", Type: domain.ItemBug, Title: "新增的 Bug", Status: "new"},
		},
		Changes: []domain.Change{
			{Kind: domain.ChangeNew},
			{Kind: domain.ChangeNew},
			{Kind: domain.ChangeStatus},
		},
	}
	app.Post(func() { data.Set(refreshed) })
	app.Settle()

	if !app.Find("id:filter-bug").Props.Selected {
		t.Fatal("Bug filter was reset after the data signal changed")
	}
	app.Find("label:刷新后的 Bug")
	app.Find("label:新增的 Bug")
	app.Find("text:2 条记录")
	for _, label := range []string{"修复登录页", "登录后白屏", "补充接口文档", "刷新后的任务"} {
		if app.Exists("label:" + label) {
			t.Errorf("filtered row %q remains after refresh", label)
		}
	}
	for id, want := range map[string]string{
		"kpi-tasks-value":  "1",
		"kpi-bugs-value":   "2",
		"kpi-new-value":    "2",
		"kpi-status-value": "1",
	} {
		if got := app.Find("id:" + id).Text; got != want {
			t.Errorf("%s after refresh = %q, want %q", id, got, want)
		}
	}
	app.Find("text:账号：refreshed.account")
	app.Find("text:未连接")
	app.Find("text:上次刷新：2026-11-27 19:30")
	app.Find("id:loading-state")
	app.Find("text:刷新完成，请检查连接")
	if refresh := app.Find("id:refresh"); !refresh.Props.Disabled || !refresh.Props.Busy {
		t.Fatalf("refresh disabled/busy after refresh = %v/%v, want true/true", refresh.Props.Disabled, refresh.Props.Busy)
	}
}

func TestWorkbenchUsesExternalSelectedKey(t *testing.T) {
	items := testItems()
	var selectedKey *core.Signal[string]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		selectedKey = core.NewSignal(cx, "")
		return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)}, Children: []core.Node{
			Workbench(cx, WorkbenchProps{
				Data:        WorkbenchData{Connected: true, Items: items},
				SelectedKey: selectedKey,
			}),
		}}
	}, workbenchTestOptions(
		nsgotest.Size(390, 844),
		nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomPhone, Locale: "zh-CN"}),
	)...)

	if app.Exists("id:item-detail") {
		t.Fatal("detail is visible before the external selection is set")
	}
	app.Post(func() { selectedKey.Set(items[1].Key()) })
	app.Settle()
	detail := app.Find("id:item-detail")
	if detail.Props.Role != core.RoleDialog {
		t.Fatalf("external detail role = %v, want dialog", detail.Props.Role)
	}
	for id, want := range map[string]string{
		"detail-title": items[1].Title,
		"detail-key":   items[1].Key(),
	} {
		if got := app.Find("id:" + id).Text; got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}

	app.Press("id:close-item-detail")
	app.WaitForGone("id:item-detail", time.Second)
	if got := selectedKey.Get(); got != "" {
		t.Fatalf("external selection after closing detail = %q, want empty", got)
	}

	app.Press("label:修复登录页")
	if got := selectedKey.Get(); got != items[0].Key() {
		t.Fatalf("external selection after row press = %q, want %q", got, items[0].Key())
	}
}

func TestWorkbenchDataFuncUpdatesLoginAndSettings(t *testing.T) {
	initial := WorkbenchData{
		NeedsLogin:           true,
		PollSeconds:          60,
		NotificationsEnabled: true,
	}
	var data *core.Signal[WorkbenchData]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		data = core.NewSignalFunc(cx, initial, nil)
		return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)}, Children: []core.Node{
			Workbench(cx, WorkbenchProps{DataFunc: data.Get}),
		}}
	}, workbenchTestOptions()...)

	app.Type("id:login-account", "account")
	app.Type("id:login-password", "test-only-secret")
	if app.Find("id:login-submit").Props.Disabled {
		t.Fatal("login submit is disabled before the reactive loading update")
	}
	loading := initial
	loading.Loading = true
	app.Post(func() { data.Set(loading) })
	app.Settle()
	if !app.Find("id:login-submit").Props.Disabled {
		t.Fatal("login submit did not react to the loading update")
	}

	connected := WorkbenchData{
		Connected:   true,
		PollSeconds: 90,
		Items:       testItems(),
	}
	app.Post(func() { data.Set(connected) })
	app.Settle()
	app.Find("id:item-list")
	if app.Exists("id:login-panel") {
		t.Fatal("login panel remains after NeedsLogin becomes false")
	}
	app.Press("id:open-settings")
	if got := app.Find("id:poll-seconds").Text; got != "90" {
		t.Fatalf("reactive poll seconds = %q, want 90", got)
	}
	if app.Find("id:notifications-enabled").Props.Checked {
		t.Fatal("reactive notifications setting is still enabled")
	}

	connected.PollSeconds = 120
	connected.NotificationsEnabled = true
	app.Post(func() { data.Set(connected) })
	app.Settle()
	if got := app.Find("id:poll-seconds").Text; got != "120" {
		t.Fatalf("updated poll seconds = %q, want 120", got)
	}
	if !app.Find("id:notifications-enabled").Props.Checked {
		t.Fatal("notifications setting did not react to the data update")
	}
}

func TestWorkbenchRendersItemRows(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Connected: true, Items: testItems()}})

	app.Find("id:item-list")
	for _, text := range []string{"修复登录页", "Web 平台", "doing", "2026-11-27 10:00", "登录后白屏", "移动端", "active", "2026-11-27 11:30"} {
		app.Find("text:" + text)
	}
	app.Find("label:修复登录页")
	app.Find("label:登录后白屏")
}

func TestWorkbenchRespondsToWindowResize(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Connected: true, Items: testItems()}})
	app.Press("label:修复登录页")
	if got := app.Find("id:item-detail").Frame.Size.Width; got != 380 {
		t.Fatalf("wide detail width = %v, want 380", got)
	}

	app.Resize(390, 844)
	if got := app.Find("id:item-detail").Props.Role; got != core.RoleDialog {
		t.Fatalf("resized detail role = %v, want dialog", got)
	}
	app.Find("id:detail-title")

	app.Resize(1200, 900)
	if got := app.Find("id:item-detail").Frame.Size.Width; got != 380 {
		t.Fatalf("detail width after resizing wide = %v, want 380", got)
	}
	if app.Exists("id:close-item-detail") {
		t.Fatal("dialog close action remains in the wide layout")
	}
}

func TestWorkbenchVirtualizesLongLists(t *testing.T) {
	items := make([]domain.Item, 500)
	for i := range items {
		items[i] = domain.Item{
			ID: strconv.Itoa(i), Type: domain.ItemTask, Title: "记录 " + strconv.Itoa(i),
			Project: "Web", Status: "doing", UpdatedAt: "2026-11-27T10:00:00Z",
		}
	}
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Connected: true, Items: items}})
	if app.Exists("label:记录 499") {
		t.Fatal("last row was built before it was visible")
	}
	app.Press("label:记录 0")
	app.Key("end")
	app.Find("label:记录 499")
}

func TestWorkbenchShowsHeaderAndKPIs(t *testing.T) {
	var refreshed bool
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{
		Account:     "rt.liang",
		Connected:   true,
		LastSuccess: time.Date(2026, 11, 27, 18, 30, 0, 0, time.Local),
		Items: []domain.Item{
			{ID: "1", Type: domain.ItemTask, Title: " unfinished task ", Status: "doing"},
			{ID: "2", Type: domain.ItemTask, Status: "done"},
			{ID: "3", Type: domain.ItemBug, Status: "active"},
			{ID: "4", Type: domain.ItemBug, Status: "resolved"},
		},
		Changes: []domain.Change{
			{Kind: domain.ChangeNew},
			{Kind: domain.ChangeNew},
			{Kind: domain.ChangeStatus},
		},
	}, OnRefresh: func() { refreshed = true }})

	if got := app.Find("id:workbench").Style.Background; got != core.RGB(0xF5F8FC) {
		t.Errorf("workbench background = %x, want F5F8FC", got)
	}
	card := app.Find("id:kpi-tasks")
	if card.Style.Background != core.RGB(0xFFFFFF) || card.Style.BorderColor != core.RGB(0xDCE6F2) {
		t.Errorf("card background/border = %x/%x, want FFFFFF/DCE6F2", card.Style.Background, card.Style.BorderColor)
	}
	if got := app.Find("id:refresh").Children[0].Style.Background; got != core.RGB(0x2563EB) {
		t.Errorf("refresh background = %x, want 2563EB", got)
	}

	app.Find("text:禅道助手")
	app.Find("text:账号：rt.liang")
	app.Find("text:已连接")
	app.Find("text:上次刷新：2026-11-27 18:30")
	for _, label := range []string{"未完成任务", "未解决 Bug", "本轮新增", "本轮状态变化"} {
		app.Find("text:" + label)
	}
	for id, want := range map[string]string{
		"kpi-tasks-value":  "1",
		"kpi-bugs-value":   "1",
		"kpi-new-value":    "2",
		"kpi-status-value": "1",
	} {
		if got := app.Find("id:" + id).Text; got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	app.Find("id:refresh")
	app.Press("id:refresh")
	if !refreshed {
		t.Fatal("refresh callback was not called")
	}
}

func TestWorkbenchSettingsOpensAndCloses(t *testing.T) {
	var calls int
	app := startWorkbench(t, WorkbenchProps{
		Data:       WorkbenchData{Connected: true, PollSeconds: 60, NotificationsEnabled: true},
		OnSettings: func(int, bool) { calls++ },
	})

	if app.Exists("id:settings-dialog") {
		t.Fatal("settings dialog is visible before it is opened")
	}
	app.Find("id:open-settings")
	app.Press("id:open-settings")
	dialog := app.Find("id:settings-dialog")
	if dialog.Props.Role != core.RoleDialog {
		t.Fatalf("settings role = %v, want dialog", dialog.Props.Role)
	}
	if dialog.Style.Background != core.RGB(0xFFFFFF) || dialog.Style.BorderColor != core.RGB(0xDCE6F2) {
		t.Fatalf("settings background/border = %x/%x, want FFFFFF/DCE6F2", dialog.Style.Background, dialog.Style.BorderColor)
	}
	if got := app.Find("id:poll-seconds").Text; got != "60" {
		t.Fatalf("poll seconds = %q, want 60", got)
	}
	if !app.Find("id:notifications-enabled").Props.Checked {
		t.Fatal("notifications switch is not prefilled")
	}

	app.Clear("id:poll-seconds")
	app.Type("id:poll-seconds", "99")
	app.Press("id:notifications-enabled")
	app.Press("id:cancel-settings")
	app.WaitForGone("id:settings-dialog", time.Second)
	if calls != 0 {
		t.Fatal("cancel called OnSettings")
	}

	app.Press("id:open-settings")
	if got := app.Find("id:poll-seconds").Text; got != "60" {
		t.Fatalf("poll seconds after reopening = %q, want 60", got)
	}
	if !app.Find("id:notifications-enabled").Props.Checked {
		t.Fatal("cancelled notification change survived reopening")
	}
	app.Key("escape")
	app.WaitForGone("id:settings-dialog", time.Second)
	if calls != 0 {
		t.Fatal("closing with Escape called OnSettings")
	}
}

func TestWorkbenchSettingsSavesValidValues(t *testing.T) {
	for _, seconds := range []int{30, 120, 300} {
		t.Run(strconv.Itoa(seconds), func(t *testing.T) {
			var gotSeconds int
			var gotNotifications bool
			var calls int
			app := startWorkbench(t, WorkbenchProps{
				Data: WorkbenchData{Connected: true, PollSeconds: 60, NotificationsEnabled: true},
				OnSettings: func(pollSeconds int, notificationsEnabled bool) {
					calls++
					gotSeconds = pollSeconds
					gotNotifications = notificationsEnabled
				},
			})
			app.Press("id:open-settings")
			app.Clear("id:poll-seconds")
			app.Type("id:poll-seconds", strconv.Itoa(seconds))
			app.Press("id:notifications-enabled")
			app.Press("id:save-settings")
			app.WaitForGone("id:settings-dialog", time.Second)
			if calls != 1 || gotSeconds != seconds || gotNotifications {
				t.Fatalf("settings callback calls/values = %d/%d/%t, want 1/%d/false", calls, gotSeconds, gotNotifications, seconds)
			}
		})
	}
}

func TestWorkbenchSettingsRejectsInvalidPollValues(t *testing.T) {
	for _, value := range []string{"29", "301", "abc", "30.5", "", "999999999999999999999999"} {
		t.Run("poll_"+value, func(t *testing.T) {
			var calls int
			app := startWorkbench(t, WorkbenchProps{
				Data:       WorkbenchData{Connected: true, PollSeconds: 60, NotificationsEnabled: true},
				OnSettings: func(int, bool) { calls++ },
			})
			app.Press("id:open-settings")
			app.Clear("id:poll-seconds")
			if value != "" {
				app.Type("id:poll-seconds", value)
			}
			app.Press("id:save-settings")
			if calls != 0 {
				t.Fatal("invalid poll value called OnSettings")
			}
			if !app.Exists("id:settings-dialog") {
				t.Fatal("settings dialog closed after invalid input")
			}
			if !app.Find("id:poll-seconds").Props.Invalid {
				t.Fatal("poll input is not marked invalid")
			}
			errorBanner := app.Find("id:settings-error")
			if errorBanner.Props.Role != core.RoleAlert {
				t.Fatalf("validation error role = %v, want alert", errorBanner.Props.Role)
			}
			app.Find("text:" + pollErrorText)

			app.Clear("id:poll-seconds")
			app.Type("id:poll-seconds", "90")
			if app.Exists("id:settings-error") {
				t.Fatal("validation error remains after correcting the input")
			}
			app.Press("id:save-settings")
			app.WaitForGone("id:settings-dialog", time.Second)
			if calls != 1 {
				t.Fatalf("OnSettings calls after correction = %d, want 1", calls)
			}
		})
	}
}

func TestWorkbenchSettingsNotificationsToggle(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(strconv.FormatBool(initial), func(t *testing.T) {
			app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{
				Connected: true, PollSeconds: 45, NotificationsEnabled: initial,
			}})
			app.Press("id:open-settings")
			toggle := app.Find("id:notifications-enabled")
			if toggle.Props.Checked != initial {
				t.Fatalf("initial notifications state = %v, want %v", toggle.Props.Checked, initial)
			}
			app.Press("id:notifications-enabled")
			if got := app.Find("id:notifications-enabled").Props.Checked; got == initial {
				t.Fatal("notifications switch did not toggle")
			}
			app.Press("id:cancel-settings")
			app.WaitForGone("id:settings-dialog", time.Second)
		})
	}
}

func TestWorkbenchSettingsFitNarrowScreen(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{
		Connected: true, PollSeconds: 60, NotificationsEnabled: true,
	}}, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{
		OS: core.OSWindows, Idiom: core.IdiomPhone, Locale: "zh-CN",
	}))

	app.Press("id:open-settings")
	if got := app.Find("id:settings-dialog").Frame.Size.Width; got > 390 {
		t.Fatalf("narrow settings dialog width = %v, exceeds the window", got)
	}
	for _, id := range []string{"poll-seconds", "notifications-enabled", "cancel-settings", "save-settings"} {
		control := app.Find("id:" + id)
		bounds := control.Bounds()
		if bounds.Origin.X+core.Points(control.Frame.Size.Width) > 390 {
			t.Errorf("%s extends beyond the narrow window", id)
		}
	}
	app.Press("id:cancel-settings")
	app.WaitForGone("id:settings-dialog", time.Second)
}

func assertPasswordNotRendered(t *testing.T, view *nsgotest.View, password string) {
	t.Helper()
	if view.Kind == nsgotest.KindText && view.Text == password {
		t.Fatal("password is rendered in a text node")
	}
	for _, child := range view.Children {
		assertPasswordNotRendered(t, child, password)
	}
}

func testItems() []domain.Item {
	return []domain.Item{
		{ID: "1", Type: domain.ItemTask, Title: "修复登录页", Project: "Web 平台", Status: "doing", UpdatedAt: time.Date(2026, 11, 27, 10, 0, 0, 0, time.Local).Format(time.RFC3339), URL: "https://zentao.example.com/task-1.html"},
		{ID: "1", Type: domain.ItemBug, Title: "登录后白屏", Project: "移动端", Status: "active", UpdatedAt: time.Date(2026, 11, 27, 11, 30, 0, 0, time.Local).Format(time.RFC3339), URL: "https://zentao.example.com/bug-1.html"},
		{ID: "3", Type: domain.ItemTask, Title: "补充接口文档", Project: "开放平台", Status: "wait", UpdatedAt: time.Date(2026, 11, 27, 12, 0, 0, 0, time.Local).Format(time.RFC3339), URL: "https://zentao.example.com/task-3.html"},
	}
}

func startWorkbench(t *testing.T, props WorkbenchProps, opts ...nsgotest.Option) *nsgotest.App {
	t.Helper()
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)}, Children: []core.Node{Workbench(cx, props)}}
	}, workbenchTestOptions(opts...)...)
}

func workbenchTestOptions(opts ...nsgotest.Option) []nsgotest.Option {
	options := []nsgotest.Option{
		nsgotest.Size(1200, 900),
		nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Locale: "zh-CN"}),
	}
	return append(options, opts...)
}
