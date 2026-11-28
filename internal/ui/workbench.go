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
	notifyHistoryOpen := core.NewSignal(cx, false)
	paletteOpen := core.NewSignal(cx, false)
	// Ctrl+K 打开命令面板，Ctrl+R 立即刷新；绑定随工作台存活。
	cx.OnKey("mod+k", func() { paletteOpen.Set(!paletteOpen.Get()) })
	cx.OnKey("mod+r", func() {
		if props.OnRefresh != nil && !props.data().Loading && !props.data().NeedsLogin {
			props.OnRefresh()
		}
	})
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
	browse := newLocalBrowse(cx)
	filteredItems := core.NewMemoFunc(cx, func() []domain.Item {
		data := props.data()
		query, project := browse.query.Get(), browse.project.Get()
		kind, onlyChanged := filter.Get(), changesOnly.Get()
		changedKeys := make(map[string]struct{}, len(data.Changes))
		for _, change := range data.Changes {
			changedKeys[change.Item.Key()] = struct{}{}
		}
		var visible []domain.Item
		for _, item := range data.Items {
			_, changed := changedKeys[item.Key()]
			if onlyChanged && !changed {
				continue
			}
			if project != "" && projectName(item.Project) != project {
				continue
			}
			if (kind == itemFilterAll || string(item.Type) == string(kind)) && matchesLocalQuery(item, query) {
				visible = append(visible, item)
			}
		}
		return visible
	}, slices.Equal)
	listEntries := core.NewMemoFunc(cx, func() []listEntry {
		return projectEntries(filteredItems.Get())
	}, slices.Equal)
	kpis := core.NewMemo(cx, func() kpiCounts { return workbenchKPIs(props.data()) })

	// 确认框与详情必须读取同一份、未经过列表筛选的最新选中事项。
	selectedItem := core.NewMemo(cx, func() domain.Item {
		if key := selectedKey.Get(); key != "" {
			for _, item := range props.data().Items {
				if item.Key() == key {
					return item
				}
			}
		}
		return domain.Item{}
	}).Get
	actions := newItemActionController(cx, props, selectedItem)
	return core.View{
		TestID: "workbench",
		Style: core.Style{
			FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0), Gap: 18,
		},
		StyleFunc: workbenchCanvas(cx),
		Children: []core.Node{
			workbenchHeader(cx, props, settingsOpen, notifyHistoryOpen),
			settingsDialog(cx, props, settingsOpen),
			notificationHistoryDialog(cx, props.NotifyHistory, notifyHistoryOpen),
			commandDialog(cx, props, settingsOpen, notifyHistoryOpen, changesOnly, paletteOpen),
			actions.confirmDialog(cx),
			browse.overview(cx, props.data),
			core.Show{
				When: func() bool { return strings.TrimSpace(props.data().ErrorMessage) != "" },
				Then: func(cx *core.Scope) core.Node {
					return errorBanner(cx, func() string { return props.data().ErrorMessage })
				},
			},
			workbenchSummary(cx, kpis.Get),
			core.Show{
				When: func() bool { return props.data().NeedsLogin },
				Then: func(cx *core.Scope) core.Node { return loginPanel(cx, props) },
				Else: func(cx *core.Scope) core.Node {
					return workbenchBody(cx, props, listEntries.Get, selection, onSelectionChange, selectedKey, filter, changesOnly, actions, browse)
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
	itemFilterAll   itemFilter = "all"
	itemFilterTask  itemFilter = "task"
	itemFilterStory itemFilter = "story"
	itemFilterBug   itemFilter = "bug"
)

func workbenchFilters(cx *core.Scope, filter *core.Signal[itemFilter], changesOnly *core.Signal[bool]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 16, Wrap: core.Wrap}, Children: []core.Node{
		ui.Tabs[itemFilter]{
			Label: "按类型筛选", Value: filter.Get, OnChange: filter.Set,
			Options: []ui.Option[itemFilter]{
				{Value: itemFilterAll, Label: "全部", TestID: "filter-all"},
				{Value: itemFilterTask, Label: "任务", TestID: "filter-task"},
				{Value: itemFilterStory, Label: "需求", TestID: "filter-story"},
				{Value: itemFilterBug, Label: "Bug", TestID: "filter-bug"},
			},
		},
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
			ui.Switch{Label: "仅看本轮变化", TestID: "changes-only", Checked: changesOnly.Get, OnChange: changesOnly.Set},
			core.Text{Content: "仅看本轮变化", Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
		}},
	}}
}

func workbenchBody(cx *core.Scope, props WorkbenchProps, entries func() []listEntry, selection *ui.Selection, onSelectionChange func(*ui.Selection), selectedKey *core.Signal[string], filter *core.Signal[itemFilter], changesOnly *core.Signal[bool], actions *itemActionController, browse *localBrowse) core.Node {
	selectedItem := actions.selected
	closeDetail := func() {
		selection.Clear()
		selectedKey.Set("")
	}
	cx.Effect(func() {
		current := selectedItem()
		if current.ID == "" && selectedKey.Get() != "" {
			closeDetail()
		}
	})
	wide := core.NewMemo(cx, func() bool { return cx.Env().Window().Width >= 900 })
	emptyMessage := func() string {
		if len(props.data().Items) == 0 {
			return "暂无指派给我的事项"
		}
		return "没有符合当前筛选条件的记录"
	}
	initialLoad := func() bool {
		data := props.data()
		return data.Loading && data.LastSuccess.IsZero() && len(data.Items) == 0
	}
	return core.Dynamic(func(cx *core.Scope) core.Node {
		list := workbenchList(cx, entries, selection, onSelectionChange, emptyMessage, initialLoad, filter, changesOnly, browse, props.data)
		if !wide.Get() {
			return core.View{
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
				Children: []core.Node{
					list,
					itemDetailDialog(cx, selectedItem, props.OnOpenURL, closeDetail, actions),
				},
			}
		}
		return core.View{
			Style: core.Style{Direction: core.Row, Gap: 16, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
			Children: []core.Node{
				list,
				core.Show{
					When: func() bool { return selectedItem().ID != "" },
					Then: func(cx *core.Scope) core.Node {
						return itemDetailPane(cx, selectedItem, props.OnOpenURL, actions, closeDetail)
					},
				},
			},
		}
	})
}

func workbenchList(cx *core.Scope, entries func() []listEntry, selection *ui.Selection, onSelectionChange func(*ui.Selection), emptyMessage func() string, loading func() bool, filter *core.Signal[itemFilter], changesOnly *core.Signal[bool], browse *localBrowse, data func() WorkbenchData) core.Node {
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
			browse.controls(cx),
			core.Text{ContentFunc: func() string {
				scope := snapshotScope(data())
				if project := browse.project.Get(); project != "" {
					scope += " · 项目：" + project
				}
				if q := browse.query.Get(); q != "" {
					scope += " · 搜索：" + q
				}
				return scope
			}, MaxLines: 2, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},

			ui.List{
				TestID: "item-list", Label: "禅道待办事项", Selection: selection, OnSelectionChange: onSelectionChange,
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
				Empty: core.Dynamic(func(cx *core.Scope) core.Node {
					if loading() {
						return loadingState(cx)
					}
					return core.View{TestID: "empty-state", Style: core.Style{Padding: core.All(24), Gap: 10, AlignItems: core.AlignCenter, CornerRadius: 8}, StyleFunc: func() core.Style {
						return core.Style{Background: colors.Inset(), BorderWidth: 1, BorderColor: colors.Border()}
					}, Children: []core.Node{
						illustration("empty-list-art", inboxAtRest),
						core.Text{ContentFunc: emptyMessage, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
					}}
				}),
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)},
				Row: func(rowCx *core.Scope, row core.ListRow) core.Node {
					current := core.NewMemo(rowCx, func() listEntry {
						rows, i := entries(), row.Index()
						if i < 0 || i >= len(rows) {
							return listEntry{}
						}
						return rows[i]
					})
					return core.Dynamic(func(cx *core.Scope) core.Node {
						entry := current.Get()
						if entry.header {
							return projectGroupHeader(cx, entry)
						}
						item := entry.item
						if item.ID == "" {
							return core.View{}
						}
						return core.View{Style: core.Style{Gap: 4, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
							core.Text{Content: item.Title, MaxLines: 2, Style: ui.TypeLabel},
							core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Wrap: core.Wrap}, Children: []core.Node{
								typePill(cx, item.Type),
								core.Text{Content: "#" + item.ID, Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono}), StyleFunc: ui.Colored(colors.Muted)},
								statusBadge(func() string { return item.Status }),
								core.Text{Content: formatUpdatedAt(item.UpdatedAt), MaxLines: 1, Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono}), StyleFunc: ui.Colored(colors.Muted)},
							}},
						}}
					})
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
	var order []string
	groups := make(map[string][]listEntry)
	for _, item := range items {
		project := projectName(item.Project)
		if _, exists := groups[project]; !exists {
			order = append(order, project)
		}
		groups[project] = append(groups[project], listEntry{item: item})
	}
	out := make([]listEntry, 0, len(items)+len(order))
	for _, project := range order {
		rows := groups[project]
		out = append(out, listEntry{header: true, name: project, count: len(rows)})
		out = append(out, rows...)
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
		TestID: "project-group-" + entry.name,
		Style:  core.Style{Direction: core.Row, AlignItems: core.AlignBaseline, Gap: 8, Padding: core.Edges{Top: core.Points(4), Bottom: core.Points(2)}, MinWidth: core.Pt(0)},
		Children: []core.Node{
			monoCap(cx, entry.name, colors.Muted),
			core.Text{ContentFunc: func() string { return fmt.Sprintf("%d 项", entry.count) }, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Faint)},
		},
	}
}

// statusBadge 是行内状态胶囊：语义色由状态映射（进行中钴蓝、活跃 Bug
// 红、待处理琥珀、完结绿），文本保持禅道原词。
func statusBadge(status func() string) core.Node {
	return ui.Badge{
		TextFunc: status,
		VariantFunc: func() ui.BadgeVariant {
			switch strings.ToLower(strings.TrimSpace(status())) {
			case "doing":
				return ui.BadgeAccent
			case "active":
				return ui.BadgeDanger
			case "wait":
				return ui.BadgeWarn
			case "closed", "done", "completed", "cancelled", "resolved", "fixed":
				return ui.BadgeSuccess
			}
			return ui.BadgeNeutral
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

// loadingState 是列表的加载骨架屏：几行脉冲占位块加一句说明，比单一
// 文案更接近加载完成后的列表形态。
func loadingState(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	rows := make([]core.Node, 0, 4)
	for i := range 4 {
		rows = append(rows, core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10},
			Children: []core.Node{
				ui.SkeletonBlock{Width: core.Pt(44), Height: core.Pt(16), Index: i},
				ui.SkeletonBlock{Width: core.Pct(float32(78 - i*14)), Height: core.Pt(12), Index: i + 1},
			},
		})
	}
	return core.View{
		TestID: "loading-state",
		Style:  core.Style{Padding: core.Symmetric(10, 12), Gap: 10, CornerRadius: ui.RadiusControl},
		StyleFunc: func() core.Style {
			return core.Style{Background: colors.Inset(), BorderWidth: 1, BorderColor: colors.Border()}
		},
		Children: append(rows, core.Text{Content: "正在加载禅道数据…", Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Accent)}),
	}
}

func typePill(cx *core.Scope, itemType domain.ItemType) core.Node {
	tone := itemTone(itemType)
	return core.View{
		Style: core.Style{Padding: core.Symmetric(2, 8), CornerRadius: 6, BorderWidth: 1, FlexShrink: 0},
		StyleFunc: func() core.Style {
			return core.Style{Background: core.RGB(tone.fill), BorderColor: core.RGB(tone.line)}
		},
		Children: []core.Node{core.Text{
			Content: itemTypeLabel(itemType), Style: ui.TypeCaption,
			StyleFunc: func() core.Style { return core.Style{Color: core.RGB(tone.ink)} },
		}},
	}
}

type kpiCounts struct {
	tasks, bugs, stories, added, statusChanges int
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
		case domain.ItemStory:
			if !finishedStatus(item.Status) {
				counts.stories++
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
func workbenchHeader(cx *core.Scope, props WorkbenchProps, settingsOpen, notifyHistoryOpen *core.Signal[bool]) core.Node {
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

	headerActions := []core.Node{}
	if props.NotifyHistory != nil {
		headerActions = append(headerActions,
			ui.Button{
				Text: "报表", TestID: "open-report", Variant: ui.ButtonOutline,
				Icon: lucide.ChartLine, OnPress: func() {
					if props.OnOpenReport != nil {
						props.OnOpenReport()
					}
				},
			},
			ui.Button{
				Text: "通知", TestID: "open-notify-history", Variant: ui.ButtonOutline,
				Icon: lucide.Bell, OnPress: func() { notifyHistoryOpen.Set(true) },
			})
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
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Wrap: core.Wrap}, Children: append([]core.Node{
				settings,
				refresh,
			}, headerActions...)},
		},
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

func workbenchSummary(cx *core.Scope, kpis func() kpiCounts) core.Node {
	colors := ui.ColorsOf(cx)
	group := func(nodes ...core.Node) core.Node {
		return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, Wrap: core.Wrap, MinWidth: core.Pt(0), FlexShrink: 1}, Children: nodes}
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Wrap: core.Wrap, Gap: 12, Padding: core.Symmetric(10, 16)}, StyleFunc: func() core.Style { return paperSurface(cx) }, Children: []core.Node{
		group(ui.Tooltip{Text: "当前指派给我、尚未完结的事项", Trigger: monoCap(cx, "我的待办", colors.Muted)},
			core.Text{TestID: "kpi-open-value", ContentFunc: func() string { k := kpis(); return strconv.Itoa(k.tasks + k.stories + k.bugs) }, Style: ui.TypeHeading, StyleFunc: ui.Colored(colors.Accent)},
			anchorStat(cx, "未完成任务", func() int { return kpis().tasks }, "kpi-tasks-value"),
			anchorStat(cx, "需求", func() int { return kpis().stories }, "kpi-stories-value"),
			anchorStat(cx, "未解决 Bug", func() int { return kpis().bugs }, "kpi-bugs-value")),
		group(ui.Tooltip{Text: "相对上次刷新的变化；可用「仅看本轮变化」查看明细", Trigger: monoCap(cx, "本轮动态", colors.Muted)},
			anchorStat(cx, "新增", func() int { return kpis().added }, "kpi-new-value"),
			anchorStat(cx, "状态变化", func() int { return kpis().statusChanges }, "kpi-status-value")),
	}}
}

func anchorStat(cx *core.Scope, label string, value func() int, testID string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignEnd, Gap: 5}, Children: []core.Node{
		core.Text{ContentFunc: func() string { return strconv.Itoa(value()) }, TestID: testID, Style: ui.TypeTitle.Merge(core.Style{FontSize: 15, LineHeight: 20})},
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
	switch itemType {
	case domain.ItemBug:
		return "Bug"
	case domain.ItemStory:
		return "需求"
	}
	return "任务"
}
