package ui

import (
	_ "embed"

	"github.com/nsgo-dev/nsgo/core"
)

//go:embed art/assignment-desk.svg
var assignmentDeskSource []byte

//go:embed art/inbox-at-rest.svg
var inboxAtRestSource []byte

var (
	assignmentDesk = core.MustParseSVG(assignmentDeskSource)
	inboxAtRest    = core.MustParseSVG(inboxAtRestSource)
)

func illustration(id string, drawing *core.SVG) core.Node {
	return core.View{TestID: id, PointerEvents: core.PointerEventsNone,
		AccessibilityElementsHidden: func() bool { return true },
		Children:                    []core.Node{core.SVGView{SVG: drawing}},
	}
}
