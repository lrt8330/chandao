package ui

import "github.com/nsgo-dev/nsgo/core"

const (
	background  = 0xF5F8FC
	surface     = 0xFFFFFF
	primary     = 0x2563EB
	primaryDark = 0x1D4ED8
	text        = 0x172B4D
	muted       = 0x5E7184
	border      = 0xDCE6F2
	success     = 0x16A34A
	warning     = 0xD97706
	danger      = 0xDC2626
)

func workbenchTheme() *core.Theme {
	theme := core.DefaultTheme().Clone()
	applyWorkbenchPalette(&theme.Light)
	applyWorkbenchPalette(&theme.Dark)
	return theme
}

func applyWorkbenchPalette(palette *core.Palette) {
	palette.Background = core.RGB(background)
	palette.Surface = core.RGB(surface)
	palette.SurfaceHover = core.RGB(text).Faded(0.05)
	palette.SurfacePressed = core.RGB(text).Faded(0.09)
	palette.Border = core.RGB(border)
	palette.Text = core.RGB(text)
	palette.TextMuted = core.RGB(muted)
	palette.Accent = core.RGB(primary)
	palette.AccentHover = core.RGB(primaryDark)
	palette.AccentPressed = core.RGB(primaryDark)
	palette.AccentText = core.RGB(surface)
	palette.Success = core.RGB(success)
	palette.Warning = core.RGB(warning)
	palette.Danger = core.RGB(danger)
	palette.Selection = core.RGBA(primary, 0.12)
	palette.Focus = core.RGBA(primary, 0.55)
	palette.Scrollbar = core.RGB(muted).Faded(0.3)

	palette.SetColor("inset", core.RGB(background))
	palette.SetColor("raised", core.RGB(surface))
	palette.SetColor("field", core.RGB(surface))
	palette.SetColor("fieldBorder", core.RGB(border))
	palette.SetColor("line", core.RGB(border))
	palette.SetColor("faint", core.RGB(muted).Faded(0.7))
	palette.SetColor("accentFill", core.RGB(primary))
	palette.SetColor("light", core.RGB(text))
	palette.SetColor("onLight", core.RGB(surface))
	palette.SetColor("track", core.RGB(border))
	palette.SetColor("knob", core.RGB(surface))
	palette.SetColor("code", core.RGB(text))
	palette.SetColor("info", core.RGB(primary))
	palette.SetColor("down", core.RGB(danger))
}
