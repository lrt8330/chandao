package ui

import (
	"fmt"
	"strings"

	"chandao/zentaotray/internal/infrastructure/notifyhistory"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// 折线图配色：任务=钴蓝、Bug=陶红，网格与文字用云灰。
const (
	chartGridColor = "#E2E7ED"
	chartTextColor = "#657383"
)

// trendChart 渲染待办总量演变折线图（任务/Bug 双线），少于 2 个快照时
// 显示空态文案。数据点等距排布，数值按最大值归一化。
func trendChart(cx *core.Scope, points []notifyhistory.SnapshotPoint) core.Node {
	if len(points) < 2 {
		return emptyChartState(cx, "快照数据积累中，刷新几次后即可查看趋势")
	}
	width, height := float64(reportChartWidth), float64(reportChartHeight)
	marginL, marginR, marginT, marginB := 52.0, 16.0, 14.0, 30.0
	plotW := width - marginL - marginR
	plotH := height - marginT - marginB

	maxValue := 1.0
	for _, point := range points {
		if float64(point.TasksTotal) > maxValue {
			maxValue = float64(point.TasksTotal)
		}
		if float64(point.BugsActive) > maxValue {
			maxValue = float64(point.BugsActive)
		}
	}
	// 刻度上限取整到千/百量级，保证标签可读。
	ceiling := niceCeiling(maxValue)

	var grid strings.Builder
	for fraction := 0.0; fraction <= 1.0; fraction += 0.25 {
		y := marginT + plotH*fraction
		fmt.Fprintf(&grid, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1"/>`,
			marginL, y, width-marginR, y, chartGridColor)
		label := fmt.Sprintf("%.0f", ceiling*(1-fraction))
		fmt.Fprintf(&grid, `<text x="%.1f" y="%.1f" font-size="11" fill="%s" text-anchor="end">%s</text>`,
			marginL-6, y+4, chartTextColor, label)
	}

	chartTaskColor := fmt.Sprintf("#%06X", cobaltDark)
	chartBugColor := fmt.Sprintf("#%06X", bugInk)
	var taskLine, bugLine, dots strings.Builder
	last := len(points) - 1
	for index, point := range points {
		x := marginL + plotW*(float64(index)/float64(last))
		taskY := marginT + plotH*(1-float64(point.TasksTotal)/ceiling)
		bugY := marginT + plotH*(1-float64(point.BugsActive)/ceiling)
		fmt.Fprintf(&taskLine, "%.1f,%.1f ", x, taskY)
		fmt.Fprintf(&bugLine, "%.1f,%.1f ", x, bugY)
		if index == last {
			fmt.Fprintf(&dots, `<circle cx="%.1f" cy="%.1f" r="3.5" fill="%s"/><circle cx="%.1f" cy="%.1f" r="3.5" fill="%s"/>`,
				x, taskY, chartTaskColor, x, bugY, chartBugColor)
		}
	}

	firstLabel := points[0].Time.Local().Format("01-02 15:04")
	lastLabel := points[last].Time.Local().Format("01-02 15:04")

	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%[1]d" height="%[2]d" viewBox="0 0 %[1]d %[2]d">`+
			grid.String()+
			`<text x="%[3]d" y="%[4]d" font-size="11" fill="%[5]s">%[6]s</text>`+
			`<text x="%[7]d" y="%[8]d" font-size="11" fill="%[5]s" text-anchor="end">%[9]s</text>`+
			`<polyline points="%[10]s" fill="none" stroke="%[11]s" stroke-width="2"/>`+
			`<polyline points="%[12]s" fill="none" stroke="%[13]s" stroke-width="2"/>`+
			dots.String()+
			`</svg>`,
		int(width), int(height),
		int(marginL), int(height-8), chartTextColor, firstLabel,
		int(width-marginR), int(height-8), lastLabel,
		strings.TrimRight(bugLine.String(), " "), chartBugColor,
		strings.TrimRight(taskLine.String(), " "), chartTaskColor,
	)

	parsed, err := core.ParseSVG([]byte(svg))
	if err != nil {
		return emptyChartState(cx, "图表数据异常")
	}
	return core.View{Style: core.Style{Gap: 8}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14}, Children: []core.Node{
			chartLegend(cx, chartTaskColor, "任务"),
			chartLegend(cx, chartBugColor, "Bug"),
		}},
		core.View{Style: core.Style{JustifyContent: core.JustifyCenter, AlignItems: core.AlignCenter}, Children: []core.Node{
			core.Icon{SVG: parsed, Label: "待办总量演变折线图", Style: core.Style{FontSize: reportChartHeight}},
		}},
	}}
}

func hexDigit(char rune) uint32 {
	switch {
	case char >= '0' && char <= '9':
		return uint32(char - '0')
	case char >= 'a' && char <= 'f':
		return uint32(char-'a') + 10
	case char >= 'A' && char <= 'F':
		return uint32(char-'A') + 10
	}
	return 0
}

// niceCeiling 把最大值向上取整到可读刻度。
func niceCeiling(value float64) float64 {
	if value <= 0 {
		return 1
	}
	step := 1.0
	for value > step*10 {
		step *= 10
	}
	for value > step {
		step += step / 2
	}
	return step
}

func chartLegend(cx *core.Scope, colorHex, label string) core.Node {
	color := mustColor(colorHex)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
		core.View{Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), CornerRadius: ui.RadiusFull}, StyleFunc: func() core.Style {
			return core.Style{Background: color}
		}},
		core.Text{Content: label, Style: ui.TypeCaption, StyleFunc: ui.Colored(ui.ColorsOf(cx).Muted)},
	}}
}

// mustColor 解析 #RRGGBB 十六进制颜色。
func mustColor(hex string) core.Color {
	var value uint32
	for _, char := range strings.TrimPrefix(hex, "#") {
		value = value*16 + hexDigit(char)
	}
	return core.RGB(value)
}

// eventBars 按天渲染新增/状态变化的双段横条统计，最多展示最近 14 天。
func eventBars(cx *core.Scope, stats []notifyhistory.EventStatsRow) core.Node {
	colors := ui.ColorsOf(cx)
	if len(stats) == 0 {
		return emptyChartState(cx, "还没有变化事件，检出变化后会按天统计")
	}
	rows := stats
	if len(rows) > reportMaxDays {
		rows = rows[len(rows)-reportMaxDays:]
	}
	maxPerDay := 1
	for _, row := range rows {
		if row.New+row.Status > maxPerDay {
			maxPerDay = row.New + row.Status
		}
	}
	children := make([]core.Node, 0, len(rows))
	for _, row := range rows {
		newWidth := core.Pct(float32(float64(row.New) / float64(maxPerDay) * 100))
		statusWidth := core.Pct(float32(float64(row.Status) / float64(maxPerDay) * 100))
		children = append(children, core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, MinWidth: core.Pt(0)},
			Children: []core.Node{
				core.Text{Content: row.Day[5:], Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono, Width: core.Pt(46), FlexShrink: 0}), StyleFunc: ui.Colored(colors.Muted)},
				core.View{Style: core.Style{FlexGrow: 1, Direction: core.Row, Gap: 2, MinWidth: core.Pt(0)}, Children: []core.Node{
					barSegment(cx, row.New, newWidth, "新增", core.RGB(cobaltSoft), core.RGB(cobaltDark)),
					barSegment(cx, row.Status, statusWidth, "状态", core.RGB(bugSoftFill), core.RGB(cloudDanger)),
					zeroBarFiller(cx, row.New+row.Status),
				}},
				core.Text{Content: fmt.Sprintf("+%d", row.New+row.Status), Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono, Width: core.Pt(40), FlexShrink: 0}), StyleFunc: ui.Colored(colors.Muted)},
			},
		})
	}
	legend := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14}, Children: []core.Node{
		chartLegend(cx, "#3157C7", "新增"),
		chartLegend(cx, "#BD5548", "状态变化"),
	}}
	return core.View{Style: core.Style{Gap: 10}, Children: append([]core.Node{legend}, children...)}
}

func barSegment(cx *core.Scope, count int, width core.Length, label string, fill, ink core.Color) core.Node {
	if count == 0 {
		return core.View{Style: core.Style{Width: core.Pt(0)}}
	}
	return core.View{
		Style:     core.Style{Height: core.Pt(18), Width: width, CornerRadius: 4, AlignItems: core.AlignCenter, Padding: core.Symmetric(0, 6), FlexShrink: 0},
		StyleFunc: func() core.Style { return core.Style{Background: fill} },
		Children: []core.Node{core.Text{Content: fmt.Sprintf("%s %d", label, count), MaxLines: 1, Style: ui.TypeCaption, StyleFunc: func() core.Style {
			return core.Style{Color: ink}
		}}},
	}
}

func zeroBarFiller(cx *core.Scope, count int) core.Node {
	if count != 0 {
		return core.View{Style: core.Style{FlexGrow: 1}}
	}
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter}, Children: []core.Node{
		core.Text{Content: "0", Style: ui.TypeCaption, StyleFunc: ui.Colored(ui.ColorsOf(cx).Muted)},
	}}
}

// eventList 事件明细行列表。
func eventList(cx *core.Scope, entries []notifyhistory.Entry) core.Node {
	colors := ui.ColorsOf(cx)
	if len(entries) == 0 {
		return emptyChartState(cx, "当前筛选条件下没有匹配的事件")
	}
	rows := make([]core.Node, 0, len(entries))
	for _, entry := range entries {
		entry := entry
		transition := ""
		if entry.Kind == "status" && entry.OldStatus != "" {
			transition = fmt.Sprintf("%s → %s", entry.OldStatus, entry.NewStatus)
		}
		kindLabel := "新增"
		if entry.Kind == "status" {
			kindLabel = "状态变化"
		}
		rows = append(rows, core.View{
			Style: core.Style{Gap: 4, Padding: core.Symmetric(8, 10), CornerRadius: ui.RadiusControl, MinWidth: core.Pt(0)},
			StyleFunc: func() core.Style {
				return core.Style{Background: colors.Inset(), BorderWidth: 1, BorderColor: colors.Border()}
			},
			Children: []core.Node{
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, MinWidth: core.Pt(0)}, Children: []core.Node{
					core.Text{Content: kindLabel, Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono}), StyleFunc: ui.Colored(colors.Accent)},
					core.Text{ContentFunc: func() string { return entry.Title }, MaxLines: 1, Style: ui.TypeLabel.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)})},
					core.Text{ContentFunc: func() string { return entry.Time.Local().Format("01-02 15:04") }, Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono}), StyleFunc: ui.Colored(colors.Muted)},
				}},
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, Wrap: core.Wrap, MinWidth: core.Pt(0)}, Children: []core.Node{
					core.Text{ContentFunc: func() string {
						if transition != "" {
							return "状态 " + transition
						}
						return entry.Project
					}, MaxLines: 1, Style: ui.TypeCaption.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}), StyleFunc: ui.Colored(colors.Muted)},
					core.Text{ContentFunc: func() string { return fmt.Sprintf("%s %s", entry.ItemType, entry.ItemID) }, Style: ui.TypeCaption.Merge(core.Style{FontFamily: Mono}), StyleFunc: ui.Colored(colors.Faint)},
				}},
			},
		})
	}
	return core.View{Style: core.Style{Gap: 8}, Children: rows}
}

func emptyChartState(cx *core.Scope, message string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "report-empty", Style: core.Style{Padding: core.All(28), Gap: 10, AlignItems: core.AlignCenter}, Children: []core.Node{
		core.Icon{SVG: lucide.ChartLine(), Label: "", Style: core.Style{FontSize: 24}, StyleFunc: ui.Colored(colors.Faint)},
		core.Text{Content: message, Style: ui.TypeBody, StyleFunc: ui.Colored(colors.Muted)},
	}}
}
