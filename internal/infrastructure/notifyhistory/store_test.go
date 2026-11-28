package notifyhistory

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreAppendQueryMarkRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "notify.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	at := time.Date(2026, 11, 28, 9, 0, 0, 0, time.Local)
	err = store.Append(ctx, at, []Entry{
		{Kind: "new", ItemType: "bug", ItemID: "2", Title: "新增缺陷", Project: "Web", NewStatus: "active"},
		{Kind: "status", ItemType: "task", ItemID: "1", Title: "完成任务", Project: "Web", OldStatus: "doing", NewStatus: "done"},
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, err := store.Recent(ctx, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("recent entries = %d, want 2", len(entries))
	}
	if entries[0].Kind != "status" || entries[1].Kind != "new" {
		t.Errorf("order = %s,%s, want status,new (newest id first)", entries[0].Kind, entries[1].Kind)
	}
	unread, err := store.UnreadCount(ctx)
	if err != nil || unread != 2 {
		t.Fatalf("unread = %d/%v, want 2/nil", unread, err)
	}

	if err := store.MarkAllRead(ctx); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}
	if unread, _ := store.UnreadCount(ctx); unread != 0 {
		t.Fatalf("unread after mark = %d, want 0", unread)
	}
}

func TestStoreAppendSummarizesHugeBatches(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "notify.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	changes := make([]Entry, 0, appendBatchLimit+100)
	for index := 0; index < appendBatchLimit+100; index++ {
		changes = append(changes, Entry{Kind: "new", ItemType: "bug", ItemID: fmt.Sprint(index), Title: "批量", NewStatus: "active"})
	}
	if err := store.Append(ctx, time.Now(), changes); err != nil {
		t.Fatalf("Append: %v", err)
	}
	entries, err := store.Recent(ctx, appendBatchLimit+100)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != appendBatchLimit+1 {
		t.Fatalf("rows = %d, want %d (+1 summary)", len(entries), appendBatchLimit+1)
	}
	foundSummary := false
	for _, entry := range entries {
		if entry.Kind == "summary" && strings.Contains(entry.Title, "批量变化") {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatal("summary row not found")
	}
}

func TestStoreTrimKeepsRecentRows(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "notify.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	changes := make([]Entry, 0, 30)
	for index := 0; index < 30; index++ {
		changes = append(changes, Entry{Kind: "new", ItemType: "bug", ItemID: fmt.Sprint(index), Title: "裁剪"})
	}
	if err := store.Append(ctx, time.Now(), changes); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Trim(ctx, 10); err != nil {
		t.Fatalf("Trim: %v", err)
	}
	entries, _ := store.Recent(ctx, 100)
	if len(entries) != 10 {
		t.Fatalf("rows after trim = %d, want 10", len(entries))
	}
	if entries[0].ItemID != "29" {
		t.Errorf("newest row = %s, want 29", entries[0].ItemID)
	}
}
