package ui

import (
	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/core"
)

// 云白钴蓝主题，对齐 imgSaas /tv 现行 cloud-cobalt 皮肤的颜色角色：
// 云白画布、白卡描边、钴蓝主操作与软芯片、小字等宽代号、衬线大数字。
// 参考皮肤声明 color-scheme: light，因此这里把浅色调色板同时应用到
// Light 和 Dark 外观，应用不随系统深色模式变黑。
// 大数字与代号的字体族在此统一定义，供各视图取用。
const (
	cloudCanvas      = 0xF5F7FA
	cloudSurface     = 0xFFFFFF
	cloudSoft        = 0xF8FAFC
	cloudInk         = 0x263342
	cloudMuted       = 0x657383
	cloudDim         = 0x8A97A6
	cloudLine        = 0xE2E7ED
	cloudLineHard    = 0xCBD5E1
	cobalt           = 0x4169E1
	cobaltDark       = 0x3157C7
	cobaltSoft       = 0xEDF1FF
	cobaltChipLine   = 0xB8C6F8
	cloudSuccess     = 0x2D8A63
	cloudSuccessSoft = 0xE8F6EF
	cloudWarning     = 0xA8662C
	cloudDanger      = 0xBD5548
	bugInk           = 0xA44837
	bugSoftFill      = 0xFBECE6
	bugSoftLine      = 0xD9AA99
	storyInk         = 0x216B60
	storySoftFill    = 0xE3F4EF
	storySoftLine    = 0x97CABE

	// Serif 是大数字与金额的衬线族；Mono 是 kicker、代号与时间戳的等宽族。
	Serif = "Georgia, serif"
	Mono  = core.FontMonospace
)

type chipTone struct{ fill, line, ink uint32 }

func itemTone(kind domain.ItemType) chipTone {
	switch kind {
	case domain.ItemStory:
		return chipTone{storySoftFill, storySoftLine, storyInk}
	case domain.ItemBug:
		return chipTone{bugSoftFill, bugSoftLine, bugInk}
	default:
		return chipTone{cobaltSoft, cobaltChipLine, cobaltDark}
	}
}

func workbenchTheme() *core.Theme {
	theme := core.DefaultTheme().Clone()
	applyWorkbenchPalette(&theme.Light)
	applyWorkbenchPalette(&theme.Dark)
	return theme
}

func applyWorkbenchPalette(palette *core.Palette) {
	palette.Background = core.RGB(cloudCanvas)
	palette.Surface = core.RGB(cloudSurface)
	palette.SurfaceHover = core.RGB(cobaltSoft)
	palette.SurfacePressed = core.RGB(0xDBE3FF)
	palette.Border = core.RGB(cloudLine)
	palette.Text = core.RGB(cloudInk)
	palette.TextMuted = core.RGB(cloudMuted)
	palette.Accent = core.RGB(cobalt)
	palette.AccentHover = core.RGB(cobaltDark)
	palette.AccentPressed = core.RGB(cobaltDark)
	palette.AccentText = core.RGB(cloudSurface)
	palette.Success = core.RGB(cloudSuccess)
	palette.Warning = core.RGB(cloudWarning)
	palette.Danger = core.RGB(cloudDanger)
	palette.Selection = core.RGBA(cobalt, 0.12)
	palette.Focus = core.RGBA(cobalt, 0.45)
	palette.Scrollbar = core.RGB(cloudMuted).Faded(0.3)

	palette.SetColor("inset", core.RGB(cloudSoft))
	palette.SetColor("raised", core.RGB(cloudSurface))
	palette.SetColor("field", core.RGB(cloudSoft))
	palette.SetColor("fieldBorder", core.RGB(cloudLine))
	palette.SetColor("line", core.RGB(cloudLine))
	palette.SetColor("faint", core.RGB(cloudDim))
	palette.SetColor("accentFill", core.RGB(cobalt))
	palette.SetColor("light", core.RGB(cobalt))
	palette.SetColor("onLight", core.RGB(cloudSurface))
	palette.SetColor("track", core.RGBA(cloudInk, 0.14))
	palette.SetColor("knob", core.RGB(cloudSurface))
	palette.SetColor("code", core.RGB(cloudInk))
	palette.SetColor("info", core.RGB(cobaltDark))
	palette.SetColor("down", core.RGB(bugInk))
}
