package ui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"chandao/zentaotray/internal/infrastructure/notifyhistory"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// 报表窗体内容：趋势折线、按天统计与事件明细查询（含类型筛选）。
func TestReportWindowShowsTrendAndEvents(t *testing.T) {
	store, err := notifyhistory.Open(filepath.Join(t.TempDir(), "report.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	base := time.Now().Add(-2 * time.Hour)
	sequence := []notifyhistory.SnapshotPoint{
		{Time: base, TasksTotal: 2, TasksDoing: 2, BugsActive: 5, Total: 7},
		{Time: base.Add(time.Hour), TasksTotal: 2, TasksDoing: 1, BugsActive: 8, Total: 10},
	}
	for _, point := range sequence {
		if err := store.AppendSnapshot(ctx, point.Time, point); err != nil {
			t.Fatalf("AppendSnapshot: %v", err)
		}
	}
	if err := store.Append(ctx, base.Add(time.Hour), []notifyhistory.Entry{
		{Kind: "new", ItemType: "bug", ItemID: "6", Title: "报表验证缺陷", Project: "Web 平台", NewStatus: "active"},
		{Kind: "status", ItemType: "task", ItemID: "1", Title: "报表验证任务", Project: "Web 平台", OldStatus: "doing", NewStatus: "done"},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return reportWindowBody(cx, store, func() {})
	}, nsgotest.Size(960, 760), nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))

	app.Find("id:report")
	app.Find("text:趋势 · 待办总量演变")
	app.Find("text:变化统计 · 按天")
	app.Find("text:事件明细")
	app.Find(`text:"报表验证缺陷"`)

	// 类型筛选：只看新增时状态变化事件应消失，且选中态要跟随。
	app.Press("id:report-kind-new")
	app.Settle()
	app.Find("text:报表验证缺陷")
	if app.Exists("text:报表验证任务") {
		t.Fatal("status event remains after filtering to new")
	}
	if newTab := app.Find("id:report-kind-new"); !newTab.Props.Selected {
		t.Fatalf("new tab not selected after press: %+v", newTab.Props)
	}
	if allTab := app.Find("id:report-kind-all"); allTab.Props.Selected {
		t.Fatal("all tab still selected after switching to new")
	}
	// 新增与状态变化各一条：从「新增」直接切到「状态变化」，条数相同也必须
	// 换成新筛选的内容。
	app.Press("id:report-kind-status")
	app.Settle()
	app.Find("text:报表验证任务")
	if app.Exists("text:报表验证缺陷") {
		t.Fatal("report kept the previous filter's rows when the counts were equal")
	}

	// 切回全部，两条都在。
	app.Press("id:report-kind-all")
	app.Settle()
	app.Find(`text:"报表验证任务"`)
}
