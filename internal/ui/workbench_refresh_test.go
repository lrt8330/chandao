package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDetailFollowsRefreshedItem(t *testing.T) {
	for _, width := range []int{390, 1200} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			items := testItems()
			data := WorkbenchData{Items: items, LastSuccess: time.Now()}
			var state *core.Signal[WorkbenchData]
			var key *core.Signal[string]
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				state = core.NewSignalFunc(cx, data, nil)
				key = core.NewSignal(cx, "")
				return Workbench(cx, WorkbenchProps{DataFunc: state.Get, SelectedKey: key,
					OnOpenURL:       func(string) { t.Error("copy opened the browser") },
					OnRunItemAction: func(domain.Item, string, func(string)) { t.Error("removal submitted an action") },
				})
			}, workbenchTestOptions(nsgotest.Size(core.Points(width), 850))...)
			app.Press("label:" + items[0].Title)
			updated := append([]domain.Item(nil), items...)
			updated[0].Title = "更新后的标题"
			updated[0].URL += "?updated=1"
			app.Post(func() { data.Items = updated; state.Set(data) })
			app.Settle()
			if got := app.Find("id:detail-title").Text; got != updated[0].Title {
				t.Fatalf("title = %q", got)
			}
			app.Press("id:copy-item-link")
			if app.Clipboard != updated[0].URL {
				t.Fatalf("copied %q, want %q", app.Clipboard, updated[0].URL)
			}
			app.Press("id:item-action-finish")
			app.Find("id:item-action-confirm")
			app.Post(func() { data.Items = updated[1:]; state.Set(data) })
			app.WaitForGone("id:item-action-confirm", time.Second)
			app.WaitForGone("id:item-detail", time.Second)
			if key.Get() != "" {
				t.Fatalf("removed selection = %q", key.Get())
			}
		})
	}
}

func TestRefreshOfKnownEmptyList(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Loading: true, LastSuccess: time.Now()}})
	app.Find("id:empty-list-art")
	if app.Exists("id:loading-state") {
		t.Fatal("known empty snapshot replaced by first-load skeleton")
	}
}

func TestLongTitleFitsList(t *testing.T) {
	for _, width := range []int{390, 900} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			items := testItems()
			items[0].Title = strings.Repeat("检查很长的待办事项标题", 15)
			items[1].Title = strings.Repeat("ABC0123456789", 15)
			app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Items: items}}, nsgotest.Size(core.Points(width), 850))
			if width == 900 {
				app.Press("label:" + items[0].Title)
			}
			list := app.Find("id:item-list").Bounds()
			for _, item := range items[:2] {
				title := app.Find("id:item-list >> text:" + item.Title).Bounds()
				if title.Origin.X+title.Size.Width > list.Origin.X+list.Size.Width {
					t.Errorf("title width %v overflows list width %v", title.Size.Width, list.Size.Width)
				}
			}
		})
	}
}
