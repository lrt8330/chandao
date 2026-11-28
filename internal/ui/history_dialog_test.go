package ui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"chandao/zentaotray/internal/infrastructure/notifyhistory"
)

func TestWorkbenchOpensNotificationHistory(t *testing.T) {
	store, err := notifyhistory.Open(filepath.Join(t.TempDir(), "notify.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	at := time.Date(2026, 11, 28, 9, 30, 0, 0, time.Local)
	if err := store.Append(context.Background(), at, []notifyhistory.Entry{
		{Kind: "new", ItemType: "bug", ItemID: "7", Title: "弹窗压测-Bug-0001", Project: "并发压测产品", NewStatus: "active"},
		{Kind: "status", ItemType: "task", ItemID: "9", Title: "完成任务", Project: "Web 平台", OldStatus: "doing", NewStatus: "done"},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	props := WorkbenchProps{Data: WorkbenchData{Connected: true}}
	props.NotifyHistory = store
	app := startWorkbench(t, props)
	app.Find("id:open-notify-history")
	app.Press("id:open-notify-history")
	app.Settle()
	app.Find("id:notify-history")
	app.Find("text:\"弹窗压测-Bug-0001\"")
	app.Find("text:\"完成任务\"")

	app.Press("id:mark-notifications-read")
	app.Settle()
	if unread, _ := store.UnreadCount(context.Background()); unread != 0 {
		t.Fatalf("unread after mark-all-read = %d, want 0", unread)
	}
	app.Press("id:close-notify-history")
	app.WaitForGone("id:notify-history", time.Second)
}

// 已读状态改变但条数不变时，列表必须重绘；否则行上会残留未读点。
func TestHistoryEntriesCompareContent(t *testing.T) {
	at := time.Now()
	read := []notifyhistory.Entry{{ID: 1, Time: at, Kind: "new", Title: "a", Read: true}}
	unread := []notifyhistory.Entry{{ID: 1, Time: at, Kind: "new", Title: "a"}}
	if sameHistoryEntries(read, unread) {
		t.Fatal("entries differing only in read state compared equal")
	}
	if !sameHistoryEntries(read, read) {
		t.Fatal("identical entries compared different")
	}
}

// 打开后才到达的通知，再次打开时应显示出来。
func TestNotificationHistoryReloadsOnOpen(t *testing.T) {
	store, err := notifyhistory.Open(filepath.Join(t.TempDir(), "notify.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	props := WorkbenchProps{Data: WorkbenchData{Connected: true}, NotifyHistory: store}
	app := startWorkbench(t, props)

	at := time.Now()
	if err := store.Append(context.Background(), at, []notifyhistory.Entry{
		{Kind: "new", ItemType: "bug", ItemID: "11", Title: "打开后才到", Project: "Web 平台", NewStatus: "active"},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	app.Press("id:open-notify-history")
	app.Settle()
	app.Find(`text:"打开后才到"`)
}

func TestWorkbenchHidesNotifyHistoryWithoutStore(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Connected: true}})
	if app.Exists("id:open-notify-history") {
		t.Fatal("notify history button is visible without a store")
	}
}
