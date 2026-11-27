package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"chandao/zentaotray/internal/application"
	"chandao/zentaotray/internal/domain"
	"chandao/zentaotray/internal/infrastructure/zentao"
	"chandao/zentaotray/internal/ui"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestAppStartsAndShowsLoginPanel(t *testing.T) {
	var appScope *core.Scope
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		appScope = cx
		App(cx)
	},
		nsgotest.Size(1200, 800),
		nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Locale: "zh-CN"}),
	)
	window := app.Surface("title:禅道助手")
	config := window.Config()
	if config.Width != 1200 || config.Height != 800 {
		t.Fatalf("window size = %vx%v, want 1200x800", config.Width, config.Height)
	}
	app.Post(func() {
		if !appScope.MainWindow().Visible() {
			t.Fatal("main window is not visible on launch")
		}
	})
	app.Settle()
	window.Find("id:workbench")
	window.Find("id:login-panel")
	window.Find("text:请先登录禅道账号")
	window.Find("text:未登录")
	window.Find("id:login-account")
	if !window.Find("id:login-password").Input.Masked {
		t.Fatal("login password field is not masked")
	}
	if !window.Find("id:login-submit").Props.Disabled {
		t.Fatal("login submit is enabled before credentials are entered")
	}
	if window.Exists("id:item-list") || window.Exists("id:filter-all") {
		t.Fatal("workbench data controls are visible before login")
	}
}

func TestAppUsesMainViewWithoutDesktopWindow(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses a simulated windowless platform")
	}

	var appScope *core.Scope
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		appScope = cx
		App(cx)
	},
		nsgotest.Size(390, 844),
		nsgotest.Environment(core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone, Locale: "zh-CN"}),
	)
	app.Post(func() {
		if appScope.MainWindow() != nil {
			t.Fatal("windowless platform unexpectedly opened a main window")
		}
	})
	app.Settle()
	surface := app.Surface("index:0")
	surface.Find("id:workbench")
	surface.Find("id:login-panel")
	surface.Find("text:请先登录禅道账号")
}

func TestWorkbenchTrayOptionsUseEmbeddedIconAndTooltip(t *testing.T) {
	var clicks int
	opts := workbenchTrayOptions(func() { clicks++ }, func() []desktop.MenuItem { return nil })
	if !bytes.HasPrefix(opts.Icon, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("tray icon is not an embedded PNG")
	}
	if opts.Tooltip != "禅道助手" || opts.Title != "禅道助手" {
		t.Fatalf("tray title/tooltip = %q/%q, want 禅道助手", opts.Title, opts.Tooltip)
	}
	if opts.OnClick == nil || opts.Menu == nil {
		t.Fatal("tray click or menu handler is missing")
	}
	opts.OnClick()
	if clicks != 1 {
		t.Fatalf("tray click callbacks = %d, want 1", clicks)
	}
}

func TestTrayMenuReflectsWorkbenchState(t *testing.T) {
	tests := []struct {
		name string
		data ui.WorkbenchData
	}{
		{name: "login required", data: ui.WorkbenchData{NeedsLogin: true, PollSeconds: 60, NotificationsEnabled: true}},
		{name: "loading", data: ui.WorkbenchData{Loading: true, PollSeconds: 90, NotificationsEnabled: false}},
		{name: "ready", data: ui.WorkbenchData{PollSeconds: 120, NotificationsEnabled: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			menu := buildTrayMenu(tc.data, func() {}, func() {}, func(int, bool) {}, func() {})
			if len(menu) != 5 {
				t.Fatalf("menu has %d items, want 5", len(menu))
			}
			for i, want := range []string{"打开工作台", "立即刷新", "", "桌面通知", "退出"} {
				if menu[i].Label != want {
					t.Errorf("menu[%d].Label = %q, want %q", i, menu[i].Label, want)
				}
			}
			if !menu[2].Separator || menu[0].Separator || menu[1].Separator || menu[3].Separator || menu[4].Separator {
				t.Fatal("tray menu separator is not between refresh and notifications")
			}
			if got, want := menu[1].Disabled, tc.data.Loading || tc.data.NeedsLogin; got != want {
				t.Errorf("refresh disabled = %v, want %v", got, want)
			}
			if !menu[3].Checkable || menu[3].Checked != tc.data.NotificationsEnabled {
				t.Errorf("notification item checkable/checked = %v/%v, want true/%v", menu[3].Checkable, menu[3].Checked, tc.data.NotificationsEnabled)
			}
			for _, i := range []int{0, 1, 3, 4} {
				if menu[i].OnSelect == nil {
					t.Errorf("menu[%d] has no action", i)
				}
			}
		})
	}
}

func TestTrayMenuActionsUseCurrentSettings(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("notifications %v", enabled), func(t *testing.T) {
			var opened, refreshed, quit int
			var gotPollSeconds int
			var gotNotifications bool
			menu := buildTrayMenu(ui.WorkbenchData{PollSeconds: 75, NotificationsEnabled: enabled},
				func() { opened++ },
				func() { refreshed++ },
				func(pollSeconds int, notifications bool) {
					gotPollSeconds, gotNotifications = pollSeconds, notifications
				},
				func() { quit++ },
			)
			menu[0].OnSelect()
			menu[1].OnSelect()
			menu[3].OnSelect()
			menu[4].OnSelect()
			if opened != 1 || refreshed != 1 || quit != 1 {
				t.Fatalf("open/refresh/quit calls = %d/%d/%d, want 1/1/1", opened, refreshed, quit)
			}
			if gotPollSeconds != 75 || gotNotifications == enabled {
				t.Fatalf("settings callback = (%d, %v), want (75, %v)", gotPollSeconds, gotNotifications, !enabled)
			}
		})
	}
}

func TestAppWindowCloseHidesAndTrayClickShows(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses headless window-close and tray-click models")
	}
	app, cx, runtime := startMountedWorkbench(t)
	window := cx.MainWindow()
	if window == nil {
		t.Fatal("main window is missing")
	}
	if app.CloseWindow() {
		t.Fatal("normal window close destroyed the main window")
	}
	app.Settle()
	app.Post(func() {
		if window.Closed() || window.Visible() {
			t.Fatalf("after close: closed = %v, visible = %v; want false/false", window.Closed(), window.Visible())
		}
		if cx.MainWindow() != window || app.Quits != 0 || runtime.ctx.Err() != nil {
			t.Fatalf("close stopped the app: main window preserved = %v, quits = %d, runtime error = %v", cx.MainWindow() == window, app.Quits, runtime.ctx.Err())
		}
	})
	app.Settle()
	app.Find("id:workbench")

	app.ClickTrayTitle("禅道助手", core.MouseButtonLeft)
	app.Settle()
	app.Post(func() {
		if window.Closed() || !window.Visible() {
			t.Fatalf("after tray click: closed = %v, visible = %v; want false/true", window.Closed(), window.Visible())
		}
	})
	app.Settle()
}

func TestAppTrayOpenAndQuitActions(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses the headless tray menu model")
	}
	app, cx, _ := startMountedWorkbench(t)
	window := cx.MainWindow()
	app.Post(window.Hide)
	app.Settle()

	app.ClickTrayTitle("禅道助手", core.MouseButtonRight)
	app.ChoosePopup("打开工作台")
	app.Settle()
	app.Post(func() {
		if window.Closed() || !window.Visible() {
			t.Fatal("tray Open Workbench action did not show the main window")
		}
	})
	app.Settle()

	app.ClickTrayTitle("禅道助手", core.MouseButtonRight)
	app.ChoosePopup("退出")
	app.Settle()
	if app.Quits != 1 {
		t.Fatalf("quit requests = %d, want 1", app.Quits)
	}
}

func TestAppForcedQuitAllowsWindowClose(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses the headless window-close model")
	}
	app, cx, _ := startMountedWorkbench(t)
	window := cx.MainWindow()
	app.RequestQuit(true)
	if app.Quits != 1 {
		t.Fatalf("forced quit requests = %d, want 1", app.Quits)
	}
	if !app.CloseWindow() || !window.Closed() {
		t.Fatal("forced shutdown did not allow the normal window-close path")
	}
}

func TestAppNotificationPressShowsWindowAndSelectsKey(t *testing.T) {
	app, cx, runtime := startMountedWorkbench(t)
	window := cx.MainWindow()
	if window == nil {
		t.Fatal("main window is missing")
	}
	app.GrantPermission(core.PermissionNotifications)
	var showErr error
	app.Post(func() {
		showErr = cx.ShowNotification(core.Notification{ID: "bug:7", Title: "Bug 状态变化"}, nil)
	})
	app.Settle()
	if showErr != nil {
		t.Fatal(showErr)
	}
	if len(app.Notifications) != 1 {
		t.Fatalf("shown notifications = %d, want 1", len(app.Notifications))
	}
	app.Post(window.Hide)
	app.Settle()

	app.PressNotification("bug:7")
	app.Post(func() {
		if window.Closed() || !window.Visible() {
			t.Fatal("notification press did not show the main window")
		}
		if got := runtime.selectedKey.Get(); got != "bug:7" {
			t.Fatalf("selected key after notification press = %q, want bug:7", got)
		}
	})
	app.Settle()
}

func startMountedWorkbench(t *testing.T) (*nsgotest.App, *core.Scope, *workbenchRuntime) {
	t.Helper()
	var appScope *core.Scope
	var runtime *workbenchRuntime
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		appScope = cx
		runtime = mountWorkbench(cx)
	},
		nsgotest.Size(1200, 800),
		nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Locale: "zh-CN"}),
	)
	return app, appScope, runtime
}

func TestSnapshotItemsSortsByStableKey(t *testing.T) {
	items := map[string]domain.Item{
		"bug:2":  {ID: "2", Type: domain.ItemBug, Title: "Bug"},
		"task:9": {ID: "9", Type: domain.ItemTask, Title: "Later task"},
		"task:1": {ID: "1", Type: domain.ItemTask, Title: "Earlier task"},
	}
	got := snapshotItems(items)
	keys := []string{got[0].Key(), got[1].Key(), got[2].Key()}
	want := []string{"bug:2", "task:1", "task:9"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("sorted keys = %v, want %v", keys, want)
	}
	if empty := snapshotItems(nil); len(empty) != 0 {
		t.Fatalf("nil snapshot produced %d items", len(empty))
	}
}

func TestApplyRefreshUpdatesWorkbenchData(t *testing.T) {
	lastSuccess := time.Date(2026, 11, 27, 20, 0, 0, 0, time.Local)
	item := domain.Item{ID: "7", Type: domain.ItemBug, Title: "登录后白屏", Status: "active"}
	changes := []domain.Change{{Kind: domain.ChangeStatus, Item: item, Old: "new", New: "active"}}
	current := ui.WorkbenchData{
		NeedsLogin: true, Loading: true, Connected: false, ErrorMessage: "旧错误",
		PollSeconds: 60, NotificationsEnabled: true,
	}
	got := applyRefresh(current, "test.account", application.RefreshResult{
		Snapshot: application.Snapshot{
			Items: map[string]domain.Item{item.Key(): item}, LastSuccess: lastSuccess,
			PollSeconds: 90, NotificationsEnabled: false, Initialized: true,
		},
		Changes: changes,
	})

	if got.Account != "test.account" || got.NeedsLogin || !got.Connected || got.Loading || got.ErrorMessage != "" {
		t.Fatalf("session state was not applied: %+v", got)
	}
	if !got.LastSuccess.Equal(lastSuccess) || got.PollSeconds != 90 || got.NotificationsEnabled {
		t.Fatalf("refresh metadata was not applied: %+v", got)
	}
	if !reflect.DeepEqual(got.Items, []domain.Item{item}) || !reflect.DeepEqual(got.Changes, changes) {
		t.Fatalf("items/changes were not applied: %+v", got)
	}
}

func TestUserMessageSanitizesFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "login", err: fmt.Errorf("login: %w", zentao.ErrLoginFailed), want: loginMessage},
		{name: "application session", err: fmt.Errorf("fetch: %w", application.ErrSessionExpired), want: sessionMessage},
		{name: "zentao session", err: fmt.Errorf("fetch: %w", zentao.ErrSessionExpired), want: sessionMessage},
		{name: "state load", err: fmt.Errorf("load: %w", application.ErrStateLoad), want: stateLoadMessage},
		{name: "state save", err: errors.New("save snapshot: private filesystem detail"), want: stateSaveMessage},
		{name: "settings save", err: errors.New("save settings: private filesystem detail"), want: stateSaveMessage},
		{name: "invalid settings", err: fmt.Errorf("update: %w", application.ErrInvalidSettings), want: settingsMessage},
		{name: "parse", err: fmt.Errorf("page 1: %w", zentao.ErrUnexpectedStructure), want: parseMessage},
		{name: "login response parse", err: errors.New("invalid zentao login response"), want: parseMessage},
		{name: "response size", err: fmt.Errorf("fetch: %w", zentao.ErrResponseTooLarge), want: responseSizeMessage},
		{name: "secure store", err: fmt.Errorf("read: %w", core.ErrSecretsUnavailable), want: secureReadMessage},
		{name: "cancelled", err: fmt.Errorf("refresh: %w", context.Canceled), want: cancelledMessage},
		{name: "network", err: errors.New("private transport detail"), want: networkMessage},
		{name: "unknown", err: errors.New("private implementation detail"), want: networkMessage},
		{name: "none", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := userMessage(tc.err); got != tc.want {
				t.Fatalf("userMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRequiresRelogin(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{err: application.ErrSessionExpired, want: true},
		{err: zentao.ErrSessionExpired, want: true},
		{err: zentao.ErrLoginFailed, want: true},
		{err: zentao.ErrMissingCredentials, want: true},
		{err: errors.New("network unavailable")},
		{err: zentao.ErrUnexpectedStructure},
		{err: application.ErrStateLoad},
	}
	for _, tc := range tests {
		if got := requiresRelogin(tc.err); got != tc.want {
			t.Errorf("requiresRelogin(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
