package ui

import (
	"fmt"
	"slices"
	"strings"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// 本地浏览不持有刷新、登录或服务端接口。输入草稿也不作为列表的依赖。
type localBrowse struct {
	draft, query, project *core.Signal[string]
	projectsOpen          *core.Signal[bool]
}

func newLocalBrowse(cx *core.Scope) *localBrowse {
	return &localBrowse{core.NewSignal(cx, ""), core.NewSignal(cx, ""), core.NewSignal(cx, ""), core.NewSignal(cx, false)}
}

func projectName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "未分组"
}

func matchesLocalQuery(item domain.Item, query string) bool {
	if query == "" {
		return true
	}
	if id, ok := strings.CutPrefix(query, "#"); ok {
		return item.ID == id
	}
	if kind, _, ok := strings.Cut(query, ":"); ok && (kind == "task" || kind == "bug" || kind == "story") {
		return item.Key() == query
	}
	return strings.Contains(strings.ToLower(item.Title), query) || strings.Contains(strings.ToLower(projectName(item.Project)), query) || strings.Contains(item.ID, query)
}

func (b *localBrowse) controls(cx *core.Scope) core.Node {
	submit := func(s string) { b.query.Set(strings.ToLower(strings.TrimSpace(s))) }
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, Wrap: core.Wrap}, Children: []core.Node{
		ui.Input{Label: "搜索本机待办", TestID: "local-query", Placeholder: "标题、项目、#编号", Value: b.draft.Get, OnChange: b.draft.Set, OnSubmit: submit, ReturnKeyType: core.ReturnKeySearch, Style: core.Style{Width: core.Pt(180), FlexShrink: 1, MaxWidth: core.Pct(100)}},
		ui.Button{Text: "搜索", TestID: "local-search", Variant: ui.ButtonOutline, OnPress: func() { submit(b.draft.Get()) }},
		ui.Button{Text: "项目概览", TestID: "open-projects", Variant: ui.ButtonOutline, OnPress: func() { b.projectsOpen.Set(true) }},
		core.Show{When: func() bool { return b.query.Get() != "" || b.draft.Get() != "" }, Then: func(*core.Scope) core.Node {
			return ui.Button{Text: "清除搜索", TestID: "clear-local-query", Variant: ui.ButtonGhost, OnPress: func() { b.draft.Set(""); b.query.Set("") }}
		}},
		core.Show{When: func() bool { return b.project.Get() != "" }, Then: func(*core.Scope) core.Node {
			return ui.Button{Text: "取消项目限制", TestID: "clear-project", Variant: ui.ButtonGhost, OnPress: func() { b.project.Set("") }}
		}},
	}}
}

type projectSummary struct {
	name                 string
	tasks, stories, bugs int
}

func summarizeProjects(items []domain.Item) []projectSummary {
	var projects []projectSummary
	index := make(map[string]int)
	for _, item := range items {
		name := projectName(item.Project)
		i, ok := index[name]
		if !ok {
			i = len(projects)
			index[name] = i
			projects = append(projects, projectSummary{name: name})
		}
		switch item.Type {
		case domain.ItemTask:
			projects[i].tasks++
		case domain.ItemStory:
			projects[i].stories++
		case domain.ItemBug:
			projects[i].bugs++
		}
	}
	return projects
}

func snapshotScope(data WorkbenchData) string {
	if !data.Connected && !data.LastSuccess.IsZero() {
		return "离线 · 本机快照 " + data.LastSuccess.Local().Format("01-02 15:04")
	}
	if len(data.Items) == 0 && data.LastSuccess.IsZero() {
		return "暂无本机快照"
	}
	return "本机快照 · 仅指派给我"
}

func (b *localBrowse) overview(cx *core.Scope, data func() WorkbenchData) core.Node {
	return core.Show{When: b.projectsOpen.Get, Then: func(cx *core.Scope) core.Node {
		projects := core.NewMemoFunc(cx, func() []projectSummary { return summarizeProjects(data().Items) }, slices.Equal)
		selection := ui.NewSelection(cx, false)
		pick := func(name string) { b.project.Set(name); b.projectsOpen.Set(false) }
		return ui.Dialog{TestID: "project-overview", Title: "项目概览", DescriptionFunc: func() string { return snapshotScope(data()) + " · 选择项目查看待办" }, Open: b.projectsOpen.Get, OnOpenChange: func(v bool, _ base.Reason) { b.projectsOpen.Set(v) }, Style: dialogChrome(cx), Children: []core.Node{
			ui.List{Label: "本地项目", TestID: "local-projects", Count: func() int { return len(projects.Get()) }, Key: func(i int) any { return projects.Get()[i].name }, RowLabel: func(i int) string { return projects.Get()[i].name }, Selection: selection, OnSelectionChange: func(s *ui.Selection) {
				for _, k := range s.Keys() {
					if name, ok := k.(string); ok {
						pick(name)
						return
					}
				}
			}, Style: core.Style{Height: core.Pt(240), FlexShrink: 1}, EstimatedSize: 50,
				Empty: core.Text{Content: "没有可汇总的项目", Style: ui.TypeBody}, Row: func(rowCx *core.Scope, row core.ListRow) core.Node {
					return core.Dynamic(func(cx *core.Scope) core.Node {
						list, i := projects.Get(), row.Index()
						if i < 0 || i >= len(list) {
							return core.View{}
						}
						p := list[i]
						return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 4}, Children: []core.Node{core.Text{Content: p.name, MaxLines: 1, Style: ui.TypeLabel}, core.Text{Content: fmt.Sprintf("任务 %d · 需求 %d · Bug %d", p.tasks, p.stories, p.bugs), Style: ui.TypeCaption, StyleFunc: ui.Colored(ui.ColorsOf(cx).Muted)}}}
					})
				}},
		}, Footer: []core.Node{ui.Button{Text: "关闭", Variant: ui.ButtonOutline, OnPress: func() { b.projectsOpen.Set(false) }}}}
	}}
}
