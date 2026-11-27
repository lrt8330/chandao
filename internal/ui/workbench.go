package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Workbench renders the Zentao dashboard and owns its local filters and dialogs.
func Workbench(cx *core.Scope, props WorkbenchProps) core.Node {
	cx.SetTheme(workbenchTheme())
	filter := core.NewSignal(cx, itemFilterAll)
	changesOnly := core.NewSignal(cx, false)
	settingsOpen := core.NewSignal(cx, false)
	selection := ui.NewSelection(cx, false)
	selectedKey := props.SelectedKey
	if selectedKey == nil {
		selectedKey = core.NewSignal(cx, "")
	}
	onSelectionChange := func(selection *ui.Selection) {
		// 分组头不参与选中：只把事项行落到 selectedKey。
		for _, key := range selection.Keys() {
			if s, ok := key.(string); ok && !strings.HasPrefix(s, groupKeyPrefix) {
				selectedKey.Set(s)
				return
			}
		}
		selectedKey.Set("")
	}
	filteredItems := core.NewMemoFunc(cx, func() []domain.Item {
		data := props.data()
		changedKeys := make(map[string]struct{}, len(data.Changes))
		for _, change := range data.Changes {
			changedKeys[change.Item.Key()] = struct{}{}
		}
		var visible []domain.Item
		for _, item := range data.Items {
			_, changed := changedKeys[item.Key()]
			if changesOnly.Get() && !changed {
				continue
			}
			if filter.Get() == itemFilterAll || string(item.Type) == string(filter.Get()) {
				visible = append(visible, item)
			}
		}
		return visible
	}, slices.Equal)
	listEntries := core.NewMemoFunc(cx, func() []listEntry {
		return projectEntries(filteredItems.Get())
	}, slices.Equal)
	kpis := core.NewMemo(cx, func() kpiCounts { return workbenchKPIs(props.data()) })

	return core.View{
		TestID: "workbench",
		Style: core.Style{
			FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0), Gap: 18,
		},
		StyleFunc: workbenchCanvas(cx),
		Children: []core.Node{
			workbenchHeader(cx, props, settingsOpen),
			settingsDialog(cx, props, settingsOpen),
			core.Show{
				When: func() bool { return strings.TrimSpace(props.data().ErrorMessage) != "" },
				Then: func(cx *core.Scope) core.Node {
					return errorBanner(cx, func() string { return props.data().ErrorMessage })
				},
			},
			anchorCards(cx, kpis.Get, func() []domain.Change { return props.data().Changes }),
			core.Show{
				When: func() bool { return props.data().NeedsLogin },
				Then: func(cx *core.Scope) core.Node { return loginPanel(cx, props) },
				Else: func(cx *core.Scope) core.Node {
					return workbenchBody(cx, props, listEntries.Get, selection, onSelectionChange, selectedKey, filter, changesOnly)
				},
			},
		},
	}
}

// cloudCanvas 铺云白画布（cloud-cobalt 的扁平底色），窄窗口收一档页边距。
func workbenchCanvas(cx *core.Scope) func() core.Style {
	return func() core.Style {
		padding := core.Points(28)
		if cx.Env().Window().Width < 720 {
			padding = 16
		}
		colors := ui.ColorsOf(cx)
		return core.Style{Background: colors.Background(), Padding: core.All(padding)}
	}
}

type itemFilter string

const (
	itemFilterAll  itemFilter = "all"
	itemFilterTask itemFilter = "task"
	itemFilterBug  itemFilter = "bug"
)

func workbenchFilters(cx *core.Scope, filter *core.Signal[itemFilter], changesOnly *core.Signal[bool]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 16, Wrap: core.Wrap}, Children: []core.Node{
		ui.Tabs[itemFilter]{
			Label: "按类型筛选", Value: filter.Get, OnChange: filter.Set,
			Options: []ui.Option[itemFilter]{
				{Value: itemFilterAll, Label: "全部", TestID: "filter-all"},
				{Value: itemFilterTask, Label: "任务", TestID: "filter-task"},
				{Value: itemFilterBug, Label: "Bug", TestID: "filter-bug"},
			},
		},
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
			ui.Switch{Label: "仅看本轮变化", TestID: "changes-only", Checked: changesOnly.Get, OnChange: changesOnly.Set},
			core.Text{Content: "仅看本轮变化", Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
		}},
	}}
}

func workbenchBody(cx *core.Scope, props WorkbenchProps, entries func() []listEntry, selection *ui.Selection, onSelectionChange func(*ui.Selection), selectedKey *core.Signal[string], filter *core.Signal[itemFilter], changesOnly *core.Signal[bool]) core.Node {
	selectedItem := func() domain.Item {
		for _, item := range props.data().Items {
			if item.Key() == selectedKey.Get() {
				return item
			}
		}
		return domain.Item{}
	}
	wide := core.NewMemo(cx, func() bool { return cx.Env().Window().Width >= 900 })
	emptyMessage := func() string {
		if len(props.data().Items) == 0 {
			return "暂无待办任务和 Bug"
		}
		return "没有符合当前筛选条件的记录"
	}
	return core.Dynamic(func(cx *core.Scope) core.Node {
		list := workbenchList(cx, entries, selection, onSelectionChange, emptyMessage, func() bool { return props.data().Loading }, filter, changesOnly)
		if !wide.Get() {
			return core.View{
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
				Children: []core.Node{
					list,
					itemDetailDialog(cx, selectedItem, props.OnOpenURL, selection, selectedKey),
				},
			}
		}
		return core.View{
			Style: core.Style{Direction: core.Row, Gap: 16, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
			Children: []core.Node{
				list,
				core.Show{
					When: func() bool { return selectedKey.Get() != "" },
					Then: func(cx *core.Scope) core.Node { return itemDetailPane(cx, selectedItem, props.OnOpenURL) },
					Else: detailPlaceholder,
				},
			},
		}
	})
}

func detailPlaceholder(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "detail-placeholder",
		Style:  core.Style{Width: core.Pt(380), FlexShrink: 0, Padding: core.All(24), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 10},
		StyleFunc: func() core.Style {
			return paperSurface(cx)
		},
		Children: []core.Node{
			core.Icon{SVG: lucide.Inbox(), Label: "", Style: core.Style{FontSize: 26}, StyleFunc: ui.Colored(colors.Faint)},
			core.Text{Content: "选择一条记录查看详情", Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
		},
	}
}

func workbenchList(cx *core.Scope, entries func() []listEntry, selection *ui.Selection, onSelectionChange func(*ui.Selection), emptyMessage func() string, loading func() bool, filter *core.Signal[itemFilter], changesOnly *core.Signal[bool]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "list-panel",
		Style:  core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0), Padding: core.All(18), Gap: 12},
		StyleFunc: func() core.Style {
			return paperSurface(cx)
		},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween}, Children: []core.Node{
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
					core.Icon{SVG: lucide.ListTodo(), Label: "", Style: core.Style{FontSize: 15}, StyleFunc: ui.Colored(colors.Accent)},
					core.Text{Content: "待办列表", Style: ui.TypeTitle},
				}},
				core.Text{ContentFunc: func() string { return fmt.Sprintf("%d 条记录", countItems(entries())) }, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
			}},
			// 筛选和「仅看本轮变化」收进列表卡，靠近内容，参考页分区头的做法。
			workbenchFilters(cx, filter, changesOnly),
			core.Show{When: loading, Then: func(cx *core.Scope) core.Node { return loadingState(cx) }},
			ui.List{
				TestID: "item-list", Label: "禅道任务和 Bug", Selection: selection, OnSelectionChange: onSelectionChange,
				Count: func() int { return len(entries()) },
				Key: func(i int) any {
					if entry := entries()[i]; entry.header {
						return groupKeyPrefix + entry.name
					}
					return entries()[i].item.Key()
				},
				RowLabel: func(i int) string {
					entry := entries()[i]
					if entry.header {
						return entry.name
					}
					return entry.item.Title
				}, EstimatedSize: 72, Gap: 6,
				Empty: core.View{TestID: "empty-state", Style: core.Style{Padding: core.All(24), Gap: 10, AlignItems: core.AlignCenter, CornerRadius: 8}, StyleFunc: func() core.Style {
					return core.Style{Background: colors.Inset(), BorderWidth: 1, BorderColor: colors.Border()}
				}, Children: []core.Node{
					core.Icon{SVG: lucide.Inbox(), Label: "", Style: core.Style{FontSize: 24}, StyleFunc: ui.Colored(colors.Faint)},
					core.Text{ContentFunc: emptyMessage, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
				}},
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)},
				Row: func(rowCx *core.Scope, row core.ListRow) core.Node {
					entry := entries()[row.Index()]
					if entry.header {
						return projectGroupHeader(rowCx, entry)
					}
					item := func() domain.Item { return entry.item }
					return core.View{Style: core.Style{Gap: 6, MinWidth: core.Pt(0)}, Children: []core.Node{
						core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, MinWidth: core.Pt(0)}, Children: []core.Node{
							typePill(rowCx, item().Type),
							core.Text{ContentFunc: func() string { return item().Title }, MaxLines: 2, Style: ui.TypeLabel.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)})},
						}},
						core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Wrap: core.Wrap}, Children: []core.Node{
							core.Text{ContentFunc: func() string { return item().Status }, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Accent)},
							core.Text{ContentFunc: func() string { return formatUpdatedAt(item().UpdatedAt) }, MaxLines: 1, Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono}), StyleFunc: ui.Colored(colors.Muted)},
						}},
					}}
				},
			},
		},
	}
}

// listEntry 是列表行流的一行：项目分组头或一条事项。
type listEntry struct {
	header bool
	name   string
	count  int
	item   domain.Item
}

const groupKeyPrefix = "proj:"

// projectEntries 把事项按项目分组：组头按首次出现顺序排，事项保持原
// 相对顺序；空项目归入「未分组」。
func projectEntries(items []domain.Item) []listEntry {
	var out []listEntry
	position := make(map[string]int, len(items))
	for _, item := range items {
		project := strings.TrimSpace(item.Project)
		if project == "" {
			project = "未分组"
		}
		at, ok := position[project]
		if !ok {
			at = len(out)
			position[project] = at
			out = append(out, listEntry{header: true, name: project})
		}
		out[at].count++
		out = append(out, listEntry{item: item})
	}
	return out
}

func countItems(entries []listEntry) int {
	total := 0
	for _, entry := range entries {
		if !entry.header {
			total++
		}
	}
	return total
}

// projectGroupHeader 是项目分组头：等宽项目名 + 条数，与参考页分组标签
// 的角色一致，不参与选中。
func projectGroupHeader(cx *core.Scope, entry listEntry) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignBaseline, Gap: 8, Padding: core.Edges{Top: core.Points(4), Bottom: core.Points(2)}, MinWidth: core.Pt(0)},
		Children: []core.Node{
			monoCap(cx, entry.name, colors.Muted),
			core.Text{ContentFunc: func() string { return fmt.Sprintf("%d 项", entry.count) }, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Faint)},
		},
	}
}

func errorBanner(cx *core.Scope, message func() string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "error-banner", Role: core.RoleAlert,
		Style: core.Style{Padding: core.Symmetric(10, 14), Gap: 8, CornerRadius: ui.RadiusControl},
		StyleFunc: func() core.Style {
			return core.Style{Background: ui.Alpha(colors.Danger(), 0.08), BorderColor: ui.Alpha(colors.Danger(), 0.25)}
		},
		Children: []core.Node{core.Text{ContentFunc: message, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Danger)}},
	}
}

func loadingState(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "loading-state",
		Style:  core.Style{Padding: core.Symmetric(8, 12), CornerRadius: ui.RadiusControl},
		StyleFunc: func() core.Style {
			return core.Style{Background: colors.Inset(), BorderWidth: 1, BorderColor: colors.Border()}
		},
		Children: []core.Node{core.Text{Content: "正在加载禅道数据…", Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Accent)}},
	}
}

// typePill 是参考页流水行的类型小胶囊：白底细边、6px 圆角，任务走钴蓝
// 软色，Bug 维持红色语义。
func typePill(cx *core.Scope, itemType domain.ItemType) core.Node {
	fill, line, ink := core.RGB(cobaltSoft), core.RGB(cobaltChipLine), core.RGB(cobaltDark)
	if itemType == domain.ItemBug {
		fill, line, ink = core.RGB(bugSoftFill), core.RGB(bugSoftLine), core.RGB(cloudDanger)
	}
	return core.View{
		Style:     core.Style{Padding: core.Symmetric(2, 8), CornerRadius: 6, BorderWidth: 1, FlexShrink: 0},
		StyleFunc: func() core.Style { return core.Style{Background: fill, BorderColor: line} },
		Children: []core.Node{core.Text{
			Content: itemTypeLabel(itemType), Style: ui.TypeCaption,
			StyleFunc: func() core.Style { return core.Style{Color: ink} },
		}},
	}
}

type kpiCounts struct {
	tasks, bugs, added, statusChanges int
}

func workbenchKPIs(data WorkbenchData) kpiCounts {
	var counts kpiCounts
	for _, item := range data.Items {
		switch item.Type {
		case domain.ItemTask:
			if !finishedStatus(item.Status) {
				counts.tasks++
			}
		case domain.ItemBug:
			if !finishedStatus(item.Status) {
				counts.bugs++
			}
		}
	}
	for _, change := range data.Changes {
		switch change.Kind {
		case domain.ChangeNew:
			counts.added++
		case domain.ChangeStatus:
			counts.statusChanges++
		}
	}
	return counts
}

func finishedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "closed", "cancel", "cancelled", "finished", "completed", "resolved",
		"已完成", "已关闭", "已取消", "已解决":
		return true
	default:
		return false
	}
}

// workbenchHeader 是账户型头部：kicker、账号名主标题、连接与刷新元信息，
// 右侧保留设置和唯一主操作「立即刷新」。
func workbenchHeader(cx *core.Scope, props WorkbenchProps, settingsOpen *core.Signal[bool]) core.Node {
	colors := ui.ColorsOf(cx)
	account := func() string {
		value := strings.TrimSpace(props.data().Account)
		if value == "" {
			return "未登录"
		}
		return value
	}
	connection := func() string {
		if props.data().Connected {
			return "已连接"
		}
		return "未连接"
	}
	statusColor := func() core.Color {
		if props.data().Connected {
			return colors.Up()
		}
		return colors.Warn()
	}
	lastSuccess := func() string {
		value := props.data().LastSuccess
		if value.IsZero() {
			return "尚未刷新"
		}
		return "上次刷新 " + value.Local().Format("2006-01-02 15:04")
	}

	settings := ui.Button{
		Text: "设置", TestID: "open-settings", Variant: ui.ButtonOutline,
		Icon: lucide.Settings, OnPress: func() { settingsOpen.Set(true) },
	}
	// 立即刷新是本页唯一主操作：cloud-cobalt 的钴蓝实心按钮。
	refresh := ui.Button{
		Text: "立即刷新", TestID: "refresh", Size: ui.SizeLarge,
		Icon:      lucide.RefreshCw,
		Color:     ui.Fixed(core.RGB(cobalt)),
		TextStyle: core.Style{Color: core.RGB(0xFFFFFF)},
		Tint:      ui.Fixed(core.RGB(0xFFFFFF)),
		Busy:      func() bool { return props.data().Loading }, BusyLabel: "刷新中…",
		Disabled: func() bool { return props.data().Loading || props.data().NeedsLogin },
	}
	if props.OnRefresh != nil {
		refresh.OnPress = props.OnRefresh
	}

	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 16, Wrap: core.Wrap},
		Children: []core.Node{
			core.View{Style: core.Style{Gap: 5, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
				monoCap(cx, "禅道助手 · 工作台", colors.Accent),
				core.Text{
					ContentFunc: account, Role: core.RoleHeader,
					Style: ui.TypeHeading.Merge(core.Style{FontSize: 26, LineHeight: 32, LetterSpacingEm: -0.02}),
				},
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, Wrap: core.Wrap}, Children: []core.Node{
					core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
						core.View{Style: core.Style{Width: core.Pt(7), Height: core.Pt(7), CornerRadius: ui.RadiusFull}, StyleFunc: func() core.Style { return core.Style{Background: statusColor()} }},
						core.Text{ContentFunc: connection, Style: ui.TypeCaption, StyleFunc: ui.Colored(statusColor)},
					}},
					core.Text{ContentFunc: lastSuccess, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
				}},
			}},
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Wrap: core.Wrap}, Children: []core.Node{
				settings,
				refresh,
			}},
		},
	}
}

// anchorCards 把四个等权 KPI 收成两张信息锚点卡：待办卡给一个大数字，
// 动态卡给本轮变化摘要，其余层级交给列表纸面。
func anchorCards(cx *core.Scope, kpis func() kpiCounts, changes func() []domain.Change) core.Node {
	return core.View{
		Style: core.Style{Direction: core.Row, Gap: 14, Wrap: core.Wrap, AlignItems: core.AlignStart},
		Children: []core.Node{
			anchorCard(cx, todoAnchorCard(cx, kpis)),
			anchorCard(cx, changeAnchorCard(cx, kpis, changes)),
		},
	}
}

func anchorCard(cx *core.Scope, content core.Node) core.Node {
	return core.View{
		Style:     core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(260), Padding: core.Symmetric(16, 18), Gap: 10},
		StyleFunc: func() core.Style { return paperSurface(cx) },
		Children:  []core.Node{content},
	}
}

// monoCap 是参考页 .cap/.code 的等宽小字代号角色：小号、加粗、字距排开。
func monoCap(cx *core.Scope, content string, colored func() core.Color) core.Node {
	return core.Text{
		Content: content,
		Style:   ui.TypeCaption.Merge(core.Style{FontFamily: Mono, FontWeight: ui.Semibold, LetterSpacingEm: 0.12}),
		StyleFunc: func() core.Style {
			style := ui.Colored(colored)()
			style.FontFamily = Mono
			return style
		},
	}
}

func todoAnchorCard(cx *core.Scope, kpis func() kpiCounts) core.Node {
	colors := ui.ColorsOf(cx)
	open := func() int { return kpis().tasks + kpis().bugs }
	return core.View{Style: core.Style{Gap: 8}, Children: []core.Node{
		monoCap(cx, "我的待办", colors.Muted),
		core.Text{
			ContentFunc: func() string { return strconv.Itoa(open()) }, TestID: "kpi-open-value",
			Style:     core.Style{FontSize: 42, LineHeight: 44, FontWeight: ui.Regular, FontFamily: Serif, LetterSpacingEm: -0.045},
			StyleFunc: func() core.Style { return core.Style{Color: core.RGB(cobaltDark)} },
		},
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14, Wrap: core.Wrap}, Children: []core.Node{
			anchorStat(cx, "未完成任务", func() int { return kpis().tasks }, "kpi-tasks-value"),
			anchorStat(cx, "未解决 Bug", func() int { return kpis().bugs }, "kpi-bugs-value"),
		}},
	}}
}

func changeAnchorCard(cx *core.Scope, kpis func() kpiCounts, changes func() []domain.Change) core.Node {
	colors := ui.ColorsOf(cx)
	list := changes()
	const previewRows = 3
	rows := list
	more := 0
	if len(rows) > previewRows {
		rows, more = list[:previewRows], len(list)-previewRows
	}
	return core.View{Style: core.Style{Gap: 10, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(300)}, Children: []core.Node{
		// 参考页「近期支出」卡的头部：等宽代号 + 标题 + 计数 + 底部 hairline。
		core.View{Style: core.Style{Gap: 6}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignBaseline, Gap: 8}, Children: []core.Node{
				monoCap(cx, "ACTIVITY", colors.Muted),
				core.Text{Content: "本轮动态", Style: ui.TypeTitle},
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignBaseline, Gap: 5, FlexGrow: 1, JustifyContent: core.JustifyEnd}, Children: []core.Node{
					core.Text{ContentFunc: func() string { return strconv.Itoa(kpis().added) }, TestID: "kpi-new-value", Style: ui.TypeTitle},
					core.Text{Content: "新增", Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
					core.Text{ContentFunc: func() string { return strconv.Itoa(kpis().statusChanges) }, TestID: "kpi-status-value", Style: ui.TypeTitle},
					core.Text{Content: "变化", Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
				}},
			}},
			core.View{Style: core.Style{Height: core.Pt(1)}, StyleFunc: func() core.Style {
				return core.Style{Background: colors.Border()}
			}},
		}},
		// 变化行按参考页流水行的软底小卡排，最多预览三条，完整列表见弹窗提醒。
		changeRows(cx, rows, more),
	}}
}

// changeRows 是本轮变化的预览行：movement 胶囊着色，标题 + 项目 + 编号，
// 与待办列表的条目一一对应。
func changeRows(cx *core.Scope, rows []domain.Change, more int) core.Node {
	colors := ui.ColorsOf(cx)
	children := make([]core.Node, 0, len(rows)+1)
	for _, change := range rows {
		change := change
		children = append(children, core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, Padding: core.Symmetric(8, 10), MinWidth: core.Pt(0)},
			StyleFunc: func() core.Style {
				return core.Style{Background: colors.Inset(), BorderWidth: 1, BorderColor: colors.Border(), CornerRadius: 8, BorderCurve: core.BorderCurveContinuous}
			},
			Children: []core.Node{
				movementPill(cx, change.Kind),
				core.View{Style: core.Style{Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
					core.Text{ContentFunc: func() string { return changeTitle(change) }, MaxLines: 1, Style: ui.TypeLabel.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)})},
					core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
						core.Text{ContentFunc: func() string {
							if change.Item.Project == "" {
								return changeSummary(change)
							}
							return changeSummary(change) + " · " + change.Item.Project
						}, MaxLines: 1, Style: ui.TypeCaption.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}), StyleFunc: ui.Colored(colors.Muted)},
						core.Text{ContentFunc: func() string { return change.Item.Key() }, MaxLines: 1, Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono}), StyleFunc: ui.Colored(colors.Faint)},
					}},
				}},
			},
		})
	}
	if len(rows) == 0 {
		children = append(children, core.View{
			Style: core.Style{Padding: core.Symmetric(10, 12), CornerRadius: 8, AlignItems: core.AlignCenter},
			StyleFunc: func() core.Style {
				return core.Style{Background: colors.Inset(), BorderWidth: 1, BorderColor: colors.Border()}
			},
			Children: []core.Node{core.Text{Content: "本轮还没有变化，稍后自动刷新再看一眼。", Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)}},
		})
	}
	if more > 0 {
		children = append(children, core.Text{
			ContentFunc: func() string { return fmt.Sprintf("还有 %d 条变化，以桌面弹窗提醒", more) },
			Style:       ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted),
		})
	}
	return core.View{Style: core.Style{Gap: 8}, Children: children}
}

// movementPill 给变化方向着色：新增走成功绿，状态变化走钴蓝。
func movementPill(cx *core.Scope, kind domain.ChangeKind) core.Node {
	fill, line, ink, label := core.RGB(cloudSuccessSoft), core.RGB(0xA9D9C0), core.RGB(cloudSuccess), "新增"
	if kind == domain.ChangeStatus {
		fill, line, ink, label = core.RGB(cobaltSoft), core.RGB(cobaltChipLine), core.RGB(cobaltDark), "状态"
	}
	return core.View{
		Style:     core.Style{Padding: core.Symmetric(2, 8), CornerRadius: 6, BorderWidth: 1, FlexShrink: 0},
		StyleFunc: func() core.Style { return core.Style{Background: fill, BorderColor: line} },
		Children: []core.Node{core.Text{
			Content: label, Style: ui.TypeCaption,
			StyleFunc: func() core.Style { return core.Style{Color: ink} },
		}},
	}
}

func changeTitle(change domain.Change) string {
	if title := strings.TrimSpace(change.Item.Title); title != "" {
		return title
	}
	if id := strings.TrimSpace(change.Item.ID); id != "" {
		return id
	}
	return itemTypeLabel(change.Item.Type)
}

func changeSummary(change domain.Change) string {
	if change.Kind == domain.ChangeStatus {
		return fmt.Sprintf("%s → %s", change.Old, change.New)
	}
	return "新增" + itemTypeLabel(change.Item.Type)
}

func anchorStat(cx *core.Scope, label string, value func() int, testID string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignEnd, Gap: 5}, Children: []core.Node{
		core.Text{ContentFunc: func() string { return strconv.Itoa(value()) }, TestID: testID, Style: ui.TypeTitle.Merge(core.Style{FontSize: 19, LineHeight: 24})},
		core.Text{Content: label, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
	}}
}

// accentChip 是参考页导航图标的钴蓝软座：浅钴底、钴边、钴蓝线性图标，
// 夜色下换成半透明钴底保持对比，仅用于标签行，不承担操作。
func accentChip(cx *core.Scope, svg func() *core.SVG) core.Node {
	tint, line, ink := core.RGB(cobaltSoft), core.RGB(cobaltChipLine), core.RGB(cobaltDark)
	return core.View{
		Style:     core.Style{Width: core.Pt(28), Height: core.Pt(28), CornerRadius: 7, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, BorderWidth: 1},
		StyleFunc: func() core.Style { return core.Style{Background: tint, BorderColor: line} },
		Children: []core.Node{core.Icon{
			SVG: svg(), Label: "",
			Style: core.Style{FontSize: 16}, StyleFunc: func() core.Style { return core.Style{Color: ink} },
		}},
	}
}

// paperSurface 是主内容卡面：白底、细描边、12px 圆角加一层很轻的投影，
// 对应 cloud-cobalt 的白卡材质。
func paperSurface(cx *core.Scope) core.Style {
	colors := ui.ColorsOf(cx)
	return core.Style{
		Background: colors.Surface(), BorderWidth: 1, BorderColor: colors.Border(),
		CornerRadius: 12, BorderCurve: core.BorderCurveContinuous,
		Shadows: []core.BoxShadow{cardShadow()},
	}
}

// cardShadow 是白卡的柔和投影。
func cardShadow() core.BoxShadow {
	return ui.Shadow(0, 6, 18, 0, core.RGBA(cloudInk, 0.05))
}

// dialogChrome 是 cloud-cobalt 的对话框壳：白底、细边、14px 圆角加一层
// 明确的深投影，设置弹窗和详情弹窗共用。
func dialogChrome(cx *core.Scope) core.Style {
	colors := ui.ColorsOf(cx)
	return core.Style{
		Background: colors.Surface(), BorderWidth: 1, BorderColor: colors.Border(),
		CornerRadius: 14, BorderCurve: core.BorderCurveContinuous,
		Shadows: []core.BoxShadow{ui.Shadow(0, 20, 54, 0, core.RGBA(cloudInk, 0.18))},
	}
}

func formatUpdatedAt(value string) string {
	if value == "" {
		return "—"
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Local().Format("2006-01-02 15:04")
	}
	return value
}

func itemTypeLabel(itemType domain.ItemType) string {
	if itemType == domain.ItemBug {
		return "Bug"
	}
	return "任务"
}

func itemDetailTitle(item domain.Item) string {
	return fmt.Sprintf("%s详情", itemTypeLabel(item.Type))
}
