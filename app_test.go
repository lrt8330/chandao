package main

import (
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
