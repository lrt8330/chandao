package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Workbench renders the Zentao dashboard and owns its local filters and dialogs.
func Workbench(cx *core.Scope, props WorkbenchProps) core.Node {
	cx.SetTheme(workbenchTheme())
	colors := ui.ColorsOf(cx)
	filter := core.NewSignal(cx, itemFilterAll)
	changesOnly := core.NewSignal(cx, false)
	settingsOpen := core.NewSignal(cx, false)
	selection := ui.NewSelection(cx, false)
	selectedKey := props.SelectedKey
	if selectedKey == nil {
		selectedKey = core.NewSignal(cx, "")
	}
	onSelectionChange := func(selection *ui.Selection) {
		keys := selection.Keys()
		if len(keys) == 0 {
			selectedKey.Set("")
			return
		}
		selectedKey.Set(keys[0].(string))
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
	kpis := core.NewMemo(cx, func() kpiCounts { return workbenchKPIs(props.data()) })

	return core.View{
		TestID: "workbench",
		Style: core.Style{
			FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0),
			Padding: core.All(24), Gap: 20,
		},
		StyleFunc: ui.Filled(colors.Background),
		Children: []core.Node{
			workbenchHeader(cx, props, settingsOpen),
			settingsDialog(cx, props, settingsOpen),
			core.Show{
				When: func() bool { return strings.TrimSpace(props.data().ErrorMessage) != "" },
				Then: func(cx *core.Scope) core.Node {
					return errorBanner(cx, func() string { return props.data().ErrorMessage })
				},
			},
			core.View{
				Style: core.Style{Display: core.DisplayGrid, Gap: 12},
				StyleFunc: func() core.Style {
					columns := "repeat(4, minmax(0, 1fr))"
					if cx.Env().Window().Width < 720 {
						columns = "repeat(2, minmax(0, 1fr))"
					}
					return core.Style{GridTemplateColumns: core.GridTracks(columns)}
				},
				Children: []core.Node{
					kpiCard(cx, "未完成任务", func() int { return kpis.Get().tasks }, "kpi-tasks"),
					kpiCard(cx, "未解决 Bug", func() int { return kpis.Get().bugs }, "kpi-bugs"),
					kpiCard(cx, "本轮新增", func() int { return kpis.Get().added }, "kpi-new"),
					kpiCard(cx, "本轮状态变化", func() int { return kpis.Get().statusChanges }, "kpi-status"),
				},
			},
			core.Show{
				When: func() bool { return props.data().NeedsLogin },
				Then: func(cx *core.Scope) core.Node { return loginPanel(cx, props) },
				Else: func(cx *core.Scope) core.Node {
					return core.Group{
						workbenchFilters(cx, filter, changesOnly),
						workbenchBody(cx, props, filteredItems.Get, selection, onSelectionChange, selectedKey),
					}
				},
			},
		},
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

func workbenchBody(cx *core.Scope, props WorkbenchProps, items func() []domain.Item, selection *ui.Selection, onSelectionChange func(*ui.Selection), selectedKey *core.Signal[string]) core.Node {
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
		list := workbenchList(cx, items, selection, onSelectionChange, emptyMessage, func() bool { return props.data().Loading })
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
		Style:  core.Style{Width: core.Pt(380), FlexShrink: 0, Padding: core.All(20), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
		StyleFunc: func() core.Style {
			return ui.FinOf(cx).Card().Over(core.Style{Background: colors.Surface(), BorderWidth: 1, BorderColor: colors.Border(), CornerRadius: ui.RadiusCard})
		},
		Children: []core.Node{core.Text{Content: "选择一条记录查看详情", Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)}},
	}
}

func workbenchList(cx *core.Scope, items func() []domain.Item, selection *ui.Selection, onSelectionChange func(*ui.Selection), emptyMessage func() string, loading func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "list-panel",
		Style:  core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0), Padding: core.All(16), Gap: 12},
		StyleFunc: func() core.Style {
			return ui.FinOf(cx).Card().Over(core.Style{Background: colors.Surface(), BorderWidth: 1, BorderColor: colors.Border(), CornerRadius: ui.RadiusCard})
		},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween}, Children: []core.Node{
				core.Text{Content: "我的待办", Style: ui.TypeTitle},
				core.Text{ContentFunc: func() string { return fmt.Sprintf("%d 条记录", len(items())) }, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
			}},
			core.Show{When: loading, Then: func(cx *core.Scope) core.Node { return loadingState(cx) }},
			ui.List{
				TestID: "item-list", Label: "禅道任务和 Bug", Selection: selection, OnSelectionChange: onSelectionChange,
				Count: func() int { return len(items()) }, Key: func(i int) any { return items()[i].Key() },
				RowLabel: func(i int) string { return items()[i].Title }, EstimatedSize: 72, Gap: 6,
				Empty: core.View{TestID: "empty-state", Style: core.Style{Padding: core.All(24), Gap: 8, AlignItems: core.AlignCenter}, Children: []core.Node{
					core.Text{ContentFunc: emptyMessage, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
				}},
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)},
				Row: func(rowCx *core.Scope, row core.ListRow) core.Node {
					item := func() domain.Item { return items()[row.Index()] }
					return core.View{Style: core.Style{Gap: 6, MinWidth: core.Pt(0)}, Children: []core.Node{
						core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, MinWidth: core.Pt(0)}, Children: []core.Node{
							ui.Badge{TextFunc: func() string { return itemTypeLabel(item().Type) }, VariantFunc: func() ui.BadgeVariant {
								if item().Type == domain.ItemBug {
									return ui.BadgeDanger
								}
								return ui.BadgeAccent
							}},
							core.Text{ContentFunc: func() string { return item().Title }, MaxLines: 2, Style: ui.TypeLabel.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)})},
						}},
						core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Wrap: core.Wrap}, Children: []core.Node{
							core.Text{ContentFunc: func() string { return "项目：" + item().Project }, MaxLines: 1, Style: ui.TypeCaption.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}), StyleFunc: ui.Colored(colors.Muted)},
							core.Text{ContentFunc: func() string { return item().Status }, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Accent)},
							core.Text{ContentFunc: func() string { return "更新于 " + formatUpdatedAt(item().UpdatedAt) }, MaxLines: 1, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
						}},
					}}
				},
			},
		},
	}
}

func errorBanner(cx *core.Scope, message func() string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "error-banner", Role: core.RoleAlert,
		Style: core.Style{Padding: core.Symmetric(10, 14), Gap: 8, BorderWidth: 1, CornerRadius: ui.RadiusControl},
		StyleFunc: func() core.Style {
			return core.Style{Background: ui.Alpha(colors.Danger(), 0.08), BorderColor: ui.Alpha(colors.Danger(), 0.25)}
		},
		Children: []core.Node{core.Text{ContentFunc: message, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Danger)}},
	}
}

func loadingState(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID:   "loading-state",
		Style:    core.Style{Padding: core.Symmetric(8, 12), BorderWidth: 1, CornerRadius: ui.RadiusControl, Background: ui.Alpha(colors.Accent(), 0.06), BorderColor: ui.Alpha(colors.Accent(), 0.18)},
		Children: []core.Node{core.Text{Content: "正在加载禅道数据…", Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Accent)}},
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
			return "上次刷新：尚未刷新"
		}
		return "上次刷新：" + value.Local().Format("2006-01-02 15:04")
	}

	refresh := ui.Button{
		Text: "立即刷新", TestID: "refresh", Size: ui.SizeLarge,
		Busy: func() bool { return props.data().Loading }, BusyLabel: "刷新中…",
		Disabled: func() bool { return props.data().Loading || props.data().NeedsLogin },
	}
	if props.OnRefresh != nil {
		refresh.OnPress = props.OnRefresh
	}

	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 16, Wrap: core.Wrap},
		Children: []core.Node{
			core.View{Style: core.Style{Gap: 4, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
				core.Text{Content: "禅道助手", Role: core.RoleHeader, Style: ui.TypeHeading},
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Wrap: core.Wrap}, Children: []core.Node{
					core.Text{ContentFunc: func() string { return "账号：" + account() }, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
					core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5}, Children: []core.Node{
						core.View{Style: core.Style{Width: core.Pt(7), Height: core.Pt(7), CornerRadius: ui.RadiusFull}, StyleFunc: func() core.Style { return core.Style{Background: statusColor()} }},
						core.Text{ContentFunc: connection, Style: ui.TypeCaption, StyleFunc: ui.Colored(statusColor)},
					}},
					core.Text{ContentFunc: lastSuccess, Style: ui.TypeCaption, StyleFunc: ui.Colored(colors.Muted)},
				}},
			}},
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Wrap: core.Wrap}, Children: []core.Node{
				settingsButton(settingsOpen),
				refresh,
			}},
		},
	}
}

func kpiCard(cx *core.Scope, label string, value func() int, testID string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: testID,
		Style:  core.Style{Padding: core.All(16), Gap: 8, MinWidth: core.Pt(0)},
		StyleFunc: func() core.Style {
			return ui.FinOf(cx).Card().Over(core.Style{
				Background: colors.Surface(), BorderWidth: 1, BorderColor: colors.Border(),
				CornerRadius: ui.RadiusCard, BorderCurve: core.BorderCurveContinuous,
			})
		},
		Children: []core.Node{
			core.Text{Content: label, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
			core.Text{ContentFunc: func() string { return strconv.Itoa(value()) }, TestID: testID + "-value", Style: ui.TypeHeading.Merge(core.Style{FontSize: 28, LineHeight: 34}), StyleFunc: ui.Colored(colors.Text)},
		},
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
