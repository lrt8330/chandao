package ui

import (
	"testing"
	"time"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestLocalBrowseOfflineWithoutRefresh(t *testing.T) {
	items := testItems()
	items = append(items, domain.Item{ID: "1", Type: domain.ItemStory, Project: "Web 平台", Title: "导出需求"})
	snapshot := WorkbenchData{Items: items, LastSuccess: time.Now(), Changes: []domain.Change{{Kind: domain.ChangeNew, Item: items[0]}}}
	var state *core.Signal[WorkbenchData]
	refreshes := 0
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		state = core.NewSignalFunc(cx, snapshot, nil)
		return Workbench(cx, WorkbenchProps{DataFunc: state.Get, OnRefresh: func() { refreshes++ }})
	}, workbenchTestOptions()...)
	app.Find("text:离线 · 本机快照")
	app.Type("id:local-query", "#1")
	app.Find("text:4 条记录")
	app.Press("id:local-search")
	app.Find("text:3 条记录")
	app.Press("id:open-projects")
	app.Find("id:local-projects >> text:任务 1 · 需求 1 · Bug 0")
	app.Press("id:local-projects >> label:Web 平台")
	app.WaitForGone("id:project-overview", time.Second)
	app.Find("text:2 条记录")
	app.Press("id:changes-only")
	app.Find("text:1 条记录")
	app.Press("id:filter-story")
	app.Find("id:empty-state")
	app.Press("id:changes-only")
	app.Find("label:导出需求")
	app.Post(func() {
		snapshot.Items = append(append([]domain.Item(nil), items...), domain.Item{ID: "2", Type: domain.ItemStory, Title: "另一个需求", Project: "Web 平台"})
		state.Set(snapshot)
	})
	app.Settle()
	app.Find("text:1 条记录")
	app.Find("label:导出需求")
	app.Press("id:clear-local-query")
	app.Find("text:2 条记录")
	app.Press("id:clear-project")
	app.Press("id:filter-all")
	app.Type("id:local-query", "BUG:1")
	app.Submit("id:local-query")
	app.Settle()
	app.Find("text:1 条记录")
	app.Find("label:登录后白屏")
	if refreshes != 0 {
		t.Fatalf("local browsing requested %d refreshes", refreshes)
	}
}

func TestLocalProjectSummaryAndSearch(t *testing.T) {
	items := []domain.Item{{ID: "108", Type: domain.ItemTask, Title: "Fix Timeline", Project: " "}, {ID: "2108", Type: domain.ItemBug, Title: "登录", Project: "移动端"}}
	projects := summarizeProjects(items)
	if len(projects) != 2 || projects[0] != (projectSummary{name: "未分组", tasks: 1}) || projects[1] != (projectSummary{name: "移动端", bugs: 1}) {
		t.Fatal(projects)
	}
	for _, tc := range []struct {
		query string
		index int
		want  bool
	}{{"timeline", 0, true}, {"未分组", 0, true}, {"移动", 1, true}, {"108", 1, true}, {"#108", 1, false}, {"task:108", 0, true}, {"bug:108", 0, false}} {
		if got := matchesLocalQuery(items[tc.index], tc.query); got != tc.want {
			t.Errorf("match %q on %s = %v", tc.query, items[tc.index].Key(), got)
		}
	}
}
