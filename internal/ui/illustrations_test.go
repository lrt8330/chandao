package ui

import (
	"math"
	"testing"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestLoginIllustrationLeavesRoomForForm(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{NeedsLogin: true}}, nsgotest.Size(1000, 800))
	art := app.Find("id:login-art").Bounds()
	card := app.Find("id:login-card").Bounds()
	if art.Size.Height == 0 || art.Origin.X+art.Size.Width > card.Origin.X {
		t.Fatalf("login illustration overlaps form or has no height: art=%v form=%v", art, card)
	}
	app.Resize(1000, 600)
	if app.Exists("id:login-art") {
		t.Fatal("short window shows login illustration")
	}
	app.Resize(390, 844)
	if app.Exists("id:login-art") {
		t.Fatal("narrow window shows login illustration")
	}
	app.Find("id:login-submit")
}

func TestInboxIllustrationUsesListEmptyState(t *testing.T) {
	app := startWorkbench(t, WorkbenchProps{Data: WorkbenchData{Items: testItems()}})
	if app.Exists("id:empty-list-art") {
		t.Fatal("illustration visible above populated list")
	}
	app.Press("id:filter-story")
	app.Find("id:empty-list-art")
	app.Find("text:没有符合当前筛选条件的记录")
	app.Press("id:filter-task")
	if app.Exists("id:empty-list-art") {
		t.Fatal("illustration remains after clearing empty filter")
	}
}

func TestItemToneTextContrast(t *testing.T) {
	luminance := func(rgb uint32) float64 {
		linear := func(component uint32) float64 {
			v := float64(component&255) / 255
			if v <= 0.04045 {
				return v / 12.92
			}
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		return 0.2126*linear(rgb>>16) + 0.7152*linear(rgb>>8) + 0.0722*linear(rgb)
	}
	for _, kind := range []domain.ItemType{domain.ItemTask, domain.ItemStory, domain.ItemBug} {
		tone := itemTone(kind)
		ratio := (luminance(tone.fill) + 0.05) / (luminance(tone.ink) + 0.05)
		t.Logf("%s: %.2f:1", kind, ratio)
		if ratio < 4.5 {
			t.Errorf("%s chip text contrast %.2f:1, want at least 4.5:1", kind, ratio)
		}
	}
}
