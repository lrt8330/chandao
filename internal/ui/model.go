package ui

import (
	"time"

	"chandao/zentaotray/internal/infrastructure/notifyhistory"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/core"
)

// WorkbenchData is the presentation snapshot supplied by the application layer.
type WorkbenchData struct {
	Account              string
	NeedsLogin           bool
	Connected            bool
	Loading              bool
	LastSuccess          time.Time
	ErrorMessage         string
	Items                []domain.Item
	Changes              []domain.Change
	PollSeconds          int
	NotificationsEnabled bool
	PopupSeconds         int
}

// WorkbenchProps connects the workbench to application and platform actions.
type WorkbenchProps struct {
	Data         WorkbenchData
	DataFunc     func() WorkbenchData
	SelectedKey  *core.Signal[string]
	OnRefresh    func()
	OnOpenURL    func(string)
	OnLogin      func(account, password string)
	OnSettings   func(pollSeconds int, notificationsEnabled bool, popupSeconds int)
	OnOpenReport func()
	// OnRunItemAction 对一条事项执行生命周期操作；done 在 UI 线程收到
	// 人类可读的错误（成功为空串）。
	OnRunItemAction func(item domain.Item, action string, done func(errorMessage string))
	NotifyHistory   *notifyhistory.Store
}

func (props WorkbenchProps) data() WorkbenchData {
	if props.DataFunc != nil {
		return props.DataFunc()
	}
	return props.Data
}
