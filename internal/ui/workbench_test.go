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

func TestWorkbenchShowsLoginPlaceholder(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{NeedsLogin: true, Items: testItems()}})

	app.Find("text:未登录")
	app.Find("id:login-placeholder")
	app.Find("text:请先登录禅道账号")
	if app.Exists("id:item-list") || app.Exists("id:filter-all") {
		t.Fatal("workbench controls are visible before login")
	}
	if !app.Find("id:refresh").Props.Disabled {
		t.Fatal("refresh is enabled before login")
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

func testItems() []domain.Item {
	return []domain.Item{
		{ID: "1", Type: domain.ItemTask, Title: "修复登录页", Project: "Web 平台", Status: "doing", UpdatedAt: time.Date(2026, 11, 27, 10, 0, 0, 0, time.Local).Format(time.RFC3339), URL: "https://zentao.example.com/task-1.html"},
		{ID: "1", Type: domain.ItemBug, Title: "登录后白屏", Project: "移动端", Status: "active", UpdatedAt: time.Date(2026, 11, 27, 11, 30, 0, 0, time.Local).Format(time.RFC3339), URL: "https://zentao.example.com/bug-1.html"},
		{ID: "3", Type: domain.ItemTask, Title: "补充接口文档", Project: "开放平台", Status: "wait", UpdatedAt: time.Date(2026, 11, 27, 12, 0, 0, 0, time.Local).Format(time.RFC3339), URL: "https://zentao.example.com/task-3.html"},
	}
}

func startWorkbench(t *testing.T, props WorkbenchProps, opts ...nsgotest.Option) *nsgotest.App {
	t.Helper()
	options := []nsgotest.Option{
		nsgotest.Size(1200, 900),
		nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Locale: "zh-CN"}),
	}
	options = append(options, opts...)
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)}, Children: []core.Node{Workbench(cx, props)}}
	}, options...)
}
