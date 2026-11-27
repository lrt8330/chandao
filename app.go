// Command chandao is an NSGo app.
package main

import (
	"fmt"

	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// Root is the app's first screen.
func Root(cx *core.Scope) core.Node {
	count := core.NewSignal(cx, 0)
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, Padding: core.All(24), PaddingEnv: core.EnvAll(core.EnvSafeArea), Gap: 16}, Children: []core.Node{
		core.Text{Content: "chandao", Style: core.Style{FontSize: 28, FontWeight: core.FontWeightBold}},
		core.Text{ContentFunc: func() string { return fmt.Sprintf("Pressed %d times", count.Get()) }},
		base.Button{OnPress: func(core.PressEvent) { count.Update(func(n int) int { return n + 1 }) }, Label: "Press me",
			Style: core.Style{Background: core.RGB(0x0969da), CornerRadius: 8, Padding: core.Symmetric(10, 16),
				AlignSelf: core.AlignStart, Active: &core.Style{Opacity: core.Alpha(0.8)}},
			Children: []core.Node{core.Text{Content: "Press me", Style: core.Style{Color: core.RGB(0xffffff)}}}},
	}}
}

func main() { nsgo.Run(Root) }
