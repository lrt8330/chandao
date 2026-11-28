package ui

import (
	"context"
	"strings"
	"time"

	"chandao/zentaotray/internal/infrastructure/notifyhistory"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	reportWindowWidth  = 760
	reportWindowHeight = 680

	reportChartWidth  = 600
	reportChartHeight = 220
	reportMaxPoints   = 200
	reportMaxDays     = 14
	reportMaxEvents   = 100
)

// ReportWindow 是独立窗体承载的报表：生命周期不受主窗口控制，重复打开
// 时只显示并聚焦已存在的窗体。
type ReportWindow struct {
	cx     *core.Scope
	store  *notifyhistory.Store
	window *desktop.Window
}

func NewReportWindow(cx *core.Scope, store *notifyhistory.Store) *ReportWindow {
	return &ReportWindow{cx: cx, store: store}
}

// Open 显示报表窗体：不存在则创建，已存在则显示并聚焦。Key 机制保证
// 同一时刻只有一个报表窗体。
func (w *ReportWindow) Open() {
	if w.window != nil && !w.window.Closed() {
		w.window.Show()
		w.window.Focus()
		return
	}
	w.window = desktop.OpenWindow(w.cx, desktop.WindowOptions{
		Key:    "report",
		Title:  "报表 · 禅道助手",
		Width:  reportWindowWidth,
		Height: reportWindowHeight,
	}, func(windowCx *core.Scope) core.Node {
		return reportWindowBody(windowCx, w.store, func() {
			if w.window != nil {
				w.window.Close()
			}
		})
	})
}

type reportRange string

const (
	reportRange7   reportRange = "7d"
	reportRange30  reportRange = "30d"
	reportRangeAll reportRange = "all"
)

func (r reportRange) window() time.Duration {
	switch r {
	case reportRange7:
		return 7 * 24 * time.Hour
	case reportRange30:
		return 30 * 24 * time.Hour
	}
	return 0 // 全部
}

func (r reportRange) label() string {
	switch r {
	case reportRange7:
		return "近 7 天"
	case reportRange30:
		return "近 30 天"
	}
	return "全部"
}

// reportWindowBody 是报表窗体的全部内容：标题行、工具行、趋势卡、变化
// 统计卡与事件明细卡。加载在组件构建与范围/类型/关键字变化时执行。
func reportWindowBody(cx *core.Scope, store *notifyhistory.Store, onClose func()) core.Node {
	rangeSignal := core.NewSignal(cx, reportRange7)
	entries := core.NewSignalFunc(cx, []notifyhistory.Entry(nil), func(a, b []notifyhistory.Entry) bool {
		return len(a) == len(b)
	})
	points := core.NewSignalFunc(cx, []notifyhistory.SnapshotPoint(nil), func(a, b []notifyhistory.SnapshotPoint) bool {
		return len(a) == len(b)
	})
	stats := core.NewSignalFunc(cx, []notifyhistory.EventStatsRow(nil), func(a, b []notifyhistory.EventStatsRow) bool {
		return len(a) == len(b)
	})
	keyword := core.NewSignal(cx, "")
	kindFilter := core.NewSignal(cx, "")

	load := func() {
		if store == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		window := rangeSignal.Get().window()
		var since time.Time
		if window > 0 {
			since = time.Now().Add(-window)
		}
		if list, err := store.Snapshots(ctx, since, reportMaxPoints); err == nil {
			points.Set(list)
		}
		if statsRows, err := store.EventStats(ctx, since); err == nil {
			if len(statsRows) > reportMaxDays {
				statsRows = statsRows[len(statsRows)-reportMaxDays:]
			}
			stats.Set(statsRows)
		}
		filter := notifyhistory.EventFilter{
			Since:   since,
			Kind:    kindFilter.Get(),
			Keyword: strings.TrimSpace(keyword.Get()),
			Limit:   reportMaxEvents,
		}
		if list, err := store.QueryEvents(ctx, filter); err == nil {
			entries.Set(list)
		}
	}
	setRange := func(next reportRange) {
		rangeSignal.Set(next)
		load()
	}
	setKeyword := func(text string) {
		keyword.Set(text)
		load()
	}
	setKind := func(kind string) {
		kindFilter.Set(kind)
		load()
	}
	// 组件随窗体挂载构建：无条件加载一次，保证打开时数据是最新的。
	load()

	return core.View{
		TestID: "report",
		Style: core.Style{
			FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0), Gap: 14,
		},
		StyleFunc: workbenchCanvas(cx),
		Children: []core.Node{
			// 标题行 + 工具行：范围 tabs、刷新、关闭
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, Wrap: core.Wrap}, Children: []core.Node{
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
					core.Icon{SVG: lucide.ChartLine(), Label: "", Style: core.Style{FontSize: 18}},
					core.View{Style: core.Style{Gap: 2}, Children: []core.Node{
						core.Text{Content: "报表", Style: ui.TypeTitle},
						core.Text{ContentFunc: func() string {
							return "待办跟踪与变化趋势 · 当前范围 " + rangeSignal.Get().label()
						}, Style: ui.TypeCaption},
					}},
				}},
				core.View{Style: core.Style{FlexGrow: 1}},
				rangeTabs(cx, rangeSignal.Get, setRange),
				ui.Button{
					Text: "刷新数据", TestID: "report-reload", Variant: ui.ButtonOutline,
					Icon: lucide.RefreshCw, OnPress: load,
				},
				ui.Button{
					Text: "关闭", TestID: "close-report", Variant: ui.ButtonOutline,
					OnPress: func() { onClose() },
				},
			}},
			ui.ScrollView{
				Style:        ui.FillRest,
				ContentStyle: core.Style{Gap: 12, Padding: core.Edges{Right: 6}},
				Children: []core.Node{
					reportCard(cx, "趋势 · 待办总量演变", lucide.Activity, []core.Node{
						core.Dynamic(func(cx *core.Scope) core.Node {
							return trendChart(cx, points.Get())
						}),
					}),
					reportCard(cx, "变化统计 · 按天", lucide.Activity, []core.Node{
						core.Dynamic(func(cx *core.Scope) core.Node {
							return eventBars(cx, stats.Get())
						}),
					}),
					reportCard(cx, "事件明细", lucide.Bell, []core.Node{
						core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Wrap: core.Wrap}, Children: []core.Node{
							kindTabs(cx, kindFilter.Get, setKind),
							core.View{Style: core.Style{FlexGrow: 1}},
							ui.Input{
								TestID: "report-keyword", Label: "搜索", Placeholder: "标题或编号",
								Value: keyword.Get, OnChange: setKeyword,
								Style: core.Style{Width: core.Pt(180)},
							},
						}},
						core.Dynamic(func(cx *core.Scope) core.Node {
							return eventList(cx, entries.Get())
						}),
					}),
				},
			},
		},
	}
}

// rangeTabs 用原生 Tabs 组件承载时间范围切换（按压处理可靠）。Value 用
// getter 而非快照值，保证切换后选中态跟随最新信号。
func rangeTabs(cx *core.Scope, current func() reportRange, onPick func(reportRange)) core.Node {
	return ui.Tabs[reportRange]{
		Label: "统计范围", Value: current,
		OnChange: func(next reportRange) { onPick(next) },
		Options: []ui.Option[reportRange]{
			{Value: reportRange7, Label: "近 7 天", TestID: "report-range-7d"},
			{Value: reportRange30, Label: "近 30 天", TestID: "report-range-30d"},
			{Value: reportRangeAll, Label: "全部", TestID: "report-range-all"},
		},
	}
}

// kindTabs 用原生 Tabs 组件承载事件类型筛选。Value 用 getter 而非快照
// 值，保证筛选后选中态跟随最新信号。
func kindTabs(cx *core.Scope, current func() string, onPick func(string)) core.Node {
	return ui.Tabs[string]{
		Label: "事件类型", Value: current,
		OnChange: func(next string) { onPick(next) },
		Options: []ui.Option[string]{
			{Value: "", Label: "全部", TestID: "report-kind-all"},
			{Value: "new", Label: "新增", TestID: "report-kind-new"},
			{Value: "status", Label: "状态变化", TestID: "report-kind-status"},
		},
	}
}

// reportCard 是报表里的白卡分区。
func reportCard(cx *core.Scope, title string, svg func() *core.SVG, children []core.Node) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Padding: core.Symmetric(14, 16), Gap: 10},
		StyleFunc: func() core.Style {
			style := dialogChrome(cx)
			style.CornerRadius = 12
			style.Shadows = []core.BoxShadow{cardShadow()}
			return style
		},
		Children: append([]core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
				core.Icon{SVG: svg(), Label: "", Style: core.Style{FontSize: 15}, StyleFunc: ui.Colored(colors.Accent)},
				core.Text{Content: title, Style: ui.TypeTitle},
			}},
		}, children...),
	}
}
