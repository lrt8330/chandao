package notifyhistory

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestSnapshotSeriesAndStats(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "report.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	base := time.Date(2026, 11, 28, 8, 0, 0, 0, time.Local)
	sequence := []SnapshotPoint{
		{Time: base, TasksTotal: 2, TasksDoing: 2, BugsActive: 17, Total: 19},
		{Time: base.Add(time.Hour), TasksTotal: 2, TasksDoing: 2, BugsActive: 1017, Total: 1019},
		{Time: base.Add(2 * time.Hour), TasksTotal: 2, TasksDoing: 2, BugsActive: 1019, Total: 1021},
		{Time: base.Add(3 * time.Hour), TasksTotal: 2, TasksDoing: 2, BugsActive: 1019, Total: 1021}, // 重复应跳过
	}
	for _, point := range sequence {
		if err := store.AppendSnapshot(ctx, point.Time, point); err != nil {
			t.Fatalf("AppendSnapshot: %v", err)
		}
	}
	points, err := store.Snapshots(ctx, base.Add(-time.Minute), 100)
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("points = %d, want 3 (identical deduped)", len(points))
	}
	if points[2].BugsActive != 1019 || points[2].Total != 1021 {
		t.Errorf("last point = %+v", points[2])
	}

	at := time.Date(2026, 11, 28, 9, 0, 0, 0, time.Local)
	changes := make([]Entry, 0, 30)
	for index := 0; index < 12; index++ {
		changes = append(changes, Entry{Kind: "new", ItemType: "bug", ItemID: strconv.Itoa(index), Title: "新增缺陷 " + strconv.Itoa(index), NewStatus: "active"})
	}
	for index := 0; index < 5; index++ {
		changes = append(changes, Entry{Kind: "status", ItemType: "task", ItemID: "task-" + strconv.Itoa(index), Title: "任务状态", OldStatus: "doing", NewStatus: "done"})
	}
	if err := store.Append(ctx, at, changes); err != nil {
		t.Fatalf("Append events: %v", err)
	}
	stats, err := store.EventStats(ctx, base)
	if err != nil {
		t.Fatalf("EventStats: %v", err)
	}
	if len(stats) != 1 || stats[0].Day != "2026-11-28" || stats[0].New != 12 || stats[0].Status != 5 {
		t.Fatalf("stats = %+v, want 2026-11-28 new=12 status=5", stats)
	}

	filtered, err := store.QueryEvents(ctx, EventFilter{Kind: "new", Keyword: "缺陷 1", Limit: 5})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(filtered) != 3 {
		t.Fatalf("filtered = %d, want 3 (titles 1, 10, 11)", len(filtered))
	}
	for _, entry := range filtered {
		if entry.Kind != "new" {
			t.Errorf("kind = %s, want new", entry.Kind)
		}
	}
}
